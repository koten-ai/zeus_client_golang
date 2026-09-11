// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const contractComponent = "domain.contract"

// Hash-excluded inject markers (Python SCOPE_BRIEF_MARKER / MINI_SCHEMA_MARKER).
const (
	ScopeBriefMarker = "\n\n## SCOPE BRIEF"
	MiniSchemaMarker = "\n\n## MINI-SCHEMA"
)

const trailingWS = " \t\n\r"

// HashExcludedRoots are catalog keys that never participate in the digest
// (Python HASH_EXCLUDED_ROOTS). "_*" is any underscore-prefixed key.
var HashExcludedRoots = []string{"guidance", "contract", "metadata", "_*"}

// HashExcludedStringMarkers are stripped from message/system text before hash.
var HashExcludedStringMarkers = []string{
	strings.TrimSpace(ScopeBriefMarker),
	strings.TrimSpace(MiniSchemaMarker),
}

// LockedPointers participate in the contract hash (informational; server wins).
var LockedPointers = []string{
	"/instructions/system_prompt",
	"/instructions/verb_usage_guide",
	"/instructions/verb_order",
	"/instructions/response_expectations",
	"/masq",
	"/verbs",
	"/messages/*/content",
}

var runtimeKnobKeys = map[string]struct{}{
	"targets":     {},
	"model":       {},
	"tool_choice": {},
	"temperature": {},
	"top_p":       {},
	"max_tokens":  {},
}

// SessionHashChoice is the /v2/session contract_hash pick (Python SessionHashChoice).
type SessionHashChoice struct {
	Hash   string
	Source string
}

// ComputeContractHash is the local diagnostic digest (Python compute_contract_hash).
// Preferred production path is ExtractStampedHash. Local compute is for
// unstamped / offline / diagnostics and must match Zeus contract.Hash strip +
// canonicalize + encoding/json HTML escapes. Never a Hub production stamp.
func ComputeContractHash(chatRequest map[string]any) string {
	if chatRequest == nil {
		return ""
	}
	cleaned := stripForHash(chatRequest)
	canon := canonicalize(cleaned)
	blob, err := json.Marshal(canon)
	if err != nil {
		return ""
	}
	sum := md5.Sum(blob)
	return "md5:" + hex.EncodeToString(sum[:])
}

// ExtractStampedHash reads the Hub stamp (Python extract_stamped_hash).
// Priority: contract.hash → _hash / hash on the doc, then the same keys under
// stamped_chat_request / stamped wrappers. Placeholders like md5:TO_BE_FILLED
// are treated as absent. Does not compute a digest.
func ExtractStampedHash(doc map[string]any) string {
	if doc == nil {
		return ""
	}
	candidates := []any{
		doc,
		doc["stamped_chat_request"],
		doc["stamped"],
	}
	for _, raw := range candidates {
		candidate, ok := raw.(map[string]any)
		if !ok || candidate == nil {
			continue
		}
		if cb, ok := candidate["contract"].(map[string]any); ok && cb != nil {
			if h := realHash(asString(cb["hash"])); h != "" {
				return h
			}
		}
		for _, k := range []string{"_hash", "hash"} {
			if h := realHash(asString(candidate[k])); h != "" {
				return h
			}
		}
	}
	return ""
}

// ExtractContractID reads contract.id (empty when absent).
func ExtractContractID(doc map[string]any) string {
	if doc == nil {
		return ""
	}
	cb, _ := doc["contract"].(map[string]any)
	if cb == nil {
		return ""
	}
	return strings.TrimSpace(asString(cb["id"]))
}

// ResolveSessionContractHash picks contract_hash for /v2/session APIs.
// Zeus compares contract_hash to the hash of the chat_request body. Config
// bindings and embedded stamps are advisory; on drift, send the payload hash
// or session create returns 409.
func ResolveSessionContractHash(boundHash, stampedHash, payloadHash string) SessionHashChoice {
	if payloadHash != "" {
		if stampedHash != "" && stampedHash == payloadHash {
			return SessionHashChoice{Hash: payloadHash, Source: "stamped_matches_payload"}
		}
		if boundHash != "" && boundHash == payloadHash {
			return SessionHashChoice{Hash: payloadHash, Source: "config_matches_payload"}
		}
		return SessionHashChoice{Hash: payloadHash, Source: "payload_hash"}
	}
	if stampedHash != "" {
		return SessionHashChoice{Hash: stampedHash, Source: "stamped_fallback"}
	}
	return SessionHashChoice{Hash: boundHash, Source: "config_fallback"}
}

// HealTrailingWSStampDrift rewrites a stamp that only disagrees because of
// trailing system-prompt whitespace. Does not rewrite stamps that already
// disagree for other reasons (never forges production stamps).
func HealTrailingWSStampDrift(doc map[string]any) map[string]any {
	if doc == nil {
		return doc
	}
	stamped := ExtractStampedHash(doc)
	if stamped == "" {
		return doc
	}
	hRaw := ComputeContractHash(doc)
	if stamped != hRaw {
		return doc
	}
	normalized, changed := rstripSystemPromptSurfaces(doc)
	if !changed {
		return doc
	}
	hNorm := ComputeContractHash(normalized)
	if hNorm == "" || hNorm == hRaw {
		return doc
	}
	rewriteEmbeddedHash(normalized, stamped, hNorm)
	return normalized
}

