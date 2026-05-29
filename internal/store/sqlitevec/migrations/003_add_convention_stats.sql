-- Migration 003: per-package convention statistics on convention chunks.
--
-- ChunkConvention chunks (chunk_kind = 'convention') carry a
-- human-readable summary in the existing text column. The raw stats
-- structure (map[string]any) lives in this dedicated TEXT column so
-- the agent can query it directly without parsing the text body.
--
-- Empty string means "stats not computed" (mirrors guidance / invariants
-- columns). Non-convention chunks always have empty convention_stats.

ALTER TABLE chunks ADD COLUMN convention_stats TEXT NOT NULL DEFAULT '';
