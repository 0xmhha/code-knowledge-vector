-- Migration 001: add policy-driven category + guidance to chunks.
--
-- These fields are populated at build time from a policy yaml. They
-- surface to the agent at query time so it can react to consensus /
-- state / crypto-sensitivity hints without re-discovering the path.
--
-- Both columns default to empty so the migration is non-destructive
-- on existing rows; an empty value means "unclassified by the loaded
-- policy" (equivalent to "no policy file in use").

ALTER TABLE chunks ADD COLUMN category TEXT NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN guidance TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_chunks_category ON chunks(category);
