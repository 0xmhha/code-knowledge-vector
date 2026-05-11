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
// plain string for forward-compatibility with new languages (e.g. Solidity
// "Event"/"Modifier" pending the plan §10 q1 decision).
type SymbolKind string

const (
	KindFunction  SymbolKind = "Function"
	KindMethod    SymbolKind = "Method"
	KindType      SymbolKind = "Type"
	KindStruct    SymbolKind = "Struct"
	KindInterface SymbolKind = "Interface"
	KindContract  SymbolKind = "Contract"  // Solidity
	KindEvent     SymbolKind = "Event"     // Solidity (TBD)
	KindModifier  SymbolKind = "Modifier"  // Solidity (TBD)
	KindFileHeader SymbolKind = "FileHeader"
)

// ChunkKind classifies the chunking strategy that produced the chunk.
// Distinct from SymbolKind because a long function may produce several
// "function_split" chunks, all of SymbolKind=Function.
type ChunkKind string

const (
	ChunkSymbol        ChunkKind = "symbol"         // whole function/method/type
	ChunkFunctionSplit ChunkKind = "function_split" // sub-chunk of a long function
	ChunkFileHeader    ChunkKind = "file_header"    // import block / top-of-file
)

// Citation is the {file, start_line, end_line, commit_hash} tuple CKV
// attaches to every chunk and every search hit. CKG uses the same shape,
// so hybrid responses can be merged without translation (plan §10.1).
type Citation struct {
	File       string `json:"file"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	CommitHash string `json:"commit_hash"`
}

// Chunk is the unit CKV embeds and stores. It is the indexable record
// produced by parse → chunk; the embedder turns Text into a vector and
// the store persists everything except Text-derived caches.
type Chunk struct {
	ID            string     `json:"id"`              // see ChunkID
	File          string     `json:"file"`
	StartLine     int        `json:"start_line"`
	EndLine       int        `json:"end_line"`
	Language      string     `json:"language"`        // "go" | "typescript" | "solidity"
	SymbolName    string     `json:"symbol_name,omitempty"`
	SymbolKind    SymbolKind `json:"symbol_kind,omitempty"`
	ChunkKind     ChunkKind  `json:"chunk_kind"`
	CommitHash    string     `json:"commit_hash"`
	ContentSHA256 string     `json:"content_sha256"`
	CKGNodeID     string     `json:"ckg_node_id,omitempty"` // 1:1 alignment when CKG path is provided
	Text          string     `json:"text"`                  // chunk source (for re-embedding / display)
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

// ChunkID computes the deterministic chunk identifier per plan §5.4:
//
//	sha256(file + "\n" + start_line + ":" + end_line + "\n" + content_sha256)
//
// content_sha256 is the SHA-256 of the chunk Text (raw bytes — no whitespace
// normalization). A rename of the file changes the ID; this is intentional —
// rename tracking is the caller's responsibility (see §5.4 "파일 이동" note).
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
