// SPDX-License-Identifier: BUSL-1.1

package zeushttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/koten-ai/zeus_client_golang/adapters/secretsenv"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/internal/httpx"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const sessionComponent = "adapters.zeushttp.session"

// SessionHTTPResult is one /v2/session* hop (Python SessionHttpResult).
type SessionHTTPResult struct {
	OK         bool
	StatusCode int
	Body       map[string]any
	URL        string
	ReqID      string
	Error      string
}

// SessionClient is the HTTP surface for /v2/session* (Python HttpxSessionClient).
// Server mints session_id; this client never invents one. PostTrace joins
// Detective on the dispatch hop req_id (ZCG-16).
type SessionClient struct {
	endpoint  config.ZeusEndpointConfig
	secrets   ports.SecretStore
	http      ports.HttpPort
	ownsHTTP  bool
	auth      *AuthResolver
	ownsAuth  bool
	version   string
	identity  config.ClientIdentity
	stampUser string
	preMint   bool

	mu        sync.Mutex
	closed    bool
	lastReqID string
	logs      []string
}

var _ ports.Closer = (*SessionClient)(nil)

// SessionClientOptions constructs SessionClient. HTTP nil → dedicated httpx client.
type SessionClientOptions struct {
	Endpoint  config.ZeusEndpointConfig
	Secrets   ports.SecretStore
	HTTP      ports.HttpPort
	Auth      *AuthResolver
	Version   string
	Identity  config.ClientIdentity
	StampUser string
	PreMint   bool
	Log       func(level, msg string, attrs map[string]any)
}

// NewSessionClient returns a /v2/session* adapter.
func NewSessionClient(opts SessionClientOptions) *SessionClient {
	sec := opts.Secrets
	if sec == nil {
		sec = secretsenv.New()
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
	return &SessionClient{
		endpoint:  ep,
		secrets:   sec,
		http:      httpPort,
		ownsHTTP:  ownsHTTP,
		auth:      auth,
		ownsAuth:  ownsAuth,
		version:   opts.Version,
		identity:  opts.Identity,
		stampUser: opts.StampUser,
		preMint:   opts.PreMint,
	}
}

// LastReqID is the last captured X-Zeus-Req-Id (Python last_req_id).
func (c *SessionClient) LastReqID() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastReqID
}

// Logs is the testable hop log blob (no secrets / bodies).
func (c *SessionClient) Logs() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.logs, "\n")
}

// Close releases owned HTTP / AuthResolver. Injected ports stay. Idempotent.
func (c *SessionClient) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	auth := c.auth
	ownsAuth := c.ownsAuth
	httpPort := c.http
	ownsHTTP := c.ownsHTTP
	c.mu.Unlock()
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

// Create POSTs /v2/session. Does not mint session_id.
func (c *SessionClient) Create(ctx context.Context, req SessionCreateRequest) (SessionHTTPResult, error) {
	if err := c.guard(ctx); err != nil {
		return SessionHTTPResult{}, err
	}
	base := strings.TrimRight(c.endpoint.URL, "/")
	postURL := base + "/v2/session"
	payload := map[string]any{
		"contract_id":   req.ContractID,
		"contract_hash": req.ContractHash,
		"chat_request":  mapOrEmpty(req.ChatRequest),
		"conversation":  req.Conversation,
	}
	if payload["conversation"] == nil {
		payload["conversation"] = []any{}
	}
	for k, v := range c.sinkStamp("") {
		payload[k] = v
	}
	if req.Rewind {
		payload["rewind"] = true
	}
	headers, err := c.headers(ctx, req.Mode, req.Headers, req.Target)
	if err != nil {
		return SessionHTTPResult{}, err
	}
	return c.post(ctx, postURL, headers, payload, map[int]struct{}{200: {}, 201: {}}, RewindQueryParams(req.Rewind))
}

// Rehydrate GETs /v2/session/{id}. Non-200 or transport error → (nil, nil) so
// lifecycle can same-turn recreate. Never invents a session id.
func (c *SessionClient) Rehydrate(ctx context.Context, sessionID string, rounds int, mode string, headers map[string]string, target config.DataTarget) (map[string]any, error) {
	if err := c.guard(ctx); err != nil {
		return nil, err
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, nil
	}
	if rounds <= 0 {
		rounds = 6
	}
	base := strings.TrimRight(c.endpoint.URL, "/")
	getURL := withQuery(base+"/v2/session/"+url.PathEscape(sid), map[string]string{"rounds": strconv.Itoa(rounds)})
	h, err := c.headers(ctx, mode, headers, target)
	if err != nil {
		return nil, err
	}
	httpPort, err := c.httpPort()
	if err != nil {
		return nil, err
	}
	resp, err := httpPort.Request(ctx, ports.HTTPRequest{
		Method:   "GET",
		URL:      getURL,
		Headers:  h,
		TimeoutS: c.endpoint.TimeoutS,
	})
	if err != nil {
		c.emit("error", "zeus_client.session.failed", map[string]any{
			"session.id":       sid,
			"req_id":           "",
			"http.status_code": 0,
			"zeus.url":         strings.TrimRight(c.endpoint.URL, "/"),
			"result":           "error",
		})
		return nil, nil
	}
	reqID := domain.ReqIDFromHeaders(resp.Headers)
	c.setLastReqID(reqID)
	if resp.Status != 200 {
		c.emit("error", "zeus_client.session.failed", map[string]any{
			"session.id":       sid,
			"req_id":           reqID,
			"http.status_code": resp.Status,
			"zeus.url":         strings.TrimRight(c.endpoint.URL, "/"),
			"result":           "error",
		})
		return nil, nil
	}
	body := parseVerbBody(resp.Body)
	if reqID != "" {
		body["_req_id"] = reqID
	}
	return body, nil
}

