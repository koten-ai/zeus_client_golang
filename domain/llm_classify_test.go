// SPDX-License-Identifier: BUSL-1.1

package domain

import "testing"

func TestClassifyTable(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      any
		class     LlmErrorClass
		code      Code
		retryable bool
	}{
		{
			"g7.1 rate_limit_exceeded",
			429,
			map[string]any{"error": map[string]any{"code": "rate_limit_exceeded", "type": "tokens", "message": "Rate limit"}},
			LlmClassRateLimit, CodeLLMRateLimit, true,
		},
		{
			"g7.2 insufficient_quota",
			429,
			map[string]any{"error": map[string]any{
				"code": "insufficient_quota", "type": "insufficient_quota",
				"message": "You exceeded your current quota",
			}},
			LlmClassQuotaExhausted, CodeLLMQuotaExhausted, false,
		},
		{
			"429 too many requests",
			429,
			map[string]any{"error": map[string]any{"message": "Too Many Requests", "type": "rate_limit_error"}},
			LlmClassRateLimit, CodeLLMRateLimit, true,
		},
		{
			"429 billing hard limit",
			429,
			map[string]any{"error": map[string]any{"message": "Billing hard limit reached", "code": "billing_not_active"}},
			LlmClassQuotaExhausted, CodeLLMQuotaExhausted, false,
		},
		{
			"401 incorrect key",
			401,
			map[string]any{"error": map[string]any{"message": "Incorrect API key", "type": "invalid_request_error"}},
			LlmClassAuth, CodeLLMAuth, false,
		},
		{
			"403 model not allowed",
			403,
			map[string]any{"error": map[string]any{"message": "model not allowed"}},
			LlmClassAuth, CodeLLMAuth, false,
		},
		{
			"403 billing suspended",
			403,
			map[string]any{"error": map[string]any{"message": "billing account suspended"}},
			LlmClassQuotaExhausted, CodeLLMQuotaExhausted, false,
		},
		{
			"g7.3 context length",
			400,
			map[string]any{"error": map[string]any{
				"message": "This model's maximum context length is 128000 tokens",
				"code":    "context_length_exceeded",
				"type":    "invalid_request_error",
			}},
			LlmClassContextLength, CodeLLMContextLength, false,
		},
		{
			"400 prompt too long",
			400,
			map[string]any{"error": map[string]any{"message": "prompt is too long: 200000 tokens"}},
			LlmClassContextLength, CodeLLMContextLength, false,
		},
		{
			"400 content filter",
			400,
			map[string]any{"error": map[string]any{"message": "Content filter blocked the request", "code": "content_filter"}},
			LlmClassContentFilter, CodeLLMContentFilter, false,
		},
		{
			"422 safety",
			422,
			map[string]any{"error": map[string]any{"message": "safety system blocked output"}},
			LlmClassContentFilter, CodeLLMContentFilter, false,
		},
		{
			"400 tool_choice",
			400,
			map[string]any{"error": map[string]any{
				"message": "tool_choice was set but no tools were specified",
				"type":    "invalid_request_error",
			}},
			LlmClassToolSchema, CodeLLMToolSchema, false,
		},
		{
			"400 unsupported param",
			400,
			map[string]any{"error": map[string]any{"message": "Unsupported parameter: foo", "type": "invalid_request_error"}},
			LlmClassInvalidRequest, CodeLLMInvalidRequest, false,
		},
		{
			"529 overload",
			529,
			map[string]any{"error": map[string]any{"message": "Overloaded"}},
			LlmClassProviderOverload, CodeLLMProviderOverload, true,
		},
		{
			"503 capacity overloaded",
			503,
			map[string]any{"error": map[string]any{"message": "capacity exhausted, overloaded"}},
			LlmClassProviderOverload, CodeLLMProviderOverload, true,
		},
		{
			"502 string body",
			502,
			"bad gateway",
			LlmClassProviderUnavailable, CodeLLMProviderUnavailable, true,
		},
		{
			"500 internal",
			500,
			map[string]any{"error": map[string]any{"message": "internal"}},
			LlmClassProviderUnavailable, CodeLLMProviderUnavailable, true,
		},
		{
			"402 payment",
			402,
			map[string]any{"error": map[string]any{"message": "Payment required"}},
			LlmClassQuotaExhausted, CodeLLMQuotaExhausted, false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := ClassifyHTTP(tc.status, tc.body)
			if c.ErrorClass != tc.class {
				t.Fatalf("class %s want %s", c.ErrorClass, tc.class)
			}
			if c.Code != tc.code {
				t.Fatalf("code %s want %s", c.Code, tc.code)
			}
			if c.Retryable != tc.retryable {
				t.Fatalf("retryable %v", c.Retryable)
			}
			if c.HTTPStatus == nil || *c.HTTPStatus != tc.status {
				t.Fatalf("status %v", c.HTTPStatus)
			}
			if len(string(c.Code)) < 3 || string(c.Code)[:3] != "050" {
				t.Fatalf("code band %s", c.Code)
			}
		})
	}
}

func TestClassifyTimeoutAndTransport(t *testing.T) {
	to := ClassifyLlmFailure(ClassifyOpts{Timeout: true})
	if to.ErrorClass != LlmClassTimeout || to.Code != CodeAgentLLMTimeout || !to.Retryable {
		t.Fatalf("%+v", to)
	}
	u := ClassifyLlmFailure(ClassifyOpts{TransportError: true})
	if u.ErrorClass != LlmClassProviderUnavailable || u.Code != CodeLLMProviderUnavailable {
		t.Fatalf("%+v", u)
	}
}

func TestRetryBudgetOnlyRetryable(t *testing.T) {
	b := RetryBudget{MaxAttempts: 2, MaxExtraMS: 10_000}
	if b.Allow(false, 0) {
		t.Fatal("non-retryable")
	}
	if !b.Allow(true, 100) {
		t.Fatal("first retry")
	}
	b.Consume(100)
	if b.AttemptsUsed != 1 || b.MSUsed != 100 {
		t.Fatalf("%+v", b)
	}
	b.Consume(50)
	if b.AttemptsUsed != 2 {
		t.Fatalf("used %d", b.AttemptsUsed)
	}
	if b.Allow(true, 0) {
		t.Fatal("attempts exhausted")
	}
}

func TestRetryBudgetMSCap(t *testing.T) {
	b := RetryBudget{MaxAttempts: 10, MaxExtraMS: 500}
	if !b.Allow(true, 400) {
		t.Fatal("400 ok")
	}
	b.Consume(400)
	if b.Allow(true, 200) {
		t.Fatal("ms cap")
	}
}
