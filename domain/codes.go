// SPDX-License-Identifier: BUSL-1.1

package domain

// Code is a family client error code: exactly six digits, never dotted.
// Values are copied from zeus_client_python ErrorCode (ZCP-4 oracle).
type Code string

func (c Code) String() string { return string(c) }

const (
	// 00 generic (API_ERROR_CODES §3). CANCELLED + CONTEXT_DEADLINE are
	// the Go ctx mapping (Python asyncio.Event / timeout).
	CodeUnknown            Code = "000001"
	CodeNotImplemented     Code = "000002"
	CodeInvalidArgument    Code = "000003"
	CodePreconditionFailed Code = "000004"
	CodeTimeout            Code = "000005"
	CodeCancelled          Code = "000006"
	CodeInternalInvariant  Code = "000007"
	CodeFeatureDisabled    Code = "000008"
	CodeClientRateLimited  Code = "000009"
	CodeContextDeadline    Code = "000010"

	// 01 config
	CodeConfigPathNotFound    Code = "010001"
	CodeConfigNotReadable     Code = "010002"
	CodeConfigParseFailed     Code = "010003"
	CodeConfigNormalizeFailed Code = "010004"
	CodeConfigInvalid         Code = "010006"
	CodeZeusURLMissing        Code = "010007"
	CodeZeusAuthModeInvalid   Code = "010008"
	CodeZeusAuthIncomplete    Code = "010009"
	CodeLLMBaseURLMissing     Code = "010010"
	CodeLLMModelMissing       Code = "010011"
	CodeLLMAPIKeyMissing      Code = "010012"

	// 03 catalog / contract stamps
	CodeCatalogNotFound             Code = "030001"
	CodeCatalogParseFailed          Code = "030002"
	CodeCatalogLineageUnknown       Code = "030003"
	CodeContractHashMissing         Code = "030004"
	CodeContractHashInventForbidden Code = "030005"
	CodeContractBindMismatch        Code = "030006"
	CodeCatalogSyncFailed           Code = "030007"

	// 04 session + agent-memory (Python band 040008–040010)
	CodeSessionCreateFailed     Code = "040001"
	CodeSessionContinueFailed   Code = "040002"
	CodeSessionRehydrateFailed  Code = "040003"
	CodeSessionIDMissing        Code = "040004"
	CodeSessionDurableDisabled  Code = "040005"
	CodeSessionCommitFailed     Code = "040006"
	CodeAgentMemoryRecallFailed Code = "040008"
	CodeAgentMemoryWriteFailed  Code = "040009"
	CodeAgentMemoryUnavailable  Code = "040010"

	// 05 agent / LLM
	CodeAgentTurnFailed        Code = "050001"
	CodeAgentMessageEmpty      Code = "050002"
	CodeAgentMaxRounds         Code = "050003"
	CodeAgentLLMRequestFailed  Code = "050004"
	CodeAgentLLMTimeout        Code = "050005"
	CodeAgentToolPlanInvalid   Code = "050006"
	CodeAgentTerminateMissing  Code = "050007"
	CodeAgentHooksAborted      Code = "050008"
	CodeLLMRateLimit           Code = "050010"
	CodeLLMQuotaExhausted      Code = "050011"
	CodeLLMAuth                Code = "050012"
	CodeLLMContextLength       Code = "050013"
	CodeLLMContentFilter       Code = "050014"
	CodeLLMProviderOverload    Code = "050015"
	CodeLLMProviderUnavailable Code = "050016"
	CodeLLMInvalidRequest      Code = "050017"
	CodeLLMToolSchema          Code = "050018"

	// 06 Zeus direct
	CodeZeusTransport           Code = "060001"
	CodeZeusHTTP4xx             Code = "060002"
	CodeZeusHTTP5xx             Code = "060003"
	CodeZeusContractRequired    Code = "060004"
	CodeZeusAuthFailed          Code = "060005"
	CodeZeusVerbNotAllowed      Code = "060006"
	CodeZeusSearchInvalid       Code = "060007"
	CodeZeusResponseParse       Code = "060008"
	CodeZeusReqIDMissing        Code = "060009"
	CodeZeusPipelineNotOnDirect Code = "060010"

	// 07 policy. Python oracle ZCP-4 binds POLICY_REFUSE to 070002
	// (design catalogue 070002 is trigger-id-unknown; 070005 is
	// "policy forced refuse"). Error codes follow Python.
	CodePolicyRefuse Code = "070002"

	// 09 Layer A
	CodeLayerAValidateFailed Code = "090001"

	// 10 auth
	CodeAuthFailed             Code = "100001"
	CodeAuthSessionUnavailable Code = "100002"

	// 13 jobs / units — stub now so Pattern A (ZCG-2) does not fork codes.
	CodeJobsUnavailable     Code = "130001"
	CodeJobsInvalidUnitMap  Code = "130002"
	CodeJobsBudgetInvalid   Code = "130003"
	CodeJobsNotFound        Code = "130004"
	CodeJobsWatchFailed     Code = "130005"
	CodeJobsUnitFailed      Code = "130010"
	CodeUnitsCatalogMissing Code = "130011"
	CodeUnitsInjectMissing  Code = "130012"
	CodeUnitsIsolation      Code = "130013"

	// 99 internal
	CodeInternalBug Code = "990001"
)

