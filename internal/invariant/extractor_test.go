package invariant

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestExtract_Tier1_ExistingMarkers(t *testing.T) {
	src := []byte(`package x

// CRITICAL: balance must remain non-negative after transfer.
// Otherwise underflow allows infinite minting.
func A() {}

// IMPORTANT: this initializer must run before any handler binds.
func B() {}

// WARNING: single-member governance is centralized.
func C() {}

// Deprecated: use NewV2 instead.
func Old() {}

// Just a normal comment.
func D() {}
`)
	results, err := Extract("x.go", src, Options{SkipTier3InTests: true})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	want := []struct {
		marker string
		tier   types.InvariantTier
	}{
		{"CRITICAL", types.InvariantTierExistingMarker},
		{"IMPORTANT", types.InvariantTierExistingMarker},
		{"WARNING", types.InvariantTierExistingMarker},
		{"Deprecated", types.InvariantTierExistingMarker},
	}
	if len(results) != len(want) {
		t.Fatalf("got %d results, want %d: %+v", len(results), len(want), results)
	}
	for i, w := range want {
		if results[i].Marker != w.marker || results[i].Tier != w.tier {
			t.Errorf("results[%d] = {%s, %d}, want {%s, %d}",
				i, results[i].Marker, results[i].Tier, w.marker, w.tier)
		}
	}
}

func TestExtract_Tier2_NewMarkers(t *testing.T) {
	src := []byte(`package x

// INVARIANT: gas refund must not exceed half the gas used.
func A() {}

// CONSENSUS: validator set rotates only at epoch boundaries.
func B() {}

// SECURITY: redact PII before logging.
func C() {}
`)
	results, err := Extract("x.go", src, Options{})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d, want 3: %+v", len(results), results)
	}
	for _, r := range results {
		if r.Tier != types.InvariantTierNewMarker {
			t.Errorf("expected tier 2, got %d for %s", r.Tier, r.Marker)
		}
	}
}

func TestExtract_Tier3_HeuristicPanic(t *testing.T) {
	src := []byte(`package x

func A() {
	panic("validator set must not shrink mid-epoch")
}

func B() {
	panic("just a generic crash")
}
`)
	results, err := Extract("x.go", src, Options{SkipTier3InTests: false})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d, want 1: %+v", len(results), results)
	}
	if results[0].Tier != types.InvariantTierHeuristic {
		t.Errorf("expected tier 3, got %d", results[0].Tier)
	}
	if results[0].Marker != "panic" {
		t.Errorf("Marker=%s, want panic", results[0].Marker)
	}
}

func TestExtract_Tier3_PanicFmtSprintf(t *testing.T) {
	src := []byte(`package x

import "fmt"

func A(v string) {
	panic(fmt.Sprintf("validator must verify %s", v))
}

func B(v string) {
	panic(fmt.Sprintf("plain crash %s", v))
}
`)
	results, err := Extract("x.go", src, Options{SkipTier3InTests: false})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d, want 1 (only A's panic has policy keyword): %+v", len(results), results)
	}
	if !strings.Contains(results[0].Text, "validator must") {
		t.Errorf("expected 'validator must' in text, got %q", results[0].Text)
	}
	if !strings.Contains(results[0].Marker, "fmt.Sprintf") {
		t.Errorf("expected Marker to mention fmt.Sprintf, got %q", results[0].Marker)
	}
}

func TestExtract_Tier3_PanicFmtErrorf(t *testing.T) {
	src := []byte(`package x

import "fmt"

func A() {
	panic(fmt.Errorf("byzantine validator detected: %w", nil))
}

func B() {
	panic(fmt.Errorf("connection refused: %w", nil))
}
`)
	results, err := Extract("x.go", src, Options{SkipTier3InTests: false})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d, want 1: %+v", len(results), results)
	}
	if !strings.Contains(strings.ToLower(results[0].Text), "byzantine") {
		t.Errorf("expected byzantine match, got %q", results[0].Text)
	}
}

func TestExtract_Tier3_PanicIdentWithNearbyComment(t *testing.T) {
	src := []byte(`package x

func A(err error) {
	// CRITICAL: validator quorum must hold; bail out hard rather than
	// continue with a corrupt state.
	if err != nil {
		panic(err)
	}
}

func B(err error) {
	if err != nil {
		panic(err) // no nearby policy comment — must NOT be flagged
	}
}

func C(err error) {
	// just a debug log
	if err != nil {
		panic(err)
	}
}
`)
	results, err := Extract("x.go", src, Options{SkipTier3InTests: false})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// A's panic should be flagged via Tier 1 (CRITICAL marker) AND Tier 3
	// (panic(err) with nearby policy comment). B and C must not be flagged.
	tier3 := 0
	for _, r := range results {
		if r.Tier == types.InvariantTierHeuristic {
			tier3++
			if !strings.Contains(strings.ToLower(r.Text), "validator") {
				t.Errorf("Tier 3 hit should carry the nearby comment text, got %q", r.Text)
			}
		}
	}
	if tier3 != 1 {
		t.Errorf("expected exactly 1 Tier 3 hit (A's panic(err) with CRITICAL comment), got %d", tier3)
	}
}

