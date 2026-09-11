// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// pythonErrorCodes is copied from zeus_client.domain.errors.ErrorCode (ZCP-4).
var pythonErrorCodes = []struct {
	name string
	code string
}{
	{"UNKNOWN", "000001"},
	{"NOT_IMPLEMENTED", "000002"},
	{"INVALID_ARGUMENT", "000003"},
	{"PRECONDITION_FAILED", "000004"},
	{"TIMEOUT", "000005"},
	{"CANCELLED", "000006"},
	{"INTERNAL_INVARIANT", "000007"},
	{"FEATURE_DISABLED", "000008"},
	{"CLIENT_RATE_LIMITED", "000009"},
	{"CONTEXT_DEADLINE", "000010"},
	{"CONFIG_PATH_NOT_FOUND", "010001"},
	{"CONFIG_NOT_READABLE", "010002"},
	{"CONFIG_PARSE_FAILED", "010003"},
	{"CONFIG_NORMALIZE_FAILED", "010004"},
	{"CONFIG_INVALID", "010006"},
	{"ZEUS_URL_MISSING", "010007"},
	{"ZEUS_AUTH_MODE_INVALID", "010008"},
	{"ZEUS_AUTH_INCOMPLETE", "010009"},
	{"LLM_BASE_URL_MISSING", "010010"},
	{"LLM_MODEL_MISSING", "010011"},
	{"LLM_API_KEY_MISSING", "010012"},
	{"CATALOG_NOT_FOUND", "030001"},
	{"CATALOG_PARSE_FAILED", "030002"},
	{"CATALOG_LINEAGE_UNKNOWN", "030003"},
	{"CONTRACT_HASH_MISSING", "030004"},
	{"CONTRACT_HASH_INVENT_FORBIDDEN", "030005"},
	{"CONTRACT_BIND_MISMATCH", "030006"},
	{"CATALOG_SYNC_FAILED", "030007"},
	{"SESSION_CREATE_FAILED", "040001"},
	{"SESSION_CONTINUE_FAILED", "040002"},
	{"SESSION_REHYDRATE_FAILED", "040003"},
	{"SESSION_ID_MISSING", "040004"},
	{"SESSION_DURABLE_DISABLED", "040005"},
	{"SESSION_COMMIT_FAILED", "040006"},
	{"AGENT_MEMORY_RECALL_FAILED", "040008"},
	{"AGENT_MEMORY_WRITE_FAILED", "040009"},
	{"AGENT_MEMORY_UNAVAILABLE", "040010"},
	{"AGENT_TURN_FAILED", "050001"},
	{"AGENT_MESSAGE_EMPTY", "050002"},
	{"AGENT_MAX_ROUNDS", "050003"},
	{"AGENT_LLM_REQUEST_FAILED", "050004"},
	{"AGENT_LLM_TIMEOUT", "050005"},
	{"AGENT_TOOL_PLAN_INVALID", "050006"},
	{"AGENT_TERMINATE_MISSING", "050007"},
	{"AGENT_HOOKS_ABORTED", "050008"},
	{"LLM_RATE_LIMIT", "050010"},
	{"LLM_QUOTA_EXHAUSTED", "050011"},
	{"LLM_AUTH", "050012"},
	{"LLM_CONTEXT_LENGTH", "050013"},
	{"LLM_CONTENT_FILTER", "050014"},
	{"LLM_PROVIDER_OVERLOAD", "050015"},
	{"LLM_PROVIDER_UNAVAILABLE", "050016"},
	{"LLM_INVALID_REQUEST", "050017"},
	{"LLM_TOOL_SCHEMA", "050018"},
	{"ZEUS_TRANSPORT", "060001"},
	{"ZEUS_HTTP_4XX", "060002"},
	{"ZEUS_HTTP_5XX", "060003"},
	{"ZEUS_CONTRACT_REQUIRED", "060004"},
	{"ZEUS_AUTH_FAILED", "060005"},
	{"ZEUS_VERB_NOT_ALLOWED", "060006"},
	{"ZEUS_SEARCH_INVALID", "060007"},
	{"ZEUS_RESPONSE_PARSE", "060008"},
	{"ZEUS_REQ_ID_MISSING", "060009"},
	{"ZEUS_PIPELINE_NOT_ON_DIRECT", "060010"},
	{"LAYER_A_VALIDATE_FAILED", "090001"},
	{"POLICY_REFUSE", "070002"},
	{"AUTH_FAILED", "100001"},
	{"AUTH_SESSION_UNAVAILABLE", "100002"},
	{"JOBS_UNAVAILABLE", "130001"},
	{"JOBS_INVALID_UNIT_MAP", "130002"},
	{"JOBS_BUDGET_INVALID", "130003"},
	{"JOBS_NOT_FOUND", "130004"},
	{"JOBS_WATCH_FAILED", "130005"},
	{"JOBS_UNIT_FAILED", "130010"},
	{"UNITS_CATALOG_MISSING", "130011"},
	{"UNITS_INJECT_MISSING", "130012"},
	{"UNITS_ISOLATION", "130013"},
	{"INTERNAL_BUG", "990001"},
}

