// Package types holds the cross-package data contracts: Chunk, Hit, Filter,
// the Embedder and VectorStore interfaces. Keeping these here (rather than
// inside internal/) lets future CKS code import them without pulling in
// indexer/store implementations.
package types

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// SymbolKind enumerates the AST node kinds CKV chunks against. Stored as a
// plain string for forward-compatibility with new languages.
type SymbolKind string

const (
	KindFunction   SymbolKind = "Function"
	KindMethod     SymbolKind = "Method"
	KindType       SymbolKind = "Type"
	KindStruct     SymbolKind = "Struct"
	KindInterface  SymbolKind = "Interface"
	KindContract   SymbolKind = "Contract" // Solidity
	KindEvent      SymbolKind = "Event"    // Solidity (TBD)
	KindModifier   SymbolKind = "Modifier" // Solidity (TBD)
	KindFileHeader SymbolKind = "FileHeader"
	// Markdown indexing kinds.
	// Each heading-bounded section in a *.md / *.markdown file becomes one
	// SymbolSpan; the chunker emits a chunk per span so "왜 X 결정했나" style
	// queries can hit a specific decision section.
	KindDocSection SymbolKind = "DocSection" // markdown heading section
	KindADRSection SymbolKind = "ADRSection" // ADR-* / docs/adr/* markdown sections
)

// ChunkKind classifies the chunking strategy that produced the chunk.
// Distinct from SymbolKind because a long function may produce several
// "function_split" chunks, all of SymbolKind=Function.
type ChunkKind string

const (
	ChunkSymbol        ChunkKind = "symbol"         // whole function/method/type
	ChunkFunctionSplit ChunkKind = "function_split" // sub-chunk of a long function
	ChunkFileHeader    ChunkKind = "file_header"    // import block / top-of-file
	// ChunkFileFull is a coarse-granularity chunk covering a whole source file
	// (Phase B multi-granularity, roadmap §3.1). Additive and opt-in: emitted
	// only when Options.IncludeFileFull is set, so it competes in search
	// alongside the fine symbol chunks for file/module-level queries.
	ChunkFileFull ChunkKind = "file_full"
	// ChunkDoc covers markdown heading sections (DocSection/ADRSection).
	// Kept distinct from ChunkSymbol so callers can filter the corpus by
	// "code vs documentation" without inspecting SymbolKind. The chunker
	// promotes spans whose SymbolKind is DocSection or ADRSection.
	ChunkDoc ChunkKind = "doc"

	// PR corpus kinds. Additive — existing schema_version 1.0
	// indexes continue working; these appear only in indexes built with
	// --include-pr-history.
	ChunkPRBackground  ChunkKind = "pr_background"
	ChunkPRSolution    ChunkKind = "pr_solution"
	ChunkCommitMessage ChunkKind = "commit_message"

	// ChunkInvariant carries an invariant statement found inside or
	// adjacent to a source chunk. Each invariant chunk is paired (via
	// the source chunk's Invariants []InvariantRef list) with the code
	// it constrains. The agent can query invariants for a file to
	// learn what changes must NOT break.
	ChunkInvariant ChunkKind = "invariant"

	// ChunkConvention is a per-package summary of AST-derived patterns
	// (error handling style, logging library, naming, concurrency).
	// The agent queries these to learn what idioms the package follows
	// before proposing edits — preventing convention drift.
	ChunkConvention ChunkKind = "convention"

	// Flow-corpus kinds. A curated flow corpus (corpus.jsonl, loaded via
	// --flow-corpus) describes "현상 → 원인" causal paths through the code so
	// an agent can trace a symptom to its cause. Additive — present only in
	// indexes built with --flow-corpus.
	ChunkFlowStep  ChunkKind = "flow_step"  // one step in a flow (symbol + branches)
	ChunkFlowSpine ChunkKind = "flow_spine" // a flow's entry/summary backbone
)

