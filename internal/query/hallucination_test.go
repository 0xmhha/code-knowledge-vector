package query

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// fixtureSrcRoot writes a tiny "real source tree" into t.TempDir() so
// VerifyHit has an actual file to compare against. Returns the dir.
func fixtureSrcRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	content := "package x\n" +
		"\n" +
		"func ProcessOrder(o Order) error {\n" +
		"\tif o.Total <= 0 {\n" +
		"\t\treturn errors.New(\"invalid amount\")\n" +
		"\t}\n" +
		"\treturn nil\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(dir, "order.go"), []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return dir
}

func mkVerifyHit(file string, start, end int, snippet string) Hit {
	return Hit{
		Citation: types.Citation{File: file, StartLine: start, EndLine: end},
		Snippet:  snippet,
	}
}

// TestVerifyHit_ExactSnippetMatchesFile is the happy path: snippet
// equals the file's content at the cited line range.
func TestVerifyHit_ExactSnippetMatchesFile(t *testing.T) {
	dir := fixtureSrcRoot(t)
	h := mkVerifyHit("order.go", 3, 8,
		"func ProcessOrder(o Order) error {\n\tif o.Total <= 0 {\n\t\treturn errors.New(\"invalid amount\")\n\t}\n\treturn nil\n}")
	got := VerifyHit(h, dir)
	if !got.Verified {
		t.Errorf("expected Verified=true; got Reason=%q", got.Reason)
	}
}

// TestVerifyHit_SignatureOnlyIsSubstringMatch: snippet is just the
// signature line — must still verify (substring of the body slice).
// This covers the DensitySignatureOnly tier from B3.
func TestVerifyHit_SignatureOnlyIsSubstringMatch(t *testing.T) {
	dir := fixtureSrcRoot(t)
	h := mkVerifyHit("order.go", 3, 8, "func ProcessOrder(o Order) error {")
	got := VerifyHit(h, dir)
	if !got.Verified {
		t.Errorf("signature-only snippet should verify; got Reason=%q", got.Reason)
	}
}

// TestVerifyHit_WhitespaceNormalization: tab vs space and trailing
// whitespace differences don't count as hallucinations. The user reads
// the snippet vs file by content, not by formatting bytes.
func TestVerifyHit_WhitespaceNormalization(t *testing.T) {
	dir := fixtureSrcRoot(t)
	// File uses tabs; snippet uses spaces.
	h := mkVerifyHit("order.go", 4, 6, "if o.Total <= 0 {\n    return errors.New(\"invalid amount\")\n}")
	got := VerifyHit(h, dir)
	if !got.Verified {
		t.Errorf("whitespace-normalized snippet should verify; got Reason=%q", got.Reason)
	}
}

// TestVerifyHit_FileMissing flags the citation-points-nowhere case.
func TestVerifyHit_FileMissing(t *testing.T) {
	dir := fixtureSrcRoot(t)
	h := mkVerifyHit("nonexistent.go", 1, 5, "anything")
	got := VerifyHit(h, dir)
	if got.Verified {
		t.Error("expected Verified=false for missing file")
	}
	if got.Reason != "file_missing" {
		t.Errorf("Reason = %q, want file_missing", got.Reason)
	}
}

// TestVerifyHit_OutOfRange flags citation line range exceeding file.
func TestVerifyHit_OutOfRange(t *testing.T) {
	dir := fixtureSrcRoot(t)
	h := mkVerifyHit("order.go", 1, 500, "func ProcessOrder")
	got := VerifyHit(h, dir)
	if got.Verified {
		t.Error("expected Verified=false for out-of-range")
	}
	if got.Reason != "out_of_range" {
		t.Errorf("Reason = %q, want out_of_range", got.Reason)
	}
}

// TestVerifyHit_SnippetNotFound is the actual hallucination case —
// snippet text isn't anywhere in the cited file range.
func TestVerifyHit_SnippetNotFound(t *testing.T) {
	dir := fixtureSrcRoot(t)
	h := mkVerifyHit("order.go", 3, 8,
		"func DeleteUser(u User) error { return nil }") // not in the file
	got := VerifyHit(h, dir)
	if got.Verified {
		t.Error("expected Verified=false for fabricated snippet")
	}
	if got.Reason != "snippet_not_found" {
		t.Errorf("Reason = %q, want snippet_not_found", got.Reason)
	}
}

// TestVerifyHit_EmptySrcRoot returns Verified=false with the
// distinguishing no_src_root reason — caller asked for verification
// but didn't provide a tree.
func TestVerifyHit_EmptySrcRoot(t *testing.T) {
	got := VerifyHit(mkVerifyHit("order.go", 1, 1, "x"), "")
	if got.Verified {
		t.Error("empty srcRoot must not verify")
	}
	if got.Reason != "no_src_root" {
		t.Errorf("Reason = %q, want no_src_root", got.Reason)
	}
}

// TestVerifyResponse_AggregatesHitsAndExamples checks the helper that
// fans VerifyHit across both result slices.
func TestVerifyResponse_AggregatesHitsAndExamples(t *testing.T) {
	dir := fixtureSrcRoot(t)
	resp := &Response{
		Hits: []Hit{
			mkVerifyHit("order.go", 3, 8, "func ProcessOrder"), // verified
			mkVerifyHit("fake.go", 1, 1, "nothing"),            // file_missing
		},
		Examples: []Hit{
			mkVerifyHit("order.go", 3, 8, "func DeleteUser"), // snippet_not_found
		},
	}
	verdicts, halluc := VerifyResponse(resp, dir)
	if len(verdicts) != 3 {
		t.Fatalf("expected 3 verdicts (2 hits + 1 example), got %d", len(verdicts))
	}
	if halluc != 2 {
		t.Errorf("hallucinated = %d, want 2", halluc)
	}
	if !verdicts[0].Verified {
		t.Error("verdicts[0] should be verified")
	}
	if verdicts[1].Reason != "file_missing" {
		t.Errorf("verdicts[1].Reason = %q, want file_missing", verdicts[1].Reason)
	}
	if verdicts[2].Reason != "snippet_not_found" {
		t.Errorf("verdicts[2].Reason = %q, want snippet_not_found", verdicts[2].Reason)
	}
}
