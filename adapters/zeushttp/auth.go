// SPDX-License-Identifier: BUSL-1.1

package zeushttp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/koten-ai/zeus_client_golang/adapters/secretsenv"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/internal/httpx"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const (
	authComponent = "adapters.zeushttp.auth"

	headerAuthorization = "Authorization"
	headerZeusSession   = "X-Zeus-Session"

	defaultPasswordEnv = "ZEUS_PASSWORD"
	defaultBearerEnv   = "ZEUS_BEARER_TOKEN"
	defaultSessionEnv  = "ZEUS_SESSION_ID"

	defaultIdleTTL    = 1800.0
	defaultHardTTL    = 43200.0
	sessionIdleMargin = 30 * time.Second
)

// AuthResolver is Python ZeusAuthResolver: per-runtime Zeus header mint.
// Cache is instance-scoped (mutex). No package-level session store.
type AuthResolver struct {
	endpoint config.ZeusEndpointConfig
	secrets  ports.SecretStore
	nowFn    func() time.Time
	logf     func(level, msg string, attrs map[string]any)

	mu       sync.Mutex
	http     ports.HttpPort
	ownsHTTP bool
	closed   bool
	cache    map[cacheKey]cachedSID
	logs     []string
}

var (
	_ ports.ZeusPort = (*AuthResolver)(nil)
	_ ports.Closer   = (*AuthResolver)(nil)
)

type cacheKey struct {
	URL    string
	Bucket string
	Scope  string
	User   string
}

type cachedSID struct {
	sid          string
	pwd          string
	idleTTL      time.Duration
	hardDeadline time.Time
	lastUsed     time.Time
}

// AuthResolverOptions constructs AuthResolver. HTTP nil → dedicated
// internal/httpx client (owned; Close releases it). Secrets nil → process env.
type AuthResolverOptions struct {
	Endpoint config.ZeusEndpointConfig
	Secrets  ports.SecretStore
	HTTP     ports.HttpPort
	Now      func() time.Time
	Log      func(level, msg string, attrs map[string]any)
}

// NewAuthResolver returns an instance-scoped resolver (Python ZeusAuthResolver).
func NewAuthResolver(opts AuthResolverOptions) *AuthResolver {
	sec := opts.Secrets
	if sec == nil {
		sec = secretsenv.New()
	}
	ep := opts.Endpoint
	ep.ScopeCredentials = copyScopeCredentials(ep.ScopeCredentials)
	return &AuthResolver{
		endpoint: ep,
		secrets:  sec,
		http:     opts.HTTP,
		ownsHTTP: false,
		nowFn:    opts.Now,
		logf:     opts.Log,
		cache:    map[cacheKey]cachedSID{},
	}
}

// ResolveAuth returns hop headers for target (Python ZeusAuthResolver.resolve).
// force=true skips the basic session cache (401 remint). Mode is always set
// (empty on the zero AuthContext still means "none").
func (r *AuthResolver) ResolveAuth(ctx context.Context, target config.DataTarget, force bool) (ports.AuthContext, error) {
	if r == nil {
		return ports.AuthContext{}, domain.NewAuth(domain.CodeAuthFailed, authComponent,
			domain.WithMessage("auth resolver is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return ports.AuthContext{}, domain.NewAuth(domain.CodeAuthFailed, authComponent,
			domain.WithMessage("auth resolver closed"))
	}

	switch r.mode() {
	case "none":
		return ports.AuthContext{Headers: map[string]string{}, Mode: "none"}, nil
	case "certificate":
		return ports.AuthContext{}, domain.NewAuth(domain.CodeNotImplemented, authComponent,
			domain.WithMessage("certificate/mTLS auth_mode is documented but not implemented; set cert_file / key_file_env on ZeusEndpointConfig"),
			domain.WithDetails(map[string]any{
				"cert_file":    r.endpoint.CertFile,
				"key_file_env": r.endpoint.KeyFileEnv,
			}),
		)
	case "bearer":
		return r.resolveBearer(ctx)
	case "session":
		return r.resolveSession(ctx)
	case "basic":
		return r.resolveBasic(ctx, target, force)
	default:
		return ports.AuthContext{}, domain.NewAuth(domain.CodeZeusAuthModeInvalid, authComponent,
			domain.WithMessage("zeus.auth_mode invalid: "+strconv.Quote(r.mode())))
	}
}

// CallVerb is ZCG-13. AuthResolver only mints headers.
func (r *AuthResolver) CallVerb(_ context.Context, _ ports.VerbRequest) (ports.VerbHopResult, error) {
	return ports.VerbHopResult{}, domain.New(domain.CodeNotImplemented, authComponent,
		domain.WithMessage("CallVerb is ZCG-13 (P-Direct); auth resolver does not dispatch verbs"))
}

// Close releases an owned HTTP client. Injected HttpPort is left for the owner.
// Idempotent and nil-safe.
func (r *AuthResolver) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	httpPort := r.http
	owns := r.ownsHTTP
	r.mu.Unlock()
	if owns && httpPort != nil {
		return httpPort.Close(ctx)
	}
	return nil
}

