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
| CKV-1 | High | `cks.context.semantic_search` hangs on specific queries (no error, no response) | `eval/reports/baseline-dogfood.json` shows two scenarios consistently hit `context deadline exceeded` at `ckv semantic search round 1`: `stamp-integrity-lookup` (intent=arch_explain) and `test-add-filesystem-fetcher` (intent=test_add). Same queries succeed against the in-memory Fake (USE_CKV=0). | Reproduce inside ckv with the failing queries (see "Reproduction" below). Likely candidates: (a) embedder hang on specific token patterns, (b) index lookup deadlock, (c) MCP write blocking when the response buffer overflows. cks-side mitigation: `DefaultCallTimeout=10s` returns the error to the caller but leaves the subprocess in whatever state it ended in. |
| CKV-2 | High | No public Go-level `Open` for the ckv query engine | cks `internal/ckvclient/real.go:36-43` documents: "the relevant Open functions live in internal/, so the Real adapter spawns the ckv binary in `mcp` mode and proxies cks API calls through MCP stdio" | Expose a stable Go API (e.g., `pkg/ckv` or `pkg/query`) with an `Open(path string, opts ...Option) (*Engine, error)` + `SemanticSearch(ctx, query, k) ([]Hit, error)` shape. cks can then drop the subprocess hop entirely — fewer moving parts, lower latency, no subprocess restart logic needed. The cks adapter is designed so the swap is a constructor change in `cmd/cks-mcp/main.go`, not a composer change. |
| CKV-3 | High | `--embedder=mock` provides zero semantic signal | cks dogfood with `CKV_EMBEDDER=mock` produces avg recall = 0.667, same ballpark as ckg-only. Latency roughly doubles (~120ms → ~250ms) for no recall gain. | Document the production-ready embedder path end-to-end (binary install, model download, `--embedder=bgeonnx --model-dir=...`). cks `RealOpts.ModelDir` is already wired for this. Right now there's no working semantic-search test environment for a downstream consumer. |
| CKV-4 | Mid | Transport-closed errors during normal operation | cks added subprocess restart in `internal/ckvclient/real.go` (`be35715`) because `transport closed` would surface mid-eval, not just on shutdown | Investigate whether ckv's MCP server closes its stdio writer on certain errors (e.g., embedder failure, index miss). The restart pattern on the consumer side is fine as a defense, but the underlying transport should stay alive for the lifetime of the subprocess. |
| CKV-5 | Mid | Embedder warm-up not documented | cks `DefaultCallTimeout` comments hypothesize "Real bgeonnx loads on first call can take 1-3s" | Confirm this and document it. If cold-start is real, expose a `warmup` MCP tool (or do it during `initialize`) so the first user-facing call doesn't pay the cost. cks currently caps every call at 10s, which would mask warm-up failures as user-query failures. |
| CKV-6 | Mid | `cks.ops.health` does not report embedder status | cks aggregates ckv health into `degraded` state per HLD §10, but the ckv response is binary `reachable: true/false` with no signal about embedder ready / model loaded / index version | Expand the health response: `embedder: {name, status, model_dir}`, `index: {chunk_count, last_built_at}`. Lets cks (and operators) distinguish "ckv alive but no model" from "ckv ready". |
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

CKV-1 (hang reproduction) is highest priority — it blocks 2 / 9 scenarios in
the cks baseline. CKV-3 (real embedder docs) unblocks meaningful semantic-uplift
measurement. CKV-2 (public Open) eliminates the entire subprocess proxy stack
and is the long-term right shape, but the subprocess proxy is working as a
bridge until then.
