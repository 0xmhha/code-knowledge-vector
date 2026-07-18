# Code Knowledge Vector (CKV)

Semantic code search over a local vector index. CKV indexes a source
repository as embedding vectors at function / type / heading
granularity, stores them in an embedded SQLite + `sqlite-vec`
database, and serves retrieval over a CLI, an in-process Go API, and
an MCP server. The companion project
[`code-knowledge-graph`](https://github.com/0xmhha/code-knowledge-graph)
(CKG) provides symbol-graph search; the two are designed to be
combined by larger systems (CKS) for hybrid retrieval.

> **Resuming work on a different machine or in a new session?**
> Start with [`docs/session-handoff-2026-05-23.md`](docs/session-handoff-2026-05-23.md)
> — it carries the prereq checklist, env-var matrix, current decision
> state, and the next-Wave entry conditions in a single document.

## Features

- **Languages**: Go (`go/parser`), TypeScript / TSX, JavaScript / JSX / MJS / CJS, Solidity, Markdown.
- **Embedders**: `mock` (no system dependencies, deterministic feature-hash) and `bgeonnx` (ONNX Runtime + HuggingFace tokenizers, BERT-class models).
- **CLI**: `build`, `query`, `eval`, `freshness`, `mcp`, `model`.
- **MCP server**: stdio JSON-RPC. Tools: `cks.context.semantic_search`, `cks.ops.health`, `cks.ops.warmup`, `cks.ops.get_freshness`. Every response carries a top-level `schema_version`.
- **Go API**: import `github.com/0xmhha/code-knowledge-vector/pkg/ckv` for `Open` / `SemanticSearch` / `Warmup` / `Manifest` / `Close` in the calling process.
- **Operational**: host memory pre-check + adaptive batching (`CKV_MEM_GUARD`), CoreML execution provider tuning on macOS (`CKV_COREML_*`), ORT thread overrides, panic-safe MCP middleware.

## Quickstart

### CLI with the mock embedder (no system dependencies)

```bash
make build
./bin/ckv build --src /path/to/repo --out ./ckv-data
./bin/ckv query "TCP socket bind on port" --out ./ckv-data
```

### CLI with `bgeonnx` (real semantic embeddings)

Requires `libonnxruntime`, `libtokenizers.a`, and a downloaded model.
See [`docs/d1-installation-guide.md`](docs/d1-installation-guide.md).

```bash
CGO_LDFLAGS="-L$HOME/lib" go build -tags bgeonnx -o ./bin/ckv ./cmd/ckv
./bin/ckv build --embedder bgeonnx --src /path/to/repo --out ./ckv-data
./bin/ckv query "..." --embedder bgeonnx --out ./ckv-data
```

### In-process Go API

```go
import (
    "context"

    "github.com/0xmhha/code-knowledge-vector/pkg/ckv"
)

func search() error {
    engine, err := ckv.Open(".ckv-data", ckv.OpenOptions{
        Embedder: ckv.MockEmbedder(),
    })
    if err != nil {
        return err
    }
    defer engine.Close()

    if err := engine.Warmup(context.Background()); err != nil {
        // log and continue; first query will pay the cost instead
    }

    resp, err := engine.SemanticSearch(context.Background(),
        "TCP socket bind on port",
        ckv.SearchOptions{K: 5})
    if err != nil {
        return err
    }
    _ = resp.Hits // []ckv.Hit — citation, snippet, score per result
    return nil
}
```

See [`docs/embedder-integration.md`](docs/embedder-integration.md)
for the production embedder path, environment overrides, and
migration off subprocess MCP.

### MCP server

```bash
./bin/ckv mcp --out ./ckv-data
```

Speaks MCP JSON-RPC over stdio. Register with Claude Code:

```bash
claude mcp add ckv --command "$(pwd)/bin/ckv mcp --out=$(pwd)/ckv-data"
```

## Supported languages

| Language | Parser | Extensions |
|---|---|---|
| Go | `go/parser` | `.go` |
| TypeScript | tree-sitter | `.ts`, `.tsx` |
| JavaScript | tree-sitter (via TS grammar) | `.js`, `.jsx`, `.mjs`, `.cjs` |
| Solidity | tree-sitter | `.sol` |
| Markdown | heading-section chunks | `.md`, `.markdown` |

## Embedders

| Backend | Build tag | System deps | Use case |
|---|---|---|---|
| `mock` | none (default) | none | tests, smoke checks — no semantic signal |
| `bgeonnx` | `-tags bgeonnx` | `libonnxruntime`, `libtokenizers.a`, model files | production semantic search |

The `bgeonnx` registry contains two model configs:
`bge-large-en-v1.5` (default, BERT-class, 1024 dim) and
`embeddinggemma-300m` (Gemma-class, 768 dim). Model files live under
`~/.cache/ckv/models/<name>/`. The Gemma config is registered; the
weights are not bundled with this repository.

## Architecture

```
ckv build   discover ── parse ── chunk ── embed ── sqlite-vec
                                                    │
                                                    └─ manifest.json
ckv query   embed(intent) ── store.Search ── citation enforce ── snippet ── top-K
ckv mcp     JSON-RPC stdio ── cks.context.* / cks.ops.*
pkg/ckv     Engine wrapper around internal/query (in-process consumers)
```

## Build requirements

- Go 1.25+
- CGO enabled (for `sqlite-vec` via `mattn/go-sqlite3`)
- `gcc` or `clang` toolchain
- `libonnxruntime` and `libtokenizers.a` only when building with
  `-tags bgeonnx`

## Documentation

- [`docs/README.md`](docs/README.md) — **documentation index** (living reference vs design record vs archive). Start here.
- [`docs/remaining.md`](docs/remaining.md) — **work status single source of truth** (code-verified).
- [`docs/session-handoff-2026-06-29.md`](docs/session-handoff-2026-06-29.md) — narrative/background entry point (work status defers to `remaining.md`).
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), [`docs/SCHEMA.md`](docs/SCHEMA.md) — module map + chunk/store schema.
- [`docs/mcp-tools.md`](docs/mcp-tools.md) — MCP tool I/O specs (19 tools).
- [`docs/embedder-integration.md`](docs/embedder-integration.md) — consumer integration: in-process API, MCP, env overrides.
- [`docs/d1-installation-guide.md`](docs/d1-installation-guide.md) — building the optional `bgeonnx`/ONNX path (default runtime is ollama/bge-m3).
- [`docs/eval-metrics.md`](docs/eval-metrics.md) — `ckv eval` metrics.
- [`docs/adr/`](docs/adr/) — architecture decision records (001–010).

"What is true now" = code + git.

## License

AGPL-3.0. See [`LICENSE`](LICENSE).
