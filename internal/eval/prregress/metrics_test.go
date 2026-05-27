package prregress

import (
	"context"
	"math"
	"strings"
	"testing"
)

// --- IntentScore (E1) -----------------------------------------------------

func TestIntentScore_IdenticalText(t *testing.T) {
	s := IntentScore(
		"Refresh AnzeonTipEnv currentBlock when GasTip changes",
		"Refresh AnzeonTipEnv currentBlock when GasTip changes",
	)
	if math.Abs(s-1.0) > 1e-9 {
		t.Errorf("identical text should score 1.0, got %g", s)
	}
}

func TestIntentScore_DisjointTokens(t *testing.T) {
	s := IntentScore(
		"alpha bravo charlie",
		"delta echo foxtrot",
	)
	if s != 0 {
		t.Errorf("disjoint sets should score 0, got %g", s)
	}
}

func TestIntentScore_PartialOverlap(t *testing.T) {
	plan := "refresh tip env when gas tip changes via governance"
	ref := "refresh AnzeonTipEnv currentBlock when GasTip changes"
	// Shared tokens: refresh, tip, env, when, gas, changes (after lowercase + stopword strip).
	// "tip" appears in both; "currentBlock" → currentblock (only in ref);
	// "anzeontipenv" only in ref; "governance" only in plan. Token-F1 in (0, 1).
	s := IntentScore(plan, ref)
	if s <= 0 || s >= 1 {
		t.Errorf("partial overlap should score in (0,1), got %g", s)
	}
}

func TestIntentScore_EmptyInput(t *testing.T) {
	if got := IntentScore("", "ref"); got != 0 {
		t.Errorf("empty plan → 0, got %g", got)
	}
	if got := IntentScore("plan", ""); got != 0 {
		t.Errorf("empty reference → 0, got %g", got)
	}
}

func TestIntentScore_CaseFold(t *testing.T) {
	// Case differences must not change the score.
	a := IntentScore("Refresh GasTip Env", "refresh gastip env")
	b := IntentScore("refresh gastip env", "refresh gastip env")
	if math.Abs(a-b) > 1e-9 {
		t.Errorf("case-fold mismatch: a=%g b=%g", a, b)
	}
}

func TestIntentScore_KoreanTokens(t *testing.T) {
	// Hangul syllables tokenize as runs; this test only requires the
	// metric to *not* crash and to produce a non-zero score when the
	// inputs share a Hangul word.
	s := IntentScore("거버넌스 가스팁 변경", "거버넌스 가스팁 정책")
	if s <= 0 {
		t.Errorf("shared Hangul tokens → >0 expected, got %g", s)
	}
}

func TestIntentScore_StopwordsDropped(t *testing.T) {
	// "the of and" are stopwords; remaining shared token is "code".
	// With only "code" in both sets, F1 = 1.0.
	s := IntentScore("the code", "code and the")
	if math.Abs(s-1.0) > 1e-9 {
		t.Errorf("after stopword strip both sides reduce to {code}; want 1.0, got %g", s)
	}
}

// --- SymbolF1 (E2) --------------------------------------------------------

func TestSymbolF1_ExactMatch(t *testing.T) {
	plan := []string{"AnzeonTipEnv.SetCurrentBlock", "RemotesBelowTip"}
	truth := []string{"AnzeonTipEnv.SetCurrentBlock", "RemotesBelowTip"}
	p, r, f := SymbolF1(plan, truth)
	if p != 1 || r != 1 || f != 1 {
		t.Errorf("exact match → 1/1/1, got p=%g r=%g f=%g", p, r, f)
	}
}

func TestSymbolF1_EmptyTruth(t *testing.T) {
	p, r, f := SymbolF1([]string{"Foo"}, nil)
	if p != 0 || r != 0 || f != 0 {
		t.Errorf("empty truth → 0/0/0, got p=%g r=%g f=%g", p, r, f)
	}
}

