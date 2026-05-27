// Package footprint records structured events about every CKV operation
// — build, query, MCP tool call — to two sinks:
//
//  1. **slog** (stderr by default). Operator-readable.
//  2. **JSONL** at <out>/footprint.jsonl, one object per line. Append-
//     only, machine-readable. This is the *seed* of the future
//     read-write working-memory MCP: the memory layer reads this log
//     (or a SQLite mirror of it) to recall what was asked, what we
//     answered, and how well it worked.
//
// Schema stability matters: every event has the same envelope
// (timestamp, event, trace_id, latency_ms, error) plus an event-specific
// fields map. Adding new fields is non-breaking; renaming/removing is.
package footprint

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// SchemaVersion is stamped into every event so downstream consumers
// (working memory MCP, eval harness) can detect breaking changes.
const SchemaVersion = "1"

// Event is the on-the-wire shape: stable envelope + open Fields map.
type Event struct {
	Timestamp     time.Time      `json:"ts"`
	SchemaVersion string         `json:"schema_version"`
	Event         string         `json:"event"`
	TraceID       string         `json:"trace_id,omitempty"`
	RunID         string         `json:"run_id,omitempty"`
	LatencyMs     int64          `json:"latency_ms,omitempty"`
	Error         string         `json:"error,omitempty"`
	Fields        map[string]any `json:"fields,omitempty"`
}

// Options configure a Logger. Zero value gives stderr-only (no JSONL).
type Options struct {
	// JSONLPath: if non-empty, opens this file (append+create) and
	// writes one Event per line. Pass <out>/footprint.jsonl to seed
	// the working-memory MCP.
	JSONLPath string
	// Stderr controls whether human-readable slog output goes to
	// stderr. Default true. Set false for tests that snapshot stdout.
	Stderr bool
	// RunID is propagated onto every event so memory recall can group
	// by session. Optional; auto-generated when empty.
	RunID string
	// Level controls the minimum slog level emitted to stderr. Zero
	// value (slog.LevelInfo) keeps the documented default. Operators
	// raise to LevelWarn for noisier-tool integrations or lower to
	// LevelDebug for incident triage.
	Level slog.Level
	// ProfilePath, when non-empty, instructs Close to aggregate every
	// emitted event by name (count + latency_ms p50/p95/sum) and
	// write the summary as JSON to this path. Useful for build /
	// query throughput audits without parsing the raw JSONL.
	ProfilePath string
}

// Logger fans events out to its configured sinks. Safe for concurrent
// use; the JSONL file is guarded by a sync.Mutex (writes are O(event
// size) so contention is negligible vs the embedding/store work).
type Logger struct {
	slog        *slog.Logger
	jsonl       io.WriteCloser
	mu          sync.Mutex
	runID       string
	profilePath string
	// profile aggregates per-event latencies in memory; Close serializes
	// it to profilePath when set. Indexed by event name.
	profile map[string]*profileBucket
}

// profileBucket is the per-event-name aggregator behind --profile.
// Bounded memory: one bucket per distinct event name in this run,
// holding every latency we saw. For typical builds that's <20 names
// with <100 latencies each — well within budget.
type profileBucket struct {
	Count   int     `json:"count"`
	SumMs   int64   `json:"latency_ms_sum"`
	P50Ms   int64   `json:"latency_ms_p50"`
	P95Ms   int64   `json:"latency_ms_p95"`
	samples []int64 // raw latencies; finalize sorts + percentiles
}

// New constructs a Logger. The slog sink is always installed (stderr
// when opts.Stderr is true; io.Discard otherwise). The JSONL sink is
// optional. Errors opening the JSONL file are returned; the slog sink
// stays operational either way.
func New(opts Options) (*Logger, error) {
	l := &Logger{runID: opts.RunID, profilePath: opts.ProfilePath}
	if l.runID == "" {
		l.runID = newID()
	}
	if opts.ProfilePath != "" {
		l.profile = map[string]*profileBucket{}
	}
	if opts.Stderr {
		l.slog = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: opts.Level,
		}))
	} else {
		l.slog = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opts.JSONLPath != "" {
		if err := os.MkdirAll(filepath.Dir(opts.JSONLPath), 0o755); err != nil {
			return nil, fmt.Errorf("footprint: mkdir: %w", err)
		}
		f, err := os.OpenFile(opts.JSONLPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("footprint: open %s: %w", opts.JSONLPath, err)
		}
		l.jsonl = f
	}
	return l, nil
}

// Discard returns a Logger that writes nowhere. Useful in tests and in
// library callers that opt out of footprint recording.
func Discard() *Logger {
	return &Logger{
		slog:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		runID: newID(),
	}
}

// Close flushes/closes the JSONL sink and, when ProfilePath was set,
// writes the aggregated profile JSON. Idempotent.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	if err := l.writeProfile(); err != nil {
		// Profile failure is non-fatal — we still want to flush JSONL.
		l.slog.Warn("footprint: profile write failed", "error", err)
	}
	if l.jsonl == nil {
		return nil
	}
	err := l.jsonl.Close()
	l.jsonl = nil
	return err
}

