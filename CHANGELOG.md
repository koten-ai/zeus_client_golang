# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

- Zeus HTTP `RetryBudget` on idempotent read verbs (`find`/`get`/`search`/`describe`/`explain`/`traverse`) for transport + HTTP 5xx only (API_ZEUS §6.6). Never 4xx/409/429, never `set`/`pipeline`. Same budget as LLM adapters. Version stays **0.1.0**. Claim stays **candidate**. Not V1 job replan.

### Changed

- Do less on the Mode 1 product path (no version bump, claim stays **candidate**):
  - Detective is off in `Default()` / product profiles. Hub profile, `StampUser=admin`, or `debug.detective_briefing=true` still gather (nine E2E fields + briefing). `ZEUS_CLIENT_DETECTIVE=0` remains the kill-switch. Python still defaults briefing on — Hub insight is a profile here, not the SDK default.
  - `settings.durable_sessions` package default is **false** (sessions when enabled). Hub Debug Chat already passes false; rehydrate tests still set true.
  - Catalog inject clones at most once and skips when headings are already present. LLM `Complete` gets a slice copy, not a JSON deep-copy per round.
  - `Client.Zeus()` / `Agent()` / `Catalog()` / `Session()` cache facades; `Config()` still snapshots. Semantic-cache slices are not cloned when cache is off.
  - Journal event ids are monotonic per turn; `Len()` replaces `Events()` for the finish count. Duplicate jailbreak inspect of the same tool-args / answer payload is dropped (SecurityHooks + dual scores stay).

### Fixed

- Family-law / Python-oracle gaps on the 0.1.0 candidate (no version bump, claim stays **candidate**, `multi_agent` stays **demo**):
  - `mergeTurnSettings` no longer ORs true-default bools. `ignore_user_tool_path_hints=false` injects the honor TOOL PATH POLICY (ZCF-WISH-054). Same for `tool_trail_*` and `soft_require_policy_action`. Agent API overlays caller settings onto config (`config.OverlaySettings`).
  - `Agent.RunTurn` loads catalog like Python `load_for_turn`: bind pack `response_output_schema` → `PackSchema`, `EnsureScopeBrief` (live fetch skipped this train). Lazy FS from `chat_requests_dir` when no injected store.
  - Typeahead `SearchSuggest` enforces `config.rate_limit` (token bucket, `000009`) and records `zeus_client_typeahead_total` / `zeus_client_rate_limited_total`. Direct verbs record `zeus_client_zeus_hops_total`.
  - Honesty: add [`BLOCKED.md`](BLOCKED.md); stop calling Zeus/Agent stubs in package docs; `0.1.0` changelog “Not claimed” is the **tag-time** snapshot — current `multi_agent` is **demo** (Unreleased + pins).

### Added

- Options stamp switch (ZCG-36). Product default stays `user=zeus_client`. Hub Debug Chat sets `Options.StampUser=admin` on the **same** agent path — no second loop. Session / trace / `X-Zeus-Client` follow the resolved user. Unknown values fail-closed to `zeus_client`. Helios-style `AssertProductStamp` still sees product purity on the default path. Claim stays **candidate**. `multi_agent` stays **demo**. Not `supported`. Version stays `0.1.0`.
- SSE WatchJob adapter (`adapters/jobshttp`, ZCG-37). Speaks the Go `JobEvent` wire (`schema, seq, job_id, ts, type, plan_epoch, unit_id, status, stage, pct, summary, trace_id, duration_ms, dead_end, extra`). Resume via `from_seq` + `Last-Event-ID` (query wins). `Handler` serves `GET /v1/jobs/{id}/events` for Pattern B consumers. Client `Watch` only; `run`/`get`/`cancel` stay `130001`. `config.jobs.host_url` auto-wires the client. SSE is not durable SoT. Claim stays **candidate**. `multi_agent` stays **demo**. Not `supported`.
- Pattern A demo: `examples/patterna` one job, Zeus Direct + AgentTurn, isolated scopes (ZCG-33). Product host is `jobsma.New(api.UnitHost{Units: c.Units()})` via `Client.BindJobs`. Recorded `Jobs().Run` transcripts checked in (`recorded_ok.txt`, `recorded_partial.txt`). Partial path: public `pipeline` on one Direct unit (`060010`) + one ok → `status=partial`. `multi_agent` is **demo** (not `supported`). Claim stays **candidate**. Version stays `0.1.0`.
- Pattern A in-process adapter links `koten_multi_agent_golang` v0.6.1 (ZCG-29). `Engine.RunJob` owns errgroup + MaxWorkers + wall + epoch fence; WorkUnits call `Units.AgentTurn` / `ZeusDirect`. Fake remains sequential L4 seed. `multi_agent` was **docs** until ZCG-33. Claim stays **candidate**. Min Go **1.25**.
- FakeJobRuntime + Jobs API surface (ZCG-39). Sequential L4 seed; missing host → `130001`. Fake is not the engine. `multi_agent` stays **no**. Claim stays **candidate**.
- Units.AgentTurn + Units.ZeusDirect isolated Client law (ZCG-31). Durable sessions off; `ctx` cancel; Direct skips LLM. `multi_agent` stays **no**. Claim stays **candidate**.
- Jobs domain types + `ValidateUnitMap` isolation (ZCG-28). Types only; no job engine. `multi_agent` stays **no**. Claim stays **candidate**.
- Detective projector + nine E2E gather fields on `TurnResult.Debug` (ZCG-27). `Client.Debug().ExportJournal` is the journal handle. Claim stays **candidate**.