func TestSymbolF1_HandlesDescriptiveTruth(t *testing.T) {
	// pr67 / pr56 style entries: descriptive truth, regex won't pick
	// the same string from a plan. Expect zero recall (known limitation
	// per handoff §7) but no panic / NaN.
	plan := []string{"RegisterPrecompile", "HardforkConfig.Boho"}
	truth := []string{"secp256r1 precompile registration"}
	p, r, f := SymbolF1(plan, truth)
	if math.IsNaN(p) || math.IsNaN(r) || math.IsNaN(f) {
		t.Fatalf("NaN result on descriptive truth: %g/%g/%g", p, r, f)
	}
	if r != 0 {
		t.Logf("descriptive truth recall is non-zero (%g) — possible accidental overlap, expected 0 for this case", r)
	}
}

func TestSymbolF1_PartialOverlapWithCaseFold(t *testing.T) {
	// `Account.Extra` (plan) ↔ `account.extra` (truth-lowercased) must
	// match. Both normalize to `account_extra`.
	plan := []string{"Account.Extra", "Validate"}
	truth := []string{"account.extra", "ValidateExtra", "AccountExtraValidMask"}
	p, r, f := SymbolF1(plan, truth)
	// tp = 1 (account_extra matches); planSet = 2 ({account_extra, validate});
	// truthSet = 3. P = 1/2 = 0.5, R = 1/3 ≈ 0.333, F1 = 0.4.
	if math.Abs(p-0.5) > 1e-9 || math.Abs(r-1.0/3.0) > 1e-9 || math.Abs(f-0.4) > 1e-9 {
		t.Errorf("partial overlap: got p=%g r=%g f=%g; want 0.5/0.333/0.4", p, r, f)
	}
}

func TestSymbolF1_DedupOnNormalization(t *testing.T) {
	// `GovMinter._cleanupBurnDeposit` and `GovMinter cleanupBurnDeposit`
	// both fold to `govminter_cleanupburndeposit`, so the truth set has
	// one element, not two.
	truth := []string{"GovMinter._cleanupBurnDeposit", "GovMinter cleanupBurnDeposit"}
	plan := []string{"GovMinter._cleanupBurnDeposit"}
	p, r, f := SymbolF1(plan, truth)
	if p != 1 || r != 1 || f != 1 {
		t.Errorf("dedup-after-normalize: got p=%g r=%g f=%g; want 1/1/1", p, r, f)
	}
}

// --- ExtractPlanSteps / ExtractPlanSymbols --------------------------------

func TestExtractPlanSteps_StripsExpectedChanges(t *testing.T) {
	md := strings.Join([]string{
		"## Problem",
		"The GasTip refresh logic is incomplete.",
		"",
		"## Approach",
		"Add a header-change guard.",
		"",
		"## Expected Changes",
		"- gas_policy.go: add guard",
		"- gas_policy_test.go: cover",
	}, "\n")
	got := ExtractPlanSteps(md)
	if strings.Contains(got, "Expected Changes") {
		t.Errorf("ExtractPlanSteps must remove 'Expected Changes' tail; got: %q", got)
	}
	if !strings.Contains(got, "GasTip refresh") {
		t.Errorf("ExtractPlanSteps removed too much; got: %q", got)
	}
}

func TestExtractPlanSteps_NoSection(t *testing.T) {
	md := "## Approach\n\nDo the thing.\n"
	got := ExtractPlanSteps(md)
	if !strings.Contains(got, "Do the thing.") {
		t.Errorf("ExtractPlanSteps: missing-section fallback should return full markdown; got %q", got)
	}
}

func TestExtractPlanSteps_Empty(t *testing.T) {
	if got := ExtractPlanSteps(""); got != "" {
		t.Errorf("empty input → empty output; got %q", got)
	}
}

func TestExtractPlanSymbols_PascalCaseAndDotted(t *testing.T) {
	md := "Modify `AnzeonTipEnv.SetCurrentBlock` and call `RemotesBelowTip`; also touch Server."
	got := ExtractPlanSymbols(md)
	want := map[string]bool{
		"AnzeonTipEnv.SetCurrentBlock": true,
		"RemotesBelowTip":              true,
		"Server":                       true,
	}
	for _, s := range got {
		delete(want, s)
	}
	if len(want) > 0 {
		t.Errorf("ExtractPlanSymbols missed: %v (got %v)", want, got)
	}
}

