# Architecture

This document is the **internal-developer map** of CKV: where each
package lives, how the pipelines wire them up, and which decisions
(ADRs) shaped the layout. Consumer-facing material lives in
[`README.md`](../README.md) (quickstart, supported languages,
embedders); cross-tool integration lives in
[`plan-S1-ckv.md §7`](./plan-S1-ckv.md).

## 4-Layer position

CKV is **Layer 1: Vector Retrieval** in the wider Code Knowledge stack:

| Layer | Owner | Role |
|------:|-------|------|
| 0 | CKG (`code-knowledge-graph`) | Symbolic graph + BM25 lexical search |
| **1** | **CKV (this repo)** | **Dense vector retrieval over code/docs** |
| 2 | CKS (`code-knowledge-system`) | Multiplex + RRF fusion + working memory |
| 3 | Agent / IDE / CLI consumer | LLM coding agent, CLI, or IDE plugin |

CKV is the **simplest** of the four: one process, one on-disk database
(sqlite-vec), no cross-tool dependency. Hybrid retrieval (BM25 + dense)
happens at Layer 2 — see [ADR-003](./adr/003-bm25-dual-track.md).

## Package layout

```
code-knowledge-vector/
├── cmd/ckv/           CLI entry — only place that touches os.Args / stdout
├── pkg/               Public surface, semver-stable
│   ├── types/         Shared data contracts (Chunk, Hit, Filter, Embedder, VectorStore)
│   ├── ckv/           In-process Go API (Open, SemanticSearch, Warmup, ...)
│   └── mcp/           MCP server (`ckv mcp` + the surface CKS imports)
└── internal/          Implementation; no semver guarantees
    ├── discover/      File walker, ignore patterns, language classification
    ├── parse/         Language parsers (golang/typescript/javascript/solidity/markdown)
    ├── chunk/         Span → Chunk, file-header chunk, contextual prefix builder
    ├── embed/         Embedder backends: mock (in-tree), bgeonnx (ONNX Runtime)
    ├── store/sqlitevec/  sqlite-vec store: Upsert / Search / DeleteByFile / SetManifest
    ├── manifest/      manifest.json read/write (atomic via tmp + rename)
    ├── build/         Build & Reindex orchestrators
    ├── query/         Read path: Engine.Search, DensityAdjust, errors (8.4 error model)
    ├── freshness/     git HEAD diff vs manifest.IndexedHead
    ├── footprint/     Structured JSONL audit log
    ├── projectcfg/    ckv.yaml loader
    ├── eval/          Recall@K / MRR / citation-accuracy fixture runner
    ├── eval/prregress/  PR-regression LLM-judge eval mode
    └── judge/         LLM-as-judge CLI wrapper
```

`pkg/types` has **no internal dependencies** by design — anything that
imports it stays portable to future CKS code. The dependency arrow
always points `internal/*` → `pkg/types`, never the reverse.

## Internal dependency graph

Inferred from `import` lines in the source tree:

```
                              cmd/ckv
                                 │
            ┌───────────────┬────┴────┬──────────────┬──────────────┐
            ▼               ▼         ▼              ▼              ▼
       internal/build  internal/query  pkg/mcp   internal/eval  internal/judge
            │               │           │
   ┌────────┼────────┐      │           │
   ▼        ▼        ▼      ▼           ▼
discover  parse   chunk  freshness   pkg/ckv
   │        │        │      │           │
   └────────┴───┬────┴──────┴─────┐     │
                ▼                 ▼     ▼
         internal/store/sqlitevec  internal/manifest
                            │
                            ▼
                       pkg/types
```

Key invariants:

- **`pkg/types` is a leaf.** No package it imports lives in this repo.
- **`internal/build` and `internal/query` are siblings**, not parent/
  child. Build owns the write path; query owns the read path; they
  share `pkg/types`, `internal/store/sqlitevec`, and `internal/manifest`.
- **`pkg/ckv` is the in-process facade** that re-exports `internal/query`
  types via Go type aliases (`SearchOptions = query.Options` etc.).
  External consumers import `pkg/ckv`, never `internal/query`.
- **`pkg/mcp` wraps `internal/query`** the same way but speaks
  JSON-RPC instead of Go function calls. Both surfaces share the
  same Engine.
- **No cycles.** `go build ./...` enforces this — the layout above is
  the steady state.

## Pipelines

### Build pipeline (write path)

