# ADR-003: BM25 stays on CKS side; CKV is vector-only

**Status**: Accepted
**Date**: 2026-05-18

## Context

Production-grade code retrieval typically fuses dense (vector) and
sparse (BM25) signals via RRF. The question for the CKV / CKG / CKS
stack: which component owns BM25?

The options:

1. **CKV implements BM25 in-tree.** Dense + sparse in one binary.
   Pros: one tool, one query path. Cons: duplicates work already
   done in CKG (`pkg/bm25/scorer.go`), forces every CKV deployment
   to build / maintain a BM25 index even when CKS isn't in the loop.
2. **CKV stays vector-only; CKS orchestrates fusion.** CKS already
   exists as the cross-tool orchestrator that talks to CKV (vector)
   and CKG (graph + BM25). RRF naturally lives where multiple
   signals already converge.
3. **CKG owns BM25 (status quo) and exposes it on demand.** Already
   shipped (`pkg/bm25/scorer.go`). Mature, tested, indexes qname +
   signature + doc-comment.

We discussed the trade-off in `review-direction-2026-05-18.md` §6.1
(Challenge 1). The user's question was: where should BM25 *live*?

Measurement record:
- CKG BM25: already production-quality (multi-field, configurable).
- CKV vector-only: working with measured baseline (ADR-002).
- CKS RRF fusion: not yet built — exists as a CKS-repo TODO.

## Decision

**Dual-track**: CKG keeps BM25, CKV stays vector-only, CKS fuses both
via RRF. CKV exposes the in-process query engine and the MCP server;
CKS will call `cks.context.query_code` (multiplex) and run RRF on the
two top-K result sets.

Concretely:

- CKV's `pkg/mcp.Server.Underlying()` is the integration point — CKS
  can wrap or compose without forking CKV.
- CKV's `pkg/ckv.Engine` exposes `SemanticSearch` for in-process use.
- No BM25 code in CKV. No `--bm25` flag. No `chunks_bm25` table.

## Consequences

**Good**:
- Single responsibility: CKV is "vector retrieval over a code repo,"
  CKG is "symbolic graph + lexical search," CKS is "orchestration."
  Each tool stays small enough to reason about in isolation.
- BM25 implementation doesn't fork — CKG's `pkg/bm25/scorer.go` is
  the one place we tune it.
- Standalone CKV users (without CKS) still get useful retrieval; they
  just get dense-only, with documented trade-offs.

**Accepted costs**:
- CKV alone underperforms a CKV+BM25 hybrid on lexical queries
  (exact identifier lookups, error-message search). The fix is to
  put CKS in the loop — explicit, not hidden.
- Coordination cost: changes to the result shape (citation format,
  score field) must be agreed across CKG + CKV + CKS. We mitigate
  this by anchoring `pkg/types.Hit` as the shared shape.

**Closed off**:
- A standalone CKV+BM25 mode. Adding it later means duplicating
  CKG's work; reopen only if a class of users emerges that needs
  hybrid retrieval without CKS.

## Related

- Roadmap `§12 #4` (Phase C: PR/commit corpus) and `§12 #6/#7`
  (contextual prefix) work within CKV; orthogonal to fusion.
- D1 / D4 in `docs/archive/backlog.md` track the CKG and CKS dependencies.
