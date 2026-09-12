// SPDX-License-Identifier: BUSL-1.1

package llmopenai

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/koten-ai/zeus_client_golang/adapters/secretsenv"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/internal/httpx"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const llmComponent = "adapters.llm_openai_compatible"

const defaultAPIStyle = "openai_compatible"

// Client is LlmPort over OpenAI-compatible POST /chat/completions.
type Client struct {
	config   config.LlmProviderConfig
	secrets  ports.SecretStore
	retry    config.RetryPolicy
	budget   *domain.RetryBudget
	journal  journal.ExecutionJournal
	turnID   string
	http     ports.HttpPort
	ownsHTTP bool
	sleep    func(ctx context.Context, d time.Duration) error
	logf     func(level, msg string, attrs map[string]any)

	mu     sync.Mutex
	closed bool
	logs   []string
}

var (
	_ ports.LlmPort = (*Client)(nil)
	_ ports.Closer  = (*Client)(nil)
)

// Options constructs Client. HTTP nil → dedicated httpx client (LLM timeout).
type Options struct {
	Config  config.LlmProviderConfig
	Secrets ports.SecretStore
	HTTP    ports.HttpPort
	Retry   config.RetryPolicy
	Budget  *domain.RetryBudget
	Journal journal.ExecutionJournal
	TurnID  string
	Sleep   func(ctx context.Context, d time.Duration) error
	Log     func(level, msg string, attrs map[string]any)
}

// New returns an OpenAI-compatible LLM adapter.
func New(opts Options) *Client {
	sec := opts.Secrets
	if sec == nil {
		sec = secretsenv.New()
	}
	httpPort := opts.HTTP
	owns := false
	if httpPort == nil {
		httpPort = httpx.New(httpx.Options{Timeout: timeoutDuration(opts.Config.TimeoutS)})
		owns = true
	}
	return &Client{
		config:   opts.Config,
		secrets:  sec,
		retry:    opts.Retry,
		budget:   opts.Budget,
		journal:  opts.Journal,
		turnID:   opts.TurnID,
		http:     httpPort,
		ownsHTTP: owns,
		sleep:    opts.Sleep,
		logf:     opts.Log,
	}
}

// Logs is the testable hop log blob (no secrets / bodies).
func (c *Client) Logs() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.logs, "\n")
}

// Close releases an owned HTTP client. Injected ports stay. Idempotent.
func (c *Client) Close(ctx context.Context) error {
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
	httpPort := c.http
	owns := c.ownsHTTP
	c.mu.Unlock()
	if owns && httpPort != nil {
		return httpPort.Close(ctx)
	}
	return nil
}

