// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// LlmErrorClass is the closed family llm.error_class ENUM (FAMILY_LLM_ERRORS).
type LlmErrorClass string

const (
	LlmClassRateLimit           LlmErrorClass = "rate_limit"
	LlmClassQuotaExhausted      LlmErrorClass = "quota_exhausted"
	LlmClassAuth                LlmErrorClass = "auth"
	LlmClassContextLength       LlmErrorClass = "context_length"
	LlmClassContentFilter       LlmErrorClass = "content_filter"
	LlmClassProviderOverload    LlmErrorClass = "provider_overload"
	LlmClassProviderUnavailable LlmErrorClass = "provider_unavailable"
	LlmClassTimeout             LlmErrorClass = "timeout"
	LlmClassInvalidRequest      LlmErrorClass = "invalid_request"
	LlmClassToolSchema          LlmErrorClass = "tool_schema"
	LlmClassUnknown             LlmErrorClass = "unknown"
)

// LlmClassification is one LLM failure mapped to class / code / retryable.
type LlmClassification struct {
	ErrorClass     LlmErrorClass
	Code           Code
	Retryable      bool
	HTTPStatus     *int
	ProviderCode   string
	ProviderType   string
	MessagePreview string
}

// ToMeta is safe attrs for journal / result meta (no secrets).
func (c LlmClassification) ToMeta() map[string]any {
	var status any
	if c.HTTPStatus != nil {
		status = *c.HTTPStatus
	}
	var pCode any
	if c.ProviderCode != "" {
		pCode = c.ProviderCode
	}
	var pType any
	if c.ProviderType != "" {
		pType = c.ProviderType
	}
	return map[string]any{
		"llm.error_class":   string(c.ErrorClass),
		"error.code":        string(c.Code),
		"error.retryable":   c.Retryable,
		"http.status_code":  status,
		"llm.provider_code": pCode,
		"llm.provider_type": pType,
	}
}

// RetryBudget is a per-turn retry budget — only retryable classes consume it.
// MaxAttempts counts extra tries after the first failure (Python RetryPolicy).
type RetryBudget struct {
	MaxAttempts  int
	MaxExtraMS   int
	AttemptsUsed int
	MSUsed       int
}

const defaultRetryBudgetExtraMS = 30_000

// NewRetryBudget returns a budget. maxAttempts < 0 is treated as 0.
func NewRetryBudget(maxAttempts int) RetryBudget {
	if maxAttempts < 0 {
		maxAttempts = 0
	}
	return RetryBudget{MaxAttempts: maxAttempts, MaxExtraMS: defaultRetryBudgetExtraMS}
}

// Allow reports whether another retry may be attempted.
func (b *RetryBudget) Allow(retryable bool, delayMS int) bool {
	if b == nil || !retryable {
		return false
	}
	if b.AttemptsUsed >= b.MaxAttempts {
		return false
	}
	if delayMS < 0 {
		delayMS = 0
	}
	if b.MaxExtraMS > 0 && b.MSUsed+delayMS > b.MaxExtraMS {
		return false
	}
	return true
}

// Consume records one used retry.
func (b *RetryBudget) Consume(delayMS int) {
	if b == nil {
		return
	}
	b.AttemptsUsed++
	if delayMS > 0 {
		b.MSUsed += delayMS
	}
}

var classToCode = map[LlmErrorClass]Code{
	LlmClassRateLimit:           CodeLLMRateLimit,
	LlmClassQuotaExhausted:      CodeLLMQuotaExhausted,
	LlmClassAuth:                CodeLLMAuth,
	LlmClassContextLength:       CodeLLMContextLength,
	LlmClassContentFilter:       CodeLLMContentFilter,
	LlmClassProviderOverload:    CodeLLMProviderOverload,
	LlmClassProviderUnavailable: CodeLLMProviderUnavailable,
	LlmClassTimeout:             CodeAgentLLMTimeout,
	LlmClassInvalidRequest:      CodeLLMInvalidRequest,
	LlmClassToolSchema:          CodeLLMToolSchema,
	LlmClassUnknown:             CodeAgentLLMRequestFailed,
}

// ErrorCodeForClass maps llm.error_class to the family ErrorCode.
func ErrorCodeForClass(cls LlmErrorClass) Code {
	if c, ok := classToCode[cls]; ok {
		return c
	}
	return CodeAgentLLMRequestFailed
}

var (
	quotaRE = regexp.MustCompile(`(?i)insufficient[_\s-]?quota|quota[_\s-]?exceeded|billing|credit|spend.?limit|payment|out of credits|exceeded your current quota`)
	rateRE  = regexp.MustCompile(`(?i)rate[_\s-]?limit|too many requests|tokens per minute|requests per minute|rpm|tpm`)
	ctxRE   = regexp.MustCompile(`(?i)context[_\s-]?length|maximum context|max[_\s-]?tokens|token.?limit|too many tokens|prompt is too long|context window|max_model_len|input is too long`)
	filtRE  = regexp.MustCompile(`(?i)content[_\s-]?filter|content[_\s-]?policy|safety|moderation|blocked by`)
	toolRE  = regexp.MustCompile(`(?i)tool[_\s-]?choice|tools? were|function[_\s-]?call|tool[_\s-]?schema|invalid tools|functions?`)
	overRE  = regexp.MustCompile(`(?i)overload|capacity|overloaded|try again later`)
	billRE  = regexp.MustCompile(`(?i)billing|payment|credit|subscription`)
)

