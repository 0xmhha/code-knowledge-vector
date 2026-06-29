# ADR-007: canonical_id is the CKG↔CKV join key

**Status**: Accepted
**Date**: 2026-06-29

## Context

CKV chunks need a stable key to join against CKG nodes so that CKS can
fuse vector hits (CKV) with structural/graph facts (CKG) for the same
symbol. Two candidate keys existed in the code:

- **file:line alignment** (`internal/ckgalign`, PR #4) — positional, but
  drifts the moment code moves.
- **CKG node ID** = `sha256(qname|lang|startByte)` — stable per build but
  **positional** (changes when the symbol moves in the file).
- **canonical_id** — a semantic identifier owned by CKG
  (CKG `docs/adr/0001-canonical-symbol-id.md`): Go =
  `<importpath>.(*Recv).Method`, sol/ts/proto = `<relpath>:<qname>`.

PR #9 made CKV inherit `canonical_id` verbatim from the positionally
aligned CKG node onto each chunk (additive; embed text unchanged, no
re-embed). The four-session coordination (2026-06-29,
`coordination-prompts-2026-06-29.md`) then had to settle which key is
*the* contract.

CKG (§1-R/§1-R2), CKS (§2-R/§2-R2) and CKV (§3-R-CKV) converged:

- canonical_id is **rebuild-invariant and line-move-invariant** (the only
  candidate that is). CKG owns the format; CKV already inherits it byte-
  for-byte; CKS already has `FindByCanonicalID` in `ckgclient`.
- Two caveats surfaced and must be honored:
  1. **Population gate**: the *column* exists from CKG cache schema 1.16,
     but the *value* is only populated at cache **SchemaVersion ≥ 1.19**
     (current 1.22). A PRAGMA column-existence probe passes 1.16–1.18
     graphs whose canonical_id is NULL — joining on that NULL fails
     silently.
  2. **`@<line>` suffix**: when the same canonical_id repeats in one file,
     CKG appends `@<line>` (CKG refinement B3). This suffix is the only
     position-dependent part.

## Decision

**canonical_id is the CKG↔CKV join key.** No separate symbol-ID
normalization rule is introduced (closes backlog B7 as "no normalization
needed"). Non-symbol nodes (CallSite/IfStmt/Loop/…), which carry no
canonical_id by design, fall back to the positional node ID.

CKV must stop trusting the PRAGMA column-existence probe alone (which
passes 1.16–1.18 graphs whose canonical_id is empty). Two gate points:

- **In `internal/ckgalign` (per build)**: probe for actual *population* —
  the column exists AND at least one non-empty canonical_id value is
  present. `ckgalign` only opens `graph.db`, so population detection is
  the self-contained, robust proxy for "a ≥ 1.19 cache". When unpopulated,
  expose it (`CanonicalAvailable() == false`) so the build surfaces
  "canonical_id unavailable" instead of inheriting empty join keys.
- **At the wiring/measurement layer**: assert the CKG build's published
  manifest `schema_version >= 1.19` (and graph.db sha) before pointing CKV
  or CKS at a graph. The exact manifest format is CKG-published; this
  assertion is a follow-up once that format is fixed.

Either way: never join on a NULL/empty canonical_id.

A shared integration fixture (small fixed repo) asserts that CKV chunk and
CKG node carry the same canonical_id, with both caveats (≥1.19 gate,
`@<line>` duplicate) encoded as cases.

## Consequences

**Good**:
- The join survives edits: a symbol keeps its key across rebuilds and line
  moves, so CKS fusion stays correct as the repo changes.
- No bespoke normalization layer to maintain on the CKV side; CKG owns the
  format (single source per ADR-0001).
- The silent-NULL-join failure mode is structurally prevented by the
  ≥1.19 gate.

**Accepted costs**:
- CKV depends on a CKG build artifact being ≥1.19; measurement and
  production wiring must assert the manifest `schema_version` first.
- proto symbols exist in CKG (`LANG=auto`) but CKV does not parse proto,
  so a match-rate measured across the pair must be scoped to CKV's shared
  languages (go/sol/ts/js) — proto nodes have no CKV counterpart by
  design (see handoff §3.3).

**Closed off**:
- file:line as the contract join key (kept only as the internal alignment
  step that *discovers* the node to copy canonical_id from).
- A CKV-side symbol-ID normalization scheme (backlog B7).

## Realization

- PR #9 (`c554cc5`): canonical_id inheritance onto chunks + `FindByCanonicalID`
  readiness.
- 2026-06-29: `internal/ckgalign` now gates on canonical_id *population*
  (`canonicalHasValue` probe + `Index.CanonicalAvailable()`), and
  `internal/build` emits a warning + `ckg_align.canonical_unavailable`
  footprint when a graph has no populated canonical_id. Tests:
  `TestCanonicalAvailable_ColumnPresentButEmpty`, `_ColumnAbsent`.
- Pending wiring follow-up: assert CKG's published manifest
  `schema_version >= 1.19` before pointing at a graph (needs CKG manifest
  format).
- Pending shared work: integration fixture (CKV + CKG).