type codeInfo struct {
	code      Code
	message   string
	retryable bool
	typ       string
}

// Seeded from Python _PUBLIC_MESSAGES / _RETRYABLE (ZCP-4).
var codeCatalog = []codeInfo{
	{CodeUnknown, "unknown client error", false, "internal"},
	{CodeNotImplemented, "not implemented in this language SDK", false, "not_implemented"},
	{CodeInvalidArgument, "invalid argument", false, "invalid_argument"},
	{CodePreconditionFailed, "precondition failed", false, "precondition"},
	{CodeTimeout, "timeout (client-side deadline)", true, "timeout"},
	{CodeCancelled, "cancelled", false, "cancelled"},
	{CodeInternalInvariant, "internal invariant violated", false, "internal"},
	{CodeFeatureDisabled, "feature disabled by config/flag", false, "disabled"},
	{CodeClientRateLimited, "rate limited (client)", true, "rate_limited"},
	{CodeContextDeadline, "context deadline exceeded", true, "timeout"},

	{CodeConfigPathNotFound, "config path not found", false, "config"},
	{CodeConfigNotReadable, "config path not readable", false, "config"},
	{CodeConfigParseFailed, "config file parse failed", false, "config"},
	{CodeConfigNormalizeFailed, "config normalize failed", false, "config"},
	{CodeConfigInvalid, "config mapping invalid", false, "config"},
	{CodeZeusURLMissing, "zeus.url missing or empty", false, "config"},
	{CodeZeusAuthModeInvalid, "zeus.auth_mode invalid", false, "config"},
	{CodeZeusAuthIncomplete, "zeus auth fields incomplete for auth_mode", false, "config"},
	{CodeLLMBaseURLMissing, "llm.base_url missing (agent required)", false, "config"},
	{CodeLLMModelMissing, "llm.model missing (agent required)", false, "config"},
	{CodeLLMAPIKeyMissing, "llm.api_key missing", false, "config"},

	{CodeCatalogNotFound, "catalog not found for mode/base_id", false, "catalog"},
	{CodeCatalogParseFailed, "catalog parse failed", false, "catalog"},
	{CodeCatalogLineageUnknown, "catalog lineage unknown", false, "catalog"},
	{CodeContractHashMissing, "contract hash missing on stamped catalog", false, "catalog"},
	{CodeContractHashInventForbidden, "contract hash invent forbidden", false, "catalog"},
	{CodeContractBindMismatch, "contract bind mismatch for scope", false, "catalog"},
	{CodeCatalogSyncFailed, "catalog sync failed", false, "catalog"},

	{CodeSessionCreateFailed, "session create failed", false, "session"},
	{CodeSessionContinueFailed, "session continue failed", false, "session"},
	{CodeSessionRehydrateFailed, "session rehydrate failed", false, "session"},
	{CodeSessionIDMissing, "session id missing", false, "session"},
	{CodeSessionDurableDisabled, "durable sessions disabled", false, "session"},
	{CodeSessionCommitFailed, "session commit/trace failed", false, "session"},
	{CodeAgentMemoryRecallFailed, "agent memory recall failed", true, "session"},
	{CodeAgentMemoryWriteFailed, "agent memory write failed", false, "session"},
	{CodeAgentMemoryUnavailable, "agent memory API unavailable", false, "session"},

	{CodeAgentTurnFailed, "agent turn failed", false, "agent"},
	{CodeAgentMessageEmpty, "agent message empty", false, "agent"},
	{CodeAgentMaxRounds, "agent max_rounds exceeded", false, "agent"},
	{CodeAgentLLMRequestFailed, "agent LLM request failed", false, "agent"},
	{CodeAgentLLMTimeout, "agent LLM timeout", true, "agent"},
	{CodeAgentToolPlanInvalid, "agent tool plan invalid", false, "agent"},
	{CodeAgentTerminateMissing, "agent terminate missing", false, "agent"},
	{CodeAgentHooksAborted, "agent hooks aborted turn", false, "agent"},
	{CodeLLMRateLimit, "llm rate limited (throttle)", true, "agent"},
	{CodeLLMQuotaExhausted, "llm quota exhausted (billing/spend)", false, "agent"},
	{CodeLLMAuth, "llm auth failed", false, "agent"},
	{CodeLLMContextLength, "llm context length exceeded", false, "agent"},
	{CodeLLMContentFilter, "llm content filter", false, "agent"},
	{CodeLLMProviderOverload, "llm provider overload", true, "agent"},
	{CodeLLMProviderUnavailable, "llm provider unavailable", true, "agent"},
	{CodeLLMInvalidRequest, "llm invalid request", false, "agent"},
	{CodeLLMToolSchema, "llm tool schema rejected", false, "agent"},

	{CodeZeusTransport, "zeus HTTP transport error", true, "zeus_http"},
	{CodeZeusHTTP4xx, "zeus HTTP 4xx", false, "zeus_http"},
	{CodeZeusHTTP5xx, "zeus HTTP 5xx", true, "zeus_http"},
	{CodeZeusContractRequired, "zeus contract_required (409 class)", false, "zeus_http"},
	{CodeZeusAuthFailed, "zeus auth failed", false, "zeus_http"},
	{CodeZeusVerbNotAllowed, "zeus verb not allow-listed", false, "zeus_http"},
	{CodeZeusSearchInvalid, "zeus search invalid args", false, "zeus_http"},
	{CodeZeusResponseParse, "zeus response parse failed", false, "zeus_http"},
	{CodeZeusReqIDMissing, "zeus req_id missing on response", false, "zeus_http"},
	{CodeZeusPipelineNotOnDirect, "zeus pipeline not on direct surface", false, "zeus_http"},

	{CodePolicyRefuse, "policy forced refuse", false, "policy"},
	{CodeLayerAValidateFailed, "layer A validation failed", false, "layer_a"},

	{CodeAuthFailed, "auth failed", false, "auth"},
	{CodeAuthSessionUnavailable, "auth session unavailable", true, "auth"},

	{CodeJobsUnavailable, "multi-agent capability unavailable", false, "jobs"},
	{CodeJobsInvalidUnitMap, "invalid unit map / missing scope", false, "jobs"},
	{CodeJobsBudgetInvalid, "job budget invalid", false, "jobs"},
	{CodeJobsNotFound, "job not found", false, "jobs"},
	{CodeJobsWatchFailed, "job watch transport failed", false, "jobs"},
	{CodeJobsUnitFailed, "job failed (unit error)", false, "jobs"},
	{CodeUnitsCatalogMissing, "agent unit missing catalog pin", false, "jobs"},
	{CodeUnitsInjectMissing, "agent unit missing required inject", false, "jobs"},
	{CodeUnitsIsolation, "unit isolation violation", false, "jobs"},

	{CodeInternalBug, "internal client bug", false, "internal"},
}

var byCode map[Code]codeInfo

func init() {
	byCode = make(map[Code]codeInfo, len(codeCatalog))
	for _, info := range codeCatalog {
		byCode[info.code] = info
	}
}

// AllCodes returns the seeded family catalogue (stable table-test order).
func AllCodes() []Code {
	out := make([]Code, len(codeCatalog))
	for i, info := range codeCatalog {
		out[i] = info.code
	}
	return out
}

// ParseCode accepts a known six-digit family code. Dotted overflow is rejected.
func ParseCode(s string) (Code, bool) {
	if len(s) != 6 {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return "", false
		}
	}
	c := Code(s)
	_, ok := byCode[c]
	if !ok {
		return "", false
	}
	return c, true
}

// DefaultRetryable is the family retry hint (apps may still apply budgets).
func DefaultRetryable(code Code) bool {
	info, ok := byCode[code]
	return ok && info.retryable
}

// PublicMessage is the stable short English message (no secrets).
func PublicMessage(code Code) string {
	info, ok := byCode[code]
	if !ok {
		return "unknown client error"
	}
	return info.message
}

func defaultType(code Code) string {
	info, ok := byCode[code]
	if !ok {
		return "internal"
	}
	return info.typ
}