```
cmd/ckv/build.go::runBuild
  └─ internal/build/builder.go::Run
       ├─ internal/projectcfg     load ckv.yaml (optional)
       ├─ internal/discover       walk srcRoot → []File
       │     └─ DefaultIgnore + .ckvignore + DefaultSecretPatterns  (B9)
       ├─ for each file:
       │   ├─ internal/parse/<lang>   src → []SymbolSpan
       │   ├─ internal/chunk         []SymbolSpan → []Chunk
       │   │     ├─ file_header chunk (50 lines by default)
       │   │     ├─ symbol chunks
       │   │     └─ BuildEmbedText (Phase D.1 prefix)             (#6)
       │   ├─ Embedder.Embed         []text → [][]float32
       │   └─ store.Upsert           atomic chunks + vectors
       └─ internal/manifest::Save    manifest.json + DB-side mirror
```

Memory pre-check + adaptive batching live in `internal/build/memory*.go`
(refuses to start when free RAM is below the embedder's documented
headroom; halves the embed batch when free RAM drops mid-build).

### Query pipeline (read path)

```
cmd/ckv/query.go / pkg/mcp/server.go / pkg/ckv (any of the three)
  └─ internal/query/engine.go::Engine.Search
       ├─ Embedder.Embed(intent)             1 vector
       ├─ store.Search                        ANN top-K' (k * overfetchFactor)
       ├─ threshold drop                      below normalized cutoff → out
       ├─ EnforceCitations                    file existence under src_root
       │     └─ ErrCitationNotFound if all dropped   (B6)
       ├─ splitByTest                         primary vs examples (FU-10)
       ├─ DensityAdjustWith                   3-tier ladder, per-hit reporting  (B3)
       └─ Response{Hits, Examples, Metadata, Warnings}
```

The same Engine instance backs all three callers — CLI, MCP server,
and the in-process `pkg/ckv.Engine`. Concurrency-safe because the
store is read-only after Open.

### Reindex pipeline (incremental write path)

```
cmd/ckv/reindex.go::runReindex
  └─ internal/build/reindex.go::Reindex                            (#8)
       ├─ manifest.Load                       prev_head, embedder identity
       ├─ resolveChangeSet
       │     ├─ if Options.Files set         use it verbatim
       │     └─ else `git diff --name-status prev..HEAD`
       │           parseDiffNameStatus → {added, modified, deleted}
       ├─ for deleted: store.DeleteByFile
       ├─ for added+modified:
       │     ├─ classifyLanguageRel + discoverIgnored
       │     ├─ parse → chunk → store.DeleteByFile (purge) → embed → Upsert
       │     └─ (re-uses chunker + embedder + parsers from build)
       └─ manifest.Save                       new IndexedHead + BuiltAt
```

Embedder identity must match the manifest (model name + dimension);
mismatch returns `ErrEmbedderMismatch`. Mixing embeddings in one
store silently breaks retrieval.

## Decisions that shaped the layout

| Decision | ADR | Impact on package layout |
|----------|-----|--------------------------|
| sqlite-vec for storage | [001](./adr/001-sqlite-vec-storage.md) | `internal/store/sqlitevec/` is the only store; one file on disk |
| bge-large-en-v1.5 default | [002](./adr/002-bge-large-pivot.md) | `internal/embed/bgeonnx/` (BERT + CLS); Qwen2 adapter deferred |
| BM25 stays on CKS side | [003](./adr/003-bm25-dual-track.md) | No `internal/bm25/` package; `pkg/mcp.Server.Underlying()` is the integration point |
| `ckv reindex` promoted to S1.5 | [004](./adr/004-ckv-reindex-s1-5-promotion.md) | `internal/build/reindex.go` ships in S1; not blocked behind S2 |
| CoreML MLProgram + static shapes | [005](./adr/005-coreml-mlprogram-static-shapes.md) | `internal/embed/bgeonnx/session_impl.go` env-var knobs |

## Cross-tool integration

`pkg/mcp.Server.Underlying()` exposes the underlying `*server.MCPServer`
so CKS (a separate repo) can register its own tools next to CKV's
without forking. The relationship is documented in
[`plan-S1-ckv.md §7`](./plan-S1-ckv.md) — CKV is "vector retrieval over
a code repo," nothing more.

`pkg/ckv` is the alternative integration path for callers that want
in-process Go API access (single binary, no subprocess hop). See
[`embedder-integration.md`](./embedder-integration.md) for the
consumer guide.

## Out of scope

- **Working memory** (`cks.memory.*`): planned for S2; lives in CKS.
- **Sanitize** (`internal/sanitize/`): planned for S2; UC-V13.
- **mTLS / policy enforcement**: planned for S6.
- **HTTP API** (`ckv serve`): planned for S2.

Tracked in [`backlog.md`](./backlog.md) (categories C and D) and
[`featurelist.md §0.1`](./featurelist.md).