// Complete is POST {base}/chat/completions. Classifies 050010–050018.
func (c *Client) Complete(ctx context.Context, req ports.LlmRequest) (ports.LlmResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.guard(ctx); err != nil {
		return ports.LlmResponse{}, err
	}
	style := strings.ToLower(strings.TrimSpace(c.config.APIStyle))
	if style == "" {
		style = defaultAPIStyle
	}
	if strings.EqualFold(strings.TrimSpace(c.config.Provider), "custom") && style != defaultAPIStyle {
		return ports.LlmResponse{}, domain.NewLLM(domain.CodeNotImplemented, llmComponent,
			domain.WithMessage("llm api_style not implemented"))
	}

	apiKey, err := c.resolveAPIKey(ctx)
	if err != nil {
		return ports.LlmResponse{}, err
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(c.config.Model)
	}
	if model == "" {
		return ports.LlmResponse{}, domain.NewLLM(domain.CodeLLMModelMissing, llmComponent,
			domain.WithMessage("llm model missing"))
	}
	base := strings.TrimRight(c.config.BaseURL, "/")
	if base == "" {
		return ports.LlmResponse{}, domain.NewLLM(domain.CodeLLMBaseURLMissing, llmComponent,
			domain.WithMessage("llm base_url missing"))
	}

	extraHeaders, bodyExtra := CacheHints(c.config.Provider, base, req.ConvID)
	temp := 0.0
	if req.Temperature != nil {
		temp = *req.Temperature
	}
	payload := BuildChatPayload(model, req.Messages, req.Tools, &temp, req.MaxTokens, bodyExtra)
	raw, err := json.Marshal(payload)
	if err != nil {
		return ports.LlmResponse{}, domain.NewLLM(domain.CodeLLMInvalidRequest, llmComponent,
			domain.WithCause(err), domain.WithMessage("llm body encode failed"))
	}
	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
		"Accept":        "application/json",
	}
	for k, v := range extraHeaders {
		headers[k] = v
	}
	postURL := base + "/chat/completions"
	provider := c.config.Provider
	c.emit("info", "zeus_client.llm.request_started", map[string]any{
		"llm.provider":      provider,
		"llm.model":         model,
		"llm.base_url_host": hostOf(c.config.BaseURL),
	})

	budget := c.budgetForCall()
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			if de := domain.FromContext(err); de != nil {
				return ports.LlmResponse{}, de
			}
			return ports.LlmResponse{}, err
		}
		t0 := time.Now()
		resp, reqErr := c.httpPort().Request(ctx, ports.HTTPRequest{
			Method:   "POST",
			URL:      postURL,
			Headers:  headers,
			Body:     raw,
			TimeoutS: c.config.TimeoutS,
		})
		latencyMS := int(time.Since(t0).Milliseconds())
		if reqErr != nil {
			cls := classifyTransport(reqErr)
			c.journalRound(false, model, attempt, &cls, nil, latencyMS, 0)
			if de, ok := domain.AsError(reqErr); ok && de.Code == domain.CodeCancelled {
				return ports.LlmResponse{}, de
			}
			delay := backoffMS(c.retry, attempt, nil)
			if budget.Allow(cls.Retryable, delay) {
				budget.Consume(delay)
				attempt++
				if err := c.doSleep(ctx, time.Duration(delay)*time.Millisecond); err != nil {
					return ports.LlmResponse{}, err
				}
				continue
			}
			return ports.LlmResponse{}, c.raiseClassified(cls, provider, model)
		}

		body := decodeLLMBody(resp.Headers, resp.Body)
		if resp.Status < 400 {
			m, ok := body.(map[string]any)
			if !ok {
				cls := domain.ClassifyHTTP(500, body)
				c.journalRound(false, model, attempt, &cls, nil, latencyMS, 0)
				return ports.LlmResponse{}, c.raiseClassified(cls, provider, model)
			}
			parsed := ParseCompletionResponse(m)
			c.journalRound(true, model, attempt, nil, parsed.Usage, latencyMS, len(parsed.ToolCalls))
			finish := map[string]any{
				"llm.provider": provider,
				"llm.model":    model,
				"duration_ms":  latencyMS,
				"result":       "ok",
			}
			if n, ok := asInt(parsed.Usage["prompt_tokens"]); ok {
				finish["tokens.input"] = n
			}
			if n, ok := asInt(parsed.Usage["completion_tokens"]); ok {
				finish["tokens.output"] = n
			}
			c.emit("info", "zeus_client.llm.request_finished", finish)
			return parsed, nil
		}

		cls := domain.ClassifyHTTP(resp.Status, body)
		c.journalRound(false, model, attempt, &cls, nil, latencyMS, 0)
		retryAfter := parseRetryAfter(resp.Headers)
		delay := backoffMS(c.retry, attempt, retryAfter)
		if budget.Allow(cls.Retryable, delay) {
			budget.Consume(delay)
			attempt++
			if err := c.doSleep(ctx, time.Duration(delay)*time.Millisecond); err != nil {
				return ports.LlmResponse{}, err
			}
			continue
		}
		return ports.LlmResponse{}, c.raiseClassified(cls, provider, model)
	}
}

func (c *Client) guard(ctx context.Context) error {
	if c == nil {
		return domain.NewLLM(domain.CodeAgentLLMRequestFailed, llmComponent,
			domain.WithMessage("llm client is nil"))
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
		return domain.NewLLM(domain.CodeAgentLLMRequestFailed, llmComponent,
			domain.WithMessage("llm client closed"))
	}
	return nil
}

func (c *Client) httpPort() ports.HttpPort {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.http
}

func (c *Client) resolveAPIKey(ctx context.Context) (string, error) {
	name := strings.TrimSpace(c.config.APIKeyEnv)
	var val string
	var ok bool
	if name != "" && c.secrets != nil {
		val, ok = c.secrets.Get(ctx, name)
	}
	if !ok || strings.TrimSpace(val) == "" {
		return "", domain.NewLLM(domain.CodeLLMAPIKeyMissing, llmComponent,
			domain.WithMessage("llm api_key missing"),
			domain.WithDetails(map[string]any{"api_key_env": name}))
	}
	return val, nil
}

func (c *Client) budgetForCall() domain.RetryBudget {
	if c.budget != nil {
		b := *c.budget
		b.AttemptsUsed = 0
		b.MSUsed = 0
		if b.MaxExtraMS == 0 {
			b.MaxExtraMS = 30_000
		}
		return b
	}
	return domain.NewRetryBudget(c.retry.MaxAttempts)
}

