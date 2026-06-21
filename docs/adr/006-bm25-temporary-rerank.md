# ADR-006: BM25 candidate-set rerank as temporary measurement infrastructure

**Status**: Rejected (measured 2026-05-26, no lift with bgeonnx)
**Date**: 2026-05-26

## Context

ADR-003 (2026-05-18) recorded the dual-track decision: CKV stays
vector-only, CKG owns BM25, CKS fuses both via RRF. That decision is
still the long-term architecture.

A separate question has come up since: what's the *standalone* impact
of adding sparse signal to CKV's query path, before CKS-side fusion
lands? The user's `evaluation-design-2026-05-22.md` §3 D1 captured the
answer as "3-leg BM25 *임시*" — accept a temporary in-tree BM25 pass
solely to *measure* whether the lift is worth the architectural
follow-up (full ADR-003 supersede, persistent BM25 index, build-time
statistics, etc.).

The constraints on the temporary integration:

1. **No schema change**: ADR-003 explicitly excluded BM25 columns and
   tables. A measurement-only overlay must not require `ckv reindex`
   nor invalidate existing manifest contracts.
2. **No build-time cost**: throughput is already tight (CPU 0.74 c/s
   without ANE-friendly models per ADR-005 / backlog A1). Adding any
   per-build BM25 work would push corpus rebuild times into hours for
   stable-net-scale repos.
3. **Reversible**: if measurement shows no lift (or a regression), we
   must be able to *delete the package* without a downstream
   migration. The supersede of ADR-003 happens only if measurement
   justifies it.
4. **Comparable runs**: a single `ckv eval` invocation should be able
   to A/B the on/off behaviour against the same index, with the same
   embedder, without restarting the process.

The natural design under those constraints is **candidate-set BM25**:
build the BM25 corpus from the *vector-retrieved candidates only*
(typically `k × overfetch = 30`), score against the user's intent, and
combine vector + BM25 ranks via RRF. The corpus statistics (IDF,
avgDocLen) are recomputed per query — cheap on a 30-document set.

## Decision

Land `internal/query/bm25/` as a candidate-rerank package with the
following shape:

- **Algorithm**: Okapi BM25 (`K1=1.5`, `B=0.75`) — the same
  implementation CKG ships at `pkg/bm25/`, adapted in-tree with a
  source-attribution comment so the CKV build does not depend on a
  CKG checkout.
- **Corpus**: per chunk, `SymbolName + first non-empty text line`
  (D3-B). Whole chunk bodies are excluded — long bodies dominate
  tokenization cost and the noise dilutes the symbol-level signal.
- **Fusion**: Reciprocal Rank Fusion with `k=60` (Cormack, Clarke,
  Buettcher 2009). Vector rank comes from the store's existing sort;
  BM25 rank comes from a stable secondary sort on the candidate
  scores. The final ordering is by RRF score, with ties broken by
  vector rank.
- **Placement**: between `query.store.search` and
  `query.threshold.drop` (D2-A). The new sub-span
  `query.bm25.rerank` is the sixth in the Phase 1 footprint
  decomposition.
- **Default**: **off**. `Options.EnableBM25Rerank` defaults to
  `false`; users opt in via `ckv query --bm25-rerank` or the MCP
  `bm25_rerank: true` arg. The footprint span fires unconditionally
  (with `enabled` / `disabled` flags) so trace consumers see a
  symmetric shape regardless of the toggle.
- **No schema change**: `HitScore` gains two `omitempty` fields
  (`BM25Score`, `HybridRank`); they're absent from JSON output when
  the toggle is off. `vector.db`, `manifest.json`, and the build
  pipeline are untouched.

The corresponding code lives in:

- `internal/query/bm25/{scorer,okapi,tokenize}.go` — adapted from
  CKG's `pkg/bm25/` (attribution in each file's header).
- `internal/query/bm25/rerank.go` — CKV-specific: candidate-set
  `Rerank`, `BuildCorpusText` (D3-B canonical form), and `Stats`
  (rank_changes, top1_score_delta) for footprint summarization.
- `internal/query/engine.go` — Step 2.5 between store.search and
  threshold.drop. The `query.bm25.rerank` span is the new Phase 1
  sub-event.

## Consequences

**Good**:

- Standalone CKV deployments can now measure BM25 lift without a
  CKS dependency. The 12-entry PR fixture (`testdata/prs.yaml`) +
  N=50 query fixture (`testdata/queries.yaml`) drive A/B comparison
  in two `ckv eval` invocations.
