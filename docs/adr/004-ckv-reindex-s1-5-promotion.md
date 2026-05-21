# ADR-004: Promote `ckv reindex` from S2 to S1.5

**Status**: Accepted
**Date**: 2026-05-19

## Context

The original milestone plan (`plan-S1-ckv.md`) put incremental
reindexing in S2: S1 ships full-rebuild indexing, S2 adds
`ckv reindex` for partial updates.

Re-reading the retrieval-quality roadmap surfaced a coupling we had
missed:

- Phase B (multi-granularity chunking) is the next major retrieval-
  quality lever after the contextual prefix (Phase D.1).
- Phase B multiplies chunks per file — early estimates put throughput
  at **0.4–0.6 chunks/s** with bge-large + contextual prefix.
- At that throughput a 5k-file repo takes 6+ hours for a full rebuild.
- A 6+ hour rebuild every time a few files change is unworkable for
  the "ckv indexes my repo and stays useful through edits" experience.

`retrieval-quality-roadmap.md §7.5` documents the dependency: any
Phase B / C / D rollout assumes incremental reindex is *already*
operational, otherwise the throughput numbers become loss-leaders.

So the question became: do we ship Phase B without `ckv reindex`
(and tell users to set aside an evening for rebuilds), or do we
pull reindex forward?

Measurement record (at decision time):
- bge-large CPU baseline (2026-05-20): 0.74 chunks/s, 6h+ rebuild
  projected for the target corpus.
- Reindex effort estimate: ~300 LOC + 7 tests.
- Phase B effort estimate: ~250 LOC + measurement.

## Decision

Promote `ckv reindex` from S2 to **S1.5** — an intermediate milestone
between S1 (full rebuild) and S2 (Phase B / multi-granularity).

S1.5 entry condition: `ckv reindex` runs in under 5 minutes for a
file-level change set on a real corpus.

S2 entry now depends on S1.5. Any phase that increases per-build cost
(B, C, D.2) waits until reindex is operational.

## Consequences

**Good**:
- Phase B / C / D.2 throughput projections become realistic — they
  apply to the daily delta, not the initial seed.
- The contract "rebuild once, reindex many times" matches how every
  serious indexer (search engines, LSP servers) operates.
- `featurelist.md §6.2` and `plan-S1-ckv.md §13` get a clean cross-
  reference; readers see the dependency in the spec, not just in
  a roadmap section.

**Accepted costs**:
- S1 milestone marker moves slightly later (we ship S1.5 before
  declaring S1 "done" for production use). Acceptable — S1 callers
  are early adopters who can run full rebuilds.
- Three extra files touched (`featurelist`, `plan-S1`, `backlog`)
  to keep cross-references consistent.

**Closed off**:
- Shipping Phase B without reindex. We'd rather wait than publish
  numbers that nobody can reproduce on a real repo.

## Realization

`ckv reindex` shipped 2026-05-21 (commit `f2bb8d2`):
`internal/build.Reindex` + `cmd/ckv reindex`, git diff `--name-status`
based change set, embedder identity enforcement, 7 unit tests.
See `internal/build/reindex.go`.
