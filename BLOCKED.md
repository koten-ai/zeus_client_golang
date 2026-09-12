# BLOCKED

Residuals that block a higher claim than **candidate**. Offline CHECKLIST E
(conformance `conformance-0.2-dev` 16/16 locally) does **not** lift this.
Version stays **0.1.0**. Claim stays **candidate**. `multi_agent` stays **demo**
(not `supported`).

## Clones / fixtures

- Sibling `zeus_client_design` is required for `make conformance` (GHA skips the
  live suite when the design repo is absent).
- Pattern A (`adapters/jobsma`) needs sibling `koten_multi_agent_golang`
  (`go.mod` `replace`). GHA skips Pattern A until `KOTEN_CI_PAT` exists.

## Suite / kit

- Full ZF-WISH-003 Detective **agent rewind** (drive `RunAgentTurn` from tape
  LLM + Zeus hops) is not claimed. Kit-β: companions + slim `assert_only`.
- chat_request **COMPAT** triple for Go 0.1.0 × Zeus × BASE is not authored here.
  Do not invent production `contract_hash` / Hub stamps.

## Live smoke

`pins.zeus.live_smoke` is **false**. Live Zeus + LLM are not the package default.

## Later CHECKLIST sections

- **D** OTLP Logs exporter — family slog + REDACT only; config keys exist.
  Hub Debug Chat **imports** this module (ZCG-32 / U5); that is not a product
  `supported` claim.
- **E2** semantic cache — config knobs only (`session.semantic_cache`, default
  off). No recall/write runtime. Pins: **no** (do not copy Python `flag`).
- **F** multi-agent — **demo** (Pattern A `examples/patterna` recorded
  `Jobs().Run` + SSE WatchJob). Not `supported`. FakeJobRuntime is not the
  engine. Pattern B `run`/`get`/`cancel` stay `130001`.
- Catalog **live sync** / `catalog_remote` / agent_memory adapters — residual
  (offline mock load is the candidate path).
- Plugins facade — **no**.
- Design-repo [MATRIX.md](https://github.com/koten-ai/zeus_client_design/blob/main/MATRIX.md)
  Go row on `main` is stale (Hub-only). Honesty refresh is human-gated
  (do not self-merge).

## Notes

- Do not self-award MATRIX `supported` from this package or CI green alone.
- Do not invent production `contract_hash` or Hub stamps in fixtures labeled prod.
- Current claim table: [README.md](README.md) · pins: [sdk_bootstrap.pins.json](sdk_bootstrap.pins.json).