// HopFn is one authenticated Zeus hop. Status is the HTTP status (0 if none).
// ZCG-13 will pass a verb POST; this ticket only needs the remint policy.
type HopFn func(ctx context.Context, auth ports.AuthContext) (status int, err error)

// DoWithBasic401Remint resolves auth, runs hop, and on HTTP 401 when
// auth_mode is basic remints once (force=true) then retries the hop. A second
// 401 is returned as-is — this never loops. Non-basic modes do not remint.
func (r *AuthResolver) DoWithBasic401Remint(ctx context.Context, target config.DataTarget, hop HopFn) (ports.AuthContext, int, error) {
	if r == nil {
		return ports.AuthContext{}, 0, domain.NewAuth(domain.CodeAuthFailed, authComponent,
			domain.WithMessage("auth resolver is nil"))
	}
	if hop == nil {
		return ports.AuthContext{}, 0, domain.New(domain.CodeInvalidArgument, authComponent,
			domain.WithMessage("hop is nil"))
	}
	auth, err := r.ResolveAuth(ctx, target, false)
	if err != nil {
		return ports.AuthContext{}, 0, err
	}
	status, err := hop(ctx, auth)
	if err != nil {
		return auth, status, err
	}
	if status != 401 || r.mode() != "basic" {
		return auth, status, nil
	}
	auth, err = r.ResolveAuth(ctx, target, true)
	if err != nil {
		return ports.AuthContext{}, status, err
	}
	status, err = hop(ctx, auth)
	return auth, status, err
}

func (r *AuthResolver) mode() string {
	m := strings.ToLower(strings.TrimSpace(string(r.endpoint.AuthMode)))
	if m == "" {
		return "none"
	}
	return m
}

func (r *AuthResolver) resolveBearer(ctx context.Context) (ports.AuthContext, error) {
	envName := r.endpoint.TokenEnv
	if envName == "" {
		envName = defaultBearerEnv
	}
	tok, ok := r.secret(ctx, envName)
	if !ok {
		return ports.AuthContext{}, domain.NewAuth(domain.CodeAuthFailed, authComponent,
			domain.WithMessage("bearer mode selected but token env empty"),
			domain.WithDetails(map[string]any{"token_env": envName}),
		)
	}
	r.emit("info", "zeus.auth resolved", map[string]any{"mode": "bearer", "token_env": envName})
	return ports.AuthContext{
		Headers: map[string]string{headerAuthorization: "Bearer " + tok},
		Mode:    "bearer",
	}, nil
}

func (r *AuthResolver) resolveSession(ctx context.Context) (ports.AuthContext, error) {
	envName := r.endpoint.TokenEnv
	if envName == "" {
		envName = defaultSessionEnv
	}
	sid, ok := r.secret(ctx, envName)
	if !ok {
		return ports.AuthContext{}, domain.NewAuth(domain.CodeAuthFailed, authComponent,
			domain.WithMessage("session mode selected but session id env empty"),
			domain.WithDetails(map[string]any{"token_env": envName}),
		)
	}
	r.emit("info", "zeus.auth resolved", map[string]any{"mode": "session"})
	return ports.AuthContext{
		Headers: map[string]string{headerZeusSession: sid},
		Mode:    "session",
	}, nil
}

