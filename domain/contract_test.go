// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"strings"
	"testing"
)

func cloneDoc(doc map[string]any) map[string]any {
	return cloneJSON(doc).(map[string]any)
}

func mergeScopeBriefLocal(chatReq map[string]any, brief string) map[string]any {
	if brief == "" {
		return chatReq
	}
	out := cloneDoc(chatReq)
	if messages, ok := out["messages"].([]any); ok && len(messages) > 0 {
		if msg, ok := messages[0].(map[string]any); ok {
			current := asString(msg["content"])
			if !strings.Contains(current, "## SCOPE BRIEF") && !strings.Contains(current, "## MINI-SCHEMA") {
				msg["content"] = strings.TrimRight(current, trailingWS) + "\n\n" + strings.TrimSpace(brief)
			}
		}
	}
	if instr, ok := out["instructions"].(map[string]any); ok {
		sp := asString(instr["system_prompt"])
		if sp != "" && !strings.Contains(sp, "## SCOPE BRIEF") && !strings.Contains(sp, "## MINI-SCHEMA") {
			instr["system_prompt"] = strings.TrimRight(sp, trailingWS) + "\n\n" + strings.TrimSpace(brief)
			out["instructions"] = instr
		}
	}
	return out
}

func TestStripScopeBrief(t *testing.T) {
	content := "rules" + ScopeBriefMarker + "\n## MINI-SCHEMA\nstuff"
	if got := stripScopeBrief(content); got != "rules" {
		t.Fatalf("%q", got)
	}
	if stripScopeBrief(123) != "" {
		t.Fatal("non-string")
	}
	if stripScopeBrief("no marker") != "no marker" {
		t.Fatal("passthrough")
	}
}

func TestStripForHashExcludesMetadata(t *testing.T) {
	obj := map[string]any{
		"_hidden":  1,
		"guidance": map[string]any{"x": 1},
		"contract": map[string]any{"hash": "md5:abc"},
		"metadata": map[string]any{"at": 1},
		"model":    "gpt",
		"messages": []any{
			map[string]any{"role": "system", "content": "x" + ScopeBriefMarker + " brief"},
		},
	}
	out := stripForHash(obj).(map[string]any)
	for _, k := range []string{"_hidden", "guidance", "contract", "model"} {
		if _, ok := out[k]; ok {
			t.Fatalf("excluded %s still present", k)
		}
	}
	msgs := out["messages"].([]any)
	if msgs[0].(map[string]any)["content"] != "x" {
		t.Fatalf("content %v", msgs[0])
	}
}

func TestCanonicalizeSortsKeys(t *testing.T) {
	got := canonicalize(map[string]any{"b": 1, "a": 2}).(map[string]any)
	if got["a"] != 2 || got["b"] != 1 {
		t.Fatalf("%v", got)
	}
}

func TestComputeContractHashNonDict(t *testing.T) {
	if ComputeContractHash(nil) != "" {
		t.Fatal("nil")
	}
}

func TestComputeContractHashHTMLEscape(t *testing.T) {
	doc := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "a & b < c > d"},
		},
	}
	h := ComputeContractHash(doc)
	if h != "md5:9912726d6d59c94abf2398a83f1862f9" {
		t.Fatalf("html golden %s", h)
	}
	if ComputeContractHash(doc) != h {
		t.Fatal("unstable")
	}
}

