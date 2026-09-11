// SPDX-License-Identifier: BUSL-1.1

package config

import "testing"

func TestSnapshotMapIsolation(t *testing.T) {
	cfg := Default()
	cfg.Settings.StickyFlags["keep"] = true
	snap := cfg.Snapshot()
	snap.Settings.StickyFlags["injected"] = true
	if _, ok := cfg.Settings.StickyFlags["injected"]; ok {
		t.Fatal("snapshot mutated runtime StickyFlags")
	}
	if !cfg.Settings.StickyFlags["keep"] {
		t.Fatal("lost keep")
	}
	snap.LLM.Roles = map[string]LlmRoleConfig{"worker": {Model: "x"}}
	if len(cfg.LLM.Roles) != 0 {
		t.Fatal("roles alias")
	}
}
