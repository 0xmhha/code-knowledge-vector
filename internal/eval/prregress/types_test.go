package prregress

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prs.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFixture_Roundtrip(t *testing.T) {
	path := writeFixture(t, `
schema_version: "1"
prs:
  - id: pr70
    repo: stable-net/go-stablenet
    pr_number: 70
    source_path: /tmp/repo
    base_sha: aa28927fb12048a59ac34608702eef5e1be90931
    threshold: 0.85
`)
	fx, err := LoadFixture(path)
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}
	if len(fx.PRs) != 1 {
		t.Fatalf("want 1 entry, got %d", len(fx.PRs))
	}
	if fx.PRs[0].ID != "pr70" || fx.PRs[0].Threshold != 0.85 {
		t.Errorf("entry parsed wrong: %+v", fx.PRs[0])
	}
}

// TestLoadFixture_ParsesNewMultiStageFields verifies multi-stage fixture
// fields — Entry now carries intent_ground_truth / changed_symbols /
// category, all optional. Legacy entries without them must still load
// unchanged.
func TestLoadFixture_ParsesNewMultiStageFields(t *testing.T) {
	path := writeFixture(t, `
schema_version: "1"
prs:
  - id: pr77
    repo: stable-net/go-stablenet
    pr_number: 77
    source_path: /tmp/repo
    base_sha: 0bf2f4d1bfeb6605006d556957ef8c045d8f8ed8
    intent_ground_truth: |
      Refresh AnzeonTipEnv currentBlock when header GasTip value changes.
    changed_symbols:
      - AnzeonTipEnv.SetCurrentBlock
      - AnzeonTipEnv.gasTipChanged
    category: gas_policy
  - id: pr_legacy
    repo: foo/bar
    pr_number: 1
    source_path: /tmp/repo
    base_sha: aaaaaaa
    threshold: 0.75
`)
	fx, err := LoadFixture(path)
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}
	if len(fx.PRs) != 2 {
		t.Fatalf("want 2 entries, got %d", len(fx.PRs))
	}
	e := fx.PRs[0]
	if e.Category != "gas_policy" {
		t.Errorf("category = %q, want gas_policy", e.Category)
	}
	if len(e.ChangedSymbols) != 2 || e.ChangedSymbols[0] != "AnzeonTipEnv.SetCurrentBlock" {
		t.Errorf("changed_symbols parsed wrong: %+v", e.ChangedSymbols)
	}
	if e.IntentGroundTruth == "" {
		t.Errorf("intent_ground_truth not parsed: %q", e.IntentGroundTruth)
	}
	// Legacy entry: new fields must be zero, not error out the loader.
	legacy := fx.PRs[1]
	if legacy.Category != "" || len(legacy.ChangedSymbols) != 0 || legacy.IntentGroundTruth != "" {
		t.Errorf("legacy entry should leave new fields empty: %+v", legacy)
	}
}

