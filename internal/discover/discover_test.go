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
	mkfile(t, dir, "README.md", "# readme") // unknown lang → skipped
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
	want := []string{"Token.sol", "internal/x.go", "main.go", "ui.tsx"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
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
		t.Errorf("got %v, want %v (nil map = pre-FU-9 behavior)", got, want)
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