// InventProductionHash is the forbidden production mint (API_CATALOG 030005).
// ComputeContractHash is diagnostics only and must never be stored as a Hub stamp.
func InventProductionHash(doc map[string]any) (string, error) {
	details := map[string]any{"hash_source": "computed"}
	if doc != nil {
		details["has_stamp"] = ExtractStampedHash(doc) != ""
	}
	return "", NewContract(CodeContractHashInventForbidden, contractComponent,
		WithMessage("contract hash invent forbidden"),
		WithDetails(details),
	)
}

// PreferStampedForProduction returns the Hub stamp. A non-empty local compute
// is never substituted — that is invent (030005). Empty stamp and empty
// compute → 030004 missing.
func PreferStampedForProduction(stamped, localCompute string) (string, error) {
	if s := strings.TrimSpace(stamped); s != "" {
		return s, nil
	}
	if strings.TrimSpace(localCompute) != "" {
		return InventProductionHash(nil)
	}
	return "", NewCatalog(CodeContractHashMissing, contractComponent,
		WithMessage("contract hash missing on stamped catalog"))
}

// StampedHash is catalog.contract.hash at the domain layer: extract only.
// Missing → 030004. Never falls back to ComputeContractHash.
func StampedHash(doc map[string]any) (string, error) {
	h := ExtractStampedHash(doc)
	if h == "" {
		return "", NewCatalog(CodeContractHashMissing, contractComponent,
			WithMessage("contract hash missing on stamped catalog"))
	}
	return h, nil
}

// ContractService is the Python ContractService façade.
type ContractService struct{}

func (ContractService) ComputeHash(chatRequest map[string]any) string {
	return ComputeContractHash(chatRequest)
}

func (ContractService) ExtractStampedHash(doc map[string]any) string {
	return ExtractStampedHash(doc)
}

func (ContractService) ResolveSessionHash(stamped, content, bound string) SessionHashChoice {
	return ResolveSessionContractHash(bound, stamped, content)
}

func (ContractService) HealTrailingWSDrift(doc map[string]any) map[string]any {
	return HealTrailingWSStampDrift(doc)
}

func stripScopeBrief(content any) string {
	s, ok := content.(string)
	if !ok {
		return ""
	}
	for _, marker := range []string{ScopeBriefMarker, MiniSchemaMarker} {
		if i := strings.Index(s, marker); i >= 0 {
			return strings.TrimRight(s[:i], trailingWS)
		}
	}
	return s
}

func stripForHash(obj any) any {
	switch x := obj.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			if strings.HasPrefix(k, "_") {
				continue
			}
			if _, skip := runtimeKnobKeys[k]; skip {
				continue
			}
			if k == "guidance" || k == "contract" || k == "metadata" {
				continue
			}
			out[k] = stripForHash(v)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = stripForHash(v)
		}
		return out
	case []map[string]any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = stripForHash(v)
		}
		return out
	case string:
		return stripScopeBrief(x)
	default:
		return obj
	}
}

func canonicalize(obj any) any {
	switch x := obj.(type) {
	case map[string]any:
		cp := x
		if asString(x["role"]) == "system" {
			if _, ok := x["content"].(string); ok {
				cp = make(map[string]any, len(x))
				for k, v := range x {
					cp[k] = v
				}
				cp["content"] = stripScopeBrief(x["content"])
			}
		}
		out := make(map[string]any, len(cp))
		for k, v := range cp {
			out[k] = canonicalize(v)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = canonicalize(v)
		}
		return out
	default:
		return obj
	}
}

func rstripSystemPromptSurfaces(doc map[string]any) (map[string]any, bool) {
	out, ok := cloneJSON(doc).(map[string]any)
	if !ok {
		return doc, false
	}
	changed := false
	brief := strings.TrimSpace(ScopeBriefMarker)
	mini := strings.TrimSpace(MiniSchemaMarker)

	if messages, ok := out["messages"].([]any); ok && len(messages) > 0 {
		if msg, ok := messages[0].(map[string]any); ok {
			if content, ok := msg["content"].(string); ok && content != "" {
				if !strings.Contains(content, brief) && !strings.Contains(content, mini) {
					stripped := strings.TrimRight(content, trailingWS)
					if stripped != content {
						msg["content"] = stripped
						changed = true
					}
				}
			}
		}
	}
	if instr, ok := out["instructions"].(map[string]any); ok {
		if sp, ok := instr["system_prompt"].(string); ok && sp != "" {
			if !strings.Contains(sp, brief) && !strings.Contains(sp, mini) {
				stripped := strings.TrimRight(sp, trailingWS)
				if stripped != sp {
					instr["system_prompt"] = stripped
					out["instructions"] = instr
					changed = true
				}
			}
		}
	}
	return out, changed
}

func rewriteEmbeddedHash(doc map[string]any, oldHash, newHash string) {
	if doc == nil || oldHash == "" || newHash == "" || oldHash == newHash {
		return
	}
	if cb, ok := doc["contract"].(map[string]any); ok && cb != nil {
		if strings.TrimSpace(asString(cb["hash"])) == oldHash {
			cb["hash"] = newHash
		}
	}
	if strings.TrimSpace(asString(doc["_hash"])) == oldHash {
		doc["_hash"] = newHash
	}
	if strings.TrimSpace(asString(doc["hash"])) == oldHash {
		doc["hash"] = newHash
	}
}

func realHash(h string) string {
	hs := strings.TrimSpace(h)
	if hs == "" {
		return ""
	}
	if strings.Contains(hs, "TO_BE_FILLED") {
		return ""
	}
	return hs
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func cloneJSON(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = cloneJSON(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = cloneJSON(val)
		}
		return out
	case []map[string]any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = cloneJSON(val)
		}
		return out
	default:
		return v
	}
}