func (r *AuthResolver) resolveBasic(ctx context.Context, target config.DataTarget, force bool) (ports.AuthContext, error) {
	if target.Bucket == "" || target.Scope == "" {
		return ports.AuthContext{}, domain.NewAuth(domain.CodeZeusAuthIncomplete, authComponent,
			domain.WithMessage("basic mode needs bucket+scope for per-scope POST /v1/{bucket}/{scope}/auth/session"),
		)
	}
	user, pwd, pwdEnv := r.scopeUserPwd(ctx, target)
	if user == "" || pwd == "" {
		return ports.AuthContext{}, domain.NewAuth(domain.CodeZeusAuthIncomplete, authComponent,
			domain.WithMessage("basic auth fields incomplete for auth_mode"),
			domain.WithDetails(map[string]any{
				"username_set": user != "",
				"password_env": pwdEnv,
			}),
		)
	}
	key := cacheKey{
		URL:    r.endpoint.URL,
		Bucket: target.Bucket,
		Scope:  target.Scope,
		User:   user,
	}
	now := r.now()
	if !force {
		if auth, ok := r.cacheHit(key, pwd, now); ok {
			r.emit("info", "zeus.auth basic cache hit", map[string]any{
				"bucket":     target.Bucket,
				"scope":      target.Scope,
				"sid_prefix": sidPrefix(auth.Headers[headerZeusSession]),
			})
			return auth, nil
		}
	}

	url := mintURL(r.endpoint.URL, target.Bucket, target.Scope)
	httpPort, err := r.httpPort()
	if err != nil {
		return ports.AuthContext{}, err
	}
	resp, err := httpPort.Request(ctx, ports.HTTPRequest{
		Method:   "POST",
		URL:      url,
		Headers:  map[string]string{headerAuthorization: basicAuthHeader(user, pwd)},
		TimeoutS: r.endpoint.TimeoutS,
	})
	if err != nil {
		if de, ok := domain.AsError(err); ok {
			if de.Code == domain.CodeCancelled || de.Code == domain.CodeContextDeadline {
				return ports.AuthContext{}, de
			}
		}
		r.emit("warn", "zeus.auth basic login unreachable", map[string]any{
			"bucket": target.Bucket,
			"scope":  target.Scope,
			"err":    errTypeName(err),
		})
		return ports.AuthContext{}, domain.NewAuth(domain.CodeAuthSessionUnavailable, authComponent,
			domain.WithMessage("Zeus basic login unreachable: "+errTypeName(err)),
			domain.WithCause(err),
			domain.WithDetails(map[string]any{
				"bucket": target.Bucket,
				"scope":  target.Scope,
			}),
		)
	}

	reqID := domain.ReqIDFromHeaders(resp.Headers)
	if resp.Status != 200 {
		r.emit("error", "zeus.auth basic login failed", map[string]any{
			"bucket": target.Bucket,
			"scope":  target.Scope,
			"status": resp.Status,
			"req_id": reqID,
		})
		details := map[string]any{"status_code": resp.Status}
		if reqID != "" {
			details["req_id"] = reqID
		}
		return ports.AuthContext{}, domain.NewAuth(domain.CodeAuthFailed, authComponent,
			domain.WithMessage(fmt.Sprintf("Zeus basic login failed HTTP %d", resp.Status)),
			domain.WithDetails(details),
		)
	}

	body, err := parseObjectJSON(resp.Body)
	if err != nil {
		return ports.AuthContext{}, domain.NewAuth(domain.CodeAuthFailed, authComponent,
			domain.WithMessage("Zeus login returned invalid JSON"),
			domain.WithCause(err),
			domain.WithDetails(reqIDDetails(reqID)),
		)
	}
	sid := jsonString(body["session_id"])
	if sid == "" {
		return ports.AuthContext{}, domain.NewAuth(domain.CodeAuthFailed, authComponent,
			domain.WithMessage("Zeus login returned no session_id"),
			domain.WithDetails(reqIDDetails(reqID)),
		)
	}
	idleTTL := jsonNumber(body["expires_in"], defaultIdleTTL)
	hardTTL := jsonNumber(body["hard_ttl_s"], defaultHardTTL)
	r.storeCache(key, cachedSID{
		sid:          sid,
		pwd:          pwd,
		idleTTL:      time.Duration(idleTTL * float64(time.Second)),
		hardDeadline: now.Add(time.Duration(hardTTL * float64(time.Second))),
		lastUsed:     now,
	})
	r.emit("info", "zeus.auth basic session minted", map[string]any{
		"bucket":     target.Bucket,
		"scope":      target.Scope,
		"sid_prefix": sidPrefix(sid),
		"req_id":     reqID,
	})
	return ports.AuthContext{
		Headers: map[string]string{headerZeusSession: sid},
		Mode:    "basic",
	}, nil
}

func (r *AuthResolver) scopeUserPwd(ctx context.Context, target config.DataTarget) (user, pwd, pwdEnv string) {
	key := target.Bucket + "/" + target.Scope
	scoped := r.endpoint.ScopeCredentials[key]
	user = strings.TrimSpace(r.endpoint.Username)
	pwdEnv = r.endpoint.PasswordEnv
	if scoped != nil {
		if u := strings.TrimSpace(scoped["username"]); u != "" {
			user = u
		}
		if p := strings.TrimSpace(scoped["password_env"]); p != "" {
			pwdEnv = p
		}
	}
	if pwdEnv == "" {
		pwdEnv = defaultPasswordEnv
	}
	pwd, _ = r.secret(ctx, pwdEnv)
	return user, pwd, pwdEnv
}

