# Code Knowledge Vector (CKV)

> **Status**: pre-α (S1-W1 skeleton)
> **Sibling**: [`code-knowledge-graph`](https://github.com/0xmhha/code-knowledge-graph) (CKG)
> **System**: CKS Layer 1 — Vector DB + semantic retrieval

CKV indexes a code repository as embedding vectors (function/method/contract granularity), persists them in an embedded SQLite + `sqlite-vec` store, and exposes semantic search via CLI and MCP. Designed to be imported by **CKS** alongside CKG for hybrid (vector + graph + BM25) retrieval.

See [`docs/use-cases.md`](docs/use-cases.md), [`docs/featurelist.md`](docs/featurelist.md), and [`docs/plan-S1-ckv.md`](docs/plan-S1-ckv.md) for the full design.

## Quickstart

```bash
make build
./bin/ckv --help

# Build an index over a Go repo
./bin/ckv build --src /path/to/repo --out ./ckv-data

# Ask a natural-language question (W3+)
./bin/ckv query "TCP socket bind on port"

# Start MCP server (W3+)
./bin/ckv mcp
```

## S1 scope

| Milestone | Status |
|---|---|
| M0 — Skeleton (Cobra CLI, Make, go.mod) | in progress |
| M1 — Indexer α (tree-sitter + chunking) | planned |
| M2 — Vector store (sqlite-vec, embedded) | planned |
| M3 — Query α (`ckv query`) | planned |
| M5 — MCP server (`ckv mcp`) + `query_code` | planned |
| M6 (partial) — RRF hybrid hook | planned |

See [`docs/plan-S1-ckv.md`](docs/plan-S1-ckv.md) for the week-by-week plan.

## Architecture (briefly)

```
ckv build → discover → parse (tree-sitter) → chunk → embed → store (sqlite-vec)
                                                     │
                                                     └→ manifest.json
ckv query  → embed(query) → store.Search → citation enforce → ranked hits
ckv mcp    → JSON-RPC stdio → cks.context.* / cks.ops.* tools
```

Detailed schema and decision matrix: [`docs/plan-S1-ckv.md`](docs/plan-S1-ckv.md).

## Build requirements

- Go 1.23+ (target 1.25 once CKG dependency lands in W3)
- CGO enabled (for `sqlite-vec` via `mattn/go-sqlite3`)
- `gcc` or `clang` toolchain

## License

See [`LICENSE`](LICENSE).
