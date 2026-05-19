package prregress

import (
	"math"
	"os"
	"path/filepath"
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
