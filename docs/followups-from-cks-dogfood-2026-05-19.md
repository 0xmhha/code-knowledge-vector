# ckv Follow-ups Surfaced by cks Dogfood — 2026-05-19

> Source: `code-knowledge-system` (cks) ran `make dogfood-eval USE_CKV=1` with a
> real ckv subprocess proxy as the Stage-1 semantic backend. This document lists
> ckv-side gaps and bugs **observed from a downstream consumer's perspective**.
> Reproductions, root cause, and fixes belong in this repo.
>
> Companion docs:
> - cks repo: `docs/followups-from-dogfood-2026-05-19.md`
> - ckg repo: `<ckg>/docs/followups-from-cks-dogfood-2026-05-19.md`

## Context

cks integrates ckv as a Stage-1 semantic-search backend. Because ckv currently
exposes no Go-level `Open` for its query engine, cks spawns the `ckv` binary in
`mcp` mode and proxies tool calls (`cks.context.semantic_search`,
`cks.ops.health`) over MCP stdio. The cks adapter is
`internal/ckvclient/real.go`. Subprocess restart + per-call timeout (cks commit
`be35715`) caps the failure modes below but doesn't fix them.

ckv status: in active development. Findings below are reported as a downstream
consumer; the fixes belong here.

## Open items (ckv-side)

| # | Priority | Item | Evidence from cks | Suggested direction |
|---|---|---|---|---|
| CKV-1 | ~~High~~ → cks-side | `cks.context.semantic_search` hangs on specific queries (no error, no response) | `eval/reports/baseline-dogfood.json` shows two scenarios consistently hit `context deadline exceeded` at `ckv semantic search round 1`: `stamp-integrity-lookup` (intent=arch_explain) and `test-add-filesystem-fetcher` (intent=test_add). Same queries succeed against the in-memory Fake (USE_CKV=0). | **2026-05-20 검증 결과: ckv-side 재현 불가.** 두 failing query를 ckv MCP stdio로 직접 호출 → 모두 10-25 ms 정상 응답 (cks repo 81-file index 및 ckv repo 116-file index 양쪽 검증). 8 concurrent queries 동시 호출도 총 85 ms 완료, hang 0회. 세 가설 모두 반증: (a) 다양한 query 패턴 OK / (b) concurrent도 deadlock 없음 / (c) 응답 6-17 KB는 stdio 64KB buffer 한참 아래. **진짜 원인은 cks composer 또는 mcp-go client 사용 패턴 쪽에 있음**: composer 후처리에서 ctx propagation, multiple keyword에 대한 round-trip 패턴, 또는 subprocess transport lifecycle 등. ckv-side action 종료. cks 측 후속 조사 권장. |
| CKV-2 | ✅ ckv-side done | No public Go-level `Open` for the ckv query engine | cks `internal/ckvclient/real.go:36-43` documents: "the relevant Open functions live in internal/, so the Real adapter spawns the ckv binary in `mcp` mode and proxies cks API calls through MCP stdio" | **2026-05-20: 완료.** `pkg/ckv` 신설 — `ckv.Open(path, ckv.OpenOptions{Embedder})` + `Engine.SemanticSearch(ctx, intent, ckv.SearchOptions)` + `Engine.Manifest()` + `Engine.Close()`. `MockEmbedder()` / `NewMockEmbedder()` factory 포함. bgeonnx embedder는 caller가 직접 import (build tag 메커니즘 유지). 9개 unit test 통과. cks-side action: `internal/ckvclient/real.go` 대체 — `ckv.Open()` 한 줄로 spawn / stdio / restart 로직 모두 제거. swap point는 `cmd/cks-mcp/main.go` 의 constructor 한 곳. CKV-1 hang의 구조적 해결 경로이기도 함 (stdio MCP 자체가 사라짐). |
| CKV-3 | ✅ ckv-side done | `--embedder=mock` provides zero semantic signal | cks dogfood with `CKV_EMBEDDER=mock` produces avg recall = 0.667, same ballpark as ckg-only. Latency roughly doubles (~120ms → ~250ms) for no recall gain. | **2026-05-20: 완료.** `docs/embedder-integration.md` 신설 — consumer 시점 end-to-end 가이드 (quickstart mock → bgeonnx production setup, env override 12종, performance baseline, subprocess→in-process migration). `d1-installation-guide.md` 는 개발자용으로 유지하고 새 문서가 cross-reference. cks-side action: 이 가이드대로 `internal/ckvclient/real.go` 의 RealOpts 를 pkg/ckv 호출로 갈아끼우면 production 검색 path가 즉시 활성 (CKV-2 의 swap step과 동일 작업). |
| CKV-4 | ✅ ckv-side done | Transport-closed errors during normal operation | cks added subprocess restart in `internal/ckvclient/real.go` (`be35715`) because `transport closed` would surface mid-eval, not just on shutdown | **2026-05-20: 완료.** Root cause = mcp-go의 `WithRecovery()` middleware 미설치. tool handler 안에서 panic이 발생하면 mcp-go가 panic 을 catch 하지 않고 → MCP dispatcher 를 unwind → `ServeStdio` 종료 → ckv mcp process exit → stdout 닫힘 → cks 측 "transport closed". `pkg/mcp/server.go::NewServer` 의 `NewMCPServer(...)` 호출에 `server.WithRecovery()` 옵션 추가로 해소 — panic 이 *MCP tool error response* 로 변환되고 서버는 살아있는다. 검증: `TestServerRecoversFromHandlerPanic` — 일부러 panic 하는 tool 을 등록한 뒤 (1) panic 이 `JSONRPCError` 로 변환되는지, (2) 후속 `cks.ops.health` call 이 정상 응답하는지 (server 가 죽지 않았는지) 확인. cks-side action: 이제 `internal/ckvclient/real.go` 의 subprocess-restart 로직 (`callToolWithRestart`, `isTransportClosed`) 은 *defensive only* 로 둘 수 있고, ckv 자체 panic 으로 인한 transport drop 은 발생하지 않는다. |
| CKV-5 | ✅ ckv-side done | Embedder warm-up not documented | cks `DefaultCallTimeout` comments hypothesize "Real bgeonnx loads on first call can take 1-3s" | **2026-05-20: 완료.** ANE static-shape 측정에서 첫 file (file 1/5, ~8-10 chunks) 이 13초 — bgeonnx cold start (ONNX session + tokenizer + CoreML compile artifact) 가 *first embed* 시점에 잔존함을 확인. `Engine.Warmup(ctx)` (internal/query, pkg/ckv) + `cks.ops.warmup` MCP tool 추가. dummy embed 1회로 cold start 비용을 명시적으로 끌어올린다. 응답: `{ready, duration_ms, embedder: EmbedderInfo, error?}` — operator 가 cold-start metric 을 기록하고 "ready" gate 까지 보류 가능. cks-side action: `Initialize` 직후 `cks.ops.warmup` 1회 호출 → `DefaultCallTimeout=10s` 에 잡히던 warmup failure 가 *별도의 명시적 호출*로 분리되어 user-query timeout 과 혼동되지 않음. embedder-integration.md §1 quickstart에 권장 흐름 추가. |
| CKV-6 | ✅ ckv-side done | `cks.ops.health` does not report embedder status | cks aggregates ckv health into `degraded` state per HLD §10, but the ckv response is binary `reachable: true/false` with no signal about embedder ready / model loaded / index version | **2026-05-20: 완료.** `cks.ops.health` 응답에 두 nested 객체 추가: `embedder: {name, dimension, status, provider, model_dir}` + `index: {chunk_count, last_built_at, indexed_head}`. `status` 값 = `ready` / `stub` / `unavailable` — mock 같은 stub embedder를 cks가 즉시 degraded로 표시 가능. provider / model_dir 은 bgeonnx 가 노출하는 optional field (duck-typed; mock 은 미노출). 기존 flat field (embedding_model 등) 는 backward compat 로 유지. cks-side action: `internal/ckvclient/real.go::parseHealthResult` 가 nested 객체를 읽어 `Health.EmbedderStatus` 같은 필드 채우면 HLD §10 degraded 룰 표현 가능. |
| CKV-7 | Low | `cks.context.semantic_search` response schema versioning | cks parses the MCP tool result content opportunistically | Pin the result schema and bump version on change. Avoids silent breakage when ckv evolves. |

