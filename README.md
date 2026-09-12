# Zeus Client Go

Native Go client for Zeus AI data servers. Orchestrates LLM agents that call
Zeus tools — catalog, auth, contracts, durable sessions, and the agent loop —
without a web UI.

This is **Option A**: a native SDK (no FFI). Same **Client law** as
[zeus_client_python](https://github.com/koten-ai/zeus_client_python). Python is
the behavioral oracle when implementation details differ; family law still wins
on stamps, COMPAT, and claim honesty.

> **0.1.0 candidate** — hexagonal packages through P-Suite (G0–G8 offline).
> Claim remains **candidate** until a human
> [MATRIX](https://github.com/koten-ai/zeus_client_design/blob/main/MATRIX.md)
> row. Everyday Q&A stays Mode 1; jobs are never auto-promoted from chat.
> See [CHANGELOG.md](CHANGELOG.md).

```go
package main

import (
    "fmt"
    "log"

    zeusclient "github.com/koten-ai/zeus_client_golang"
)

func main() {
    fmt.Println(zeusclient.Version) // 0.1.0
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
| **version** | `0.1.0` (`zeusclient.Version`) |
| **min Go** | **1.25** (Pattern A links `koten_multi_agent_golang` v0.6.1) |
| **license** | **BUSL-1.1** (Additional Use Grant: None; Change License Apache-2.0 on 2030-09-10) |
| **claim_level** | **candidate** (not `supported`) |
| **client_floor** | `client-floor-5` |
| **modes** | `agent`, `direct` |
| **multi_agent** | **demo** (Pattern A `examples/patterna`; recorded `Jobs().Run` — not `supported`) |
| **plugins** | no |
| **suite** | `conformance-0.2-dev` (offline) |
| **BASE packs tested** | mock path pinned (`base-5-mock`); no production COMPAT triple |
| **Zeus versions tested** | `0.6.x` pin; `live_smoke=false` (Detective tapes sample 0.6.64) |

Pins file: [`sdk_bootstrap.pins.json`](sdk_bootstrap.pins.json) (HOW_TO G0).
Release notes: [`CHANGELOG.md`](CHANGELOG.md).
Never invent production `contract_hash` / Hub stamps. Never put API keys in pins.
Do not claim `supported` or “production ready”.

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
| Product stamp | `user=zeus_client` (Hub Debug Chat uses `user=admin` via `Options.StampUser`; same agent path) |

Canonical turn (Mode 1 Agent):

```text
Auth → catalog (Bag A) → contract + session → injects (Bag B)
  → LLM ↔ Zeus rounds (Bag C append-only) → Layer A + policy.decide
  → commit + traces → audit (Bag D) → envelope
```

## Concurrency (Go is Pattern A)

Python Mode 3 is **Pattern B** (HTTP/SSE to a Go sidecar). This module is
**Pattern A**: same process, link `koten_multi_agent_golang` via
`adapters/jobsma`. Wire with `jobsma.New(api.UnitHost{Units: c.Units()})`
then `c.BindJobs` (or inject on `Options.Jobs`). Optional SSE WatchJob
(`adapters/jobshttp`) speaks the same event wire for Pattern B consumers:
`GET /v1/jobs/{id}/events?from_seq=N` + `Last-Event-ID`. Set
`config.jobs.host_url` to attach as a sidecar client (`run`/`get`/`cancel`
stay `130001`). Do not reimplement RunJob / replan / store / chaos.
Everyday Q&A stays Mode 1. Claim is **demo** (`examples/patterna`), not
`supported`.

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

Runnable demo (sibling `koten_multi_agent_golang` required):

```bash
go run -tags patterna ./examples/patterna
go run -tags patterna ./examples/patterna -partial
```

See [`examples/patterna/README.md`](examples/patterna/README.md). Recorded transcripts
are the proof path (`live_smoke=false`).

## Package layout

Python V2 hexagonal tree (not a flat `loop.go`). GO_CLIENT_BOOTSTRAP §2 sketch
is the public `New` / `Close` shape; folders follow
[IMPLEMENTATION_GUIDE](https://github.com/koten-ai/zeus_client_python/blob/main/docs/V2/IMPLEMENTATION_GUIDE.md) Phase 0.

```text
github.com/koten-ai/zeus_client_golang
  client.go runtime.go version.go
  config/          # RuntimeConfig, profiles, loader (ZCG-12)
  domain/          # ids + ErrorCode (ZCG-10); Result envelope (ZCG-8); journal (ZCG-9); contract+stamps (ZCG-18); catalog path/lineage/mini_schema/floor (ZCG-15); rules freeze + inject proof + tool trail (ZCG-20); SessionHandle (ZCG-19); LLM classify (ZCG-14)
  ports/           # SecretStore (ZCG-12); Zeus, LLM, catalog, clock, ids, HTTP, jobs (ZCG-11)
  adapters/        # secretsenv (ZCG-12); zeushttp headers+auth+verbs (ZCG-8, ZCG-17, ZCG-13); session HTTP (ZCG-19, ZCG-16); catalogfs (ZCG-15); llmopenai (ZCG-14)
  application/     # data verbs + typeahead (ZCG-13); Bag B inject (ZCG-20); session lifecycle (ZCG-19); projectors (ZCG-16); agent turn (ZCG-24); SecurityHooks (ZCG-25); detective + debug_export (ZCG-27)
  api/             # agent, data, catalog, debug, session, jobs, units
  examples/        # Pattern A demo (ZCG-33; //go:build patterna)
  observability/   # slog family events + REDACT
  security/        # DefaultRedactor + RedactAttrs (ZCG-5); jailbreak (ZCG-25)
  internal/httpx/  # dedicated *http.Client, req_id capture, 30s timeout (ZCG-8)
  conformance/     # offline suite adapter (G8 / ZCG-23); same suite_version as pins
```

Domain stays free of `net/http` and provider SDKs. CI enforces that.

## Local gates

```bash
make ci            # fmt vet race test
make conformance   # offline suite (requires sibling zeus_client_design)
```

Min Go **1.25** (runtime floor of `koten_multi_agent_golang` v0.6.1). Race is
required (GO_CLIENT_BOOTSTRAP §4). GitHub Actions runs `make ci` on every PR
and push to `main`. Pattern A tests (`-tags patterna`) run when the sibling
`koten_multi_agent_golang` clone is present. `go test ./conformance`
**skips** the live suite when the private design repo is not cloned; it does
not fabricate passed cases. `make conformance` hard-fails if the sibling is
missing. The adapter does **not** award MATRIX `supported`.

## Sibling layout

Prefer clones next to this repo so pin paths resolve:

```text
../zeus_client_design/     # law + suite + pins template
../zeus_chat_request/      # BASE packs + COMPAT
../Zeus/                   # engine OpenAPI
../zeus_client_python/     # behavioral oracle
../koten_multi_agent_golang/  # Pattern A runtime (go.mod replace; required for jobsma)
```

## License

Zeus Client Go is licensed under the **Business Source License 1.1 (BUSL-1.1)**
with `Additional Use Grant: None` — **no production use is permitted without
a commercial license** from Koten AI. The license converts to **Apache License
2.0** on the Change Date (**2030-09-10**).

- Repo-wide default: [`LICENSE`](LICENSE)
- Full BSL text and parameters: [`licenses/BSL-1.1.txt`](licenses/BSL-1.1.txt)

Commercial licensing: info@koten.ai