// TestLoadFixture_RealCorpusHasTwelveEntries verifies the actual
// testdata/prs.yaml shipped in the repo has exactly 12 entries.
// Catches accidental deletion / duplication during future fixture
// edits.
//
// The fixture's source_path field is a ${CKV_STABLENET_PATH} placeholder
// (portable handoff). LoadFixture validates source_path is non-empty, so
// we set the env var to any non-empty value for this test.
func TestLoadFixture_RealCorpusHasTwelveEntries(t *testing.T) {
	t.Setenv("CKV_STABLENET_PATH", "/tmp/stablenet-fake-for-test")
	// internal/eval/prregress/ → ../../../testdata/prs.yaml
	path := filepath.Join("..", "..", "..", "testdata", "prs.yaml")
	fx, err := LoadFixture(path)
	if err != nil {
		t.Fatalf("LoadFixture(%s): %v", path, err)
	}
	if got, want := len(fx.PRs), 12; got != want {
		t.Errorf("entry count = %d, want %d (multi-stage fixture set)", got, want)
	}
	// Every entry's source_path must be the expanded env value
	// (not the literal placeholder) — the file currently ships with
	// ${CKV_STABLENET_PATH} on every line.
	for _, e := range fx.PRs {
		if e.SourcePath == "${CKV_STABLENET_PATH}" {
			t.Errorf("%s: source_path not env-expanded (got literal placeholder)", e.ID)
		}
		if e.SourcePath != "/tmp/stablenet-fake-for-test" {
			t.Errorf("%s: source_path = %q, want /tmp/stablenet-fake-for-test", e.ID, e.SourcePath)
		}
	}
	// Every new entry (PR# >= 55, except the 4 legacy) must carry the
	// three new fields. Spot-check the structural promise.
	newIDs := map[string]bool{
		"pr77": true, "pr75": true, "pr73": true, "pr67": true,
		"pr63": true, "pr58": true, "pr56": true, "pr55": true,
	}
	for _, e := range fx.PRs {
		if !newIDs[e.ID] {
			continue
		}
		if e.IntentGroundTruth == "" {
			t.Errorf("%s: missing intent_ground_truth", e.ID)
		}
		if len(e.ChangedSymbols) == 0 {
			t.Errorf("%s: missing changed_symbols", e.ID)
		}
		if e.Category == "" {
			t.Errorf("%s: missing category", e.ID)
		}
	}
}

