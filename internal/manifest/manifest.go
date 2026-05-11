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
// Plan §4 anchors CKV at 1.0 (CKG is at 1.7 independently).
const SchemaVersionCurrent = "1.0"

// FileName is the file written into the --out directory.
const FileName = "manifest.json"

// Manifest is the structured index metadata. Field names use plan §4 keys
// so CKS Orchestrator can compare CKG and CKV manifests by raw key.
type Manifest struct {
	SchemaVersion string `json:"schema_version"`
	CKVVersion    string `json:"ckv_version"`
	BuiltAt       string `json:"built_at"` // RFC3339

	// Source / git
	SrcRoot     string `json:"src_root"`
	SrcCommit   string `json:"src_commit,omitempty"`   // commit at indexing time
	IndexedHead string `json:"indexed_head,omitempty"` // alias for SrcCommit (back-compat with featurelist §1.6)

	// Embedding identity (plan §3)
	EmbeddingModel     string `json:"embedding_model"`
	EmbeddingDim       int    `json:"embedding_dim"`
	EmbeddingChecksum  string `json:"embedding_checksum,omitempty"`
	EmbeddingNormalize string `json:"embedding_normalize,omitempty"` // "l2" | "none"

	// Aggregate stats
	ChunkCount int            `json:"chunk_count"`
	Languages  map[string]int `json:"languages,omitempty"` // language → chunk count

	// Ignore patterns surfaced for transparency
	CKVIgnore []string `json:"ckvignore,omitempty"`
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

// ErrNotFound signals that no manifest exists at the given path.
var ErrNotFound = errors.New("manifest: not found")
