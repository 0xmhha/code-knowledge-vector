# ADR-001: sqlite-vec for embedded vector storage

**Status**: Accepted
**Date**: 2026-04

## Context

CKV indexes embeddings on the developer's machine, ships as a single
Go binary, and is meant to be embeddable inside larger orchestrators
(CKS, future SDK consumers). The storage layer for vectors had to:

1. **Have no separate server process.** A dev tool that requires
   `docker-compose up` to read an index is dead on arrival for the
   "drop into any repo and run `ckv build`" experience.
2. **Support cosine-distance search out of the box** with reasonable
   recall on ≤1M chunks. CKV doesn't pretend to be a billion-scale
   vector DB; the realistic ceiling per repo is well below 1M.
3. **Run inside one process, one file on disk.** Backup, sync, and
   inspection should be `cp <file>` and `sqlite3 <file>`, nothing more.
4. **Have a Go binding that builds via cgo without exotic setup.**
   We already ship cgo (sqlite, tokenizers); adding another C++ build
   step would compound the cross-compile pain.

Alternatives considered:

- **pgvector**: requires Postgres — a separate process, network
  socket, lifecycle problem. Out of bounds for an embedded tool.
- **chroma**: separate process, Python-native. Same disqualification
  as pgvector, plus a different language runtime.
- **milvus**: operationally heavy, billion-scale storage, way larger
  than CKV's needs.
- **Implementing a flat-index search in Go**: feasible at small scale
  but loses cosine sqlite-vec gets out of the box, and we'd own the
  recall correctness ourselves.

## Decision

Use `sqlite-vec` (vec0 virtual tables) as the on-disk vector store,
accessed via `github.com/asg017/sqlite-vec-go-bindings` (cgo).

The store layer (`internal/store/sqlitevec/`) hides the details from
the rest of CKV — callers see `Upsert` / `Search` / `DeleteByFile` /
`SetManifest` and never touch SQL or sqlite-vec syntax.

## Consequences

**Good**:
- One file on disk: `<out>/vector.db`. Move it, back it up, grep it.
- No daemon, no port, no auth — same security boundary as the source
  tree it indexes.
- SQL is still available next to the vec0 table, so chunk metadata
  (file, language, symbol, commit_hash) sits in a real `chunks` table
  joined to the vector index by chunk ID. Filtering by language or
  symbol kind is a `WHERE` clause, not a separate index.

**Accepted costs**:
- cgo dependency. Cross-builds need a C toolchain — already true for
  tokenizers, so the marginal cost is small.
- Single-process write. CKV is single-writer by design; this matches.
- 1M-chunk practical ceiling. A monorepo with >1M chunks would force
  sharding or a different store; we accept that as a future problem.

**Closed off**:
- Multi-tenant shared vector stores. We index one repo per `--out`
  directory and call it done.
- Distributed search across many CKV instances. CKS-side fusion (see
  ADR-003) handles cross-corpus retrieval when needed.
