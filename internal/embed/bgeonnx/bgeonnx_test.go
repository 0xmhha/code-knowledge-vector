package bgeonnx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeModelDir writes the bare-minimum files Open() validates for
// the default model. Contents are irrelevant — Open only stats the
// paths. Creates any parent directories needed (e.g. `onnx/`).
// The directory basename matches DefaultModelName so Open() can
// resolve the right ModelConfig without an explicit ModelName.
func fakeModelDir(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, DefaultModelName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := LookupModel(DefaultModelName)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{cfg.OnnxFile, cfg.TokenizerFile} {
		full := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// openWithStubs constructs an Adapter that bypasses the default
// factory. Tests use this when they want to exercise Adapter wiring
// without spinning up real CGO libraries — the `-tags bgeonnx` build
// otherwise calls hfTokenizer/onnxSession, which would fail on the
// stub files written by fakeModelDir.
func openWithStubs(t *testing.T) *Adapter {
	t.Helper()
	a, err := Open(Options{
		ModelDir:  fakeModelDir(t),
		Tokenizer: stubTokenizer{},
		Session:   stubSession{},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return a
}

func TestIdentityFromRegistry(t *testing.T) {
	a := openWithStubs(t)
	defer a.Close()
	cfg, err := LookupModel(DefaultModelName)
	if err != nil {
		t.Fatal(err)
	}
	if a.Name() != cfg.Name {
		t.Errorf("Name = %q, want %q", a.Name(), cfg.Name)
	}
	if a.Dimension() != cfg.Dim {
		t.Errorf("Dimension = %d, want %d", a.Dimension(), cfg.Dim)
	}
	if a.MaxInputTokens() != cfg.MaxInput {
		t.Errorf("MaxInputTokens = %d, want %d", a.MaxInputTokens(), cfg.MaxInput)
	}
}

func TestOpenRejectsMissingModelFiles(t *testing.T) {
	if _, err := Open(Options{ModelDir: t.TempDir()}); err == nil {
		t.Fatal("expected error when model files missing")
	}
}

func TestEmbedReturnsErrNotImplementedWithStubs(t *testing.T) {
	a := openWithStubs(t)
	defer a.Close()
	_, err := a.Embed(context.Background(), []string{"hello"})
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
	}
}

// fakeTokenizer returns deterministic single-token output so we can
// assert the Adapter passes them through to the Session unchanged.
type fakeTokenizer struct {
	calls *int
}

func (f *fakeTokenizer) Tokenize(_ context.Context, batch []string, _ int) (TokenizedBatch, error) {
	if f.calls != nil {
		*f.calls++
	}
	out := TokenizedBatch{
		InputIDs:      make([][]int64, len(batch)),
		AttentionMask: make([][]int64, len(batch)),
	}
	for i := range batch {
		out.InputIDs[i] = []int64{1, 2, 3}
		out.AttentionMask[i] = []int64{1, 1, 1}
	}
	return out, nil
}

type fakeSession struct {
	received int
	dim      int
}

func (f *fakeSession) Run(_ context.Context, tokens TokenizedBatch) ([][]float32, error) {
	f.received = len(tokens.InputIDs)
	out := make([][]float32, len(tokens.InputIDs))
	for i := range out {
		out[i] = make([]float32, f.dim)
		out[i][0] = 1.0
	}
	return out, nil
}

func (f *fakeSession) Close() error { return nil }

func TestEmbedOrchestratesTokenizerAndSession(t *testing.T) {
	cfg, err := LookupModel(DefaultModelName)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	sess := &fakeSession{dim: cfg.Dim}
	a, err := Open(Options{
		ModelDir:  fakeModelDir(t),
		Tokenizer: &fakeTokenizer{calls: &calls},
		Session:   sess,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	vecs, err := a.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 tokenize call, got %d", calls)
	}
	if sess.received != 2 {
		t.Errorf("session expected 2 inputs, got %d", sess.received)
	}
	if len(vecs) != 2 || len(vecs[0]) != cfg.Dim {
		t.Errorf("vector shape wrong: got %dx%d, want 2x%d", len(vecs), len(vecs[0]), cfg.Dim)
	}
}

func TestEmbedEmptyBatchIsCheap(t *testing.T) {
	a := openWithStubs(t)
	defer a.Close()
	vecs, err := a.Embed(context.Background(), nil)
	if err != nil {
		t.Errorf("nil batch should be a no-op, got %v", err)
	}
	if len(vecs) != 0 {
		t.Errorf("nil batch should return no vectors, got %d", len(vecs))
	}
}
