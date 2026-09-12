# Offline conformance adapter (ZCG-23 / CHECKLIST E)

**Suite pin:** `sdk_bootstrap.pins.json` → `suite.suite_version` + `design_repo_ref`  
**Contract:** `zeus_client_design/conformance/adapter/ADAPTER_CONTRACT.md`  
**Claim:** `candidate` until a human MATRIX row (never self-award `supported`)

## Run

```bash
# Design reference (pure-law) — optional baseline
python3 ../zeus_client_design/conformance/adapter/reference/run_suite.py

# Go language adapter (hard-fails if sibling design repo is missing)
make conformance
go run ./cmd/conformance --report /tmp/go-conformance.json

# go test hook: skips when ../zeus_client_design/conformance/manifest.json is absent
# so GitHub Actions (this repo only) stays green.
go test ./conformance -count=1
```

Report: `conformance/last_report.json`. Sibling `zeus_client_design` is **required locally**. GitHub Actions **skips** the live suite until a PAT can clone the private design repo. Do not fabricate passed cases.

## Coverage

| Level | Driver |
| --- | --- |
| L0 catalog / envelope | Design fixtures via domain catalog extract / envelope shape |
| L1 single tool return | Walk of design `llm_script` (same algorithm as the kit reference). Product cheap-final after `search` would skip the scripted `return` hop, so this case does **not** claim `RunAgentTurn`. The real loop is locked in `application/agent_turn_test.go`. |
| L1 force return | Real `application.RunAgentTurn` + `ForceReturnRoundsLeft` |
| L2 policy / layer A / G2 / triggers / rules | Domain (`ParseLayerA`, `DecidePolicy`, `MergeRulesFrozen`, `NormalizeTriggers`, `UIView`) |
| L2 settings product default | Fixture production profile **and** `config.Default().Settings.AIProcessResult==false`; profile `hub` is True |
| DT smooth_* | slim export parse + rewind companions (`llm_script` + `zeus_responses`); `layer_a.required_four` from companion `return` args via `ParseLayerA` |
| DT fail_zeus | `RunAgentTurn` + 409 hop; `error_class` from `tool_trail` / `ErrorClassFor` — never forge `contract_hash` |
| DT fail_client missing mini | `ClassifyMiniSchema(GetMiniSchema(...))` → `missing_mini_schema` |
| DT fail_llm | `ParseLayerA` + `DecidePolicy` on invalid terminate |
| DT fail_control_plane | `ParseLayerA` + empty/false triggers; data path still ok |

Handlers must not copy `case.expect` into observe.

## Blocked / residual

- Full ZF-WISH-003 agent re-run of Detective tapes is **not** claimed. Companions + slim `assert_only` satisfy kit-β `required_rewind` offline.
- Missing design repo: **hard fail** for `make conformance` (CHECKLIST E). `go test ./conformance` **skips** the live suite so GHA `make ci` stays green.
- Claim stays **candidate**. This ticket is the suite adapter, not MATRIX `supported`.
