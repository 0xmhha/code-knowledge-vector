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
| `CanonicalID` | `canonical_id` | string | ckg's import-path-qualified symbol id (ADR-0001), inherited from the aligned ckg node; the stable CKG↔CKV join key (ADR-007). Empty for non-symbol / unaligned chunks |
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
| `pr_background` | PR-corpus chunk; additive, only in indexes built with `--include-pr-history` |
| `pr_solution` | PR-corpus chunk; additive, only with `--include-pr-history` |
| `commit_message` | PR-corpus chunk; additive, only with `--include-pr-history` |
| `invariant` | invariant statement paired with the source chunk it constrains (see `InvariantRef`) |
| `convention` | per-package summary of AST-derived idioms (error handling, logging, naming, concurrency) |

### `ChunkID` (deterministic)

```
ID = sha256(file + "\n" + start_line + ":" + end_line + "\n" + content_sha256)
```

Renaming the file changes the ID by design — rename tracking is the
caller's responsibility. `internal/build/reindex.go` handles git
renames by mapping them to delete-old + add-new.

`content_sha256` is the SHA-256 of the raw `Text` bytes, no whitespace
normalization. Computed once via `types.ContentSHA256`.

### `InvariantRef` / `InvariantTier`

Source: [`pkg/types/chunk.go::InvariantRef`, `::InvariantTier`](../pkg/types/chunk.go).

A source `Chunk` carries a list of `InvariantRef` back-pointers to the
`ChunkInvariant` chunk(s) extracted from inside or near it. Kept small so
attaching it to every chunk does not balloon storage.

| Field | JSON key | Type | Notes |
|-------|----------|------|-------|
| `ChunkID` | `chunk_id` | string | ID of the `ChunkInvariant` chunk |
| `Tier` | `tier` | `InvariantTier` | 1, 2, or 3 (see below) |
| `Marker` | `marker` | string | optional; e.g. `"CRITICAL"`, `"panic"` |

`InvariantTier` classifies how an invariant was detected (lower tier =
higher confidence; callers can filter by tier when noise tolerance is low):

| Value | Tier | Detection |
|-------|------|-----------|
| `InvariantTierExistingMarker` | 1 | existing marker (`// CRITICAL`, `// IMPORTANT`, `// WARNING`, `// Deprecated:`) |
| `InvariantTierNewMarker` | 2 | new convention marker (`// INVARIANT:`, `// CONSENSUS:`, `// SECURITY:`) |
| `InvariantTierHeuristic` | 3 | heuristic (`panic(...)` / `fmt.Errorf(...)` with policy keywords) |

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
    canonical_id    TEXT,
    text            TEXT NOT NULL
);
CREATE INDEX idx_chunks_file     ON chunks(file);
CREATE INDEX idx_chunks_lang     ON chunks(language);
CREATE INDEX idx_chunks_symbol   ON chunks(symbol_name);
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

Two mechanisms coexist:

1. **Legacy `ensureColumn` (in-Open ALTERs)** — for retroactive column
   additions before the formal framework existed (`is_test`,
   `recent_prs`). Stays as-is for already-applied changes; new
   migrations should use the framework below.
2. **Formal migration framework** (`internal/store/sqlitevec/migrate.go`)
   — versioned `.sql` files in `migrations/` are loaded via `go:embed`
   and applied in lexical order. A `schema_migrations` table tracks
   which versions have been applied along with the SHA-256 of their
   source; editing an applied migration is refused.

Auto-apply policy:

- Default: `Open()` applies pending migrations after `initSchema`. A
  backup `<dbpath>.bak.<unix-ts>` is taken first.
- Override: `CKV_DISABLE_AUTO_MIGRATE=1` switches to manual mode;
  `Open()` returns `ErrMigrationRequired` and the operator must run
  `ckv migrate --out PATH`.

CLI:

```
ckv migrate --out ./ckv-data            # apply pending
ckv migrate --out ./ckv-data --status   # show applied vs pending
ckv migrate --out ./ckv-data --dry-run  # preview without writing
ckv migrate --out ./ckv-data --no-backup
```

Authoring new migrations: see
[`internal/store/sqlitevec/migrations/README.md`](../internal/store/sqlitevec/migrations/README.md).

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
- `ckg_node_id` added, then **retired 2026-07-11** (ADR-007) — the
  positional CKG node id was a dead field never read as a lookup key;
  `canonical_id` is the sole CKG↔CKV join key. Removal did not bump 1.0:
  no reader consumed the column, so dropping it breaks nothing. Existing
  DBs carry an inert unused column until the next fresh rebuild.
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
