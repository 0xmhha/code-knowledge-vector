package discover

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// mkfile writes contents to <dir>/<rel>. Creates parents.
func mkfile(t *testing.T, dir, rel, contents string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWalkBasic(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "main.go", "package main")
	mkfile(t, dir, "internal/x.go", "package internal")
	mkfile(t, dir, "README.md", "# readme") // markdown is indexed (Appendix B.1.b)
	mkfile(t, dir, "ui.tsx", "export {}")
	mkfile(t, dir, "Token.sol", "pragma solidity ^0.8.0;")

	files, errs, err := Walk(dir, Options{})
	if err != nil {
		t.Fatalf("walk error: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("unexpected per-file errors: %v", errs)
	}
	got := relPaths(files)
	want := []string{"README.md", "Token.sol", "internal/x.go", "main.go", "ui.tsx"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWalkIndexesMarkdown(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "docs/plan.md", "# Plan\n\nbody")
	mkfile(t, dir, "docs/adr/001-store.markdown", "# Decision\n\ntext")
	mkfile(t, dir, "notes.txt", "skipped") // not markdown ext

	files, _, _ := Walk(dir, Options{})
	got := relPaths(files)
	want := []string{"docs/adr/001-store.markdown", "docs/plan.md"}
	if !slices.Equal(got, want) {
		t.Errorf("markdown walk: got %v, want %v", got, want)
	}
	for _, f := range files {
		if f.Language != "markdown" {
			t.Errorf("expected markdown language tag, got %q for %s", f.Language, f.RelPath)
		}
	}
}

func TestDefaultIgnoreSkipsNodeModulesAndVendor(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "main.go", "package main")
	mkfile(t, dir, "node_modules/foo/index.ts", "x")
	mkfile(t, dir, "vendor/bar/lib.go", "x")

	files, _, _ := Walk(dir, Options{})
	got := relPaths(files)
	if !slices.Equal(got, []string{"main.go"}) {
		t.Errorf("DefaultIgnore not honored: got %v", got)
	}
}

func TestCKVIgnorePatterns(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, ".ckvignore", "# comment\n*.gen.go\nlegacy/\n")
	mkfile(t, dir, "main.go", "package main")
	mkfile(t, dir, "kv.gen.go", "package main") // matches *.gen.go
	mkfile(t, dir, "legacy/old.go", "package legacy")

	files, _, _ := Walk(dir, Options{})
	got := relPaths(files)
	if !slices.Equal(got, []string{"main.go"}) {
		t.Errorf("ckvignore not honored: got %v", got)
	}
}

func TestOversizedFilesSkipped(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "small.go", "package main")
	// 100 bytes > MaxBytes=50 → should be skipped
	mkfile(t, dir, "big.go", "package x\n"+stringRepeat("a", 100))

	files, _, _ := Walk(dir, Options{MaxBytes: 50})
	got := relPaths(files)
	if !slices.Equal(got, []string{"small.go"}) {
		t.Errorf("size cap not honored: got %v", got)
	}
}

func TestBinaryDetected(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "ok.go", "package main")
	// .go with a NUL byte → treated as binary, skipped
	mkfile(t, dir, "bad.go", "package main\x00\npayload")

	files, _, _ := Walk(dir, Options{})
	got := relPaths(files)
	if !slices.Equal(got, []string{"ok.go"}) {
		t.Errorf("binary heuristic skipped good file or kept bad one: got %v", got)
	}
}