---

## 0.1.0 — 2026-09-12

First **candidate** tag of `github.com/koten-ai/zeus_client_golang`. Drop `-dev`.
Stop at **candidate** (CANDIDATE_CHARTER STOP A). No `supported` claim. No
“production ready”. No chat_request COMPAT triple.

Later Unreleased commits stay **0.1.0** (no bump). Current pins/README:
`multi_agent=demo`. Residuals: [BLOCKED.md](BLOCKED.md). The pin block below
is the **original tag** snapshot (`multi_agent: no` at tag; Pattern A landed
after under Unreleased).

### Pins (ZCF-WISH-030)

```text
suite_version: conformance-0.2-dev
client_floor: client-floor-5
claim_level: candidate
BASE packs tested: mock path pinned (base-5-mock); no production COMPAT triple
zeus_engine: 0.6.x pin; live_smoke=false (Detective tapes sample 0.6.64)
zeus_client_design: 1b805fbc57457e7448ca74bf4ea7ad9664f05762
compat_row: none — do not invent; see chat_request COMPAT.md
multi_agent: no
modes: agent, direct
```

CHECKLIST A–E: implemented on this candidate train (G0–G8 offline). E2
`semantic_cache=no`. F at tag was `multi_agent=no`; current Unreleased is
`demo`. Honesty lives in this file + [BLOCKED.md](BLOCKED.md) + the
design-repo MATRIX row — do not treat Python CHECKLIST ticks as Go.

### Added

Hexagonal native SDK (Option A) through P-Suite, same Client law as Python:

- G0 pins + BUSL-1.1 module + hexagonal skeleton + `go test -race` in CI (ZCG-6, ZCG-7)
- Domain IDs + ErrorCode; redactor at journal / log / export; concurrent-safe journal (ZCG-10, ZCG-5, ZCG-9)
- RuntimeConfig + profiles + SecretStore (env names only); ports + `Client` / `ZeusRuntime` (`context.Context` on every hop; `Close()` idempotent) (ZCG-12, ZCG-11)
- P-HTTP dedicated `*http.Client` (no process-global HTTP), result envelope, `X-Zeus-Req-Id` capture (ZCG-8)
- P-Auth none / basic / bearer / session (never log secrets) (ZCG-17)
- Contract hash domain oracles — extract only; never forge production `contract_hash` / Hub stamps (ZCG-18)
- P-Catalog mock load, stamp extract, public `catalog.mini_schema.get` (ZCG-15)
- P-Direct search / find / get / project + typeahead; no public `pipeline`; typeahead is Direct only (ZCG-13)
- Hash-stable control-plane inject (Bag B; hash-excluded); named `rules{}` freeze; object triggers; settings bag; `ignore_user_tool_path_hints` default true (ZCG-20)
- P-Session create / continue / rehydrate; server-minted `session.id` only (ZCG-19)
- Session-trace projector joins dispatch Zeus hop `req_id` (soft-fail; never raises out of a successful domain turn) (ZCG-16)
- P-LLM OpenAI-compat adapter + 050010–050018 classify (ZCG-14)
- Layer A peel + `policy.decide`; required four; G2 never in user-facing `answer` (ZCG-22)
- P-Agent sequential turn (bags A–D, force return, ctx cancel) (ZCG-24)
- P-Control `SecurityHooks` default on + dual jailbreak scores (model `layer.jail_break_attempt` and client `hooks_jailbreak_score` stay separate) (ZCG-25)
- P-Obs family slog (INFO / ERROR / DEBUG / TRACE + REDACT) + token rollup (ZCG-26)
- P-Suite offline conformance adapter (`conformance-0.2-dev`); 16/16 required cases green locally against sibling design; GHA skips the live suite when design is absent (ZCG-23)

Product stamp `user=zeus_client`. `ai_process_result` package default **false**.
`VerbRequest.AllowPipeline` defaults false. Everyday Q&A stays Mode 1; jobs are
never auto-promoted from chat.

### Not claimed (at tag; see Unreleased + BLOCKED.md for current)

- MATRIX **`supported`** / “production ready”
- chat_request **COMPAT triple** / production `contract_hash` / Hub stamps
- Detective Hub UI
- live Zeus / live LLM as the happy path (`live_smoke=false`)
- `multi_agent=supported` (Unreleased / pins: **demo**)
- `semantic_cache` (not this train)
- plugins
- floor-6.1 as the **package floor** (settings case ran in the suite; claimed floor stays `client-floor-5`)
- OTLP Logs exporter (family slog + REDACT only; config keys exist)