func TestComputeContractHashExcludesStampAndIsStable(t *testing.T) {
	doc := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "rules only"},
		},
		"verbs": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "find"}},
		},
		"contract": map[string]any{"hash": "md5:should-be-ignored", "builder": "test"},
		"guidance": map[string]any{"optimal_paths": []any{"x"}},
		"metadata": map[string]any{"at": 1},
	}
	h := ComputeContractHash(doc)
	if h != "md5:5dbd28ea234025caed5da1bc29456415" {
		t.Fatalf("stable golden %s", h)
	}
	if len(h) != len("md5:")+32 {
		t.Fatalf("len %d", len(h))
	}
	doc2 := cloneDoc(doc)
	doc2["contract"] = map[string]any{"hash": "md5:other"}
	doc2["guidance"] = map[string]any{"y": 2}
	doc2["metadata"] = map[string]any{"at": 99}
	if ComputeContractHash(doc2) != h {
		t.Fatal("stamp/guidance must not affect digest")
	}
	doc3 := cloneDoc(doc)
	doc3["messages"].([]any)[0].(map[string]any)["content"] = "rules changed"
	if ComputeContractHash(doc3) == h {
		t.Fatal("rules change must change digest")
	}
}

func TestResolveSessionContractHashPrefersPayloadOnDrift(t *testing.T) {
	choice := ResolveSessionContractHash("md5:bound", "md5:stamped", "md5:payload")
	if choice.Hash != "md5:payload" || choice.Source != "payload_hash" {
		t.Fatalf("%+v", choice)
	}
}

func TestResolveSessionContractHashStampedMatch(t *testing.T) {
	choice := ResolveSessionContractHash("md5:bound", "md5:same", "md5:same")
	if choice.Hash != "md5:same" || choice.Source != "stamped_matches_payload" {
		t.Fatalf("%+v", choice)
	}
}

func TestResolveSessionContractHashConfigMatch(t *testing.T) {
	choice := ResolveSessionContractHash("md5:same", "md5:stamped", "md5:same")
	if choice.Hash != "md5:same" || choice.Source != "config_matches_payload" {
		t.Fatalf("%+v", choice)
	}
}

func TestResolveSessionContractHashFallbacks(t *testing.T) {
	if ResolveSessionContractHash("", "md5:stamped", "").Source != "stamped_fallback" {
		t.Fatal("stamped_fallback")
	}
	if ResolveSessionContractHash("md5:bound", "", "").Source != "config_fallback" {
		t.Fatal("config_fallback")
	}
	if ResolveSessionContractHash("", "", "").Hash != "" {
		t.Fatal("empty")
	}
}

func TestTrailingNewlinePlusScopeBriefMergeStable(t *testing.T) {
	base := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "rules only"},
		},
		"instructions": map[string]any{"system_prompt": "marker only"},
		"verbs": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "find"}},
		},
		"contract": map[string]any{"hash": "md5:placeholder"},
	}
	h0 := ComputeContractHash(base)
	if h0 != "md5:8ec03e0ffe50f84562720f92162de618" {
		t.Fatalf("base golden %s", h0)
	}
	drifted := cloneDoc(base)
	drifted["messages"].([]any)[0].(map[string]any)["content"] = "rules only\n"
	if ComputeContractHash(drifted) == h0 {
		t.Fatal("trailing newline must change digest")
	}
	merged := mergeScopeBriefLocal(base, "## SCOPE BRIEF\nscope: demo/_default\nmode: analytics")
	if ComputeContractHash(merged) != h0 {
		t.Fatal("brief merge must be hash-stable")
	}
	if !strings.Contains(merged["messages"].([]any)[0].(map[string]any)["content"].(string), "## SCOPE BRIEF") {
		t.Fatal("brief not injected")
	}
}

