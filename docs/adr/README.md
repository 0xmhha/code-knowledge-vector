# Architecture Decision Records

This directory holds the ADRs (Architecture Decision Records) for CKV.
Each record captures a *decision that was hard to reverse* and the
*context* that made it the right call at the time. Future readers can
revisit the record when the surrounding assumptions change.

## Format

One file per decision: `NNN-short-slug.md`.

- `NNN` is a zero-padded sequence (`001`, `002`, ...). Never reuse.
- Slug is kebab-case, ≤6 words.

Each ADR has these sections:

- **Status**: `Proposed` / `Accepted` / `Deprecated` / `Superseded by NNN`
- **Date**: `YYYY-MM-DD` (date of acceptance, not first draft)
- **Context**: what triggered this decision, what we knew at the time
- **Decision**: what we picked, in one paragraph
- **Consequences**: trade-offs accepted, costs paid, what we now can't do

Keep ADRs concise. If a decision needs more than a page, it probably
contains more than one ADR.

## When to write an ADR

Write one when **any** of these is true:

- The decision is expensive to undo (data layout, embedder identity,
  storage engine, public API surface).
- The decision contradicts an obvious default.
- The decision required a measured trade-off and you want the numbers
  preserved next to the rationale.

Day-to-day code style, naming, or refactor decisions don't need ADRs —
those live in code comments or `coding-style.md`.

## When to update an ADR

ADRs are immutable history. If the decision changes:

1. Don't edit the original — change history is in `git log` if needed
2. Write a new ADR with `Status: Accepted` describing the new choice
3. Edit the original's status line to `Superseded by NNN`

This keeps the record honest about what was true at each point in time.

## Index

| ID | Title | Status | Decided |
|----|-------|--------|---------|
| [001](001-sqlite-vec-storage.md) | sqlite-vec for embedded vector storage | Accepted | 2026-04-30 |
| [002](002-bge-large-pivot.md) | Pivot from bge-code-v1 to bge-large-en-v1.5 | Accepted | 2026-05-15 |
| [003](003-bm25-dual-track.md) | BM25 stays on CKS side; CKV is vector-only | Accepted | 2026-05-18 |
| [004](004-ckv-reindex-s1-5-promotion.md) | Promote ckv reindex from S2 to S1.5 | Accepted | 2026-05-19 |
| [005](005-coreml-mlprogram-static-shapes.md) | CoreML format = MLProgram with static shapes | Accepted | 2026-05-20 |
| [006](006-bm25-temporary-rerank.md) | BM25 candidate-set rerank as temporary measurement infrastructure | Rejected | 2026-05-26 |
