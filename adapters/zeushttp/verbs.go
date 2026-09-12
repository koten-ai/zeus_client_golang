// SPDX-License-Identifier: BUSL-1.1

package zeushttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/koten-ai/zeus_client_golang/adapters/secretsenv"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/internal/httpx"
	"github.com/koten-ai/zeus_client_golang/ports"
	"github.com/koten-ai/zeus_client_golang/security"
)

const verbsComponent = "adapters.zeushttp.verbs"

// Canonical V2 verbs minus pipeline (Python _V2_ORDER / EXPOSED_V2_VERBS).
var v2Order = []string{
	"describe",
	"explain",
	"get",
	"find",
	"set",
	"order",
	"enrich",
	"project",
	"traverse",
	"pipeline",
	"search",
	"analyze",
	"return",
}

// ExposedV2Verbs is the public Direct allow-list (pipeline excluded).
var ExposedV2Verbs []string

// ExposedV2VerbSet is the lookup set for ExposedV2Verbs.
var ExposedV2VerbSet = map[string]struct{}{}

// V2BareVerbs are POSTed at /v2/{verb} (no bucket/scope/collection).
var V2BareVerbs = map[string]struct{}{
	"explain": {},
	"return":  {},
}

// V2ScopeVerbs are POSTed at /v2/{bucket}/{scope}/{verb}.
var V2ScopeVerbs = map[string]struct{}{
	"describe": {},
	"analyze":  {},
}

func init() {
	for _, v := range v2Order {
		if v == "pipeline" {
			continue
		}
		ExposedV2Verbs = append(ExposedV2Verbs, v)
		ExposedV2VerbSet[v] = struct{}{}
	}
}

// IsExposedV2Verb reports whether verb is on the public Direct allow-list.
func IsExposedV2Verb(verb string) bool {
	_, ok := ExposedV2VerbSet[strings.TrimSpace(verb)]
	return ok
}

// ExposedV2VerbList copies the allow-list (stable _V2_ORDER minus pipeline).
func ExposedV2VerbList() []string {
	out := make([]string, len(ExposedV2Verbs))
	copy(out, ExposedV2Verbs)
	return out
}

// VerbURL builds the Zeus V2 verb path (Python verb_url).
func VerbURL(baseURL string, target config.DataTarget, verb string) string {
	root := strings.TrimRight(baseURL, "/")
	name := strings.TrimSpace(verb)
	b := url.PathEscape(target.Bucket)
	s := url.PathEscape(target.Scope)
	c := url.PathEscape(target.Collection)
	if _, ok := V2BareVerbs[name]; ok {
		return root + "/v2/" + name
	}
	if _, ok := V2ScopeVerbs[name]; ok {
		return root + "/v2/" + b + "/" + s + "/" + name
	}
	return root + "/v2/" + b + "/" + s + "/" + c + "/" + name
}

// Port is the ZeusPort over a dedicated HttpPort (Python HttpxZeusPort).
// Composes AuthResolver + HttpPort. Direct public surface keeps AllowPipeline false.
type Port struct {
	endpoint  config.ZeusEndpointConfig
	secrets   ports.SecretStore
	journal   journal.ExecutionJournal
	turnID    string
	version   string
	stampUser string
	redactor  security.Redactor
	clock     ports.Clock
	logf      func(level, msg string, attrs map[string]any)

	auth     *AuthResolver
	ownsAuth bool
	http     ports.HttpPort
	ownsHTTP bool

	mu     sync.Mutex
	closed bool
	logs   []string
}

var (
	_ ports.ZeusPort = (*Port)(nil)
	_ ports.Closer   = (*Port)(nil)
)

// PortOptions constructs Port. HTTP nil → dedicated internal/httpx client
// (owned; Close releases it). Secrets nil → process env.
type PortOptions struct {
	Endpoint  config.ZeusEndpointConfig
	Secrets   ports.SecretStore
	HTTP      ports.HttpPort
	Auth      *AuthResolver
	Journal   journal.ExecutionJournal
	TurnID    string
	Version   string
	StampUser string
	Redactor  security.Redactor
	Clock     ports.Clock
	Log       func(level, msg string, attrs map[string]any)
}

