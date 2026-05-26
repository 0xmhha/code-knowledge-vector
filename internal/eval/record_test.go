package eval

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordSession_appendsEntry(t *testing.T) {
	eng, srcAbs := newSampleEngine(t)

	fixturePath := filepath.Join(t.TempDir(), "queries.yaml")
	seed := `schema_version: "1"
queries:
  - id: q1
    intent: "TCP socket bind on port"
    expected:
      file: server.go
      symbol: Server.Listen
      kind: Method
      line_range: [22, 29]
`
	if err := os.WriteFile(fixturePath, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}

	input := "shutdown listener\n1\n\n"
	var out bytes.Buffer

	opts := RecordOptions{K: 5, Threshold: -1, SrcRoot: srcAbs}
	err := RecordSession(context.Background(), eng, fixturePath, opts, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("RecordSession: %v", err)
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "id: q2") {
		t.Errorf("expected q2 entry, got:\n%s", content)
	}
	if !strings.Contains(content, "recorded_via: interactive") {
		t.Errorf("expected recorded_via field, got:\n%s", content)
	}
	if !strings.Contains(content, "timestamp:") {
		t.Errorf("expected timestamp field, got:\n%s", content)
	}

	fx, err := LoadFixture(fixturePath)
	if err != nil {
		t.Fatalf("re-load fixture: %v", err)
	}
	if len(fx.Queries) != 2 {
		t.Errorf("expected 2 queries, got %d", len(fx.Queries))
	}
}

func TestRecordSession_skipNone(t *testing.T) {
	eng, srcAbs := newSampleEngine(t)

	fixturePath := filepath.Join(t.TempDir(), "queries.yaml")
	seed := `schema_version: "1"
queries:
  - id: q1
    intent: "TCP socket bind on port"
    expected:
      file: server.go
      symbol: Server.Listen
      kind: Method
      line_range: [22, 29]
`
	if err := os.WriteFile(fixturePath, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}

	input := "shutdown listener\nnone\n\n"
	var out bytes.Buffer

	opts := RecordOptions{K: 5, Threshold: -1, SrcRoot: srcAbs}
	err := RecordSession(context.Background(), eng, fixturePath, opts, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("RecordSession: %v", err)
	}

	fx, err := LoadFixture(fixturePath)
	if err != nil {
		t.Fatalf("re-load fixture: %v", err)
	}
	if len(fx.Queries) != 1 {
		t.Errorf("expected 1 query (none selected), got %d", len(fx.Queries))
	}
}

func TestRecordSession_emptyQuit(t *testing.T) {
	eng, srcAbs := newSampleEngine(t)

	fixturePath := filepath.Join(t.TempDir(), "queries.yaml")
	seed := `schema_version: "1"
queries:
  - id: q1
    intent: "TCP socket bind on port"
    expected:
      file: server.go
      symbol: Server.Listen
      kind: Method
      line_range: [22, 29]
`
	if err := os.WriteFile(fixturePath, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}

	input := "\n"
	var out bytes.Buffer

	opts := RecordOptions{K: 5, Threshold: -1, SrcRoot: srcAbs}
	err := RecordSession(context.Background(), eng, fixturePath, opts, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("RecordSession: %v", err)
	}

	fx, err := LoadFixture(fixturePath)
	if err != nil {
		t.Fatalf("re-load fixture: %v", err)
	}
	if len(fx.Queries) != 1 {
		t.Errorf("expected 1 query (immediate quit), got %d", len(fx.Queries))
	}
}

func TestNextQueryID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queries.yaml")

	// Non-existent file → 1
	id, err := nextQueryID(filepath.Join(dir, "nonexistent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Errorf("nextQueryID(nonexistent) = %d, want 1", id)
	}

	// File with q1..q5 → 6
	fixture := `schema_version: "1"
queries:
  - id: q1
    intent: "a"
    expected:
      file: x.go
      line_range: [1, 1]
  - id: q5
    intent: "b"
    expected:
      file: y.go
      line_range: [1, 1]
`
	if err := os.WriteFile(path, []byte(fixture), 0644); err != nil {
		t.Fatal(err)
	}
	id, err = nextQueryID(path)
	if err != nil {
		t.Fatal(err)
	}
	if id != 6 {
		t.Errorf("nextQueryID(q1,q5) = %d, want 6", id)
	}
}