func (r *AuthResolver) secret(ctx context.Context, name string) (string, bool) {
	if r.secrets == nil || name == "" {
		return "", false
	}
	return r.secrets.Get(ctx, name)
}

func (r *AuthResolver) cacheHit(key cacheKey, pwd string, now time.Time) (ports.AuthContext, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ent, ok := r.cache[key]
	if !ok || ent.pwd != pwd {
		return ports.AuthContext{}, false
	}
	if !now.Before(ent.hardDeadline) {
		return ports.AuthContext{}, false
	}
	idleLimit := ent.idleTTL - sessionIdleMargin
	if idleLimit < 0 {
		idleLimit = 0
	}
	if now.Sub(ent.lastUsed) >= idleLimit {
		return ports.AuthContext{}, false
	}
	ent.lastUsed = now
	r.cache[key] = ent
	return ports.AuthContext{
		Headers: map[string]string{headerZeusSession: ent.sid},
		Mode:    "basic",
	}, true
}

func (r *AuthResolver) storeCache(key cacheKey, ent cachedSID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	if r.cache == nil {
		r.cache = map[cacheKey]cachedSID{}
	}
	r.cache[key] = ent
}

func (r *AuthResolver) httpPort() (ports.HttpPort, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, domain.NewAuth(domain.CodeAuthFailed, authComponent,
			domain.WithMessage("auth resolver closed"))
	}
	if r.http == nil {
		r.http = httpx.New(httpx.Options{
			Timeout:       timeoutDuration(r.endpoint.TimeoutS),
			SkipTLSVerify: !r.endpoint.TLSVerify,
		})
		r.ownsHTTP = true
	}
	return r.http, nil
}

func (r *AuthResolver) now() time.Time {
	if r.nowFn != nil {
		return r.nowFn()
	}
	return time.Now()
}

func (r *AuthResolver) emit(level, msg string, attrs map[string]any) {
	line := formatAuthLog(level, msg, attrs)
	r.mu.Lock()
	r.logs = append(r.logs, line)
	logf := r.logf
	r.mu.Unlock()
	if logf != nil {
		logf(level, msg, attrs)
	}
}

func (r *AuthResolver) logBlob() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.logs, "\n")
}

func mintURL(base, bucket, scope string) string {
	return strings.TrimRight(base, "/") + "/v1/" + bucket + "/" + scope + "/auth/session"
}

func basicAuthHeader(user, password string) string {
	raw := user + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))
}

func sidPrefix(sid string) string {
	if len(sid) <= 12 {
		return sid
	}
	return sid[:12]
}

func parseObjectJSON(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty body")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("JSON is not an object")
	}
	return m, nil
}

func jsonString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	default:
		s := fmt.Sprint(x)
		if s == "<nil>" {
			return ""
		}
		return s
	}
}

func jsonNumber(v any, def float64) float64 {
	if v == nil {
		return def
	}
	switch x := v.(type) {
	case float64:
		if x == 0 {
			return def
		}
		return x
	case float32:
		if x == 0 {
			return def
		}
		return float64(x)
	case int:
		if x == 0 {
			return def
		}
		return float64(x)
	case int64:
		if x == 0 {
			return def
		}
		return float64(x)
	case json.Number:
		f, err := x.Float64()
		if err != nil || f == 0 {
			return def
		}
		return f
	case string:
		f, err := strconv.ParseFloat(x, 64)
		if err != nil || f == 0 {
			return def
		}
		return f
	default:
		return def
	}
}

func reqIDDetails(reqID string) map[string]any {
	if reqID == "" {
		return map[string]any{}
	}
	return map[string]any{"req_id": reqID}
}

func timeoutDuration(seconds float64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

func copyScopeCredentials(in map[string]map[string]string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(in))
	for k, v := range in {
		inner := make(map[string]string, len(v))
		for ik, iv := range v {
			inner[ik] = iv
		}
		out[k] = inner
	}
	return out
}

func errTypeName(err error) string {
	if err == nil {
		return ""
	}
	if de, ok := domain.AsError(err); ok {
		if de.Type != "" {
			return de.Type
		}
		return "Error"
	}
	return fmt.Sprintf("%T", err)
}

func formatAuthLog(level, msg string, attrs map[string]any) string {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(level)
	b.WriteByte(' ')
	b.WriteString(msg)
	for _, k := range keys {
		b.WriteByte(' ')
		b.WriteString(k)
		b.WriteByte('=')
		fmt.Fprintf(&b, "%v", attrs[k])
	}
	return b.String()
}
