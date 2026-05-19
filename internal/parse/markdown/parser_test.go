package markdown

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestParseSplitsAtHeadings(t *testing.T) {
	src := []byte(`# Title

Intro paragraph.

## Section A

A body line.
Another A line.

## Section B

B body.

### Subsection B1

Nested.
`)
	spans, err := New().Parse("docs/sample.md", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Expect 4 spans: Title, Section A, Section B, Subsection B1.
	if len(spans) != 4 {
		t.Fatalf("expected 4 spans, got %d: %+v", len(spans), spans)
	}
	wantNames := []string{"title", "section-a", "section-b", "subsection-b1"}
	for i, want := range wantNames {
		if spans[i].Name != want {
			t.Errorf("span[%d].Name: got %q, want %q", i, spans[i].Name, want)
		}
		if spans[i].Kind != types.KindDocSection {
			t.Errorf("span[%d].Kind: got %q, want DocSection", i, spans[i].Kind)
		}
	}
	// Section A text should include the heading and BOTH body lines.
	if !strings.Contains(spans[1].Text, "## Section A") {
		t.Errorf("Section A text missing heading: %q", spans[1].Text)
	}
	if !strings.Contains(spans[1].Text, "Another A line.") {
		t.Errorf("Section A text missing body: %q", spans[1].Text)
	}
	if strings.Contains(spans[1].Text, "## Section B") {
		t.Errorf("Section A text leaked into next section: %q", spans[1].Text)
	}
}

func TestParseIgnoresHeadingsInFencedCode(t *testing.T) {
	src := []byte("# Real\n\n" +
		"```\n" +
		"# not a heading\n" +
		"## also not\n" +
		"```\n\n" +
		"## Real Two\n\n" +
		"body\n",
	)
	spans, _ := New().Parse("x.md", src)
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans (heading inside fence ignored), got %d", len(spans))
	}
	if spans[0].Name != "real" || spans[1].Name != "real-two" {
		t.Errorf("names: got %q, %q; want real, real-two", spans[0].Name, spans[1].Name)
	}
	if !strings.Contains(spans[0].Text, "# not a heading") {
		t.Errorf("code-fence content should ride along with section 'real': %q", spans[0].Text)
	}
}

func TestParseHandlesPreamble(t *testing.T) {
	src := []byte("Intro before any heading.\n\nSecond intro line.\n\n# First Heading\n\nbody\n")
	spans, _ := New().Parse("x.md", src)
	if len(spans) != 2 {
		t.Fatalf("expected preamble + first heading = 2 spans, got %d", len(spans))
	}
	if spans[0].Name != "preamble" {
		t.Errorf("first span should be preamble, got %q", spans[0].Name)
	}
	if !strings.Contains(spans[0].Text, "Intro before any heading.") {
		t.Errorf("preamble missing content: %q", spans[0].Text)
	}
}

func TestParseNoHeadingsReturnsWholeFile(t *testing.T) {
	src := []byte("Just text.\nNo headings here.\n")
	spans, _ := New().Parse("x.md", src)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span for no-heading file, got %d", len(spans))
	}
	if spans[0].StartLine != 1 || spans[0].EndLine != 2 {
		t.Errorf("lines: got %d-%d, want 1-2", spans[0].StartLine, spans[0].EndLine)
	}
}

func TestParseEmptyReturnsNoSpans(t *testing.T) {
	spans, err := New().Parse("x.md", nil)
	if err != nil {
		t.Fatalf("Parse empty: %v", err)
	}
	if len(spans) != 0 {
		t.Errorf("empty file should produce 0 spans, got %d", len(spans))
	}
}

func TestParseADRPathKind(t *testing.T) {
	src := []byte("# Decision\n\nWe pick X.\n")
	cases := []struct {
		file string
		want types.SymbolKind
	}{
		{"ADR-001-vector-store.md", types.KindADRSection},
		{"docs/adr/002-embedder.md", types.KindADRSection},
		{"docs/ADR/003-rrf.md", types.KindADRSection},
		{"docs/plan.md", types.KindDocSection},
		{"README.md", types.KindDocSection},
	}
	for _, tc := range cases {
		spans, _ := New().Parse(tc.file, src)
		if len(spans) == 0 {
			t.Fatalf("%s: no spans", tc.file)
		}
		if spans[0].Kind != tc.want {
			t.Errorf("%s: kind = %q, want %q", tc.file, spans[0].Kind, tc.want)
		}
	}
}

func TestParseLineRanges(t *testing.T) {
	src := []byte(`# A
line2
line3

## B
line6

## C
line9
`)
	spans, _ := New().Parse("x.md", src)
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans, got %d", len(spans))
	}
	// A: heading at line 1, last line before B is line 4 (blank).
	if spans[0].StartLine != 1 || spans[0].EndLine != 4 {
		t.Errorf("A: got %d-%d, want 1-4", spans[0].StartLine, spans[0].EndLine)
	}
	// B: heading at line 5, last line before C is line 7 (blank).
	if spans[1].StartLine != 5 || spans[1].EndLine != 7 {
		t.Errorf("B: got %d-%d, want 5-7", spans[1].StartLine, spans[1].EndLine)
	}
	// C: heading at line 8, last line is line 9.
	if spans[2].StartLine != 8 || spans[2].EndLine != 9 {
		t.Errorf("C: got %d-%d, want 8-9", spans[2].StartLine, spans[2].EndLine)
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Vector Store", "vector-store"},
		{"Vector store — decision matrix", "vector-store-decision-matrix"},
		{"§4 Embedding", "4-embedding"},
		{"  spaces  ", "spaces"},
		{"!!!", ""},
		{"한글 헤딩", "한글-헤딩"}, // CJK is preserved (IsLetter)
	}
	for _, tc := range cases {
		if got := Slugify(tc.in); got != tc.want {
			t.Errorf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestATXHeadingRejection(t *testing.T) {
	rejected := []string{
		"",
		"###",          // no text
		"##notext",     // missing space
		"    # indent", // 4+ space indent → code block
		"plain text",
		"####### too many", // 7 hashes
	}
	for _, l := range rejected {
		if _, ok := atxHeading(l); ok {
			t.Errorf("atxHeading(%q) should return false", l)
		}
	}
	accepted := map[string]string{
		"# Title":     "Title",
		"## Sub":      "Sub",
		" # OneSpace": "OneSpace",
		"# Title #":   "Title", // trailing closing hashes trimmed
	}
	for l, want := range accepted {
		got, ok := atxHeading(l)
		if !ok {
			t.Errorf("atxHeading(%q) returned false; want %q", l, want)
			continue
		}
		if got != want {
			t.Errorf("atxHeading(%q) = %q, want %q", l, got, want)
		}
	}
}