// NewPort returns a ZeusPort that dispatches V2 verbs.
func NewPort(opts PortOptions) *Port {
	sec := opts.Secrets
	if sec == nil {
		sec = secretsenv.New()
	}
	red := opts.Redactor
	if red == nil {
		red = security.New()
	}
	clk := opts.Clock
	if clk == nil {
		clk = ports.SystemClock{}
	}
	ep := opts.Endpoint
	ep.ScopeCredentials = copyScopeCredentials(ep.ScopeCredentials)

	httpPort := opts.HTTP
	ownsHTTP := false
	if httpPort == nil {
		httpPort = httpx.New(httpx.Options{
			Timeout:       timeoutDuration(ep.TimeoutS),
			SkipTLSVerify: !ep.TLSVerify,
		})
		ownsHTTP = true
	}

	auth := opts.Auth
	ownsAuth := false
	if auth == nil {
		auth = NewAuthResolver(AuthResolverOptions{
			Endpoint: ep,
			Secrets:  sec,
			HTTP:     httpPort,
		})
		ownsAuth = true
	}

	return &Port{
		endpoint:  ep,
		secrets:   sec,
		journal:   opts.Journal,
		turnID:    opts.TurnID,
		version:   opts.Version,
		stampUser: opts.StampUser,
		redactor:  red,
		clock:     clk,
		logf:      opts.Log,
		auth:      auth,
		ownsAuth:  ownsAuth,
		http:      httpPort,
		ownsHTTP:  ownsHTTP,
	}
}

// ResolveAuth delegates to the composed AuthResolver.
func (p *Port) ResolveAuth(ctx context.Context, target config.DataTarget, force bool) (ports.AuthContext, error) {
	if p == nil || p.auth == nil {
		return ports.AuthContext{}, domain.NewAuth(domain.CodeAuthFailed, verbsComponent,
			domain.WithMessage("zeus port is nil"))
	}
	return p.auth.ResolveAuth(ctx, target, force)
}