func goCodeByPythonName(name string) Code {
	m := map[string]Code{
		"UNKNOWN":                        CodeUnknown,
		"NOT_IMPLEMENTED":                CodeNotImplemented,
		"INVALID_ARGUMENT":               CodeInvalidArgument,
		"PRECONDITION_FAILED":            CodePreconditionFailed,
		"TIMEOUT":                        CodeTimeout,
		"CANCELLED":                      CodeCancelled,
		"INTERNAL_INVARIANT":             CodeInternalInvariant,
		"FEATURE_DISABLED":               CodeFeatureDisabled,
		"CLIENT_RATE_LIMITED":            CodeClientRateLimited,
		"CONTEXT_DEADLINE":               CodeContextDeadline,
		"CONFIG_PATH_NOT_FOUND":          CodeConfigPathNotFound,
		"CONFIG_NOT_READABLE":            CodeConfigNotReadable,
		"CONFIG_PARSE_FAILED":            CodeConfigParseFailed,
		"CONFIG_NORMALIZE_FAILED":        CodeConfigNormalizeFailed,
		"CONFIG_INVALID":                 CodeConfigInvalid,
		"ZEUS_URL_MISSING":               CodeZeusURLMissing,
		"ZEUS_AUTH_MODE_INVALID":         CodeZeusAuthModeInvalid,
		"ZEUS_AUTH_INCOMPLETE":           CodeZeusAuthIncomplete,
		"LLM_BASE_URL_MISSING":           CodeLLMBaseURLMissing,
		"LLM_MODEL_MISSING":              CodeLLMModelMissing,
		"LLM_API_KEY_MISSING":            CodeLLMAPIKeyMissing,
		"CATALOG_NOT_FOUND":              CodeCatalogNotFound,
		"CATALOG_PARSE_FAILED":           CodeCatalogParseFailed,
		"CATALOG_LINEAGE_UNKNOWN":        CodeCatalogLineageUnknown,
		"CONTRACT_HASH_MISSING":          CodeContractHashMissing,
		"CONTRACT_HASH_INVENT_FORBIDDEN": CodeContractHashInventForbidden,
		"CONTRACT_BIND_MISMATCH":         CodeContractBindMismatch,
		"CATALOG_SYNC_FAILED":            CodeCatalogSyncFailed,
		"SESSION_CREATE_FAILED":          CodeSessionCreateFailed,
		"SESSION_CONTINUE_FAILED":        CodeSessionContinueFailed,
		"SESSION_REHYDRATE_FAILED":       CodeSessionRehydrateFailed,
		"SESSION_ID_MISSING":             CodeSessionIDMissing,
		"SESSION_DURABLE_DISABLED":       CodeSessionDurableDisabled,
		"SESSION_COMMIT_FAILED":          CodeSessionCommitFailed,
		"AGENT_MEMORY_RECALL_FAILED":     CodeAgentMemoryRecallFailed,
		"AGENT_MEMORY_WRITE_FAILED":      CodeAgentMemoryWriteFailed,
		"AGENT_MEMORY_UNAVAILABLE":       CodeAgentMemoryUnavailable,
		"AGENT_TURN_FAILED":              CodeAgentTurnFailed,
		"AGENT_MESSAGE_EMPTY":            CodeAgentMessageEmpty,
		"AGENT_MAX_ROUNDS":               CodeAgentMaxRounds,
		"AGENT_LLM_REQUEST_FAILED":       CodeAgentLLMRequestFailed,
		"AGENT_LLM_TIMEOUT":              CodeAgentLLMTimeout,
		"AGENT_TOOL_PLAN_INVALID":        CodeAgentToolPlanInvalid,
		"AGENT_TERMINATE_MISSING":        CodeAgentTerminateMissing,
		"AGENT_HOOKS_ABORTED":            CodeAgentHooksAborted,
		"LLM_RATE_LIMIT":                 CodeLLMRateLimit,
		"LLM_QUOTA_EXHAUSTED":            CodeLLMQuotaExhausted,
		"LLM_AUTH":                       CodeLLMAuth,
		"LLM_CONTEXT_LENGTH":             CodeLLMContextLength,
		"LLM_CONTENT_FILTER":             CodeLLMContentFilter,
		"LLM_PROVIDER_OVERLOAD":          CodeLLMProviderOverload,
		"LLM_PROVIDER_UNAVAILABLE":       CodeLLMProviderUnavailable,
		"LLM_INVALID_REQUEST":            CodeLLMInvalidRequest,
		"LLM_TOOL_SCHEMA":                CodeLLMToolSchema,
		"ZEUS_TRANSPORT":                 CodeZeusTransport,
		"ZEUS_HTTP_4XX":                  CodeZeusHTTP4xx,
		"ZEUS_HTTP_5XX":                  CodeZeusHTTP5xx,
		"ZEUS_CONTRACT_REQUIRED":         CodeZeusContractRequired,
		"ZEUS_AUTH_FAILED":               CodeZeusAuthFailed,
		"ZEUS_VERB_NOT_ALLOWED":          CodeZeusVerbNotAllowed,
		"ZEUS_SEARCH_INVALID":            CodeZeusSearchInvalid,
		"ZEUS_RESPONSE_PARSE":            CodeZeusResponseParse,
		"ZEUS_REQ_ID_MISSING":            CodeZeusReqIDMissing,
		"ZEUS_PIPELINE_NOT_ON_DIRECT":    CodeZeusPipelineNotOnDirect,
		"LAYER_A_VALIDATE_FAILED":        CodeLayerAValidateFailed,
		"POLICY_REFUSE":                  CodePolicyRefuse,
		"AUTH_FAILED":                    CodeAuthFailed,
		"AUTH_SESSION_UNAVAILABLE":       CodeAuthSessionUnavailable,
		"JOBS_UNAVAILABLE":               CodeJobsUnavailable,
		"JOBS_INVALID_UNIT_MAP":          CodeJobsInvalidUnitMap,
		"JOBS_BUDGET_INVALID":            CodeJobsBudgetInvalid,
		"JOBS_NOT_FOUND":                 CodeJobsNotFound,
		"JOBS_WATCH_FAILED":              CodeJobsWatchFailed,
		"JOBS_UNIT_FAILED":               CodeJobsUnitFailed,
		"UNITS_CATALOG_MISSING":          CodeUnitsCatalogMissing,
		"UNITS_INJECT_MISSING":           CodeUnitsInjectMissing,
		"UNITS_ISOLATION":                CodeUnitsIsolation,
		"INTERNAL_BUG":                   CodeInternalBug,
	}
	return m[name]
}

