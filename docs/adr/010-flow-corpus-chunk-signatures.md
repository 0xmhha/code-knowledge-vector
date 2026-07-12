# ADR-010: Flow-corpus chunk signatures and layer identity

**Status**: Accepted
**Date**: 2026-07-12

## Context

CKV embeds a curated *flow corpus* (`corpus.jsonl`) — the machine-loadable form
of human-written flow docs. Each record ties natural-language prose (often
Korean, e.g. "수수료 위임이 어디서 검증되나?") to a precise code site, so a
symptom-phrased query retrieves the code that implements it. The corpus is a
single generated artifact (`build_corpus.py` over markdown), regenerated whole.

Three questions had to be settled once, because they are hard to reverse — chunk
IDs are the store's primary key, so changing them invalidates every built index:

1. How do the corpus record types map to chunk kinds, and what is each chunk's
   **identity** (ID)?
2. What text is embedded per kind (the "human wording → code keyword" bridge)?
3. How does a reindex detect and apply corpus changes?

The obvious default — "one chunk kind, hash the whole record, diff per record" —
is wrong here: flow records have three distinct shapes, two of which have no
single code location, and the corpus is regenerated as a unit.

## Decision

**Map the three flow record types to three chunk kinds with granularity-
appropriate ID schemes, embed a per-kind text tuned for symptom retrieval, and
replace the whole flow layer on a corpus content-hash change.** `edge` records
are skipped (graph-only relations are CKG's job — ADR-003).

**Record → kind + ID** (`internal/flowcorpus/parser.go`):

| record | chunk kind | ID scheme | citation |
|---|---|---|---|
| `flow` | `flow_spine` | `ChunkID("flow:"+id, 0, 0, sha)` | corpus basename (fileless) |
| `step` | `flow_step` | `ChunkID(file, line, line, sha)` | real `file:line` |
| `invariant` | `invariant` (`Provenance="curated"`) | `ChunkID("invariant:"+id, 0, 0, sha)` | corpus basename (fileless) |
| `edge` | — | skipped | — |

- **Steps key on `(file, line, sha)`** so a step chunk carries the same
  `file:line` citation as the source at that site (the flow prose and the code
  share a location and answer the same query with one citation). It is a
  *separate* chunk from the code symbol — not deduped away — but re-loading the
  same corpus yields the same ID, so the upsert is idempotent.
- **Spines/invariants key on a namespaced synthetic id** (`flow:` / `invariant:`
  prefix, `start=end=0`) because they describe a *whole flow / cross-cutting
  rule*, not one line. The prefix prevents collision with any real `file:0`
  chunk; the fileless citation resolves via the corpus directory, which the
  build adds to the manifest docs roots.

**Embed text per kind**:
- spine = the flow `summary` (the one-line "what this entry point does").
- step = `prose + symbol + each branch's "when"` — branch conditions are folded
  in so a failure-symptom query ("FeePayer 서명 불일치") retrieves the step that
  branches on it.
- invariant = `title + statement + assumes + check`.
- All carry `Category="domain"`; curated invariants carry `Provenance="curated"`
  to separate them from auto-extracted invariants.

**Layer identity / reindex** (`sources.flow` = `HashedSource{Path, ContentHash}`):
the flow layer is replaced **wholesale** (delete all flow chunks → reload) when
the corpus `content_hash` changes, rather than diffed per record. The corpus is
a single generated file, so a content-hash gate is both sufficient and correct
for removals (a record deleted from the corpus leaves no orphan chunk).

## Consequences

**Good**:
- Step chunks share the code's `file:line` citation, so vector hits on flow
  prose and on source resolve to the same site — the query gets both the prose
  and the code with one location. (canonical_id alignment, ADR-007, is currently
  stamped only on source chunks, not flow chunks; steps carry a real `file:line`
  so aligning them later is a localized change, unlike the fileless spines.)
- Symptom-phrased queries hit steps because branch `when` conditions are in the
  embed text — the corpus's main reason to exist.
- Wholesale content-hash replacement makes reindex correct for edits *and*
  removals with one cheap check; no per-record bookkeeping.
- Fileless spines/invariants are addressable and citable without inventing fake
  line numbers, and the namespaced ID prevents primary-key collisions.

**Accepted costs / caveats**:
- A single-line edit anywhere in the corpus changes its content hash and
  re-embeds the *entire* flow layer. Acceptable: the corpus is small relative to
  the code corpus and regenerated as a unit; per-record incremental flow reindex
  is explicitly not worth the bookkeeping.
- Step dedup assumes at most one meaningful flow step per `(file, line)`; two
  steps on the same line with different prose would collide on ID unless their
  text (hence sha) differs. The corpus generator is expected to keep one step
  per site.
- Flow chunks are `Language="flow"` (not a real source language), so language
  filters must treat "flow" as a first-class value.

## Realization

- Parser + signatures: `internal/flowcorpus/parser.go`
  (`flowChunk`/`stepChunk`/`invariantChunk`), tests in `parser_test.go`.
- Wholesale reindex (P3b-flow): `internal/build/reindex.go` +
  `store.DeleteFlowChunks` (`flow_step`/`flow_spine`/curated `invariant`);
  test `TestReindex_ReindexesFlowOnContentChange`.
- Fileless citation resolution: corpus dir appended to `manifest.DocsRoots`
  (`internal/build/builder.go`).
- Relationship: ADR-003 (CKV is vector-only; BM25/graph on CKS) is why `edge`
  records are skipped. ADR-007 (canonical_id join): flow chunks are not stamped
  today, but steps carry a real `file:line`, so extending alignment to them is a
  localized follow-up if needed.