// CallVerb POSTs one allow-listed V2 verb. pipeline without AllowPipeline → 060010.
func (p *Port) CallVerb(ctx context.Context, req ports.VerbRequest) (ports.VerbHopResult, error) {
	if p == nil {
		return ports.VerbHopResult{}, domain.NewZeusTransport(domain.CodeZeusTransport, verbsComponent,
			domain.WithMessage("zeus port is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		if de := domain.FromContext(err); de != nil {
			return ports.VerbHopResult{}, de
		}
		return ports.VerbHopResult{}, err
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return ports.VerbHopResult{}, domain.NewZeusTransport(domain.CodeZeusTransport, verbsComponent,
			domain.WithMessage("zeus port closed"))
	}

	verb := strings.TrimSpace(req.Verb)
	if verb == "pipeline" && !req.AllowPipeline {
		return ports.VerbHopResult{}, domain.NewZeusTool(domain.CodeZeusPipelineNotOnDirect, verbsComponent,
			domain.WithMessage("zeus pipeline not on direct surface"))
	}
	if verb != "pipeline" && !IsExposedV2Verb(verb) {
		return ports.VerbHopResult{}, domain.NewZeusTool(domain.CodeZeusVerbNotAllowed, verbsComponent,
			domain.WithMessage("zeus verb not allow-listed"),
			domain.WithDetails(map[string]any{"verb": verb}),
		)
	}

	hopEndpoint := p.endpointFor(req)
	auth, err := p.authFor(ctx, req, req.Target, false)
	if err != nil {
		return ports.VerbHopResult{}, err
	}
	urlStr := VerbURL(hopEndpoint.URL, req.Target, verb)
	payload := VerbBodyWithoutRewind(req.Body)
	params := RewindQueryParams(req.Rewind)
	postURL := withQuery(urlStr, params)
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return ports.VerbHopResult{}, domain.NewZeusTransport(domain.CodeZeusTransport, verbsComponent,
			domain.WithCause(err),
			domain.WithMessage("verb body encode failed"))
	}

	httpPort, err := p.httpPort()
	if err != nil {
		return ports.VerbHopResult{}, err
	}

	scope := ""
	if req.Target.Bucket != "" && req.Target.Scope != "" {
		scope = req.Target.Bucket + "/" + req.Target.Scope
	}
	zeusURL := strings.TrimRight(hopEndpoint.URL, "/")

	headers := p.verbHeaders(auth, req, verb)
	t0 := time.Now()
	resp, err := httpPort.Request(ctx, ports.HTTPRequest{
		Method:   "POST",
		URL:      postURL,
		Headers:  headers,
		Body:     bodyBytes,
		TimeoutS: hopEndpoint.TimeoutS,
	})
	if err == nil && resp.Status == 401 && strings.ToLower(strings.TrimSpace(string(hopEndpoint.AuthMode))) == "basic" {
		auth, err = p.authFor(ctx, req, req.Target, true)
		if err != nil {
			return ports.VerbHopResult{}, err
		}
		headers = p.verbHeaders(auth, req, verb)
		resp, err = httpPort.Request(ctx, ports.HTTPRequest{
			Method:   "POST",
			URL:      postURL,
			Headers:  headers,
			Body:     bodyBytes,
			TimeoutS: hopEndpoint.TimeoutS,
		})
	}

	ms := int(time.Since(t0).Milliseconds())
	if err != nil {
		if de, ok := domain.AsError(err); ok {
			if de.Code == domain.CodeCancelled || de.Code == domain.CodeContextDeadline {
				return ports.VerbHopResult{}, de
			}
		}
		errMsg := errTypeName(err)
		p.journalHop(verb, urlStr, 0, "", false, errMsg, headers, ms, scope, zeusURL)
		failAttrs := map[string]any{
			"req_id":           "",
			"verb":             verb,
			"http.status_code": 0,
			"duration_ms":      ms,
			"scope":            scope,
			"zeus.url":         zeusURL,
			"result":           "error",
			"bytes.out":        len(bodyBytes),
		}
		p.emit("error", "zeus_client.zeus.dispatch_failed", failAttrs)
		return ports.VerbHopResult{}, domain.NewZeusTransport(domain.CodeZeusTransport, verbsComponent,
			domain.WithMessage("zeus HTTP transport error"),
			domain.WithCause(err),
			domain.WithDetails(map[string]any{
				"verb":  verb,
				"url":   urlStr,
				"scope": scope,
			}),
		)
	}

	reqID := domain.ReqIDFromHeaders(resp.Headers)
	status := resp.Status
	body := parseVerbBody(resp.Body)
	var errStr string
	if status >= 400 {
		errStr = fmt.Sprintf("zeus HTTP %d", status)
	}
	ok := status >= 200 && status < 300 && errStr == ""
	p.journalHop(verb, urlStr, status, reqID, ok, errStr, headers, ms, scope, zeusURL)
	hopAttrs := map[string]any{
		"req_id":           reqID,
		"verb":             verb,
		"http.status_code": status,
		"duration_ms":      ms,
		"scope":            scope,
		"zeus.url":         zeusURL,
		"bytes.out":        len(bodyBytes),
		"bytes.in":         len(resp.Body),
	}
	if ok {
		p.emit("info", "zeus_client.zeus.req", hopAttrs)
	} else {
		hopAttrs["result"] = "error"
		p.emit("error", "zeus_client.zeus.dispatch_failed", hopAttrs)
	}
	return ports.VerbHopResult{
		OK:         ok,
		StatusCode: status,
		ReqID:      reqID,
		Body:       body,
		Error:      errStr,
		URL:        urlStr,
		BytesIn:    len(resp.Body),
		BytesOut:   len(bodyBytes),
		HasBytes:   true,
	}, nil
}

// Close releases an owned HTTP client / AuthResolver. Injected ports stay.
// Idempotent and nil-safe.
func (p *Port) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	auth := p.auth
	ownsAuth := p.ownsAuth
	httpPort := p.http
	ownsHTTP := p.ownsHTTP
	p.mu.Unlock()

	var first error
	if ownsAuth && auth != nil {
		first = auth.Close(ctx)
	}
	if ownsHTTP && httpPort != nil {
		if err := httpPort.Close(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Logs is the testable hop log blob (no secrets / bodies).
func (p *Port) Logs() string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.logs, "\n")
}

func (p *Port) verbHeaders(auth ports.AuthContext, req ports.VerbRequest, verb string) map[string]string {
	out := MergeHeaders(
		auth.Headers,
		StampHeaders(p.stampUser, p.version),
		map[string]string{"Content-Type": "application/json", "Accept": "application/json"},
		req.Headers,
	)
	out = ApplyModeHeader(out, req.ModeHeader)
	if req.Target.Bucket != "" && req.Target.Scope != "" {
		if _, bare := V2BareVerbs[verb]; !bare && !hasFold(out, "X-Zeus-Scope") {
			out["X-Zeus-Scope"] = req.Target.Bucket + "/" + req.Target.Scope
		}
	}
	if req.PreMintReqID && !domain.HasReqIDHeader(out) {
		out[domain.ReqIDHeader] = domain.NewZeusReqID()
	}
	return out
}