func (c *Client) raiseClassified(cls domain.LlmClassification, provider, model string) error {
	details := cls.ToMeta()
	details["llm.provider"] = provider
	details["llm.model"] = model
	details["llm.base_url_host"] = hostOf(c.config.BaseURL)
	if cls.MessagePreview != "" {
		preview := cls.MessagePreview
		if len(preview) > 200 {
			preview = preview[:200]
		}
		details["llm.message_preview"] = preview
	}
	c.emit("error", "zeus_client.llm.request_failed", map[string]any{
		"llm.provider":     provider,
		"llm.model":        model,
		"llm.error_class":  string(cls.ErrorClass),
		"http.status_code": statusOrNil(cls.HTTPStatus),
		"llm.retryable":    cls.Retryable,
		"error.code":       string(cls.Code),
		"error.message":    "llm request failed",
		"result":           "error",
	})
	return domain.NewLLM(cls.Code, llmComponent,
		domain.WithRetryable(cls.Retryable),
		domain.WithDetails(details))
}

func (c *Client) journalRound(ok bool, model string, attempt int, cls *domain.LlmClassification, usage map[string]any, latencyMS, toolCallCount int) {
	if c.journal == nil {
		return
	}
	data := map[string]any{
		"ok":                ok,
		"llm.provider":      c.config.Provider,
		"llm.model":         model,
		"llm.base_url_host": hostOf(c.config.BaseURL),
		"attempt":           attempt,
		"tool_call_count":   toolCallCount,
		"latency_ms":        latencyMS,
	}
	if usage != nil {
		safe := map[string]any{}
		for _, k := range []string{"prompt_tokens", "completion_tokens", "total_tokens", "cached_tokens"} {
			if v, ok := usage[k]; ok {
				safe[k] = v
			}
		}
		if ct, ok := CachedTokensOf(map[string]any{"usage": usage}); ok {
			if _, exists := safe["cached_tokens"]; !exists {
				safe["cached_tokens"] = ct
			}
		}
		data["usage"] = safe
	}
	if cls != nil {
		for k, v := range cls.ToMeta() {
			data[k] = v
		}
	}
	c.journal.Append(journal.JournalEvent{
		EventID:   journal.NewEventID(),
		TsMs:      time.Now().UnixMilli(),
		Type:      journal.EventLLMRound,
		Component: llmComponent,
		TurnID:    c.turnID,
		Data:      data,
	})
}

func (c *Client) doSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		if c.sleep != nil {
			return c.sleep(ctx, 0)
		}
		return nil
	}
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		if de := domain.FromContext(ctx.Err()); de != nil {
			return de
		}
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) emit(level, msg string, attrs map[string]any) {
	line := formatLLMLog(level, msg, attrs)
	c.mu.Lock()
	c.logs = append(c.logs, line)
	logf := c.logf
	c.mu.Unlock()
	if logf != nil {
		logf(level, msg, attrs)
	}
}

func classifyTransport(err error) domain.LlmClassification {
	if de, ok := domain.AsError(err); ok {
		switch de.Code {
		case domain.CodeContextDeadline, domain.CodeTimeout:
			return domain.ClassifyLlmFailure(domain.ClassifyOpts{Timeout: true})
		}
	}
	return domain.ClassifyLlmFailure(domain.ClassifyOpts{TransportError: true})
}

func decodeLLMBody(headers map[string]string, raw []byte) any {
	ct := headerFold(headers, "Content-Type")
	if strings.HasPrefix(strings.ToLower(ct), "application/json") || json.Valid(raw) {
		var v any
		if err := json.Unmarshal(raw, &v); err == nil {
			return v
		}
	}
	s := string(raw)
	if len(s) > 2000 {
		s = s[:2000]
	}
	return s
}

func parseRetryAfter(headers map[string]string) *float64 {
	raw := strings.TrimSpace(headerFold(headers, "Retry-After"))
	if raw == "" {
		return nil
	}
	var f float64
	if _, err := fmt.Sscanf(raw, "%f", &f); err != nil {
		return nil
	}
	return &f
}

func headerFold(h map[string]string, name string) string {
	for k, v := range h {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

func backoffMS(policy config.RetryPolicy, attempt int, retryAfterS *float64) int {
	var base int
	if retryAfterS != nil && *retryAfterS >= 0 {
		base = int(*retryAfterS * 1000)
	} else {
		shift := attempt
		if shift < 0 {
			shift = 0
		}
		if shift > 16 {
			shift = 16
		}
		mult := 1 << shift
		base = policy.BaseDelayMS * mult
	}
	if policy.MaxDelayMS > 0 && base > policy.MaxDelayMS {
		base = policy.MaxDelayMS
	}
	if policy.Jitter && base > 0 {
		base = int(float64(base) * (0.5 + rand.Float64()*0.5))
	}
	if base < 0 {
		return 0
	}
	return base
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func timeoutDuration(seconds float64) time.Duration {
	if seconds <= 0 {
		return 120 * time.Second
	}
	return time.Duration(seconds * float64(time.Second))
}

func statusOrNil(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func formatLLMLog(level, msg string, attrs map[string]any) string {
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
