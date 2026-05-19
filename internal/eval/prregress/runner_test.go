package prregress

import (
	"strings"
	"testing"
)

func TestSummarizeResults_Counts(t *testing.T) {
	results := []Result{
		{Pass: true},
		{Pass: false}, // fail
		{Pass: false}, // fail
		{Error: "boom"},
	}
	summary := SummarizeResults(results, 0.80)
	if !strings.Contains(summary, "total=4") {
		t.Errorf("missing total=4 in %q", summary)
	}
	if !strings.Contains(summary, "pass=1") {
		t.Errorf("missing pass=1 in %q", summary)
	}
	if !strings.Contains(summary, "fail=2") {
		t.Errorf("missing fail=2 in %q", summary)
	}
	if !strings.Contains(summary, "errored=1") {
		t.Errorf("missing errored=1 in %q", summary)
	}
	if !strings.Contains(summary, "threshold=0.80") {
		t.Errorf("missing threshold=0.80 in %q", summary)
	}
}

func TestSummarizeResults_Empty(t *testing.T) {
	summary := SummarizeResults(nil, 0.80)
	if !strings.Contains(summary, "total=0") {
		t.Errorf("empty: missing total=0 in %q", summary)
	}
}

func TestRunFixture_NilFixtureGuarded(t *testing.T) {
	_, err := RunFixture(nil, nil, nil)
	if err == nil {
		t.Error("expected error for nil fixture")
	}
}

func TestRunFixture_RequiresEmbedder(t *testing.T) {
	fx := &Fixture{
		SchemaVersion: "1",
		PRs:           []Entry{{ID: "x", Repo: "a/b", PRNumber: 1, SourcePath: "/tmp", BaseSHA: "aaaaaaa"}},
	}
	// No embedder in opts → fill() fails fast.
	_, err := RunFixture(nil, fx, &RunOptions{})
	if err == nil {
		t.Error("expected error when Embedder unset")
	}
}