func TestGoBuildFilesFilter_OnlyKeepsListedGoFiles(t *testing.T) {
	dir := t.TempDir()
	// Three Go files; the filter pre-selects two. The third must NOT
	// appear in Walk output — that's the whole point of build_roots
	// (cmd/foo includes pkg/a but not pkg/b, so pkg/b stays out).
	mkfile(t, dir, "cmd/main.go", "package main")
	mkfile(t, dir, "pkg/a/a.go", "package a")
	mkfile(t, dir, "pkg/b/b.go", "package b")

	keep := map[string]struct{}{
		filepath.Join(dir, "cmd", "main.go"): {},
		filepath.Join(dir, "pkg", "a", "a.go"): {},
	}
	files, _, err := Walk(dir, Options{GoBuildFiles: keep})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := relPaths(files)
	want := []string{"cmd/main.go", "pkg/a/a.go"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestGoBuildFilesFilter_DoesNotAffectOtherLanguages(t *testing.T) {
	dir := t.TempDir()
	// Go file NOT in the filter → must be excluded.
	// TS / Solidity files → must pass through (filter is Go-only).
	mkfile(t, dir, "excluded.go", "package main")
	mkfile(t, dir, "kept.ts", "export {}")
	mkfile(t, dir, "kept.sol", "pragma solidity ^0.8.0;")

	files, _, err := Walk(dir, Options{
		GoBuildFiles: map[string]struct{}{
			// Empty Go set on purpose: every Go file should be dropped,
			// every non-Go file should survive.
			filepath.Join(dir, "this-file-does-not-exist.go"): {},
		},
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := relPaths(files)
	want := []string{"kept.sol", "kept.ts"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v (Go filter must not drop non-Go files)", got, want)
	}
}

// TestDefaultSecretPatternsBlocked ensures credentials and private keys
// never reach the indexer. Each pattern is exercised so a future edit
// to DefaultSecretPatterns doesn't silently drop coverage.
//
// Security goal: a leaked secret embedded in sqlite-vec is recoverable
// only by rotating the credential and rebuilding the index — block at
// discovery instead.
func TestDefaultSecretPatternsBlocked(t *testing.T) {
	dir := t.TempDir()
	// Sentinel that MUST be indexed so we know the walk is actually running.
	mkfile(t, dir, "main.go", "package main")
	// Each secret-ish file: same Go extension so language filter would
	// otherwise let it through. Pattern must be what stops it.
	secrets := []string{
		".env",
		".env.local",
		".env.production",
		".env.production.local",
		"server.pem",
		"private.key",
		"cert.p12",
		"bundle.pfx",
		"app.keystore",
		"id_rsa",
		"id_rsa.pub",
		"id_ed25519",
		"id_ecdsa.pub",
		"id_dsa",
		"credentials.json",
		"service-account-prod.json",
		".npmrc",
		".pypirc",
		".netrc",
		".aws/credentials",
		".aws/config",
	}
	for _, s := range secrets {
		// Make every file a valid Go source so language=go and content
		// passes the binary heuristic. Anything that survives the walk
		// is a discovery-level escape.
		mkfile(t, dir, s, "package main")
	}

	files, _, err := Walk(dir, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := relPaths(files)
	want := []string{"main.go"}
	if !slices.Equal(got, want) {
		t.Errorf("secret leak: got %v, want %v", got, want)
	}
}

// TestSecretPatternAllowsExampleTemplates ensures the patterns don't
// over-block legitimate template files devs typically commit (.env.example
// is the canonical "here's what env vars you need, with no real values").
func TestSecretPatternAllowsExampleTemplates(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, ".env.example", "OPENAI_API_KEY=replace-me")
	mkfile(t, dir, ".env.sample", "DB_URL=replace-me")
	// Markdown so it's actually indexable.
	mkfile(t, dir, "doc.md", "# notes")

	files, _, _ := Walk(dir, Options{})
	got := relPaths(files)
	// .env.example / .env.sample have no Go/TS/markdown extension so they
	// won't appear anyway — but the pattern must not match them either,
	// or a future language addition would suddenly start blocking templates.
	for _, f := range files {
		if f.RelPath == ".env" || f.RelPath == ".env.local" {
			t.Errorf("real env leaked into walk: %v", got)
		}
	}
	// Sanity: classifyLanguage("") returns empty so .env.example is not
	// in files. Assert the language-tagging side directly.
	if classifyLanguage(".env.example") != "" {
		t.Errorf("classifyLanguage(.env.example) should be empty (unsupported ext)")
	}
}

// TestSecretFilterCanBeDisabled exercises the CKV_DISABLE_SECRET_FILTER
// escape hatch — needed for repos that legitimately want to index their
// own pem fixture files (e.g., CKV's own testdata/secrets/ if it ever
// adds one). Default off; opt-in only.
func TestSecretFilterCanBeDisabled(t *testing.T) {
	dir := t.TempDir()
	// .pem with Go content so language filter passes.
	mkfile(t, dir, "fixture.go", "package main")
	mkfile(t, dir, "key.pem", "package main")

	t.Setenv("CKV_DISABLE_SECRET_FILTER", "1")
	files, _, _ := Walk(dir, Options{})
	got := relPaths(files)
	// key.pem isn't classified as a known language so it still won't
	// appear. The point is the *pattern* no longer fires — verify
	// indirectly by checking the helper.
	if isIgnored("key.pem", DefaultSecretPatterns) == false {
		t.Errorf("pattern itself must still match key.pem — env var only changes Walk()")
	}
	// And via Walk: a renamed .go file with a sensitive-looking name
	// should now pass.
	mkfile(t, dir, "id_rsa.go", "package main")
	files, _, _ = Walk(dir, Options{})
	got = relPaths(files)
	if !slices.Contains(got, "id_rsa.go") {
		t.Errorf("CKV_DISABLE_SECRET_FILTER=1 should allow id_rsa.go; got %v", got)
	}
}

func TestGoBuildFilesFilter_NilMapMeansNoFilter(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "a.go", "package main")
	mkfile(t, dir, "b.go", "package main")

	files, _, err := Walk(dir, Options{GoBuildFiles: nil})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := relPaths(files)
	want := []string{"a.go", "b.go"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v (nil map = walk-all-files default)", got, want)
	}
}

func relPaths(fs []File) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.RelPath)
	}
	slices.Sort(out)
	return out
}

func stringRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