func (p *Port) endpointFor(req ports.VerbRequest) config.ZeusEndpointConfig {
	ep := p.endpoint
	changed := false
	if req.BaseURL != "" && req.BaseURL != ep.URL {
		ep.URL = req.BaseURL
		changed = true
	}
	if req.AuthMode != "" && config.AuthMode(req.AuthMode) != ep.AuthMode {
		ep.AuthMode = config.AuthMode(req.AuthMode)
		changed = true
	}
	if req.Username != "" && req.Username != ep.Username {
		ep.Username = req.Username
		changed = true
	}
	if req.PasswordEnv != "" && req.PasswordEnv != ep.PasswordEnv {
		ep.PasswordEnv = req.PasswordEnv
		changed = true
	}
	if req.TokenEnv != "" && req.TokenEnv != ep.TokenEnv {
		ep.TokenEnv = req.TokenEnv
		changed = true
	}
	if !changed {
		return p.endpoint
	}
	ep.ScopeCredentials = copyScopeCredentials(ep.ScopeCredentials)
	return ep
}

func (p *Port) authFor(ctx context.Context, req ports.VerbRequest, target config.DataTarget, force bool) (ports.AuthContext, error) {
	ep := p.endpointFor(req)
	if ep.URL == p.endpoint.URL &&
		ep.AuthMode == p.endpoint.AuthMode &&
		ep.Username == p.endpoint.Username &&
		ep.PasswordEnv == p.endpoint.PasswordEnv &&
		ep.TokenEnv == p.endpoint.TokenEnv {
		return p.auth.ResolveAuth(ctx, target, force)
	}
	resolver := NewAuthResolver(AuthResolverOptions{
		Endpoint: ep,
		Secrets:  p.secrets,
		HTTP:     p.http,
	})
	return resolver.ResolveAuth(ctx, target, force)
}

func (p *Port) httpPort() (ports.HttpPort, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, domain.NewZeusTransport(domain.CodeZeusTransport, verbsComponent,
			domain.WithMessage("zeus port closed"))
	}
	if p.http == nil {
		p.http = httpx.New(httpx.Options{
			Timeout:       timeoutDuration(p.endpoint.TimeoutS),
			SkipTLSVerify: !p.endpoint.TLSVerify,
		})
		p.ownsHTTP = true
	}
	return p.http, nil
}

func (p *Port) journalHop(verb, urlStr string, status int, reqID string, ok bool, errStr string, headers map[string]string, durationMS int, scope, zeusURL string) {
	if p.journal == nil {
		return
	}
	red := p.redactor
	if red == nil {
		red = security.New()
	}
	turnID := p.turnID
	if turnID == "" {
		turnID = "turn_unknown"
	}
	nowMS := time.Now().UnixMilli()
	if p.clock != nil {
		nowMS = p.clock.NowMS(context.Background())
	}
	p.journal.Append(journal.JournalEvent{
		EventID:   "evt_" + domain.NewZeusReqID(),
		TsMs:      nowMS,
		Type:      journal.EventZeusHop,
		Component: verbsComponent,
		TurnID:    turnID,
		Data: map[string]any{
			"verb":        verb,
			"url":         urlStr,
			"status":      status,
			"req_id":      reqID,
			"ok":          ok,
			"error":       errStr,
			"headers":     red.Headers(headers),
			"scope":       scope,
			"zeus.url":    zeusURL,
			"duration_ms": durationMS,
		},
	})
}

func (p *Port) emit(level, msg string, attrs map[string]any) {
	line := formatAuthLog(level, msg, attrs)
	p.mu.Lock()
	p.logs = append(p.logs, line)
	logf := p.logf
	p.mu.Unlock()
	if logf != nil {
		logf(level, msg, attrs)
	}
}

func withQuery(raw string, params map[string]string) string {
	if len(params) == 0 {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func parseVerbBody(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		s := string(raw)
		if len(s) > 2000 {
			s = s[:2000]
		}
		return map[string]any{"raw": s}
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{"result": v}
}

// HopCode maps an HTTP status to a family Zeus code (Python / httpx.Wrap).
func HopCode(status int) domain.Code {
	switch {
	case status == 409:
		return domain.CodeZeusContractRequired
	case status == 401 || status == 403:
		return domain.CodeZeusAuthFailed
	case status >= 500:
		return domain.CodeZeusHTTP5xx
	case status >= 400:
		return domain.CodeZeusHTTP4xx
	default:
		return domain.CodeZeusTransport
	}
}