func TestPythonOracleCodeStrings(t *testing.T) {
	if len(AllCodes()) != len(pythonErrorCodes) {
		t.Fatalf("AllCodes=%d python=%d", len(AllCodes()), len(pythonErrorCodes))
	}
	for _, tc := range pythonErrorCodes {
		got := goCodeByPythonName(tc.name)
		if string(got) != tc.code {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.code)
		}
		parsed, ok := ParseCode(tc.code)
		if !ok || parsed != got {
			t.Errorf("ParseCode(%s) = %q,%v", tc.code, parsed, ok)
		}
	}
}

func TestErrorCodeIsSixDigitStringNoDots(t *testing.T) {
	if CodeLLMRateLimit != "050010" {
		t.Fatalf("LLM_RATE_LIMIT=%s", CodeLLMRateLimit)
	}
	if CodeLLMQuotaExhausted != "050011" {
		t.Fatalf("LLM_QUOTA_EXHAUSTED=%s", CodeLLMQuotaExhausted)
	}
	if CodeLLMContextLength != "050013" {
		t.Fatalf("LLM_CONTEXT_LENGTH=%s", CodeLLMContextLength)
	}
	for _, c := range AllCodes() {
		s := string(c)
		if len(s) != 6 {
			t.Errorf("%s: want 6 digits", s)
		}
		for i := 0; i < len(s); i++ {
			if s[i] < '0' || s[i] > '9' {
				t.Errorf("%s: non-digit", s)
			}
		}
		if strings.Contains(s, ".") {
			t.Errorf("dotted code %s", s)
		}
	}
}