## Reproduction from cks side

```bash
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-system

# 1) Build both indexes:
make ckg-index
make ckv-index             # CKV_EMBEDDER=mock by default

# 2) Run dogfood with real ckv:
make dogfood-eval USE_CKV=1

# 3) Inspect the failing scenarios in the report:
python3 -c "import json; d=json.load(open('eval/reports/baseline-dogfood.json'));
[print(r['name'], '|', r.get('error','')[:120]) for r in d['results'] if r.get('error')]"
```

The two failing scenario YAMLs (queries that hang ckv):
- `eval/scenarios/stamp-integrity-lookup.yaml`
- `eval/scenarios/test-add-filesystem-fetcher.yaml`

The cks adapter that exercises the ckv MCP surface is
`internal/ckvclient/real.go` — see `callToolTimeBounded`,
`callToolWithRestart`, `isTransportClosed` for the consumer-side defenses
already in place.

## Suggested order

2026-05-20 update: **CKV-1 ckv-side 재현 실패** (위 표 참조). cks 측 조사로 이전.
ckv-side 최우선은 이제 **CKV-3 (real embedder docs)** — `docs/d1-installation-guide.md` 와
이번 throughput investigation 결과를 cks-consumer 시각으로 정리하면 됨. **CKV-2 (public
Open)** 는 cks 측 subprocess proxy stack 전체를 제거하는 장기 right shape이며, 동시에
CKV-1의 *실질적 해소 경로* — composer가 in-process로 호출하면 stdio MCP 경로가 사라지므로
cks-side composer/transport 이슈가 자연히 무관해짐.

## CKV-1 검증 절차 (재현이 필요할 때 참고)

```bash
# 1. cks repo를 ckv로 index
cd <cks-repo>
ckv build --src=. --out=.ckv-data --embedder=mock

# 2. failing query를 ckv MCP에 직접 던지기 (cks composer 경유 X)
python3 testdata/mcp-repro/serial.py
# - control + 두 failing query 모두 10-25 ms 안에 정상 응답이면 ckv-side 재현 실패
# - hang이면 ckv-side 추가 조사 필요

# 3. concurrent reproduction (composer 다중 keyword 패턴)
python3 testdata/mcp-repro/concurrent.py
# - 8 concurrent queries가 100 ms 안에 모두 완료되어야 정상
```

스크립트 자체는 `testdata/mcp-repro/` (README.md 동봉). corpus 경로는 스크립트 안의
`CKV_DATA` 변수에서 직접 수정.
