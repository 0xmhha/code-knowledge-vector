// Package manifest is the on-disk index metadata. CKV writes
// <out>/manifest.json alongside <out>/vector.db so freshness checks,
// rebuild detection, and CKG cross-checks can run without opening SQLite.
//
// Schema versioning: bump SchemaVersion only on *breaking* changes (renamed
// fields, removed fields, changed semantics). Additive `omitempty` fields
// stay on the same version — old readers see them as zero values.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SchemaVersionCurrent is the on-disk schema version this build writes.
const SchemaVersionCurrent = "1.0"

// FileName is the file written into the --out directory.
const FileName = "manifest.json"

// Manifest is the structured index metadata. Field names are shared with
// CKG so CKS Orchestrator can compare CKG and CKV manifests by raw key.
type Manifest struct {
	SchemaVersion string `json:"schema_version"`
	CKVVersion    string `json:"ckv_version"`
	BuiltAt       string `json:"built_at"` // RFC3339

	// Source / git
	SrcRoot     string `json:"src_root"`
	SrcCommit   string `json:"src_commit,omitempty"`   // commit at indexing time
	IndexedHead string `json:"indexed_head,omitempty"` // alias for SrcCommit (back-compat)

	// Embedding identity
	EmbeddingModel     string `json:"embedding_model"`
	EmbeddingDim       int    `json:"embedding_dim"`
	EmbeddingChecksum  string `json:"embedding_checksum,omitempty"`
	EmbeddingNormalize string `json:"embedding_normalize,omitempty"` // "l2" | "none"

	// Aggregate stats
	ChunkCount int            `json:"chunk_count"`
	Languages  map[string]int `json:"languages,omitempty"` // language → chunk count

	// Ignore patterns surfaced for transparency
	CKVIgnore []string `json:"ckvignore,omitempty"`

	// DocsRoots are additional markdown corpus directories indexed via
	// `ckv build --docs` (outside SrcRoot, e.g. a cks-rendered
	// domain-knowledge corpus). Recorded so callers can see every source
	// the index covers. Additive — old readers see nil.
	DocsRoots []string `json:"docs_roots,omitempty"`

	// Sources is the per-layer knowledge-cutoff ledger (reindex-migration
	// design §2.2): what each layer was built from, so a reindex knows what
	// is stale and CKS can detect a CKG↔CKV mismatch. Additive — old readers
	// see nil; each sub-block is omitted when that layer was not built.
	Sources *Sources `json:"sources,omitempty"`
}

// Sources records the origin + cutoff of each knowledge layer in the index.
type Sources struct {
	Code   *CodeSource   `json:"code,omitempty"`
	CKG    *CKGSource    `json:"ckg,omitempty"`
	PRs    *PRSource     `json:"prs,omitempty"`
	Docs   *HashedSource `json:"docs,omitempty"`
	Flow   *HashedSource `json:"flow,omitempty"`
	Policy *HashedSource `json:"policy,omitempty"`
}

// CodeSource is the source-tree cutoff (mirrors the top-level SrcCommit/BuiltAt,
// grouped for a uniform ledger).
type CodeSource struct {
	IndexedHead string `json:"indexed_head,omitempty"`
	BuiltAt     string `json:"built_at,omitempty"`
}

// CKGSource anchors the CKG graph this index aligned against, for CKG↔CKV
// mismatch detection (design §3). GraphDigest is CKG's *logical* digest
// (sorted canonical_id + edge hash — NOT a file-byte sha, which is
// non-deterministic under SQLite's layout). It stays empty until CKG publishes
// it; SrcCommit + SchemaVersion are recorded immediately, so the alignment
// assert starts on commit+schema and strengthens to +digest once available.
type CKGSource struct {
	GraphDigest   string `json:"graph_digest,omitempty"`
	SrcCommit     string `json:"src_commit,omitempty"`
	SchemaVersion string `json:"schema_version,omitempty"`
	Path          string `json:"path,omitempty"`
}

// PRSource is the PR-corpus cutoff: the newest PR indexed, so an incremental
// PR ingest can fetch only PRs after LastPRNumber / LastMergedAt.
type PRSource struct {
	Repo         string `json:"repo,omitempty"`
	LastPRNumber int    `json:"last_pr_number,omitempty"`
	LastMergedAt string `json:"last_merged_at,omitempty"` // RFC3339
}

// HashedSource is a corpus/config whose "did it change" is detected by a
// content hash (docs tree, flow corpus file, policy file).
type HashedSource struct {
	Path        string `json:"path,omitempty"`
	ContentHash string `json:"content_hash,omitempty"` // sha256 hex
}

// Load reads <dir>/manifest.json. Returns an ErrNotFound when the file is
// missing so callers can distinguish "no index yet" from "index corrupt".
func Load(dir string) (*Manifest, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &m, nil
}

// Save writes <dir>/manifest.json atomically: write to a sibling tmp file,
// fsync it, then rename(2) over the destination. POSIX guarantees the
// rename is atomic within the same filesystem, so partial writes never
// appear under the canonical path.
func Save(dir string, m *Manifest) error {
	if m == nil {
		return errors.New("manifest: nil")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir out dir: %w", err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	final := filepath.Join(dir, FileName)
	tmp, err := os.CreateTemp(dir, FileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("fsync tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmpPath, final); err != nil {
		return fmt.Errorf("rename tmp -> manifest: %w", err)
	}
	cleanup = false
	return nil
}

// Remove deletes <dir>/manifest.json if present. A missing file is not an
// error, so callers can use it to mark an index "not ready" before a rebuild:
// while the build runs (or if it fails partway) Load returns ErrNotFound,
// keeping a partially-written index from being opened against a stale manifest.
func Remove(dir string) error {
	if err := os.Remove(filepath.Join(dir, FileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove manifest: %w", err)
	}
	return nil
}

// ErrNotFound signals that no manifest exists at the given path.
var ErrNotFound = errors.New("manifest: not found")