func TestExtractPlanSymbols_Dedup(t *testing.T) {
	md := "Foo and Foo and Foo.Bar and Foo.Bar"
	got := ExtractPlanSymbols(md)
	if len(got) != 2 {
		t.Errorf("expected 2 unique symbols, got %d: %v", len(got), got)
	}
	if got[0] != "Foo" || got[1] != "Foo.Bar" {
		t.Errorf("order-preserving dedup expected [Foo, Foo.Bar], got %v", got)
	}
}

func TestExtractPlanSymbols_IgnoresLowercaseLeading(t *testing.T) {
	// "the function modify" should be ignored — only PascalCase / dotted
	// PascalCase identifiers survive.
	md := "the function modify currentBlock and account state"
	got := ExtractPlanSymbols(md)
	if len(got) != 0 {
		t.Errorf("expected zero symbols for prose-only text, got %v", got)
	}
}

// --- PlanStepsScore (E3) --------------------------------------------------

func TestPlanStepsScore_OverlapWithCommitMessages(t *testing.T) {
	plan := "add header gastip change guard; recompute tip env on header diff"
	commits := "fix: refresh tip env when header gastip changes\n\nGuard against stale tip after governance change."
	s := PlanStepsScore(plan, commits)
	if s <= 0 || s >= 1 {
		t.Errorf("partial overlap expected (0,1); got %g", s)
	}
}

func TestPlanStepsScore_EmptyCommits(t *testing.T) {
	if got := PlanStepsScore("a plan", ""); got != 0 {
		t.Errorf("empty commits → 0, got %g", got)
	}
}

func TestPlanStepsScore_EmptyPlan(t *testing.T) {
	if got := PlanStepsScore("", "commits"); got != 0 {
		t.Errorf("empty plan → 0, got %g", got)
	}
}

// --- IntentCosine (optional embedder fallback) ----------------------------

// fakeEmbedder returns deterministic vectors keyed by input string so we
// can assert cosine outputs without dragging in mock package state.
type fakeEmbedder struct {
	vecs map[string][]float32
}

func (f *fakeEmbedder) Name() string        { return "fake" }
func (f *fakeEmbedder) Dimension() int      { return 3 }
func (f *fakeEmbedder) MaxInputTokens() int { return 1024 }
func (f *fakeEmbedder) Embed(_ context.Context, batch []string) ([][]float32, error) {
	out := make([][]float32, len(batch))
	for i, s := range batch {
		v, ok := f.vecs[s]
		if !ok {
			// Default: zero vector; cosine → 0.
			v = []float32{0, 0, 0}
		}
		out[i] = v
	}
	return out, nil
}

func TestIntentCosine_IdenticalVectorsScoreOne(t *testing.T) {
	emb := &fakeEmbedder{vecs: map[string][]float32{
		"x": {1, 0, 0},
		"y": {1, 0, 0},
	}}
	got, err := IntentCosine(context.Background(), "x", "y", emb)
	if err != nil {
		t.Fatalf("IntentCosine: %v", err)
	}
	if math.Abs(got-1.0) > 1e-6 {
		t.Errorf("parallel unit vectors should score 1.0, got %g", got)
	}
}

func TestIntentCosine_OrthogonalVectorsScoreZero(t *testing.T) {
	emb := &fakeEmbedder{vecs: map[string][]float32{
		"x": {1, 0, 0},
		"y": {0, 1, 0},
	}}
	got, err := IntentCosine(context.Background(), "x", "y", emb)
	if err != nil {
		t.Fatalf("IntentCosine: %v", err)
	}
	if math.Abs(got) > 1e-9 {
		t.Errorf("orthogonal vectors should score 0, got %g", got)
	}
}

func TestIntentCosine_NilEmbedder(t *testing.T) {
	if _, err := IntentCosine(context.Background(), "x", "y", nil); err == nil {
		t.Error("nil embedder should error")
	}
}

func TestIntentCosine_EmptyInputReturnsZero(t *testing.T) {
	emb := &fakeEmbedder{}
	got, err := IntentCosine(context.Background(), "", "x", emb)
	if err != nil {
		t.Fatalf("IntentCosine: %v", err)
	}
	if got != 0 {
		t.Errorf("empty plan → 0, got %g", got)
	}
}
