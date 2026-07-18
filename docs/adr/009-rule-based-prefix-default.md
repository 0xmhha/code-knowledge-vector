# ADR-009: Rule-based contextual prefix is the default; LLM prefix and multi-granularity are deferred

**Status**: Accepted
**Date**: 2026-07-12

## Context

The retrieval-quality roadmap (`docs/archive/retrieval-quality-roadmap.md`) proposed
several embed-text levers to raise recall, drawn from industry best practice:

- **Phase D — contextual prefix** (Anthropic Contextual Retrieval): prepend a
  short situating description to each chunk before embedding.
  - **D.1 rule-based**: a deterministic one-line prefix
    (`language: X. file: Y. symbol: Z.`) — no model call.
  - **D.2 LLM-generated**: an LLM writes a one-sentence description per chunk.
- **Phase B — multi-granularity**: index coarse chunks (class/struct body, whole
  file) alongside the fine symbol chunks so file/module-level queries have a
  coarse target.

These are the roadmap's highest-ROI quality levers. The open question was which
actually earn their cost (D.2 ≈ 19× build time; Phase B ≈ 2–3× chunks and −50%
throughput), and the default `queries.yaml` fixture was at the retrieval ceiling
(bge-m3 recall@5 0.98), so it could not discriminate.

A hard discrimination fixture reopened a measurable band
(`docs/archive/eval-hard-fixture-2026-07-12.md`, N=24: bge-m3 recall@1 0.58, MRR 0.669),
and three measurements then settled the levers:

- **Prefix sweep** (`docs/archive/prefix-lever-sweep-2026-07-12.md`): raw vs D.1 vs D.2,
  both fixtures. D.1 wins everywhere — +0.16 recall@1 over raw (easy and hard
  alike). D.2 does **not** beat D.1 even off the ceiling (hard recall@1 0.54 <
  0.58, MRR 0.665 ≈ 0.669): the LLM prose dilutes the rule-based prefix's exact
  symbol/file tokens.
- **D.2 PoC** (`docs/archive/llm-contextual-prefix-poc-2026-07-12.md`): confirmed the
  same on the easy fixture at 19× build cost.
- **Phase B probe** (`docs/archive/phase-b-multigran-probe-2026-07-12.md`): an opt-in
  whole-file coarse chunk **hurt** — on a coarse-query probe recall@3 dropped
  1.00 → 0.88 (the coarse chunk displaces the file's finer chunks); on the hard
  symbol fixture recall@5 dropped 0.88 → 0.79. The baseline already answered
  coarse queries (recall@3 1.00) because `file_header` covers these small files —
  no headroom.

## Decision

**Keep the D.1 rule-based contextual prefix ON by default. Ship the D.2 LLM
prefix and the Phase B multi-granularity chunk as opt-in, off-by-default levers.
Do not build the full Phase B feature.**

- **D.1 rule-based prefix** is the default embed text
  (`chunk.BuildEmbedText`) — measured best on every fixture, no model call,
  ~5% throughput cost.
- **D.2 LLM prefix** stays behind `--llm-prefix-model` (off by default). When
  enabled, the composition is LLM prose + rule-based + raw (the combined form
  beat LLM-alone). It underperforms D.1 on recall@1/MRR, so it is not a default.
- **Phase B / `file_full`** stays behind `CKV_EXPERIMENTAL_FILE_FULL` (off by
  default). The full feature (class-body extraction, query-time granularity
  filter) is **not** built.

## Consequences

**Good**:
- The default is the measured best on this corpus at the lowest cost — the
  rule-based prefix's exact symbol/file tokens are exactly what the north-star
  (precise recall@1) rewards.
- The two rejected levers stay in-tree, gated and tested, so a future
  large-corpus re-test flips a flag instead of re-implementing.
- The negative results are recorded, so "why not Anthropic contextual
  retrieval / hierarchical chunking?" has a measured answer and does not get
  re-proposed from first principles.

**Accepted costs / caveats**:
- All measurements are on the small, single-type-per-file `testdata/sample`
  corpus with bge-m3 and pure-vector eval (no BM25). The decision is scoped to
  that regime; a **large, heterogeneous corpus** is the standing re-test
  condition, where (a) `file_header` no longer covers whole files, (b)
  multi-method class bodies genuinely differ from fine + file chunks, and (c) a
  stronger generator (e.g. Claude) + contextual BM25 could change the D.2
  verdict. The `file_full` prototype only approximates the class-body axis.
- Keeping two off-by-default levers is a small maintenance surface (each is
  gated and unit-tested).

## Realization

- D.1: `internal/chunk/prefix.go` `BuildEmbedText` (default). Toggle raw via
  `CKV_DISABLE_CONTEXTUAL_PREFIX=1`.
- D.2: `internal/llmprefix/` + `--llm-prefix-model` (build/reindex), PR #36.
- Phase B probe: `internal/chunk` `ChunkFileFull` / `Options.IncludeFileFull`,
  gated by `CKV_EXPERIMENTAL_FILE_FULL`, PR #40.
- Fixtures: `testdata/queries-hard.yaml` (PR #38), `testdata/queries-coarse.yaml`
  (PR #40); the easy `queries.yaml` stays the CI regression gate.
- Measurements: `prefix-lever-sweep-2026-07-12.md`,
  `llm-contextual-prefix-poc-2026-07-12.md`,
  `phase-b-multigran-probe-2026-07-12.md`, `eval-hard-fixture-2026-07-12.md`.