// TestLoadFixture_EnvExpandsSourcePath verifies the portable-handoff
// behavior: a ${VAR} placeholder in source_path is resolved against
// os.Environ at load time. Two paths:
//   - env var set   → field becomes the resolved absolute path
//   - env var unset → load fails with a hint about the placeholder
func TestLoadFixture_EnvExpandsSourcePath(t *testing.T) {
	body := `
schema_version: "1"
prs:
  - id: pr_test
    repo: foo/bar
    pr_number: 1
    source_path: ${CKV_TEST_STABLENET_PATH}
    base_sha: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	path := writeFixture(t, body)

	t.Run("env_set_resolves_placeholder", func(t *testing.T) {
		t.Setenv("CKV_TEST_STABLENET_PATH", "/abs/path/to/stable-net")
		fx, err := LoadFixture(path)
		if err != nil {
			t.Fatalf("LoadFixture with env set: %v", err)
		}
		if got, want := fx.PRs[0].SourcePath, "/abs/path/to/stable-net"; got != want {
			t.Errorf("source_path = %q, want %q", got, want)
		}
	})

	t.Run("env_unset_errors_with_hint", func(t *testing.T) {
		// Make sure the var is not set.
		t.Setenv("CKV_TEST_STABLENET_PATH", "")
		os.Unsetenv("CKV_TEST_STABLENET_PATH")
		_, err := LoadFixture(path)
		if err == nil {
			t.Fatal("expected error when placeholder is unset")
		}
		// The error must mention the original placeholder so the
		// operator knows which env var to set.
		if !strings.Contains(err.Error(), "${CKV_TEST_STABLENET_PATH}") {
			t.Errorf("error should name the unset placeholder; got: %v", err)
		}
	})
}

func TestLoadFixture_DefaultThreshold(t *testing.T) {
	// Omitted threshold → DefaultThreshold (0.80) backfilled.
	path := writeFixture(t, `
schema_version: "1"
prs:
  - id: pr70
    repo: foo/bar
    pr_number: 1
    source_path: /tmp/repo
    base_sha: aaaaaaa
`)
	fx, err := LoadFixture(path)
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}
	if fx.PRs[0].Threshold != DefaultThreshold {
		t.Errorf("threshold backfill = %g, want %g", fx.PRs[0].Threshold, DefaultThreshold)
	}
}

func TestLoadFixture_RejectsBadEntries(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing schema version", `prs: [{id: a, repo: foo/bar, pr_number: 1, source_path: /tmp, base_sha: aaaaaaa}]`},
		{"empty prs list", `schema_version: "1"`},
		{"missing id", `schema_version: "1"
prs: [{repo: foo/bar, pr_number: 1, source_path: /tmp, base_sha: aaaaaaa}]`},
		{"repo without slash", `schema_version: "1"
prs: [{id: x, repo: foobar, pr_number: 1, source_path: /tmp, base_sha: aaaaaaa}]`},
		{"zero pr_number", `schema_version: "1"
prs: [{id: x, repo: foo/bar, pr_number: 0, source_path: /tmp, base_sha: aaaaaaa}]`},
		{"short base sha", `schema_version: "1"
prs: [{id: x, repo: foo/bar, pr_number: 1, source_path: /tmp, base_sha: ab}]`},
		{"threshold above 1", `schema_version: "1"
prs: [{id: x, repo: foo/bar, pr_number: 1, source_path: /tmp, base_sha: aaaaaaa, threshold: 1.5}]`},
		{"duplicate id", `schema_version: "1"
prs:
  - {id: x, repo: foo/bar, pr_number: 1, source_path: /tmp, base_sha: aaaaaaa}
  - {id: x, repo: foo/bar, pr_number: 2, source_path: /tmp, base_sha: bbbbbbb}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFixture(t, tc.body)
			if _, err := LoadFixture(path); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestFileSetF1_Perfect(t *testing.T) {
	truth := []string{"a.go", "b.go", "c.go"}
	plan := []string{"a.go", "b.go", "c.go"}
	p, r, f1 := FileSetF1(plan, truth)
	if !approxEq(p, 1) || !approxEq(r, 1) || !approxEq(f1, 1) {
		t.Errorf("perfect match: P=%g R=%g F1=%g, want all 1", p, r, f1)
	}
}

func TestFileSetF1_PartialRecall(t *testing.T) {
	// Plan correctly names 2 of 3 truth files, no extra → P=1, R=2/3, F1=0.8.
	truth := []string{"a.go", "b.go", "c.go"}
	plan := []string{"a.go", "b.go"}
	p, r, f1 := FileSetF1(plan, truth)
	if !approxEq(p, 1) {
		t.Errorf("precision = %g, want 1", p)
	}
	if !approxEq(r, 2.0/3.0) {
		t.Errorf("recall = %g, want 2/3", r)
	}
	if !approxEq(f1, 0.8) {
		t.Errorf("F1 = %g, want 0.8", f1)
	}
}

func TestFileSetF1_OverGeneration(t *testing.T) {
	// Plan names 1 extra file → P=2/3, R=1, F1=0.8.
	truth := []string{"a.go", "b.go"}
	plan := []string{"a.go", "b.go", "c.go"}
	p, r, f1 := FileSetF1(plan, truth)
	if !approxEq(p, 2.0/3.0) {
		t.Errorf("precision = %g, want 2/3", p)
	}
	if !approxEq(r, 1) {
		t.Errorf("recall = %g, want 1", r)
	}
	if !approxEq(f1, 0.8) {
		t.Errorf("F1 = %g, want 0.8", f1)
	}
}

func TestFileSetF1_NoMatch(t *testing.T) {
	truth := []string{"a.go"}
	plan := []string{"x.go"}
	p, r, f1 := FileSetF1(plan, truth)
	if p != 0 || r != 0 || f1 != 0 {
		t.Errorf("no match: P=%g R=%g F1=%g, want all 0", p, r, f1)
	}
}

func TestFileSetF1_PlanDedupes(t *testing.T) {
	// Plan listing "a.go" twice must not double-count.
	truth := []string{"a.go", "b.go"}
	plan := []string{"a.go", "a.go", "b.go"}
	p, r, f1 := FileSetF1(plan, truth)
	if !approxEq(p, 1) || !approxEq(r, 1) || !approxEq(f1, 1) {
		t.Errorf("dedup case: P=%g R=%g F1=%g, want all 1", p, r, f1)
	}
}

func TestFileSetF1_EmptyTruthGuarded(t *testing.T) {
	// Empty truth set is degenerate — return zeros instead of dividing by 0.
	_, _, f1 := FileSetF1([]string{"a.go"}, nil)
	if f1 != 0 {
		t.Errorf("empty truth: F1 = %g, want 0", f1)
	}
}

func approxEq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
