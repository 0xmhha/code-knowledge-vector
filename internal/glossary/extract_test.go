package glossary

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// extractAll is a test helper that runs ExtractLine through a multi-
// line string and returns the flat AliasMap (sorted slices).
func extractAll(t *testing.T, text string) map[string][]string {
	t.Helper()
	accum := map[string]map[string]struct{}{}
	for _, line := range strings.Split(text, "\n") {
		ExtractLine(line, accum)
	}
	flat := map[string][]string{}
	for k, set := range accum {
		var vals []string
		for v := range set {
			vals = append(vals, v)
		}
		// Sort for deterministic test assertion.
		flat[k] = sortedCopy(vals)
	}
	return flat
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestExtract_TableRow_KoreanKeyEnglishValue(t *testing.T) {
	text := `
| 항목 | 값 |
|------|-----|
| 합의 알고리즘 | WBFT (Weemix Byzantine Fault Tolerance) |
| 바이너리 이름 | ` + "`gstable`" + ` |
| 네이티브 코인 | WKRC |
`
	got := extractAll(t, text)

	mustContain(t, got, "합의 알고리즘", "WBFT")
	mustContain(t, got, "합의 알고리즘", "Weemix Byzantine Fault Tolerance")
	mustContain(t, got, "바이너리 이름", "gstable")
	mustContain(t, got, "네이티브 코인", "WKRC")
}

func TestExtract_TableHeader_IsSkipped(t *testing.T) {
	// Pure header rows (`| 항목 | 값 |`) without other context — the
	// "항목" key has hangul but its value cell is also hangul ("값"),
	// and our english-token extractor returns nothing for it, so the
	// entry should not appear.
	text := `
| 항목 | 값 |
|------|-----|
`
	got := extractAll(t, text)
	if vals, ok := got["항목"]; ok && len(vals) > 0 {
		t.Errorf("header row leaked: '항목' → %v", vals)
	}
}

func TestExtract_Separator_NeverProduces(t *testing.T) {
	got := extractAll(t, "|------|-----|")
	if len(got) != 0 {
		t.Errorf("separator row produced entries: %+v", got)
	}
}

func TestExtract_InlineParenthetical(t *testing.T) {
	text := `합의 알고리즘 (consensus engine) 은 WBFT 다. 검증인 (validator) 명단은 ...`
	got := extractAll(t, text)
	mustContain(t, got, "합의 알고리즘", "consensus engine")
	mustContain(t, got, "검증인", "validator")
}

func TestExtract_EnglishOnlyParens_NotAliased(t *testing.T) {
	// "ABC (Alpha Beta Charlie)" — both sides english, must NOT
	// create an alias (no hangul key).
	text := `WBFT (Weemix Byzantine Fault Tolerance) is the engine.`
	got := extractAll(t, text)
	if _, ok := got["WBFT"]; ok {
		t.Errorf("english key WBFT should not become an alias: %+v", got)
	}
}

func TestExtract_AttributionParens_Skipped(t *testing.T) {
	// 한국어 단어 뒤에 "(see #43)" 같은 첨자: number+symbol 만 있는
	// 괄호는 alias 가 아니어야 함.
	text := `합의 알고리즘 (see #43) 을 참조.`
	got := extractAll(t, text)
	if vals, ok := got["합의 알고리즘"]; ok {
		for _, v := range vals {
			if strings.Contains(v, "#") {
				t.Errorf("attribution-style parens leaked: %q", v)
			}
		}
	}
}

func TestExtract_PureDigitsDropped(t *testing.T) {
	text := `| Chain ID | 8282 |
| 검증인 수 | 13 |`
	got := extractAll(t, text)
	if vals, ok := got["검증인 수"]; ok {
		for _, v := range vals {
			if v == "13" {
				t.Errorf("pure-digit value should not be aliased: %v", vals)
			}
		}
	}
}

func TestExtract_DedupAcrossSources(t *testing.T) {
	// Same alias from table + inline gloss should appear once.
	text := `
| 합의 알고리즘 | WBFT |
본 시스템의 합의 알고리즘 (WBFT) 은 BFT 계열이다.
`
	got := extractAll(t, text)
	count := 0
	for _, v := range got["합의 알고리즘"] {
		if v == "WBFT" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("WBFT should appear once, got %d", count)
	}
}

func TestExtract_FullFile(t *testing.T) {
	dir := t.TempDir()
	md := `# StableNet 가이드

| 항목 | 값 |
|------|-----|
| 합의 알고리즘 | WBFT |
| 네이티브 코인 | WKRC |

본 시스템에서 검증인 (validator) 는 거버넌스 (governance) 로 관리된다.
`
	path := filepath.Join(dir, "guide.md")
	if err := os.WriteFile(path, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	// Add a non-markdown file that must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}

	aliases, err := Extract(dir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Exact-key assertions for table rows (deterministic phrasing).
	mustContain(t, aliases, "합의 알고리즘", "WBFT")
	mustContain(t, aliases, "네이티브 코인", "WKRC")
	// Inline glosses: the extractor is intentionally permissive about
	// the key boundary (v1 known limitation — user-curated glossary is
	// expected to trim multi-word captures). Verify the value lands
	// against *some* key ending in the noun.
	mustHaveSuffixKey(t, aliases, "검증인", "validator")
	mustHaveSuffixKey(t, aliases, "거버넌스", "governance")
}

// mustHaveSuffixKey asserts some key ending in noun maps to value.
// Relaxed v1 expectation — see TestExtract_FullFile for the rationale.
func mustHaveSuffixKey(t *testing.T, aliases map[string][]string, noun, value string) {
	t.Helper()
	for k, vals := range aliases {
		if !strings.HasSuffix(k, noun) {
			continue
		}
		for _, v := range vals {
			if v == value {
				return
			}
		}
	}
	t.Errorf("no key ending in %q maps to %q; got %+v", noun, value, aliases)
}

func TestWriteYAML_RoundtripAndDeterministic(t *testing.T) {
	aliases := map[string][]string{
		"합의 알고리즘": {"Weemix Byzantine Fault Tolerance", "WBFT"},
		"검증인":     {"validator"},
	}
	var first, second bytes.Buffer
	if err := WriteYAML(&first, aliases); err != nil {
		t.Fatalf("WriteYAML: %v", err)
	}
	if err := WriteYAML(&second, aliases); err != nil {
		t.Fatalf("WriteYAML rerun: %v", err)
	}
	if first.String() != second.String() {
		t.Errorf("WriteYAML non-deterministic: %q vs %q", first.String(), second.String())
	}
	out := first.String()
	if !strings.HasPrefix(out, "aliases:\n") {
		t.Errorf("missing top-level key: %q", out)
	}
	// The hangul key must show up as a YAML-quoted string.
	if !strings.Contains(out, `"합의 알고리즘"`) {
		t.Errorf("hangul key not quoted properly: %q", out)
	}
}

// mustContain asserts that aliases[key] contains value (after the
// per-test sort). Helper to keep individual cases tight.
func mustContain(t *testing.T, aliases map[string][]string, key, value string) {
	t.Helper()
	vals, ok := aliases[key]
	if !ok {
		t.Errorf("missing key %q in %+v", key, aliases)
		return
	}
	for _, v := range vals {
		if v == value {
			return
		}
	}
	t.Errorf("aliases[%q] = %v, want to contain %q", key, vals, value)
}