func TestParseCodeRejectsDottedOverflow(t *testing.T) {
	for _, s := range []string{"010003.7", "010003.12", "99000.12345", "05010", "0500100", "abcdef"} {
		if _, ok := ParseCode(s); ok {
			t.Errorf("ParseCode(%q) should fail", s)
		}
	}
}

func TestFamilyLLMCodes050010Through050018(t *testing.T) {
	expected := map[string]Code{
		"050010": CodeLLMRateLimit,
		"050011": CodeLLMQuotaExhausted,
		"050012": CodeLLMAuth,
		"050013": CodeLLMContextLength,
		"050014": CodeLLMContentFilter,
		"050015": CodeLLMProviderOverload,
		"050016": CodeLLMProviderUnavailable,
		"050017": CodeLLMInvalidRequest,
		"050018": CodeLLMToolSchema,
	}
	for digits, member := range expected {
		if string(member) != digits {
			t.Errorf("%s: got %s", member, digits)
		}
		parsed, ok := ParseCode(digits)
		if !ok || parsed != member {
			t.Errorf("ParseCode(%s) = %q,%v", digits, parsed, ok)
		}
	}
}

func TestLLMRateVsQuotaRetryableFlags(t *testing.T) {
	if !DefaultRetryable(CodeLLMRateLimit) {
		t.Fatal("rate limit should be retryable")
	}
	if DefaultRetryable(CodeLLMQuotaExhausted) {
		t.Fatal("quota must not be retryable")
	}
	if DefaultRetryable(CodeLLMContextLength) {
		t.Fatal("context length must not be retryable")
	}
	if !DefaultRetryable(CodeLLMProviderOverload) {
		t.Fatal("overload should be retryable")
	}
	if !DefaultRetryable(CodeLLMProviderUnavailable) {
		t.Fatal("unavailable should be retryable")
	}
	if DefaultRetryable(CodeLLMAuth) {
		t.Fatal("auth must not be retryable")
	}
}