// InvariantTier classifies how an invariant was detected.
//
//	Tier 1 — existing marker (// CRITICAL, // IMPORTANT, // WARNING, // Deprecated:)
//	Tier 2 — new convention marker (// INVARIANT:, // CONSENSUS:, // SECURITY:)
//	Tier 3 — heuristic (panic(...) / fmt.Errorf(...) with policy keywords)
//
// Lower tiers carry higher confidence; the agent can filter by tier
// when noise tolerance is low (e.g. only tier 1+2 during a release).
type InvariantTier int

const (
	InvariantTierExistingMarker InvariantTier = 1
	InvariantTierNewMarker      InvariantTier = 2
	InvariantTierHeuristic      InvariantTier = 3
)

// InvariantRef is a back-pointer attached to a source Chunk pointing
// at the ChunkInvariant(s) extracted from inside or near it. Kept
// small so adding it to every chunk does not balloon storage.
type InvariantRef struct {
	ChunkID string        `json:"chunk_id"`         // ID of the ChunkInvariant chunk
	Tier    InvariantTier `json:"tier"`             // 1, 2, or 3
	Marker  string        `json:"marker,omitempty"` // e.g. "CRITICAL", "panic"
}

// Branch is one conditional edge inside a flow step: under condition When,
// control goes to Then at code location At. Mapping a symptom (When) to its
// cause site (Then@At) is the core of flow-based root-cause analysis.
type Branch struct {
	When string `json:"when"`
	Then string `json:"then"`
	At   string `json:"at"`
}

// FlowStepMeta is the structured metadata for a ChunkFlowStep chunk: the
// symbol the step runs at, the symbols it calls, what it reads/writes/emits,
// its conditional branches, and the invariant ids it must uphold. Serialized
// into the flow_meta column (populated in Phase B).
type FlowStepMeta struct {
	FlowID     string   `json:"flow_id"`
	StepID     string   `json:"step_id"`
	Symbol     string   `json:"symbol,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Calls      []string `json:"calls,omitempty"`
	Reads      string   `json:"reads,omitempty"`
	Writes     string   `json:"writes,omitempty"`
	Emits      string   `json:"emits,omitempty"`
	Branches   []Branch `json:"branches,omitempty"`
	Invariants []string `json:"invariants,omitempty"`
	// AlignedChunkID is the ID of the code chunk whose line range contains this
	// step's file:line (Phase C alignment), letting a caller jump from a flow
	// step to its actual implementation chunk. Empty when the step's line did
	// not resolve to any code chunk — the corpus drifted from the code.
	AlignedChunkID string `json:"aligned_chunk_id,omitempty"`
}

// FlowSpineMeta is the structured metadata for a ChunkFlowSpine chunk: a
// flow's entry point, what triggers it, and how it links to other flows.
// Serialized into the flow_meta column (populated in Phase B).
type FlowSpineMeta struct {
	FlowID     string   `json:"flow_id"`
	EntryPoint string   `json:"entry_point,omitempty"`
	Trigger    string   `json:"trigger,omitempty"`
	RootSymbol string   `json:"root_symbol,omitempty"`
	Links      []string `json:"links,omitempty"`
	CalledBy   []string `json:"called_by,omitempty"`
}

// EnforcePoint records where a curated invariant is enforced: a step in a
// flow at a code location. Serialized into the enforced_at column.
type EnforcePoint struct {
	Flow string `json:"flow"`
	Step string `json:"step"`
	Loc  string `json:"loc"`
}

// PRRef records a PR that touched a chunk's file or symbol. Stored as
// JSON in the recent_prs column; the temporal slicing key (MergedAtUTC)
// lets query-time filtering exclude PRs merged after a cutoff.
type PRRef struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	MergedAtUTC string `json:"merged_at_utc,omitempty"`
}

// Citation is the {file, start_line, end_line, commit_hash} tuple CKV
// attaches to every chunk and every search hit. CKG uses the same shape,
// so hybrid responses can be merged without translation.
type Citation struct {
	File       string `json:"file"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	CommitHash string `json:"commit_hash"`
}