func TestHealTrailingWSStampDriftRewritesStampAndStabilizesMerge(t *testing.T) {
	clean := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "rules only"},
		},
		"instructions": map[string]any{"system_prompt": "marker only"},
		"verbs": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "find"}},
		},
	}
	hClean := ComputeContractHash(clean)
	dirty := cloneDoc(clean)
	dirty["messages"].([]any)[0].(map[string]any)["content"] = "rules only\n"
	hDirty := ComputeContractHash(dirty)
	if hDirty == hClean {
		t.Fatal("dirty")
	}
	dirty["contract"] = map[string]any{"hash": hDirty, "builder": "test@verify"}
	dirty["_hash"] = hDirty

	mergedDirty := mergeScopeBriefLocal(cloneDoc(dirty), "## SCOPE BRIEF\nscope: demo/_default")
	if ComputeContractHash(mergedDirty) != hClean {
		t.Fatal("merged dirty compute")
	}
	if ExtractStampedHash(mergedDirty) != hDirty {
		t.Fatal("stamp still dirty")
	}
	if ExtractStampedHash(mergedDirty) == ComputeContractHash(mergedDirty) {
		t.Fatal("expected drift")
	}

	healed := HealTrailingWSStampDrift(dirty)
	if ExtractStampedHash(healed) != hClean {
		t.Fatalf("healed stamp %s", ExtractStampedHash(healed))
	}
	if ComputeContractHash(healed) != hClean {
		t.Fatal("healed compute")
	}
	if healed["messages"].([]any)[0].(map[string]any)["content"] != "rules only" {
		t.Fatal("rstrip")
	}
	if healed["_hash"] != hClean {
		t.Fatal("_hash")
	}
	merged := mergeScopeBriefLocal(healed, "## SCOPE BRIEF\nscope: demo/_default")
	if ExtractStampedHash(merged) != ComputeContractHash(merged) || ComputeContractHash(merged) != hClean {
		t.Fatal("post-heal merge")
	}
}

func TestHealTrailingWSStampDriftSkipsUnrelatedStampMismatch(t *testing.T) {
	doc := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "rules only\n"},
		},
		"contract": map[string]any{"hash": "md5:not-the-real-compute"},
	}
	out := HealTrailingWSStampDrift(doc)
	if ExtractStampedHash(out) != "md5:not-the-real-compute" {
		t.Fatal("forged")
	}
	if out["messages"].([]any)[0].(map[string]any)["content"] != "rules only\n" {
		t.Fatal("must not rstrip when stamp mismatches compute")
	}
}

func TestExtractStampedHashPriority(t *testing.T) {
	doc := map[string]any{
		"contract":             map[string]any{"hash": "md5:TO_BE_FILLED"},
		"_hash":                "md5:realhash",
		"stamped_chat_request": map[string]any{"contract": map[string]any{"hash": "md5:fromwrapper"}},
	}
	if ExtractStampedHash(doc) != "md5:realhash" {
		t.Fatalf("%s", ExtractStampedHash(doc))
	}
	if ExtractStampedHash(map[string]any{"contract": map[string]any{"hash": "md5:direct"}}) != "md5:direct" {
		t.Fatal("direct")
	}
	if ExtractStampedHash(nil) != "" || ExtractStampedHash(map[string]any{}) != "" {
		t.Fatal("empty")
	}
}

func TestStripForHashPassthroughScalar(t *testing.T) {
	if stripForHash(42) != 42 {
		t.Fatal("scalar")
	}
}

func TestExtractStampedHashSkipsNonDictContractBlock(t *testing.T) {
	doc := map[string]any{
		"contract": "not-a-dict",
		"_hash":    "md5:top-level",
	}
	if ExtractStampedHash(doc) != "md5:top-level" {
		t.Fatal("top-level")
	}
}

func TestExtractStampedHashSecondLoopSkipsNonDictCandidate(t *testing.T) {
	doc := map[string]any{
		"contract":             map[string]any{"hash": "md5:TO_BE_FILLED"},
		"stamped_chat_request": "bad",
		"stamped":              map[string]any{"_hash": "md5:from-stamped-key"},
	}
	if ExtractStampedHash(doc) != "md5:from-stamped-key" {
		t.Fatalf("%s", ExtractStampedHash(doc))
	}
}

func TestExtractStampedHashSecondPassSkipsPlaceholderHash(t *testing.T) {
	doc := map[string]any{
		"stamped_chat_request": map[string]any{"_hash": "md5:TO_BE_FILLED"},
		"stamped":              map[string]any{"hash": "md5:real-from-stamped"},
	}
	if ExtractStampedHash(doc) != "md5:real-from-stamped" {
		t.Fatalf("%s", ExtractStampedHash(doc))
	}
}

