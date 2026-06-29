-- Migration 004: flow-corpus metadata columns.
--
-- A curated flow corpus (loaded via --flow-corpus) adds flow_step / flow_spine
-- chunks and curated invariants describing "현상 → 원인" causal paths. Their
-- structured metadata lives in dedicated columns so the agent can query flow
-- shape without parsing the text body:
--
--   flow_meta   — JSON of FlowStepMeta (flow_step) or FlowSpineMeta (flow_spine)
--   enforced_at — JSON of []EnforcePoint on curated invariant chunks
--   provenance  — invariant origin: "auto" (extracted) | "curated" (corpus)
--
-- Empty string means "not a flow / not a curated chunk" (mirrors the
-- guidance / invariants / convention_stats columns). Existing chunks get the
-- default on upgrade; the load pipeline that populates these lands in Phase B.

ALTER TABLE chunks ADD COLUMN flow_meta TEXT NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN enforced_at TEXT NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN provenance TEXT NOT NULL DEFAULT '';
