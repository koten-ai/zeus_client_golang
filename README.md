# Zeus Client Go

Native Go client for Zeus AI data servers. Orchestrates LLM agents that call
Zeus tools — catalog, auth, contracts, durable sessions, and the agent loop —
without a web UI.

This is **Option A**: a native SDK (no FFI). Same **Client law** as
[zeus_client_python](https://github.com/koten-ai/zeus_client_python). Python is
the behavioral oracle when implementation details differ; family law still wins
on stamps, COMPAT, and claim honesty.

> **Scaffold (G0)** — pins + module identity only. Hexagonal tree and public
> `New` / `Close` are the next gate. Claim remains **candidate** until a human
> [MATRIX](https://github.com/koten-ai/zeus_client_design/blob/main/MATRIX.md)
> row. Everyday Q&A stays Mode 1; jobs are never auto-promoted from chat.

```go
import zeusclient "github.com/koten-ai/zeus_client_golang"
```

## Claim (family honesty)

| Field | Value |
| --- | --- |
| **module** | `github.com/koten-ai/zeus_client_golang` |
| **min Go** | **1.22** |
| **license** | Apache-2.0 |
| **claim_level** | **candidate** (not `supported`) |
| **client_floor** | `client-floor-5` |
| **modes** | `agent`, `direct` |
| **multi_agent** | **no** (Pattern A later; not this train) |
| **plugins** | no |
| **suite** | `conformance-0.2-dev` (offline) |
| **BASE packs tested** | mock path pinned (`base-5-mock`); no production COMPAT triple |
| **Zeus versions tested** | `0.6.x` pin; `live_smoke=false` |

Pins file: [`sdk_bootstrap.pins.json`](sdk_bootstrap.pins.json) (HOW_TO G0).
Never invent production `contract_hash` / Hub stamps. Never put API keys in pins.

Family law and ship bar:

- [HOW_TO_MAKE_A_CLIENT.md](https://github.com/koten-ai/zeus_client_design/blob/main/HOW_TO_MAKE_A_CLIENT.md)
- [CHECKLIST.md](https://github.com/koten-ai/zeus_client_design/blob/main/CHECKLIST.md)
- [MATRIX.md](https://github.com/koten-ai/zeus_client_design/blob/main/MATRIX.md)
- [COMPAT.md](https://github.com/koten-ai/zeus_chat_request/blob/main/COMPAT.md) (engine pack triples — not invented here)
- [GO_CLIENT_BOOTSTRAP.md](https://github.com/koten-ai/zeus_client_design/blob/main/docs/autonomy/GO_CLIENT_BOOTSTRAP.md)
- [CLOSED_WORLD_GOLDEN_PATH.md](https://github.com/koten-ai/zeus_client_design/blob/main/docs/autonomy/CLOSED_WORLD_GOLDEN_PATH.md)
- Jira: [ZCG](https://kotenai.atlassian.net/jira/software/projects/ZCG/boards/149)

Do not self-award MATRIX / claim `supported` from CI green alone.

## Hard rules

| Rule | |
| --- | --- |
| Never forge `contract_hash` | Hub-published stamp only; offline mock may be unstamped |
| G2 never in `answer` | Layer A peel |
| No public `pipeline` | Direct verbs + agent loop only |
| Catalog fail-closed | Missing pack / stamp is an error, not a guess |
| Product stamp | `user=zeus_client` (Hub Debug Chat uses `user=admin` via Options later) |

Canonical turn (Mode 1 Agent):

```text
Auth → catalog (Bag A) → contract + session → injects (Bag B)
  → LLM ↔ Zeus rounds (Bag C append-only) → Layer A + policy.decide
  → commit + traces → audit (Bag D) → envelope
```

## Concurrency (Go is Pattern A)

Python Mode 3 is **Pattern B** (HTTP/SSE to a Go sidecar). This module is
**Pattern A**: same process, link `koten_multi_agent_golang`; do not reimplement
RunJob / replan / store / chaos. Mode 3 is **out of v0.1**.

| Python | Go |
| --- | --- |
| `asyncio` + `async with` | `context.Context` on every hop + `Client.Close()` |
| `cancel_event: asyncio.Event` | `ctx.Done()` / wave `context.WithCancel` |
| sequential `FakeJobRuntime` | tests: sequential fake; product: errgroup + MaxWorkers semaphore |
| one event loop | one goroutine per in-flight unit; no shared Bag C |
| journal list append | `sync.Mutex`; `go test -race` required in CI |
| epoch replan | cancel wave ctx; reject publish if `unit.epoch < job.plan_epoch` |

One agent turn stays sequential (append-only messages). Parallelism is across
units, not inside a single bag.

## Local gates

```bash
make ci    # fmt vet race test
```

Min Go **1.22**. Race is required (GO_CLIENT_BOOTSTRAP §4).

## Sibling layout

Prefer clones next to this repo so pin paths resolve:

```text
../zeus_client_design/     # law + suite + pins template
../zeus_chat_request/      # BASE packs + COMPAT
../Zeus/                   # engine OpenAPI
../zeus_client_python/     # behavioral oracle
../koten_multi_agent_golang/  # Pattern A runtime (later)
```

## License

Apache License 2.0. Copyright 2026 Koten AI. See [LICENSE](LICENSE).
