# Pattern A demo (ZCG-33)

Runnable fruit/beer job from [MULTI_AGENT_EXAMPLE.md §0](https://github.com/koten-ai/zeus_client_design/blob/main/docs/multi_agent/MULTI_AGENT_EXAMPLE.md#0-multi-agent-setup-checklist-runbook).

One `Jobs().Run`: **≥2 units**, one **`agent_turn`** (sales) + one **`zeus_direct`** (inventory). Product host is `adapters/jobsma` (`koten_multi_agent_golang` `Engine.RunJob`). **Not** `FakeJobRuntime`.

Claim: **candidate**. `multi_agent=demo` after the recorded `Jobs().Run` in this directory. **Not** `supported`.

Everyday Q&A stays Mode 1. This binary only calls `Jobs().Run` — it does not auto-promote chat into a job.

## Must ticks (EXAMPLE §0)

| Layer | This demo |
| --- | --- |
| **Job** | `goal`, `pack=desk`, `JobBudgets` (`max_workers`, `wall_ms`, `max_waves`), unit map |
| **LLM roles** | `config.llm.roles.{orchestrator,advisor,worker}` (env **names** only). AgentTurn uses **worker**. Orchestrator/advisor are configured; Pattern A demo uses `PlannerStatic` + `AdvisorOff` (engine owns plan/advise — do not reimplement) |
| **`unit_sales` agent_turn** | Own Zeus URL + `beer-sample/sales/_default` + auth mode + catalog pin `base-5-mock` + inject (`SCOPE BRIEF` + `MINI-SCHEMA`) + worker LLM. Isolated bags. Durable session off |
| **`unit_inventory` zeus_direct** | Own Zeus URL + `inventory/east/_default` + auth mode + **no** catalog |
| **Never** | Shared durable `session.id`; secrets in the job payload / events; `contract_hash` invention; Mode 1 chat auto-promoted to jobs |

## How to run

Sibling layout (required for `jobsma`):

```text
../koten_multi_agent_golang/go.mod
```

From the module root:

```bash
# Recorded (default): real Jobs().Run, in-process scripted Zeus + worker LLM.
# No live Zeus, no LLM key. This is the checked-in proof path.
go run -tags patterna ./examples/patterna

# Partial success: Direct unit uses public pipeline (060010) + AgentTurn ok
# → job status=partial.
go run -tags patterna ./examples/patterna -partial
```

Proof artifacts (one real run each):

- [`recorded_ok.txt`](recorded_ok.txt) — happy path `status=ok`
- [`recorded_partial.txt`](recorded_partial.txt) — `status=partial`

### Live (lab Zeus + LLM)

Not the `make ci` happy path (`live_smoke=false`). Env **names** only in-repo; put secret **values** in the environment, never in git.

```bash
# Zeus
export ZEUS_URL=http://127.0.0.1:8080          # or ZEUS_CLIENT_URL
export ZEUS_CLIENT_AUTH_MODE=none              # none|basic|bearer|session
export ZEUS_CLIENT_USERNAME=                   # basic
export ZEUS_CLIENT_PASSWORD_ENV=ZEUS_PASSWORD  # name of the env that holds the password
export ZEUS_CLIENT_TOKEN_ENV=ZEUS_TOKEN        # bearer

# Worker LLM (OpenAI-compatible). Default pin: XAI_API_KEY
export ZEUS_CLIENT_LLM_API_KEY_ENV=XAI_API_KEY
export XAI_API_KEY=                            # value lives here, not in the command line file
export ZEUS_CLIENT_LLM_BASE_URL=https://api.x.ai/v1
export ZEUS_CLIENT_LLM_MODEL=grok-4-1-non-reasoning

go run -tags patterna ./examples/patterna -mode=live
```

Optional `-config path/to/runtime.json` (same shape as `config.Load`; `api_key_env` / `password_env` **names**, never key material).

Live AgentTurn still ships a mock inject block in-process so the demo does not invent a Hub `contract_hash`. Point units at a lab catalog when you have a stamped pack.

## Partial-success path

`ValidateUnitMap` stays fail-closed. The **job** still runs when one unit fails after start:

| Unit | Kind | What happens |
| --- | --- | --- |
| `unit_sales` | `agent_turn` | Isolated Mode 1 turn → `status=ok` |
| `unit_inventory` | `zeus_direct` | `verb=pipeline` is rejected on the Direct surface (`060010`) → `status=error` |

Engine + `jobsma` fold that into `JobSnapshot.Status=partial` / `Partial=true` (one ok + one error). Same path as `TestPatternAPartialOnOneUnitError`.

```bash
go run -tags patterna ./examples/patterna -partial
```

## Wiring

```go
c, err := zeusclient.New(zeusclient.Options{ /* Zeus, LLM, Config */ })
c.BindJobs(jobsma.New(api.UnitHost{Units: c.Units()}))
handle, err := c.Jobs().Run(ctx, goal, api.JobsRunParams{Pack: "desk", Budgets: &bag, Units: units})
```

`//go:build patterna` — `make ci` adds `-tags patterna` only when `../koten_multi_agent_golang/go.mod` exists. GitHub Actions still skips jobsma until `KOTEN_CI_PAT` is set.

Optional SSE WatchJob for Pattern B UIs (ZCG-37): mount
`jobshttp.Handler{Jobs: c.Jobs()}` at `/v1/jobs/` or watch a sidecar with
`config.jobs.host_url`. Resume `from_seq` / `Last-Event-ID`. SSE is not durable SoT.

## Not this demo

- Reimplementing RunJob / replan / store / chaos
- `multi_agent=supported` (human MATRIX only)
- `live_smoke` as the package default
- Semver bump past `0.1.0`