// ContinueTurn POSTs /v2/session/{id}/turn.
func (c *SessionClient) ContinueTurn(ctx context.Context, req SessionContinueRequest) (SessionHTTPResult, error) {
	if err := c.guard(ctx); err != nil {
		return SessionHTTPResult{}, err
	}
	sid := strings.TrimSpace(req.SessionID)
	if sid == "" {
		return SessionHTTPResult{OK: false, Error: "no session_id"}, domain.NewSession(domain.CodeSessionIDMissing, sessionComponent,
			domain.WithMessage("session id missing"))
	}
	base := strings.TrimRight(c.endpoint.URL, "/")
	postURL := base + "/v2/session/" + url.PathEscape(sid) + "/turn"
	payload := map[string]any{
		"round":        req.ClientRound,
		"chat_request": mapOrEmpty(req.ChatRequest),
		"new_turns":    req.NewTurns,
	}
	if payload["new_turns"] == nil {
		payload["new_turns"] = []any{}
	}
	if req.Rewind {
		payload["rewind"] = true
	}
	headers, err := c.headers(ctx, req.Mode, req.Headers, req.Target)
	if err != nil {
		return SessionHTTPResult{}, err
	}
	return c.post(ctx, postURL, headers, payload, map[int]struct{}{200: {}}, RewindQueryParams(req.Rewind))
}

// PostTrace POSTs /v2/session/trace (Python post_trace).
// req_id is the dispatch Zeus hop id (join key) — not a newly minted client id.
// Does not set X-Zeus-Req-Id unless PreMint is on; Zeus mints, client captures echo.
func (c *SessionClient) PostTrace(ctx context.Context, req SessionTraceRequest) (SessionHTTPResult, error) {
	if err := c.guard(ctx); err != nil {
		return SessionHTTPResult{}, err
	}
	sid := strings.TrimSpace(req.SessionID)
	if sid == "" || req.ClientRound <= 0 {
		return SessionHTTPResult{OK: false, Error: "bad trace params"}, nil
	}
	base := strings.TrimRight(c.endpoint.URL, "/")
	postURL := base + "/v2/session/trace"
	zresp := copyAnyMap(req.ZeusResponse)
	stamp := c.sinkStamp(sid)
	for k, v := range stamp {
		if _, ok := zresp[k]; !ok {
			zresp[k] = v
		}
	}
	turns := req.Turns
	if turns == nil {
		turns = []any{}
	}
	outcome := req.Outcome
	if outcome == "" {
		outcome = "ok"
	}
	payload := map[string]any{
		"session_id":    sid,
		"round":         req.ClientRound,
		"req_id":        req.ReqID,
		"contract_id":   req.ContractID,
		"contract_hash": req.ContractHash,
		"chat_request":  mapOrEmpty(req.ChatRequest),
		"turns":         turns,
		"zeus_response": zresp,
		"outcome":       outcome,
	}
	for k, v := range stamp {
		payload[k] = v
	}
	if tid := strings.TrimSpace(req.TurnID); tid != "" {
		payload["turn_id"] = tid
	}
	if req.Rewind {
		payload["rewind"] = true
	}
	headers, err := c.headers(ctx, req.Mode, req.Headers, req.Target)
	if err != nil {
		return SessionHTTPResult{}, err
	}
	result, err := c.post(ctx, postURL, headers, payload, map[int]struct{}{200: {}, 201: {}}, RewindQueryParams(req.Rewind))
	level := "info"
	res := "ok"
	if err != nil || !result.OK {
		level = "error"
		res = "error"
	}
	c.emit(level, "zeus_client.session.trace", map[string]any{
		"session.id":       sid,
		"req_id":           req.ReqID,
		"http.status_code": result.StatusCode,
		"result":           res,
	})
	return result, err
}

// SessionCreateRequest is POST /v2/session kwargs.
type SessionCreateRequest struct {
	ContractID   string
	ContractHash string
	ChatRequest  map[string]any
	Conversation []any
	Headers      map[string]string
	Mode         string
	Target       config.DataTarget
	Rewind       bool
}

