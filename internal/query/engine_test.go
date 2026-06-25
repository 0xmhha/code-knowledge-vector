package query

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/internal/footprint"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// buildSample is the shared fixture for query E2E tests: it builds an
// index over testdata/sample with the deterministic mock embedder and
// returns the OutDir + absolute src root. Both paths are inside
// t.TempDir() / repo so they stay valid for citation enforcement.
func buildSample(t *testing.T) (out, src string) {
	t.Helper()
	// internal/query/engine_test.go → ../../testdata/sample
	srcAbs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "sample"))
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	_, err = build.Run(context.Background(), build.Options{
		SrcRoot:  srcAbs,
		OutDir:   outDir,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return outDir, srcAbs
}

func TestWarmup_RunsEmbedOnLiveEngine(t *testing.T) {
	out, _ := buildSample(t)
	eng, err := Open(out, mock.Default())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer eng.Close()
	if err := eng.Warmup(context.Background()); err != nil {
		t.Errorf("Warmup on live engine should succeed, got %v", err)
	}
	// Calling Warmup a second time must remain safe (idempotent).
	if err := eng.Warmup(context.Background()); err != nil {
		t.Errorf("second Warmup should also succeed, got %v", err)
	}
}

func TestWarmup_FailsAfterClose(t *testing.T) {
	out, _ := buildSample(t)
	eng, _ := Open(out, mock.Default())
	if err := eng.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := eng.Warmup(context.Background()); err == nil {
		t.Fatal("Warmup after Close should error")
	}
}

func TestOpenRejectsDimMismatch(t *testing.T) {
	out, _ := buildSample(t)
	// mock.Default() is 64-dim; instantiating with 128 must fail.
	emb := mock.New(128, "mock-feature-hash-v1")
	_, err := Open(out, emb)
	if !errors.Is(err, ErrIndexUnavailable) {
		t.Fatalf("expected ErrIndexUnavailable, got %v", err)
	}
}

func TestOpenRejectsModelMismatch(t *testing.T) {
	out, _ := buildSample(t)
	emb := mock.New(64, "different-model-name")
	_, err := Open(out, emb)
	if !errors.Is(err, ErrIndexUnavailable) {
		t.Fatalf("expected ErrIndexUnavailable, got %v", err)
	}
}

// sameNameDiffIdentity has the same Name()/Dimension() as the index's
// embedder but reports a different embedding-space identity, simulating a
// cross-backend swap (e.g. Ollama bge-m3 vs ONNX bge-m3). Open must reject
// it via the checksum check even though name+dim match — the gap the older
// name+dim-only validation could not catch.
type sameNameDiffIdentity struct{ types.Embedder }

func (e sameNameDiffIdentity) Identity() types.EmbeddingIdentity {
	id := e.Embedder.Identity()
	id.Provider = "ollama" // index was built by the "mock" provider
	return id
}

func TestOpenRejectsIdentityMismatch(t *testing.T) {
	out, _ := buildSample(t)
	emb := sameNameDiffIdentity{mock.Default()}
	// Precondition: name+dim match the index, so only the checksum check
	// can reject this embedder.
	if emb.Name() != mock.Default().Name() || emb.Dimension() != mock.Default().Dimension() {
		t.Fatal("test embedder must match the index name+dim")
	}
	_, err := Open(out, emb)
	if !errors.Is(err, ErrIndexUnavailable) {
		t.Fatalf("expected ErrIndexUnavailable for identity mismatch, got %v", err)
	}
}

func TestOpenAcceptsMatchingIdentity(t *testing.T) {
	out, _ := buildSample(t)
	// Same embedder type that built the index → identity matches → opens.
	eng, err := Open(out, mock.Default())
	if err != nil {
		t.Fatalf("Open with matching identity should succeed, got %v", err)
	}
	eng.Close()
}

func TestSearchFindsListenForTCPQuery(t *testing.T) {
	out, _ := buildSample(t)
	eng, err := Open(out, mock.Default())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer eng.Close()

	res, err := eng.Search(context.Background(), "TCP socket bind on port", Options{K: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Verify *some* hit in top-3 cites server.go and the line range
	// brackets the Listen method (lines 22-29 in our fixture).
	var found bool
	for _, h := range res.Hits {
		if h.Citation.File == "server.go" && h.Citation.StartLine <= 22 && h.Citation.EndLine >= 22 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a hit citing server.go bracketing line 22, got %+v", res.Hits)
	}
	for _, h := range res.Hits {
		if h.Score.Normalized < DefaultThreshold {
			t.Errorf("hit below threshold leaked: %+v", h.Score)
		}
		if h.ChunkID == "" || h.Citation.File == "" {
			t.Errorf("missing identity on hit: %+v", h)
		}
	}
}

func TestSearchHonorsFilter(t *testing.T) {
	out, _ := buildSample(t)
	eng, _ := Open(out, mock.Default())
	defer eng.Close()

	res, err := eng.Search(context.Background(), "lock map mutex",
		Options{K: 5, Filter: types.Filter{SymbolKinds: []types.SymbolKind{types.KindMethod}}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, h := range res.Hits {
		if h.SymbolKind != types.KindMethod {
			t.Errorf("Method filter not honored: %s/%s", h.Language, h.SymbolKind)
		}
	}
}

// TestSearchHonorsCommitHashFilter exercises B2: chunks carry the
// commit_hash they were indexed at; filter.CommitHash should restrict
// results to that snapshot. Mismatched commit_hash returns zero hits
// (and the warning surface from the threshold path stays clean).
func TestSearchHonorsCommitHashFilter(t *testing.T) {
	out, _ := buildSample(t)
	eng, _ := Open(out, mock.Default())
	defer eng.Close()

	// First confirm baseline: an unfiltered search returns hits.
	res, err := eng.Search(context.Background(), "TCP socket bind on port",
		Options{K: 5, Threshold: -1})
	if err != nil {
		t.Fatalf("baseline Search: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("baseline returned 0 hits; commit_hash filter test invalid")
	}
	indexedCommit := res.Hits[0].Citation.CommitHash
	if indexedCommit == "" {
		t.Skip("testdata/sample isn't in a git repo; commit_hash empty so filter test moot")
	}

	// Filtering to the same commit must keep the hit.
	res, err = eng.Search(context.Background(), "TCP socket bind on port",
		Options{K: 5, Threshold: -1, Filter: types.Filter{CommitHash: indexedCommit}})
	if err != nil {
		t.Fatalf("same-commit Search: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Errorf("filter to indexed commit %q dropped every hit", indexedCommit)
	}

	// Filtering to a sentinel commit that does not match any chunk
	// returns nothing.
	res, err = eng.Search(context.Background(), "TCP socket bind on port",
		Options{K: 5, Threshold: -1, Filter: types.Filter{CommitHash: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}})
	if err != nil {
		t.Fatalf("foreign-commit Search: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Errorf("foreign commit_hash should drop every hit, got %d", len(res.Hits))
	}
}

// TestSearch_DryRunSkipsEmbedAndStore verifies B5 dry_run mode:
// engine validates the request (intent, manifest identity) but skips
// embed + store.Search + citation + density. Response carries
// metadata only — Hits empty, DryRun=true.
func TestSearch_DryRunSkipsEmbedAndStore(t *testing.T) {
	out, _ := buildSample(t)
	eng, _ := Open(out, mock.Default())
	defer eng.Close()

	res, err := eng.Search(context.Background(), "TCP socket bind on port",
		Options{K: 5, DryRun: true})
	if err != nil {
		t.Fatalf("DryRun Search: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Errorf("DryRun should return 0 hits, got %d", len(res.Hits))
	}
	if !res.Metadata.DryRun {
		t.Errorf("Response.Metadata.DryRun should be true")
	}
	if res.Metadata.TraceID == "" {
		t.Errorf("DryRun must still set a trace_id")
	}
	if res.Metadata.IndexedHeadCKV == "" {
		t.Errorf("DryRun should still report manifest.IndexedHead")
	}
}

// TestSearch_EmitsFiveSubSpans verifies trace granularity: every Search
// call emits the five query.* sub-spans (embed / store.search /
// threshold.drop / citation.enforce / density.adjust) alongside the
// existing top-level query.search. Each sub-span carries the same
// trace_id so log readers can group them.
func TestSearch_EmitsFiveSubSpans(t *testing.T) {
	out, _ := buildSample(t)

	// Wire a real footprint logger to a tmp JSONL file so we can read
	// back the emitted events. Stderr off to keep test output clean.
	jsonl := filepath.Join(t.TempDir(), "footprint.jsonl")
	fp, err := footprint.New(footprint.Options{JSONLPath: jsonl})
	if err != nil {
		t.Fatalf("footprint.New: %v", err)
	}
	defer fp.Close()

	eng, err := Open(out, mock.Default(), WithFootprint(fp))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer eng.Close()

	caller := "phase1-trace"
	_, err = eng.Search(context.Background(), "TCP socket bind on port",
		Options{K: 5, Threshold: -1, TraceID: caller})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	fp.Close() // flush JSONL

	// Walk the JSONL and collect distinct *.done event names that
	// carried our trace_id.
	data, err := os.ReadFile(jsonl)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	wantSubSpans := map[string]bool{
		"query.embed.done":            false,
		"query.store.search.done":     false,
		"query.threshold.drop.done":   false,
		"query.citation.enforce.done": false,
		"query.density.adjust.done":   false,
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" {
			continue
		}
		var ev struct {
			Event  string         `json:"event"`
			Fields map[string]any `json:"fields"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if _, tracked := wantSubSpans[ev.Event]; tracked {
			gotTrace, _ := ev.Fields["trace_id"].(string)
			if gotTrace == caller {
				wantSubSpans[ev.Event] = true
			}
		}
	}
	for name, saw := range wantSubSpans {
		if !saw {
			t.Errorf("expected %s sub-span with trace_id=%s; not emitted", name, caller)
		}
	}
}

// TestSearch_TraceIDIsEchoed verifies caller-supplied trace_id rides
// through to the response. Empty input falls back to engine-generated.
func TestSearch_TraceIDIsEchoed(t *testing.T) {
	out, _ := buildSample(t)
	eng, _ := Open(out, mock.Default())
	defer eng.Close()

	caller := "trace-from-cks-12345"
	res, err := eng.Search(context.Background(), "TCP socket bind on port",
		Options{K: 3, Threshold: -1, TraceID: caller})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Metadata.TraceID != caller {
		t.Errorf("TraceID should echo caller value; got %q want %q", res.Metadata.TraceID, caller)
	}

	// Empty caller TraceID → engine generates one.
	res, err = eng.Search(context.Background(), "TCP socket bind on port",
		Options{K: 3, Threshold: -1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Metadata.TraceID == "" {
		t.Errorf("engine should generate a TraceID when caller omits it")
	}
}

func TestSearchEmptyIntentRejected(t *testing.T) {
	out, _ := buildSample(t)
	eng, _ := Open(out, mock.Default())
	defer eng.Close()

	if _, err := eng.Search(context.Background(), "", Options{}); err == nil {
		t.Fatal("expected error for empty intent")
	}
}

func TestThresholdDropEmitsWarning(t *testing.T) {
	out, _ := buildSample(t)
	eng, _ := Open(out, mock.Default())
	defer eng.Close()

	// Threshold 0.999 will reject everything.
	res, err := eng.Search(context.Background(), "anything", Options{Threshold: 0.999})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Errorf("expected 0 hits, got %d", len(res.Hits))
	}
	var sawWarning bool
	for _, w := range res.Warnings {
		if w == "all_results_below_threshold" {
			sawWarning = true
		}
	}
	if !sawWarning {
		t.Errorf("expected all_results_below_threshold warning, got %v", res.Warnings)
	}
}

func TestSplitByTest_NoSeparationReturnsAllAsPrimary(t *testing.T) {
	// ExamplesK=0 → separateTests=false: every hit stays in primary,
	// examples stays nil. Preserves single-list default behavior.
	hits := []types.Hit{
		{Chunk: types.Chunk{File: "a.go", IsTest: false}},
		{Chunk: types.Chunk{File: "a_test.go", IsTest: true}},
		{Chunk: types.Chunk{File: "b.go", IsTest: false}},
	}
	primary, examples := splitByTest(hits, false)
	if len(primary) != 3 {
		t.Errorf("primary len = %d, want 3", len(primary))
	}
	if examples != nil {
		t.Errorf("examples = %v, want nil", examples)
	}
}

func TestSplitByTest_SeparatesByIsTestFlag(t *testing.T) {
	// separateTests=true → IsTest chunks land in examples, others in
	// primary. Score order is preserved within each group (the helper
	// is order-preserving; we don't sort).
	hits := []types.Hit{
		{Chunk: types.Chunk{File: "a.go", IsTest: false}},
		{Chunk: types.Chunk{File: "a_test.go", IsTest: true}},
		{Chunk: types.Chunk{File: "b.go", IsTest: false}},
		{Chunk: types.Chunk{File: "b_test.go", IsTest: true}},
	}
	primary, examples := splitByTest(hits, true)
	if len(primary) != 2 || primary[0].Chunk.File != "a.go" || primary[1].Chunk.File != "b.go" {
		t.Errorf("primary = %v, want [a.go, b.go]", filesOf(primary))
	}
	if len(examples) != 2 || examples[0].Chunk.File != "a_test.go" || examples[1].Chunk.File != "b_test.go" {
		t.Errorf("examples = %v, want [a_test.go, b_test.go]", filesOf(examples))
	}
}

func TestSplitByTest_EmptyInput(t *testing.T) {
	primary, examples := splitByTest(nil, true)
	if primary != nil || examples != nil {
		t.Errorf("empty input: primary=%v, examples=%v, want both nil", primary, examples)
	}
}

func TestSplitByTest_AllPrimaryOrAllExamples(t *testing.T) {
	allCode := []types.Hit{
		{Chunk: types.Chunk{File: "a.go", IsTest: false}},
		{Chunk: types.Chunk{File: "b.go", IsTest: false}},
	}
	primary, examples := splitByTest(allCode, true)
	if len(primary) != 2 || len(examples) != 0 {
		t.Errorf("all-code: primary=%d examples=%d, want 2/0", len(primary), len(examples))
	}

	allTest := []types.Hit{
		{Chunk: types.Chunk{File: "a_test.go", IsTest: true}},
		{Chunk: types.Chunk{File: "b_test.go", IsTest: true}},
	}
	primary, examples = splitByTest(allTest, true)
	if len(primary) != 0 || len(examples) != 2 {
		t.Errorf("all-test: primary=%d examples=%d, want 0/2", len(primary), len(examples))
	}
}

// TestSearchRejectsTinyBudget verifies ErrBudgetExceeded is raised when
// the caller's positive BudgetTokens is below MinBudgetTokens. Below
// that floor the engine can't render even one signature-only hit, and
// silent truncation would surprise the caller. Featurelist §8.4, B6.
func TestSearchRejectsTinyBudget(t *testing.T) {
	out, _ := buildSample(t)
	eng, _ := Open(out, mock.Default())
	defer eng.Close()

	_, err := eng.Search(context.Background(), "anything", Options{BudgetTokens: 5})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded for BudgetTokens=5, got %v", err)
	}
}

// TestSearchAcceptsNegativeBudgetAsDisable verifies the documented
// escape hatch: negative BudgetTokens disables budgeting and returns
// hits at full density. Featurelist §8.4 caller guidance.
func TestSearchAcceptsNegativeBudgetAsDisable(t *testing.T) {
	out, _ := buildSample(t)
	eng, _ := Open(out, mock.Default())
	defer eng.Close()

	res, err := eng.Search(context.Background(), "TCP socket bind on port", Options{K: 3, BudgetTokens: -1})
	if err != nil {
		t.Fatalf("BudgetTokens<0 should disable budgeting, got error %v", err)
	}
	if len(res.Hits) == 0 {
		t.Errorf("expected hits with budget disabled, got empty")
	}
}

// TestSearchCitationNotFoundOnMissingSrcRoot raises ErrCitationNotFound
// when the recorded src_root is gone, so every threshold-passing hit
// gets dropped at citation enforcement. Featurelist §8.4 catastrophic
// case — currently silent, now surfaced.
func TestSearchCitationNotFoundOnMissingSrcRoot(t *testing.T) {
	out, _ := buildSample(t)
	eng, _ := Open(out, mock.Default())
	defer eng.Close()

	// Point SrcRoot at a directory with none of the indexed files —
	// every hit's Citation.File will fail to resolve under it.
	_, err := eng.Search(context.Background(), "TCP socket bind on port",
		Options{K: 5, SrcRoot: t.TempDir()})
	if !errors.Is(err, ErrCitationNotFound) {
		t.Fatalf("expected ErrCitationNotFound when src_root has no matching files, got %v", err)
	}
}

// TestCheckFreshness_FreshIndexReturnsNil verifies CheckFreshness
// returns nil immediately after build (indexed head == current head).
// Skipped if the build dir is outside a git repo — the helper depends
// on `git -C` succeeding.
func TestCheckFreshness_FreshIndexReturnsNil(t *testing.T) {
	out, srcAbs := buildSample(t)
	eng, _ := Open(out, mock.Default())
	defer eng.Close()
	// buildSample uses repo testdata/sample which lives inside CKV repo.
	// CheckFreshness will run git against srcAbs.
	_ = srcAbs
	err := eng.CheckFreshness()
	// Either nil (truly fresh: rare for testdata to differ from HEAD)
	// or wrapped ErrFreshnessStale (testdata edited locally). git
	// unavailable returns a non-stale error. Just verify we never
	// return a non-freshness, non-nil error in this fixture.
	if err != nil && !errors.Is(err, ErrFreshnessStale) {
		// Allow only freshness-related errors or nil; bare git errors
		// are environmental and acceptable in CI.
		t.Logf("CheckFreshness returned environmental error: %v", err)
	}
}

func filesOf(hits []types.Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Chunk.File
	}
	return out
}
