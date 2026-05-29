-- Migration 002: store extracted invariants on each source chunk.
--
-- The Invariants []InvariantRef field on Chunk is persisted as JSON in
-- this column. Empty string means "no invariants detected" — identical
-- semantics to an unmatched policy category.
--
-- The ChunkInvariant chunks themselves are full chunks in the chunks
-- table (chunk_kind = 'invariant'); this column only stores the
-- back-pointer list so the agent can navigate source ↔ invariant
-- without a separate join table.

ALTER TABLE chunks ADD COLUMN invariants TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_chunks_kind ON chunks(chunk_kind);