// ClassifyOpts is classify_llm_failure kwargs.
type ClassifyOpts struct {
	Status         int
	HasStatus      bool
	Body           any
	TransportError bool
	Timeout        bool
}

// ClassifyHTTP is ClassifyLlmFailure for a received HTTP status.
func ClassifyHTTP(status int, body any) LlmClassification {
	return ClassifyLlmFailure(ClassifyOpts{Status: status, HasStatus: true, Body: body})
}

// ClassifyLlmFailure maps HTTP status + provider JSON to family class / code / retryable.
// Algorithm: FAMILY_LLM_ERRORS §2. 429 quota must never classify as rate_limit.
func ClassifyLlmFailure(opts ClassifyOpts) LlmClassification {
	codeS, typS, msg := extractLLMErrorFields(opts.Body)
	text := strings.TrimSpace(strings.Join(nonEmpty(codeS, typS, msg), " "))

	fin := func(cls LlmErrorClass) LlmClassification {
		ec := ErrorCodeForClass(cls)
		out := LlmClassification{
			ErrorClass:     cls,
			Code:           ec,
			Retryable:      DefaultRetryable(ec),
			ProviderCode:   codeS,
			ProviderType:   typS,
			MessagePreview: clipPreview(msg, 240),
		}
		if opts.HasStatus {
			st := opts.Status
			out.HTTPStatus = &st
		}
		return out
	}

	if opts.Timeout {
		return fin(LlmClassTimeout)
	}
	if opts.TransportError || !opts.HasStatus {
		return fin(LlmClassProviderUnavailable)
	}

	st := opts.Status
	if st == 402 {
		return fin(LlmClassQuotaExhausted)
	}
	if st == 401 || st == 403 {
		if billRE.MatchString(text) || quotaRE.MatchString(text) {
			return fin(LlmClassQuotaExhausted)
		}
		return fin(LlmClassAuth)
	}
	if st == 429 {
		joined := strings.ToLower(text)
		if strings.Contains(strings.ToLower(codeS), "insufficient_quota") ||
			strings.Contains(strings.ToLower(typS), "insufficient_quota") ||
			quotaRE.MatchString(joined) {
			return fin(LlmClassQuotaExhausted)
		}
		return fin(LlmClassRateLimit)
	}
	if st == 529 {
		return fin(LlmClassProviderOverload)
	}
	if st == 500 || st == 502 || st == 503 || st == 504 {
		if st == 503 && overRE.MatchString(text) {
			return fin(LlmClassProviderOverload)
		}
		if overRE.MatchString(text) && strings.Contains(strings.ToLower(text), "capacity") {
			return fin(LlmClassProviderOverload)
		}
		return fin(LlmClassProviderUnavailable)
	}
	if st == 400 || st == 422 {
		if ctxRE.MatchString(text) {
			return fin(LlmClassContextLength)
		}
		if filtRE.MatchString(text) {
			return fin(LlmClassContentFilter)
		}
		if strings.Contains(strings.ToLower(codeS), "tool") {
			return fin(LlmClassToolSchema)
		}
		low := strings.ToLower(text)
		if toolRE.MatchString(text) && (strings.Contains(low, "schema") ||
			strings.Contains(low, "tool_choice") ||
			strings.Contains(low, "invalid") ||
			strings.Contains(low, "function")) {
			return fin(LlmClassToolSchema)
		}
		return fin(LlmClassInvalidRequest)
	}
	if rateRE.MatchString(text) && st >= 400 {
		return fin(LlmClassRateLimit)
	}
	return fin(LlmClassUnknown)
}

func extractLLMErrorFields(body any) (code, typ, msg string) {
	if s, ok := body.(string); ok {
		return "", "", clipPreview(s, 500)
	}
	m, ok := body.(map[string]any)
	if !ok || m == nil {
		if body != nil {
			return "", "", clipPreview(fmt.Sprint(body), 500)
		}
		return "", "", ""
	}
	err := m["error"]
	if s, ok := err.(string); ok {
		return "", "", clipPreview(s, 500)
	}
	em, ok := err.(map[string]any)
	if !ok {
		c, t, msg := asOptString(m["code"]), asOptString(m["type"]), firstString(m["message"], m["error_message"])
		if c != "" || t != "" || msg != "" {
			return c, t, clipPreview(msg, 500)
		}
		return "", "", clipPreview(fmt.Sprint(body), 500)
	}
	return asOptString(em["code"]), asOptString(em["type"]), clipPreview(firstString(em["message"]), 500)
}

func asOptString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func firstString(vals ...any) string {
	for _, v := range vals {
		if v == nil {
			continue
		}
		s := fmt.Sprint(v)
		if s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}

func nonEmpty(parts ...string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func clipPreview(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}