func TestErrorFields(t *testing.T) {
	err := NewLLM(CodeLLMRateLimit, "adapters.llm",
		WithMessage("LLM rate limited"),
		WithRetryable(true),
		WithCauseEventID("evt_1"),
		WithDetails(map[string]any{"http.status_code": 429}),
	)
	if err.Code != CodeLLMRateLimit {
		t.Fatalf("code %s", err.Code)
	}
	if !err.Retryable {
		t.Fatal("retryable")
	}
	if err.Component != "adapters.llm" {
		t.Fatalf("component %s", err.Component)
	}
	if err.Message != "LLM rate limited" {
		t.Fatalf("message %s", err.Message)
	}
	if err.CauseEventID != "evt_1" {
		t.Fatalf("cause %s", err.CauseEventID)
	}
	if err.Details["http.status_code"] != 429 {
		t.Fatalf("details %+v", err.Details)
	}
	if err.Type != "llm" {
		t.Fatalf("type %s", err.Type)
	}
	s := err.Error()
	if !strings.Contains(s, "050010") || !strings.Contains(s, "LLM rate limited") {
		t.Fatalf("Error() %q", s)
	}
}

func TestErrorDefaultsRetryableFromCode(t *testing.T) {
	rate := NewLLM(CodeLLMRateLimit, "llm")
	if !rate.Retryable {
		t.Fatal("rate")
	}
	quota := NewLLM(CodeLLMQuotaExhausted, "llm")
	if quota.Retryable {
		t.Fatal("quota")
	}
}

func TestTypedConstructorsSetType(t *testing.T) {
	cases := []struct {
		err *Error
		typ string
	}{
		{NewConfig(CodeConfigParseFailed, "config"), "config"},
		{NewAuth(CodeAuthFailed, "auth"), "auth"},
		{NewContract(CodeContractHashInventForbidden, "contract"), "contract"},
		{NewCatalog(CodeCatalogNotFound, "domain.catalog"), "catalog"},
		{NewSession(CodeSessionIDMissing, "session"), "session"},
		{NewZeusTransport(CodeZeusTransport, "http"), "zeus_http"},
		{NewZeusTool(CodeZeusHTTP4xx, "tool"), "zeus_tool"},
		{NewLLM(CodeLLMRateLimit, "llm"), "llm"},
		{NewPolicy(CodePolicyRefuse, "policy"), "policy"},
		{NewValidation(CodeInvalidArgument, "api"), "invalid_argument"},
		{NewInternal(CodeInternalBug, "runtime"), "internal"},
		{NewJob(CodeJobsUnavailable, "api.jobs"), "jobs"},
	}
	for _, tc := range cases {
		if tc.err.Type != tc.typ {
			t.Errorf("type %s want %s code %s", tc.err.Type, tc.typ, tc.err.Code)
		}
		var e *Error
		if !errors.As(tc.err, &e) {
			t.Errorf("errors.As failed for %s", tc.err.Code)
		}
	}
}

func TestPublicMessageStableEnglish(t *testing.T) {
	if !strings.Contains(strings.ToLower(PublicMessage(CodeLLMRateLimit)), "rate") {
		t.Fatal(PublicMessage(CodeLLMRateLimit))
	}
	if !strings.Contains(strings.ToLower(PublicMessage(CodeLLMQuotaExhausted)), "quota") {
		t.Fatal(PublicMessage(CodeLLMQuotaExhausted))
	}
	if !strings.Contains(strings.ToLower(PublicMessage(CodeLLMContextLength)), "context") {
		t.Fatal(PublicMessage(CodeLLMContextLength))
	}
}

func TestRaiseAndCatchByBase(t *testing.T) {
	err := NewCatalog(CodeCatalogNotFound, "domain.catalog", WithMessage("catalog not found"))
	got, ok := AsError(err)
	if !ok {
		t.Fatal("AsError")
	}
	if got.Code != CodeCatalogNotFound {
		t.Fatalf("code %s", got.Code)
	}
	if got.Retryable {
		t.Fatal("catalog not found is not retryable")
	}
}

