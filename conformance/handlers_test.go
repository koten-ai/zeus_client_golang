// SPDX-License-Identifier: BUSL-1.1

package conformance

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHandlersDoNotEchoExpect(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	src, err := os.ReadFile(filepath.Join(filepath.Dir(file), "handlers.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	needles := []string{
		"dict(expect)",
		`data.get("observe")`,
		`expect.get("error_class"`,
		`expect.get('error_class'`,
	}
	for _, n := range needles {
		if strings.Contains(text, n) {
			t.Errorf("handlers must not echo expect: found %q", n)
		}
	}
}

func TestHandlersCoverRequiredIDs(t *testing.T) {
	ids := []string{
		"L0.catalog.load_mock.001",
		"L0.result.envelope.001",
		"L1.loop.single_tool_return.001",
		"L1.loop.force_return_max_rounds.001",
		"L2.policy.matrix.001",
		"L2.layer_a.required_four.001",
		"L2.layer_a.g2_not_in_ui.001",
		"L2.rules.merge_freeze.001",
		"L2.triggers.object_normalize.001",
		"L2.settings.ai_process_result.001",
		"DT.smooth_short.beer_fruit_pipeline_base61.001",
		"DT.smooth_long.beer_fruit_multi_round_no_pipeline_base61.001",
		"DT.fail_client.missing_mini_schema.001",
		"DT.fail_zeus.contract_409.001",
		"DT.fail_llm.bad_layer_a.001",
		"DT.fail_control_plane.trigger_missing_data_ok.001",
	}
	for _, id := range ids {
		if HANDLERS[id] == nil {
			t.Errorf("missing handler %s", id)
		}
	}
}