func TestExtractStampedHashSecondPassHashFromWrapper(t *testing.T) {
	doc := map[string]any{
		"contract":             map[string]any{"hash": "md5:TO_BE_FILLED"},
		"stamped_chat_request": map[string]any{"_hash": "md5:wrapper-only"},
	}
	if ExtractStampedHash(doc) != "md5:wrapper-only" {
		t.Fatalf("%s", ExtractStampedHash(doc))
	}
}

func TestExtractStampedHashSkipsNonDictWrappers(t *testing.T) {
	doc := map[string]any{
		"_hash":                "md5:top",
		"stamped_chat_request": "not-a-dict",
		"stamped":              []any{"also-not"},
	}
	if ExtractStampedHash(doc) != "md5:top" {
		t.Fatal("top")
	}
}

func TestContractServiceFacade(t *testing.T) {
	var svc ContractService
	doc := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "hi"},
		},
	}
	h := svc.ComputeHash(doc)
	if h != ComputeContractHash(doc) || h != "md5:1a0f3f40fcb324a0371f6a22ca104087" {
		t.Fatalf("%s", h)
	}
	stampedDoc := map[string]any{"contract": map[string]any{"hash": h}}
	if svc.ExtractStampedHash(stampedDoc) != h {
		t.Fatal("extract")
	}
	choice := svc.ResolveSessionHash(h, h, "md5:other")
	if choice.Hash != h || choice.Source != "stamped_matches_payload" {
		t.Fatalf("%+v", choice)
	}
	_ = svc.HealTrailingWSDrift(stampedDoc)
}

func TestInventProductionHashForbidden(t *testing.T) {
	doc := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "hi"},
		},
	}
	local := ComputeContractHash(doc)
	if local == "" {
		t.Fatal("compute")
	}
	if ExtractStampedHash(doc) != "" {
		t.Fatal("unstamped")
	}
	_, err := InventProductionHash(doc)
	de, ok := AsError(err)
	if !ok || de.Code != CodeContractHashInventForbidden {
		t.Fatalf("%v", err)
	}
	_, err = PreferStampedForProduction("", local)
	de, ok = AsError(err)
	if !ok || de.Code != CodeContractHashInventForbidden {
		t.Fatalf("invent path %v", err)
	}
	got, err := PreferStampedForProduction("md5:hub-stamp", local)
	if err != nil || got != "md5:hub-stamp" {
		t.Fatalf("stamp wins %s %v", got, err)
	}
	_, err = PreferStampedForProduction("", "")
	de, ok = AsError(err)
	if !ok || de.Code != CodeContractHashMissing {
		t.Fatalf("missing %v", err)
	}
	_, err = StampedHash(doc)
	de, ok = AsError(err)
	if !ok || de.Code != CodeContractHashMissing {
		t.Fatalf("stamped hash %v", err)
	}
	got, err = StampedHash(map[string]any{"contract": map[string]any{"hash": "md5:hub"}})
	if err != nil || got != "md5:hub" {
		t.Fatalf("%s %v", got, err)
	}
}

func TestExtractContractID(t *testing.T) {
	if ExtractContractID(map[string]any{"contract": map[string]any{"id": " mock_analytics_b5 "}}) != "mock_analytics_b5" {
		t.Fatal("id")
	}
	if ExtractContractID(nil) != "" {
		t.Fatal("nil")
	}
}

func TestEmptyCatalogHashesAsEmptyObject(t *testing.T) {
	if ComputeContractHash(map[string]any{}) != "md5:99914b932bd37a50b983c5e7c90ae93b" {
		t.Fatalf("%s", ComputeContractHash(map[string]any{}))
	}
}