// writeProfile finalizes percentile math and writes profile.json. Called
// once from Close; safe when ProfilePath was empty (no-op).
func (l *Logger) writeProfile() error {
	if l == nil || l.profilePath == "" || l.profile == nil {
		return nil
	}
	l.mu.Lock()
	for _, b := range l.profile {
		finalizePercentiles(b)
	}
	out := struct {
		RunID  string                    `json:"run_id"`
		Events map[string]*profileBucket `json:"events"`
	}{RunID: l.runID, Events: l.profile}
	l.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(l.profilePath), 0o755); err != nil {
		return fmt.Errorf("mkdir profile: %w", err)
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}
	return os.WriteFile(l.profilePath, data, 0o644)
}

// finalizePercentiles sorts the bucket's samples and stamps p50 / p95
// onto the bucket. Samples are kept (not zeroed) so a future caller
// could re-aggregate without rerunning the build.
func finalizePercentiles(b *profileBucket) {
	n := len(b.samples)
	if n == 0 {
		return
	}
	slices.Sort(b.samples)
	b.P50Ms = b.samples[(n-1)/2]
	idx95 := (n * 95) / 100
	if idx95 >= n {
		idx95 = n - 1
	}
	b.P95Ms = b.samples[idx95]
}

// RunID returns the per-Logger run id; callers (MCP server, eval) may
// surface this so downstream consumers can group by session.
func (l *Logger) RunID() string { return l.runID }

// Emit records a single event. Never returns; sink errors are written
// back to slog at WARN level. fields is alternating key/value (slog
// convention): Emit("build.start", "src", "/repo", "files", 42).
func (l *Logger) Emit(name string, fields ...any) {
	if l == nil {
		return
	}
	e := Event{
		Timestamp:     time.Now().UTC(),
		SchemaVersion: SchemaVersion,
		Event:         name,
		TraceID:       newID(),
		RunID:         l.runID,
		Fields:        kvToMap(fields),
	}
	l.write(e)
}

// Span starts a timed event; the returned done() closure emits the
// "<name>.done" event with latency_ms attached. extra accepts the same
// alternating key/value shape as Emit and is merged onto the done event.
//
// Typical usage:
//
//	done := log.Span("query.search", "intent_hash", h, "k", k)
//	defer done("hits", len(hits), "citation_drops", drops)
//
// When err is non-nil at done time, pass "error", err.Error() in extra
// (no special handling needed at this layer).
func (l *Logger) Span(name string, fields ...any) func(extra ...any) {
	if l == nil {
		return func(...any) {}
	}
	traceID := newID()
	start := time.Now()
	l.write(Event{
		Timestamp:     start.UTC(),
		SchemaVersion: SchemaVersion,
		Event:         name + ".start",
		TraceID:       traceID,
		RunID:         l.runID,
		Fields:        kvToMap(fields),
	})
	return func(extra ...any) {
		l.write(Event{
			Timestamp:     time.Now().UTC(),
			SchemaVersion: SchemaVersion,
			Event:         name + ".done",
			TraceID:       traceID,
			RunID:         l.runID,
			LatencyMs:     time.Since(start).Milliseconds(),
			Fields:        mergeMaps(kvToMap(fields), kvToMap(extra)),
		})
	}
}

// write is the single sink fan-out. We marshal once and reuse for both
// sinks (slog gets a flattened version, JSONL gets the raw Event).
func (l *Logger) write(e Event) {
	// slog: flatten Fields into top-level attrs for readability.
	args := []any{"event", e.Event, "trace_id", e.TraceID, "run_id", e.RunID}
	if e.LatencyMs != 0 {
		args = append(args, "latency_ms", e.LatencyMs)
	}
	if e.Error != "" {
		args = append(args, "error", e.Error)
	}
	for k, v := range e.Fields {
		args = append(args, k, v)
	}
	l.slog.Info("ckv", args...)

	// Profile aggregation: every Span done event contributes a count
	// regardless of measured latency. Filtering by name suffix (.done)
	// instead of LatencyMs > 0 means sub-millisecond operations
	// (mock embedder, hot caches) still show up in the profile with
	// count > 0 and latency stats at 0 — accurately reflecting that
	// they ran but were unmeasurably fast. Bucket key is the event
	// name (e.g. "query.search.done").
	if l.profile != nil && strings.HasSuffix(e.Event, ".done") {
		l.mu.Lock()
		b, ok := l.profile[e.Event]
		if !ok {
			b = &profileBucket{}
			l.profile[e.Event] = b
		}
		b.Count++
		b.SumMs += e.LatencyMs
		b.samples = append(b.samples, e.LatencyMs)
		l.mu.Unlock()
	}

	if l.jsonl == nil {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		l.slog.Warn("footprint: marshal failed", "error", err)
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.jsonl.Write(append(data, '\n')); err != nil {
		l.slog.Warn("footprint: write failed", "error", err)
	}
}

// ---- helpers ----

// kvToMap converts ["k1", v1, "k2", v2, ...] → map[string]any. Skips
// odd-count or non-string-key entries silently — same forgiveness as
// slog. Returns nil for empty input so JSONL stays compact.
func kvToMap(kv []any) map[string]any {
	if len(kv) == 0 {
		return nil
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			continue
		}
		m[k] = kv[i+1]
	}
	return m
}

func mergeMaps(a, b map[string]any) map[string]any {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// newID returns a 16-hex-char random id (8 bytes of entropy). Short
// enough for log scanning, long enough to make collisions unlikely
// within a session.
func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is exceptional — fall back to a
		// deterministic counter so logs stay coherent.
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