func TestAgentMemoryErrorBand(t *testing.T) {
	if CodeAgentMemoryRecallFailed != "040008" {
		t.Fatal(CodeAgentMemoryRecallFailed)
	}
	if CodeAgentMemoryWriteFailed != "040009" {
		t.Fatal(CodeAgentMemoryWriteFailed)
	}
	if CodeAgentMemoryUnavailable != "040010" {
		t.Fatal(CodeAgentMemoryUnavailable)
	}
	if !DefaultRetryable(CodeAgentMemoryRecallFailed) {
		t.Fatal("recall retryable")
	}
	if !strings.Contains(PublicMessage(CodeAgentMemoryRecallFailed), "recall") {
		t.Fatal(PublicMessage(CodeAgentMemoryRecallFailed))
	}
}

func TestMultiAgentErrorBand(t *testing.T) {
	if CodeJobsUnavailable != "130001" {
		t.Fatal(CodeJobsUnavailable)
	}
	if CodeJobsInvalidUnitMap != "130002" {
		t.Fatal(CodeJobsInvalidUnitMap)
	}
	if CodeJobsBudgetInvalid != "130003" {
		t.Fatal(CodeJobsBudgetInvalid)
	}
	if CodeJobsNotFound != "130004" {
		t.Fatal(CodeJobsNotFound)
	}
	if CodeJobsWatchFailed != "130005" {
		t.Fatal(CodeJobsWatchFailed)
	}
	if CodeJobsUnitFailed != "130010" {
		t.Fatal(CodeJobsUnitFailed)
	}
	if CodeUnitsCatalogMissing != "130011" {
		t.Fatal(CodeUnitsCatalogMissing)
	}
	if CodeUnitsInjectMissing != "130012" {
		t.Fatal(CodeUnitsInjectMissing)
	}
	if CodeUnitsIsolation != "130013" {
		t.Fatal(CodeUnitsIsolation)
	}
	err := NewJob(CodeJobsUnavailable, "api.jobs")
	if err.Retryable {
		t.Fatal("jobs unavailable is not SDK-auto-retryable")
	}
	if !strings.Contains(err.Error(), "130001") {
		t.Fatal(err.Error())
	}
	if err.Type != "jobs" {
		t.Fatalf("type %s", err.Type)
	}
}

func TestUnwrapCause(t *testing.T) {
	inner := errors.New("boom")
	err := NewLLM(CodeLLMRateLimit, "adapters.llm", WithCause(inner))
	if !errors.Is(err, inner) {
		t.Fatal("Unwrap")
	}
}

func TestFromContextCancelledAndDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := FromContext(ctx.Err())
	if got == nil || got.Code != CodeCancelled {
		t.Fatalf("cancel: %+v", got)
	}
	if got.Type != "cancelled" {
		t.Fatalf("type %s", got.Type)
	}
	if !errors.Is(got, context.Canceled) {
		t.Fatal("cause canceled")
	}

	dctx, dcancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer dcancel()
	<-dctx.Done()
	got = FromContext(dctx.Err())
	if got == nil || got.Code != CodeContextDeadline {
		t.Fatalf("deadline: %+v", got)
	}
	if got.Type != "timeout" {
		t.Fatalf("type %s", got.Type)
	}
	if !DefaultRetryable(CodeContextDeadline) {
		t.Fatal("deadline retryable")
	}

	if FromContext(nil) != nil {
		t.Fatal("nil")
	}
	if FromContext(errors.New("other")) != nil {
		t.Fatal("other")
	}
}

func TestErrorMapWireShape(t *testing.T) {
	err := New(CodeTimeout, "httpx")
	m := err.Map()
	if m["error.code"] != "000005" {
		t.Fatalf("%v", m["error.code"])
	}
	if m["error.retryable"] != true {
		t.Fatal("retryable")
	}
	if m["error.type"] != "timeout" {
		t.Fatalf("type %v", m["error.type"])
	}
}
