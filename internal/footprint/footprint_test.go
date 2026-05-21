package footprint

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestProfileAggregatesByEvent exercises B8 --profile: latencies from
// every Span done event group by event name, percentile math runs at
// Close, and the JSON output round-trips.
func TestProfileAggregatesByEvent(t *testing.T) {
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "footprint.jsonl")
	profile := filepath.Join(dir, "profile.json")

	l, err := New(Options{JSONLPath: jsonl, ProfilePath: profile})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// 3 search spans with measurable latency.
	for range 3 {
		done := l.Span("query.search")
		time.Sleep(2 * time.Millisecond)
		done()
	}
	// 1 build span, different name.
	done := l.Span("build")
	time.Sleep(1 * time.Millisecond)
	done()

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(profile)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	var got struct {
		RunID  string                    `json:"run_id"`
		Events map[string]*profileBucket `json:"events"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal profile: %v", err)
	}
	if got.RunID == "" {
		t.Errorf("profile missing run_id")
	}
	searchBucket, ok := got.Events["query.search.done"]
	if !ok {
		t.Fatalf("expected query.search.done bucket; got keys %v", keys(got.Events))
	}
	if searchBucket.Count != 3 {
		t.Errorf("query.search.done count = %d, want 3", searchBucket.Count)
	}
	if searchBucket.SumMs <= 0 {
		t.Errorf("query.search.done sum should be positive: %d", searchBucket.SumMs)
	}
	if searchBucket.P95Ms <= 0 {
		t.Errorf("query.search.done p95 should be positive: %d", searchBucket.P95Ms)
	}
	if _, ok := got.Events["build.done"]; !ok {
		t.Errorf("expected build.done bucket; got keys %v", keys(got.Events))
	}
}

// TestProfileDisabledByDefault verifies that without ProfilePath, no
// profile file gets written even when spans happen.
func TestProfileDisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	l, err := New(Options{JSONLPath: filepath.Join(dir, "footprint.jsonl")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	done := l.Span("query.search")
	done()
	l.Close()

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() == "profile.json" {
			t.Errorf("profile.json should not exist when ProfilePath unset")
		}
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestEmitWritesJSONLLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "footprint.jsonl")
	l, err := New(Options{JSONLPath: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.Emit("build.start", "src", "/repo", "files", 42)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events := readEvents(t, path)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Event != "build.start" {
		t.Errorf("event = %q, want build.start", e.Event)
	}
	if e.SchemaVersion != SchemaVersion {
		t.Errorf("missing schema_version: %+v", e)
	}
	if e.TraceID == "" || e.RunID == "" {
		t.Errorf("trace_id/run_id should be auto-populated: %+v", e)
	}
	if e.Fields["src"] != "/repo" {
		t.Errorf("fields not propagated: %+v", e.Fields)
	}
	if v, ok := e.Fields["files"].(float64); !ok || int(v) != 42 {
		t.Errorf("integer field not round-tripped: %+v", e.Fields)
	}
}

func TestSpanEmitsStartAndDoneWithLatency(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "footprint.jsonl")
	l, _ := New(Options{JSONLPath: path})
	done := l.Span("query.search", "k", 5)
	time.Sleep(2 * time.Millisecond)
	done("hits", 3, "citation_drops", 0)
	l.Close()

	events := readEvents(t, path)
	if len(events) != 2 {
		t.Fatalf("expected start+done, got %d", len(events))
	}
	if events[0].Event != "query.search.start" {
		t.Errorf("first event: %q", events[0].Event)
	}
	if events[1].Event != "query.search.done" {
		t.Errorf("second event: %q", events[1].Event)
	}
	if events[1].LatencyMs <= 0 {
		t.Errorf("done event should carry latency_ms, got %d", events[1].LatencyMs)
	}
	if events[0].TraceID != events[1].TraceID {
		t.Errorf("start/done must share trace_id: %s vs %s",
			events[0].TraceID, events[1].TraceID)
	}
	if events[1].Fields["hits"].(float64) != 3 {
		t.Errorf("done extras not merged: %+v", events[1].Fields)
	}
	if events[1].Fields["k"].(float64) != 5 {
		t.Errorf("start fields not merged onto done: %+v", events[1].Fields)
	}
}

func TestRunIDStableAcrossEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "footprint.jsonl")
	l, _ := New(Options{JSONLPath: path, RunID: "explicit-run-id"})
	l.Emit("a")
	l.Emit("b")
	l.Close()

	events := readEvents(t, path)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].RunID != "explicit-run-id" || events[1].RunID != "explicit-run-id" {
		t.Errorf("run_id not preserved: %v", events)
	}
}

func TestDiscardLoggerSafe(t *testing.T) {
	l := Discard()
	l.Emit("nothing.recorded")
	done := l.Span("nothing.timed")
	done()
	if err := l.Close(); err != nil {
		t.Errorf("Discard.Close should be nil-safe, got %v", err)
	}
}

func TestJSONLIsAppendOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "footprint.jsonl")

	// First logger
	l1, _ := New(Options{JSONLPath: path})
	l1.Emit("session.a")
	l1.Close()
	// Second logger: must append, not truncate
	l2, _ := New(Options{JSONLPath: path})
	l2.Emit("session.b")
	l2.Close()

	events := readEvents(t, path)
	if len(events) != 2 {
		t.Fatalf("expected 2 events across sessions, got %d", len(events))
	}
}

func readEvents(t *testing.T, path string) []Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []Event
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 1<<20), 1<<20)
	for scan.Scan() {
		var e Event
		if err := json.Unmarshal(scan.Bytes(), &e); err != nil {
			t.Fatalf("decode line %q: %v", scan.Text(), err)
		}
		out = append(out, e)
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