- Multi-stage E1/E2/E3 metrics (NEW-4, commit `53964b1`) let the
  comparison answer *which stage* the rerank moves — intent capture
  doesn't change, but location identification (E2 SymbolF1) and
  plan steps may. That decomposition is the input to the supersede
  decision: a global-corpus BM25 (full ADR-003 supersede) is only
  justified if E2 / file-F1 shows clear lift on top of vector.
- The CLI / MCP toggle keeps the existing baseline (`r@5=0.740,
  MRR=0.4937` on testdata/sample + mock embedder) intact. No
  silent regression risk for callers that don't opt in.

**Accepted costs**:

- **Candidate-set IDF bias**: every candidate already matched the
  vector query, so the local IDF distribution rewards terms that
  vary *within the cluster of vector-similar chunks*. That's still
  useful for rerank (the goal is to discriminate between candidates,
  not to score against the whole corpus), but BM25 numbers from
  this package are not comparable to a global-IDF BM25 over the
  full chunk store. Operators reading `BM25Score` in logs should
  treat it as a local rank-input, not an absolute relevance score.
- **No persistence**: every query rebuilds the BM25 corpus from
  scratch on its ~30 candidates. The cost is small (sub-millisecond
  on the mock-embedder smoke), but it scales linearly with the
  overfetch factor — a larger `k * overfetchFactor` will dominate
  the rerank latency budget. If we ever raise `overfetchFactor`
  past 10× we should benchmark before assuming the per-query rebuild
  stays cheap.
- **No supersede yet**: ADR-003 stays Accepted. This ADR is Proposed
  until the post-measurement decision lands. Until then the
  vector-only stance is still the *default*, advertised path for
  CKV consumers.

**Closed off**:

- Building a global-corpus BM25 index at `ckv build` time. That would
  give a real IDF distribution but requires manifest schema changes,
  a `chunks_bm25` SQLite table, and `ckv reindex` invalidation
  semantics. We deliberately stay out of that change until the
  candidate-set measurement justifies it.
- Weighted-sum fusion (e.g. `α·vector + (1-α)·bm25`). The two scores
  have incompatible magnitudes (vector cosine in `[0,1]`, BM25 in
  `[0, ∞)`), so a weighted sum needs per-corpus tuning. RRF is
  scale-invariant by construction and is the right starting point
  for "we don't have time to tune yet."

## Related

- ADR-003 — current vector-only stance. This ADR may eventually
  supersede 003 if measurement justifies a permanent BM25 layer.
  Until then 003 stays Accepted.
- evaluation-design-2026-05-22.md §3 D1 — captures the user's
  "3-leg BM25 임시" framing this ADR implements.
- archive/session-handoff-2026-05-23.md §5 Wave C — entry-condition notes
  including the off-default-rollout rationale (preserve baseline,
  avoid silent regression).
- NEW-4 commit `53964b1` — multi-stage E1/E2/E3 metrics that this
  rerank's effect should be measured against.

## Measurement Evidence (2026-05-26)

Environment: bgeonnx (bge-large-en-v1.5, 1024-dim), CPU EP, testdata/sample
(7 files, 47 chunks), testdata/queries.yaml (N=50).

| Setting | r@5 | MRR | halluc |
|---------|-----|-----|--------|
| BM25 OFF | 1.000 | 0.8633 | 0.000 |
| BM25 ON | 1.000 | 0.8257 | 0.000 |
| delta | 0.000 | -0.0376 | 0.000 |

Per-query rank changes: 7/50 queries affected. 6 degraded, 1 improved.
The strong semantic embedder (r@5=1.0 ceiling) already retrieves all
relevant chunks; BM25 reranking disrupts the correct top-1 ordering.

Supersede criteria (evaluation-design §6.4 Step 4): r@5 lift FAIL
(+0.00 vs +0.03 required), MRR lift FAIL (-0.038 vs +0.01 required).

**Conclusion**: ADR-003 stays Accepted. The `--bm25-rerank` flag
remains in-tree as opt-in measurement infrastructure but will not
become default-on. The candidate-set BM25 code in
`internal/query/bm25/` requires no removal — it has zero cost when
disabled and may be useful for future corpus compositions where the
embedder signal is weaker (e.g. mixed-language PR descriptions).