func TestExtract_Tier3_PanicIdent_NotFlaggedWithoutPolicyKeyword(t *testing.T) {
	src := []byte(`package x

func A(err error) {
	// Initialize the cache with default values.
	if err != nil {
		panic(err)
	}
}
`)
	results, err := Extract("x.go", src, Options{SkipTier3InTests: false})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, r := range results {
		if r.Tier == types.InvariantTierHeuristic {
			t.Errorf("panic(err) without policy keyword in nearby comment must NOT be flagged: %+v", r)
		}
	}
}

func TestExtract_Tier3_HeuristicErrorf(t *testing.T) {
	src := []byte(`package x

import "fmt"

func A() error {
	return fmt.Errorf("byzantine validator detected: %v", nil)
}

func B() error {
	return fmt.Errorf("connection refused")
}
`)
	results, err := Extract("x.go", src, Options{SkipTier3InTests: false})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d, want 1: %+v", len(results), results)
	}
	if !strings.Contains(strings.ToLower(results[0].Text), "byzantine") {
		t.Errorf("expected byzantine match, got %s", results[0].Text)
	}
}

func TestExtract_Tier3_SkippedInTestFiles(t *testing.T) {
	src := []byte(`package x

func TestX() {
	panic("validator must hold")
}
`)
	results, err := Extract("x_test.go", src, Options{SkipTier3InTests: true})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Tier 3 must be suppressed in *_test.go: got %+v", results)
	}
}

func TestExtract_Tier3_CapsAtMax(t *testing.T) {
	var b strings.Builder
	b.WriteString("package x\n")
	b.WriteString("func A() {\n")
	for i := 0; i < 25; i++ {
		b.WriteString(`panic("validator must verify")` + "\n")
	}
	b.WriteString("}\n")

	results, err := Extract("x.go", []byte(b.String()), Options{
		MaxTier3PerFile:  5,
		SkipTier3InTests: false,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(results) > 5 {
		t.Errorf("MaxTier3PerFile not enforced: got %d, want ≤5", len(results))
	}
}

func TestExtract_ParseError_NoPartialResults(t *testing.T) {
	src := []byte(`package x

// CRITICAL: ok

func A( {  // syntax error
}
`)
	if _, err := Extract("x.go", src, Options{}); err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestEmitChunks_DeterministicIDs(t *testing.T) {
	results := []Result{
		{Tier: types.InvariantTierExistingMarker, Marker: "CRITICAL",
			StartLine: 10, EndLine: 10, Text: "balance must be non-negative"},
	}
	a, refsA := EmitChunks("x.go", "deadbeef", results)
	b, refsB := EmitChunks("x.go", "deadbeef", results)

	if a[0].ID != b[0].ID {
		t.Errorf("ChunkInvariant IDs must be deterministic: %s != %s",
			a[0].ID, b[0].ID)
	}
	if refsA[0].ChunkID != a[0].ID {
		t.Errorf("ref ChunkID must match emitted chunk ID")
	}
	if refsB[0].Tier != types.InvariantTierExistingMarker {
		t.Errorf("ref tier mismatch")
	}
}

func TestAttachRefs_OverlappingSourceChunk(t *testing.T) {
	results := []Result{
		{StartLine: 12, EndLine: 12, Tier: types.InvariantTierExistingMarker, Marker: "CRITICAL", Text: "must hold"},
		{StartLine: 50, EndLine: 50, Tier: types.InvariantTierNewMarker, Marker: "INVARIANT", Text: "x must y"},
	}
	chunks := []types.Chunk{
		{File: "x.go", StartLine: 10, EndLine: 20}, // contains line 12
		{File: "x.go", StartLine: 30, EndLine: 40}, // contains nothing
		{File: "x.go", StartLine: 45, EndLine: 60}, // contains line 50
	}
	_, refs := EmitChunks("x.go", "h", results)
	AttachRefs(chunks, results, refs)

	if len(chunks[0].Invariants) != 1 {
		t.Errorf("chunks[0] should hold 1 invariant, got %d", len(chunks[0].Invariants))
	}
	if len(chunks[1].Invariants) != 0 {
		t.Errorf("chunks[1] should hold 0 invariants, got %d", len(chunks[1].Invariants))
	}
	if len(chunks[2].Invariants) != 1 {
		t.Errorf("chunks[2] should hold 1 invariant, got %d", len(chunks[2].Invariants))
	}
}