// SessionContinueRequest is POST /v2/session/{id}/turn kwargs.
type SessionContinueRequest struct {
	SessionID   string
	ClientRound int
	ChatRequest map[string]any
	NewTurns    []any
	Headers     map[string]string
	Mode        string
	Target      config.DataTarget
	Rewind      bool
}

// SessionTraceRequest is POST /v2/session/trace kwargs (Python post_trace).
// ReqID is the dispatch hop id (G4.2 join key), not the trace hop's own id.
type SessionTraceRequest struct {
	SessionID    string
	ClientRound  int
	ReqID        string
	ContractID   string
	ContractHash string
	ChatRequest  map[string]any
	Turns        []any
	ZeusResponse map[string]any
	Outcome      string
	Headers      map[string]string
	Mode         string
	Target       config.DataTarget
	Rewind       bool
	TurnID       string
}

func (c *SessionClient) post(ctx context.Context, postURL string, headers map[string]string, payload map[string]any, okCodes map[int]struct{}, query map[string]string) (SessionHTTPResult, error) {
	httpPort, err := c.httpPort()
	if err != nil {
		return SessionHTTPResult{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return SessionHTTPResult{}, domain.NewSession(domain.CodeSessionCreateFailed, sessionComponent,
			domain.WithCause(err), domain.WithMessage("session body encode failed"))
	}
	resp, err := httpPort.Request(ctx, ports.HTTPRequest{
		Method:   "POST",
		URL:      withQuery(postURL, query),
		Headers:  headers,
		Body:     raw,
		TimeoutS: c.endpoint.TimeoutS,
	})
	if err != nil {
		return SessionHTTPResult{OK: false, URL: postURL, Error: err.Error()}, nil
	}
	reqID := domain.ReqIDFromHeaders(resp.Headers)
	c.setLastReqID(reqID)
	body := parseVerbBody(resp.Body)
	if _, ok := okCodes[resp.Status]; ok {
		if reqID != "" {
			body["_req_id"] = reqID
		}
		return SessionHTTPResult{OK: true, StatusCode: resp.Status, Body: body, URL: postURL, ReqID: reqID}, nil
	}
	errMsg := fmt.Sprintf("HTTP %d", resp.Status)
	return SessionHTTPResult{
		OK:         false,
		StatusCode: resp.Status,
		Body:       body,
		URL:        postURL,
		ReqID:      reqID,
		Error:      errMsg,
	}, nil
}

func (c *SessionClient) headers(ctx context.Context, mode string, extra map[string]string, target config.DataTarget) (map[string]string, error) {
	auth, err := c.auth.ResolveAuth(ctx, target, false)
	if err != nil {
		return nil, err
	}
	h := MergeHeaders(
		auth.Headers,
		StampHeaders(c.stampUser, c.version),
		map[string]string{"Content-Type": "application/json", "Accept": "application/json"},
		extra,
	)
	h = ApplyModeHeader(h, mode)
	if c.preMint && !domain.HasReqIDHeader(h) {
		h[domain.ReqIDHeader] = domain.NewZeusReqID()
	}
	return h, nil
}

func (c *SessionClient) sinkStamp(sessionID string) map[string]any {
	ip := domain.ResolveClientIP(c.identity.IPAddress, map[string]string{}, false)
	return domain.ProductStamp(domain.StampOptions{
		User:      c.stampUser,
		IPAddress: ip,
		Version:   c.version,
		SessionID: sessionID,
	})
}

func (c *SessionClient) guard(ctx context.Context) error {
	if c == nil {
		return domain.NewSession(domain.CodeSessionCreateFailed, sessionComponent,
			domain.WithMessage("session client is nil"))
	}
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		if de := domain.FromContext(err); de != nil {
			return de
		}
		return err
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return domain.NewSession(domain.CodeSessionCreateFailed, sessionComponent,
			domain.WithMessage("session client closed"))
	}
	return nil
}

func (c *SessionClient) httpPort() (ports.HttpPort, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, domain.NewSession(domain.CodeSessionCreateFailed, sessionComponent,
			domain.WithMessage("session client closed"))
	}
	if c.http == nil {
		c.http = httpx.New(httpx.Options{
			Timeout:       timeoutDuration(c.endpoint.TimeoutS),
			SkipTLSVerify: !c.endpoint.TLSVerify,
		})
		c.ownsHTTP = true
	}
	return c.http, nil
}

func (c *SessionClient) setLastReqID(id string) {
	c.mu.Lock()
	c.lastReqID = id
	c.mu.Unlock()
}

func (c *SessionClient) emit(level, msg string, attrs map[string]any) {
	line := formatAuthLog(level, msg, attrs)
	c.mu.Lock()
	c.logs = append(c.logs, line)
	c.mu.Unlock()
}

func mapOrEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func copyAnyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
