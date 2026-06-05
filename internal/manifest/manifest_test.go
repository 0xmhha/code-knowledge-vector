package manifest

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		SchemaVersion:      SchemaVersionCurrent,
		CKVVersion:         "dev",
		BuiltAt:            "2026-05-08T12:00:00Z",
		SrcRoot:            "/path/to/repo",
		SrcCommit:          "abc123",
		IndexedHead:        "abc123",
		EmbeddingModel:     "bge-large-en-v1.5",
		EmbeddingDim:       1024,
		EmbeddingNormalize: "l2",
		ChunkCount:         42,
		Languages:          map[string]int{"go": 40, "typescript": 2},
		CKVIgnore:          []string{"node_modules/**"},
	}
	if err := Save(dir, m); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.EmbeddingDim != 1024 || got.EmbeddingModel != "bge-large-en-v1.5" {
		t.Errorf("embedding fields not round-tripped: %+v", got)
	}
	if got.Languages["go"] != 40 {
		t.Errorf("languages map not round-tripped: %v", got.Languages)
	}
}

func TestLoadMissingReturnsErrNotFound(t *testing.T) {
	_, err := Load(t.TempDir())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSaveIsAtomic_NoTmpFilesLeftBehind(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{SchemaVersion: SchemaVersionCurrent, EmbeddingModel: "x", EmbeddingDim: 1}
	if err := Save(dir, m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Look for leftover tmp files.
	matches, _ := filepath.Glob(filepath.Join(dir, FileName+".tmp-*"))
	if len(matches) != 0 {
		t.Errorf("tmp files left behind: %v", matches)
	}
}

func TestSaveLoad_DocsRootsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := &Manifest{
		SchemaVersion:  SchemaVersionCurrent,
		EmbeddingModel: "mock",
		EmbeddingDim:   8,
		DocsRoots:      []string{"/abs/corpus/go-stablenet"},
	}
	if err := Save(dir, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(out.DocsRoots) != 1 || out.DocsRoots[0] != "/abs/corpus/go-stablenet" {
		t.Errorf("DocsRoots round-trip = %v, want [/abs/corpus/go-stablenet]", out.DocsRoots)
	}
}
