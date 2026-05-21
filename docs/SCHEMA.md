# Schema

The on-disk and in-memory contracts for everything CKV persists or
exposes to consumers. **Single source of truth: the Go code linked
from each section.** This doc is the human-readable view; if it
disagrees with the code, the code wins.

Schema version: **1.0** (`internal/store/sqlitevec/store.go::SchemaVersion`).
Plan §4 anchors CKV at 1.0; CKG runs 1.7 independently.

Out of scope today (deferred to S2): working memory entry schema
(`cks.memory.*`), sanitize report schema (UC-V13). Those land as
code in their own modules; this doc grows when they do.

## Contents

1. [`Chunk` — the indexable record](#1-chunk--the-indexable-record)
2. [`Citation` — what every hit cites](#2-citation--what-every-hit-cites)
3. [`Manifest` — index identity](#3-manifest--index-identity)
4. [SQLite layout](#4-sqlite-layout)
5. [Versioning rules](#5-versioning-rules)

---

## 1. `Chunk` — the indexable record

Source: [`pkg/types/chunk.go`](../pkg/types/chunk.go).

A `Chunk` is one embeddable region: a function/method, a struct
declaration, a markdown heading section, or the top-N-lines file
header. Every chunk has a deterministic ID and carries enough
metadata for CKG cross-tool alignment.

### Fields

| Field | JSON key | Type | Notes |
|-------|----------|------|-------|
| `ID` | `id` | string | `ChunkID(file, start_line, end_line, content_sha256)` — deterministic |
| `File` | `file` | string | Repo-relative, forward-slash |
| `StartLine` | `start_line` | int | 1-based, inclusive |
| `EndLine` | `end_line` | int | 1-based, inclusive |
| `Language` | `language` | string | `"go"` / `"typescript"` / `"javascript"` / `"solidity"` / `"markdown"` |
| `IsTest` | `is_test` | bool | populated by `IsTestPath` (`_test.go`, `*.test.ts`, `*.spec.ts`, `*.t.sol`, `test/...`); omitted when false |
| `SymbolName` | `symbol_name` | string | qualified when known (`"Server.Listen"`); empty for file headers |
| `SymbolKind` | `symbol_kind` | `SymbolKind` | see enum below |
| `ChunkKind` | `chunk_kind` | `ChunkKind` | see enum below |
| `CommitHash` | `commit_hash` | string | git HEAD at indexing time |
| `ContentSHA256` | `content_sha256` | string | `sha256(Text)` — drives `ID` |
| `CKGNodeID` | `ckg_node_id` | string | 1:1 with CKG node when symbol-aligned |
| `Text` | `text` | string | raw chunk source (for re-embedding / display) |

### `SymbolKind` (enum)

Source: `pkg/types/chunk.go::SymbolKind` constants.

| Value | Languages | Notes |
|-------|-----------|-------|
| `Function` | go, ts, js | top-level function or arrow |
| `Method` | all | bound to a receiver / class |
| `Type` | go, ts | named alias / `type Foo = ...` |
| `Struct` | go, ts | struct / class / interface body |
| `Interface` | go, ts, sol | interface declaration |
| `Contract` | sol | Solidity contract |
| `Event` | sol | Solidity event (TBD §10 q1) |
| `Modifier` | sol | Solidity modifier (TBD §10 q1) |
| `FileHeader` | all source | top-N-lines slice |
| `DocSection` | markdown | heading-bounded section |
| `ADRSection` | markdown | section inside `docs/adr/*.md` or `ADR-*.md` |

### `ChunkKind` (enum)

Source: `pkg/types/chunk.go::ChunkKind` constants.

| Value | Meaning |
|-------|---------|
| `symbol` | whole function/method/type |
| `function_split` | sub-chunk of a long function (Phase A, planned) |
| `file_header` | leading-N-lines slice (default 50) |
| `doc` | markdown heading section (covers `DocSection` and `ADRSection`) |

### `ChunkID` (deterministic)

```
ID = sha256(file + "\n" + start_line + ":" + end_line + "\n" + content_sha256)
```

Renaming the file changes the ID by design — rename tracking is the
caller's responsibility. `internal/build/reindex.go` handles git
renames by mapping them to delete-old + add-new.

`content_sha256` is the SHA-256 of the raw `Text` bytes, no whitespace
normalization. Computed once via `types.ContentSHA256`.

---

## 2. `Citation` — what every hit cites

Source: [`pkg/types/chunk.go::Citation`](../pkg/types/chunk.go).

| Field | JSON key | Type |
|-------|----------|------|
| `File` | `file` | string |
| `StartLine` | `start_line` | int |
| `EndLine` | `end_line` | int |
| `CommitHash` | `commit_hash` | string |

CKG uses the same shape so hybrid responses merge without translation
(plan §10.1). Every `Hit` exposes a `Citation` via `Chunk.Citation()`.

---

## 3. `Manifest` — index identity

Source: [`internal/manifest/manifest.go::Manifest`](../internal/manifest/manifest.go).

Written to `<out>/manifest.json` (atomic via tmp + rename) and mirrored
into the `manifest` SQLite table so freshness checks can read either
without coordinating opens.

| JSON key | Type | Notes |
|----------|------|-------|
| `schema_version` | string | `"1.0"` (plan §4 anchor) |
| `ckv_version` | string | `"dev"` or release tag (set via `-ldflags`) |
| `built_at` | string (RFC3339) | UTC indexing timestamp |
| `src_root` | string | absolute path of indexed source |
| `src_commit` | string | git HEAD at indexing time |
| `indexed_head` | string | alias of `src_commit` for back-compat with featurelist §1.6 |
| `embedding_model` | string | e.g. `"bge-large-en-v1.5"`, `"mock-feature-hash-v1"` |
| `embedding_dim` | int | vector dimension (must match `Embedder.Dimension()`) |
| `embedding_checksum` | string | optional model SHA-256 |
| `embedding_normalize` | string | `"l2"` / `"none"` |
| `chunk_count` | int | best-effort cumulative count; rebuild for authoritative |
| `languages` | map[string]int | language → chunk count |
| `ckvignore` | []string | extra ignore patterns recorded for transparency |

Mismatch on Open between manifest and live `Embedder` is fatal:
returns `ErrIndexUnavailable` (model/dim) or `ErrEmbedderMismatch`
(reindex with different embedder) — see
[`internal/query/errors.go`](../internal/query/errors.go).

### CKG ↔ CKV manifest compatibility

Shared keys (plan §4 alignment):

| Key | CKG | CKV |
|-----|-----|-----|
| `src_root` | ✅ | ✅ |
| `src_commit` | ✅ | ✅ |
| `schema_version` | 1.7 | 1.0 (independent) |
| `built_at` | ✅ | ✅ |

CKS Orchestrator compares `src_commit` across the two manifests to
decide if the indexes are synchronized.

---

## 4. SQLite layout

Source: [`internal/store/sqlitevec/store.go::initSchema`](../internal/store/sqlitevec/store.go).

Two regular tables plus one sqlite-vec virtual table, all in
`<out>/vector.db`:

```sql
-- Identity mirror of manifest.json.
CREATE TABLE manifest (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
-- keys: schema_version, embedding_model, embedding_dim,
--       embedding_normalize, indexed_head, built_at

-- Chunk metadata. Joined to vectors by id.
CREATE TABLE chunks (
    id              TEXT PRIMARY KEY,
    file            TEXT NOT NULL,
    start_line      INTEGER NOT NULL,
    end_line        INTEGER NOT NULL,
    language        TEXT NOT NULL,
    is_test         INTEGER NOT NULL DEFAULT 0,
    symbol_name     TEXT,
    symbol_kind     TEXT,
    chunk_kind      TEXT NOT NULL,
    commit_hash     TEXT NOT NULL,
    content_sha256  TEXT NOT NULL,
    ckg_node_id     TEXT,
    text            TEXT NOT NULL
);
CREATE INDEX idx_chunks_file     ON chunks(file);
CREATE INDEX idx_chunks_lang     ON chunks(language);
CREATE INDEX idx_chunks_symbol   ON chunks(symbol_name);
CREATE INDEX idx_chunks_ckg_node ON chunks(ckg_node_id);
CREATE INDEX idx_chunks_is_test  ON chunks(is_test);

-- Vectors (sqlite-vec). Dimension baked into DDL.
CREATE VIRTUAL TABLE chunk_vec USING vec0(
    chunk_id  TEXT PRIMARY KEY,
    embedding FLOAT[<dim>]      -- e.g. 1024 for bge-large-en-v1.5
);
```

### Joining vectors to metadata

```sql
SELECT c.file, c.start_line, c.end_line, c.symbol_name
FROM chunks c
JOIN chunk_vec v ON v.chunk_id = c.id
WHERE v.embedding MATCH :query_vector
  AND k = :top_k
ORDER BY v.distance;
```

`internal/store/sqlitevec/store.go::Search` runs this with the
`types.Filter` clauses applied to `chunks`.

### Migrations

`store.go::ensureColumn` is the idempotent ALTER helper. Pre-FU-10
indexes lacked `is_test`; Open detects the missing column via
`PRAGMA table_info` and runs `ALTER TABLE chunks ADD COLUMN is_test`
on first read. No separate `ckv migrate` step.

---

## 5. Versioning rules

The `schema_version` field follows breaking-vs-additive convention:

- **Bump on breaking changes**: renamed fields, removed fields,
  changed semantics. Old readers no longer interpret the file
  correctly.
- **Stay on the same version for additive `omitempty` fields**.
  Old readers see them as zero values — no breakage.

Examples that did *not* bump 1.0:
- `is_test` added to `chunks` (additive; default false matches old
  rows).
- `ckg_node_id` added (additive; nullable column).
- `ContentSHA256` exposed as a `Chunk` JSON field (the column was
  always there; only the Go struct widened).

Bumping requires a coordinated CKV release plus a migration path
(read-old-write-new for one release, then drop the compatibility
shim). No such bump has happened on CKV's 1.0 line.

---

## Cross-references

- [`pkg/types/chunk.go`](../pkg/types/chunk.go) — `Chunk`, `Citation`,
  `ChunkID`, `ContentSHA256`
- [`internal/manifest/manifest.go`](../internal/manifest/manifest.go) — `Manifest`
- [`internal/store/sqlitevec/store.go`](../internal/store/sqlitevec/store.go) — DDL + migrations
- [`internal/query/errors.go`](../internal/query/errors.go) — error model
- [`plan-S1-ckv.md §4`](./plan-S1-ckv.md) — original schema design
  notes (predates some additive changes)
- [`ADR-001`](./adr/001-sqlite-vec-storage.md) — *why* sqlite-vec