// ModificationGuidance is project-policy advice attached to a chunk by
// the policy loader. It surfaces "if you touch this code, here is what
// else to consider" hints derived from the chunk's path category
// (e.g. consensus, state, p2p). All fields may be empty.
//
// Guidance is informative, not enforcement. A nil pointer means the
// chunk's path did not match any policy rule.
type ModificationGuidance struct {
	AlsoReview    []string `json:"also_review,omitempty"`    // other categories/files to inspect together
	RequiredTests []string `json:"required_tests,omitempty"` // test suites the change should exercise
	WatchOut      []string `json:"watch_out,omitempty"`      // pitfalls / hard-fork / byzantine risks
}

// Chunk is the unit CKV embeds and stores. It is the indexable record
// produced by parse → chunk; the embedder turns Text into a vector and
// the store persists everything except Text-derived caches.
type Chunk struct {
	ID              string                `json:"id"` // see ChunkID
	File            string                `json:"file"`
	StartLine       int                   `json:"start_line"`
	EndLine         int                   `json:"end_line"`
	Language        string                `json:"language"`          // "go" | "typescript" | "solidity" | "markdown"
	IsTest          bool                  `json:"is_test,omitempty"` // _test.go, *.test.ts, *.spec.ts, *.t.sol, test/... — populated by IsTestPath
	SymbolName      string                `json:"symbol_name,omitempty"`
	SymbolKind      SymbolKind            `json:"symbol_kind,omitempty"`
	ChunkKind       ChunkKind             `json:"chunk_kind"`
	CommitHash      string                `json:"commit_hash"`
	ContentSHA256   string                `json:"content_sha256"`
	CanonicalID     string                `json:"canonical_id,omitempty"`     // ckg's import-path-qualified symbol id (ADR-0001), copied verbatim from the aligned ckg node; the stable key cks uses to FindByCanonicalID against ckg
	RecentPRs       []PRRef               `json:"recent_prs,omitempty"`       // PRs that touched this chunk's file
	Category        string                `json:"category,omitempty"`         // policy category: consensus|state|crypto|p2p|... (empty = unclassified)
	Guidance        *ModificationGuidance `json:"guidance,omitempty"`         // attached by policy loader; nil for unclassified
	Invariants      []InvariantRef        `json:"invariants,omitempty"`       // back-pointers to ChunkInvariant chunks extracted from this source
	ConventionStats map[string]any        `json:"convention_stats,omitempty"` // populated on ChunkConvention chunks; empty for source chunks
	FlowStep        *FlowStepMeta         `json:"flow_step,omitempty"`        // populated on ChunkFlowStep chunks (flow_meta column)
	FlowSpine       *FlowSpineMeta        `json:"flow_spine,omitempty"`       // populated on ChunkFlowSpine chunks (flow_meta column)
	Provenance      string                `json:"provenance,omitempty"`       // invariant origin: "auto" (extracted) | "curated" (corpus); empty for non-invariant chunks
	EnforcedAt      []EnforcePoint        `json:"enforced_at,omitempty"`      // populated on curated ChunkInvariant chunks (enforced_at column)
	Text            string                `json:"text"`                       // chunk source (for re-embedding / display)
}

// Citation returns the citation view of this chunk. Always populated for
// indexed chunks; never returns a zero-value citation for a real chunk.
func (c Chunk) Citation() Citation {
	return Citation{
		File:       c.File,
		StartLine:  c.StartLine,
		EndLine:    c.EndLine,
		CommitHash: c.CommitHash,
	}
}

// ChunkID computes the deterministic chunk identifier:
//
//	sha256(file + "\n" + start_line + ":" + end_line + "\n" + content_sha256)
//
// content_sha256 is the SHA-256 of the chunk Text (raw bytes — no whitespace
// normalization). A rename of the file changes the ID; this is intentional —
// rename tracking is the caller's responsibility.
func ChunkID(file string, startLine, endLine int, contentSHA256 string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%d:%d\n%s", file, startLine, endLine, contentSHA256)
	return hex.EncodeToString(h.Sum(nil))
}

// ContentSHA256 returns the canonical hash used in chunk_id and stored
// alongside each chunk for stale-detection. Single-source-of-truth helper —
// every caller (chunker, store loader, eval harness) goes through this so
// hashing stays consistent.
func ContentSHA256(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
