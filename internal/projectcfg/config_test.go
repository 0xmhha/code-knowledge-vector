package projectcfg

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeYAML(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingReturnsErrNotFound(t *testing.T) {
	_, err := Load(t.TempDir())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLoadOrDefaultMissing(t *testing.T) {
	cfg, err := LoadOrDefault(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrDefault: %v", err)
	}
	if cfg.SchemaVersion != SchemaVersionCurrent {
		t.Errorf("default SchemaVersion = %q, want %q", cfg.SchemaVersion, SchemaVersionCurrent)
	}
	if !cfg.LanguageAllowed("go") || !cfg.LanguageAllowed("typescript") {
		t.Error("empty languages list must allow every language")
	}
}

func TestLoadFullSchema(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, `schema_version: "1"
languages: [go, typescript]
ignore:
  - "vendor/**"
  - "**/*_test.go"
chunking:
  file_header_lines: 30
important_symbols:
  - "^Handle.*"
  - ".*Service$"
skills_dir: ".claude/skills"
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.LanguageAllowed("go") || !cfg.LanguageAllowed("typescript") {
		t.Error("languages list not honored: go/typescript expected enabled")
	}
	if cfg.LanguageAllowed("solidity") {
		t.Error("languages list should restrict to go+typescript")
	}
	if cfg.Chunking.FileHeaderLines != 30 {
		t.Errorf("FileHeaderLines = %d, want 30", cfg.Chunking.FileHeaderLines)
	}
	if len(cfg.Ignore) != 2 || cfg.Ignore[0] != "vendor/**" {
		t.Errorf("Ignore not parsed: %+v", cfg.Ignore)
	}
	if cfg.SkillsDir != ".claude/skills" {
		t.Errorf("SkillsDir = %q", cfg.SkillsDir)
	}
}

func TestImportantSymbolsMatch(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, `schema_version: "1"
important_symbols:
  - "^Handle.*"
  - ".*Service$"
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cases := []struct {
		sym  string
		want bool
	}{
		{"HandleRequest", true},
		{"PaymentService", true},
		{"Logger", false},
		{"", false},
	}
	for _, c := range cases {
		if got := cfg.MatchesImportant(c.sym); got != c.want {
			t.Errorf("MatchesImportant(%q) = %v, want %v", c.sym, got, c.want)
		}
	}
}

func TestLoadRejectsBadInputs(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing schema_version", `languages: [go]`},
		{"wrong schema_version", `schema_version: "99"`},
		{"unknown language", `schema_version: "1"
languages: [rust]`},
		{"negative file_header_lines", `schema_version: "1"
chunking:
  file_header_lines: -1`},
		{"invalid regex", `schema_version: "1"
important_symbols:
  - "["`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeYAML(t, dir, tc.body)
			if _, err := Load(dir); err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
}

func TestNilReceiverIsNilSafe(t *testing.T) {
	var c *Config
	if !c.LanguageAllowed("go") {
		t.Error("nil receiver must allow every language")
	}
	if c.MatchesImportant("anything") {
		t.Error("nil receiver must report no matches")
	}
}

func TestBuildRootsParsesAndNormalizes(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, `schema_version: "1"
build_roots:
  - ./cmd/ckv
  - ./internal/build
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.BuildRoots) != 2 {
		t.Fatalf("BuildRoots = %v, want 2 entries", cfg.BuildRoots)
	}
	// Entries are kept as-written so the build layer can pass them
	// straight to `go list` (which accepts ./relative or import paths).
	if cfg.BuildRoots[0] != "./cmd/ckv" {
		t.Errorf("BuildRoots[0] = %q, want %q", cfg.BuildRoots[0], "./cmd/ckv")
	}
}

func TestBuildRootsAbsentMeansFullCorpus(t *testing.T) {
	// Absent build_roots: the build layer must walk every file under
	// srcRoot — the default walk-all-files behavior.
	dir := t.TempDir()
	writeYAML(t, dir, `schema_version: "1"`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.BuildRoots) != 0 {
		t.Errorf("BuildRoots = %v, want empty", cfg.BuildRoots)
	}
}

func TestBuildRootsRejectsEmptyEntry(t *testing.T) {
	// Empty / whitespace-only entries are typos. Fail loud at load.
	dir := t.TempDir()
	writeYAML(t, dir, `schema_version: "1"
build_roots:
  - ./cmd/ckv
  - ""
`)
	if _, err := Load(dir); err == nil {
		t.Error("expected error for empty build_roots entry")
	}
}

func TestBuildRootsRejectsAbsolutePath(t *testing.T) {
	// Absolute paths defeat the "package relative to srcRoot" contract
	// and make ckv.yaml non-portable across machines / CI checkouts.
	dir := t.TempDir()
	writeYAML(t, dir, `schema_version: "1"
build_roots:
  - /home/user/code/foo
`)
	if _, err := Load(dir); err == nil {
		t.Error("expected error for absolute build_roots entry")
	}
}
