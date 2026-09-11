# Zeus Client Go

Native Go client for Zeus AI data servers. Orchestrates LLM agents that call
Zeus tools — catalog, auth, contracts, durable sessions, and the agent loop —
without a web UI.

This is **Option A**: a native SDK (no FFI). Same **Client law** as
[zeus_client_python](https://github.com/koten-ai/zeus_client_python). Python is
the behavioral oracle when implementation details differ; family law still wins
on stamps, COMPAT, and claim honesty.

> **G0.2 + ZCG-10 + ZCG-5 + ZCG-9 + ZCG-12** — hexagonal packages, `New` /
> `Close` stubs, domain IDs, family `ErrorCode` catalogue, `security`
> DefaultRedactor, concurrent-safe `domain/journal`, and `config` RuntimeConfig
> (profiles, pins/env load, SecretStore). Remaining ports stay empty until
> ZCG-11. Claim remains **candidate** until a human
> [MATRIX](https://github.com/koten-ai/zeus_client_design/blob/main/MATRIX.md)
> row. Everyday Q&A stays Mode 1; jobs are never auto-promoted from chat.

```go
package main

import (
    "fmt"
    "log"

    zeusclient "github.com/koten-ai/zeus_client_golang"
)

func main() {
    fmt.Println(zeusclient.Version) // 0.1.0-dev
    c, err := zeusclient.New(zeusclient.Options{})
    if err != nil {
        log.Fatal(err)
    }
    defer c.Close()
}
```

## Claim (family honesty)

| Field | Value |
| --- | --- |
| **module** | `github.com/koten-ai/zeus_client_golang` |
| **version** | `0.1.0-dev` (`zeusclient.Version`) |
| **min Go** | **1.22** |
| **license** | **BUSL-1.1** (Additional Use Grant: None; Change License Apache-2.0 on 2030-09-10) |
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

## Package layout

Python V2 hexagonal tree (not a flat `loop.go`). GO_CLIENT_BOOTSTRAP §2 sketch
is the public `New` / `Close` shape; folders follow
[IMPLEMENTATION_GUIDE](https://github.com/koten-ai/zeus_client_python/blob/main/docs/V2/IMPLEMENTATION_GUIDE.md) Phase 0.

```text
github.com/koten-ai/zeus_client_golang
  client.go runtime.go version.go
  config/          # RuntimeConfig, profiles, loader (ZCG-12)
  domain/          # ids + ErrorCode (ZCG-10); journal (ZCG-9); contract, catalog, layer_a, policy later
  ports/           # SecretStore (ZCG-12); Zeus, LLM, catalog, clock, ids, HTTP, jobs (ZCG-11)
  adapters/        # secretsenv (ZCG-12); zeushttp, llmopenai, catalogfs, otlp, jobsfake later
  application/     # agent turn, data verbs, typeahead, catalog sync, projectors
  api/             # agent, data, catalog, debug, session
  observability/   # slog family events + REDACT
  security/        # DefaultRedactor + RedactAttrs (ZCG-5); jailbreak later
  internal/httpx/  # shared transport (no process-global client)
  conformance/     # offline suite adapter (G8)
```

Domain stays free of `net/http` and provider SDKs. CI enforces that.

## Local gates

```bash
make ci    # fmt vet race test
```

Min Go **1.22**. Race is required (GO_CLIENT_BOOTSTRAP §4). GitHub Actions
runs `make ci` on every PR and push to `main`.

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

Zeus Client Go is licensed under the **Business Source License 1.1 (BUSL-1.1)**
with `Additional Use Grant: None` — **no production use is permitted without
a commercial license** from Koten AI. The license converts to **Apache License
2.0** on the Change Date (**2030-09-10**).

- Repo-wide default: [`LICENSE`](LICENSE)
- Full BSL text and parameters: [`licenses/BSL-1.1.txt`](licenses/BSL-1.1.txt)

Commercial licensing: info@koten.ai
