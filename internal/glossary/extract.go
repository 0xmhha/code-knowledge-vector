// Package glossary auto-extracts korean → english keyword mappings
// from markdown documents (typically a project's .claude/docs/ tree)
// and emits the AliasMap YAML that `ckv query --alias` consumes.
//
// Pattern coverage (v1):
//
//  1. Markdown table rows with a korean key cell and an english value
//     cell: `| 합의 알고리즘 | WBFT (Weemix Byzantine Fault Tolerance) |`
//     extracts `합의 알고리즘 → [WBFT, Weemix Byzantine Fault Tolerance]`.
//
//  2. Inline parenthetical glosses: `합의 알고리즘 (consensus engine)`
//     or `검증인 (validator)`. Body-text pattern, picks up
//     terminology that never makes it into a table.
//
// Both patterns require the *key* to contain at least one Hangul
// syllable so we never alias an english phrase to itself. Values are
// deduplicated and sorted before being written to YAML.
//
// Non-goals for v1: section headings, bold-bold rewriting, semantic
// translation. Add only when a measurement shows the existing
// extractor misses a class of useful entries.
//
// Output format matches internal/query.AliasFile so the produced YAML
// is directly consumable by `ckv query --alias`.
package glossary

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// AliasFile mirrors internal/query.AliasFile so we can emit YAML
// without depending on the query package (avoiding an import cycle if
// glossary ever moves under internal/query).
type AliasFile struct {
	Aliases map[string][]string `yaml:"aliases"`
}

// Extract walks every *.md / *.markdown file under root and returns
// the union AliasMap of all extracted patterns. Files are read with
// streaming line scanning so the function handles repo-sized doc
// trees without loading everything into memory.
//
// Returns an empty (non-nil) map when no aliases are found — callers
// can write it out unconditionally.
func Extract(root string) (map[string][]string, error) {
	out := map[string]map[string]struct{}{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".markdown" {
			return nil
		}
		return extractFile(path, out)
	})
	if err != nil {
		return nil, err
	}

	// Flatten the set-of-set into the public shape: sorted, deduped.
	flat := make(map[string][]string, len(out))
	for k, set := range out {
		vals := make([]string, 0, len(set))
		for v := range set {
			vals = append(vals, v)
		}
		sort.Strings(vals)
		flat[k] = vals
	}
	return flat, nil
}

// extractFile reads one markdown file line-by-line and merges every
// extracted alias into accum.
func extractFile(path string, accum map[string]map[string]struct{}) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scan.Scan() {
		line := scan.Text()
		ExtractLine(line, accum)
	}
	if err := scan.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

// ExtractLine applies the v1 patterns to one line and merges into
// accum. Exposed so callers (tests, future stream sources) can drive
// extraction without a file system.
func ExtractLine(line string, accum map[string]map[string]struct{}) {
	if k, vals := extractTableRow(line); k != "" && len(vals) > 0 {
		mergeAliases(accum, k, vals)
	}
	for _, m := range extractInlineGlosses(line) {
		mergeAliases(accum, m.key, []string{m.value})
	}
}

// extractTableRow parses one markdown table row. Returns the trimmed
// korean key and the list of english tokens from the value cell, or
// empty when the row isn't a valid alias source.
//
// Filter: skips header rows (`| --- | --- |`), rows where the first
// cell has no hangul, and the *first* data row when the previous line
// looks like a header separator (defensive — the bufio.Scanner can't
// know what came before). Header-row rejection is handled by
// requiring hangul, which a separator row never has.
func extractTableRow(line string) (string, []string) {
	if !strings.HasPrefix(strings.TrimSpace(line), "|") {
		return "", nil
	}
	// Split on `|` and trim. A valid data row has at least two cells
	// after the leading empty string.
	cells := strings.Split(line, "|")
	if len(cells) < 3 {
		return "", nil
	}
	// Strip leading + trailing empty cells (markdown table convention).
	if strings.TrimSpace(cells[0]) == "" {
		cells = cells[1:]
	}
	if len(cells) > 0 && strings.TrimSpace(cells[len(cells)-1]) == "" {
		cells = cells[:len(cells)-1]
	}
	if len(cells) < 2 {
		return "", nil
	}

	key := strings.TrimSpace(cells[0])
	value := strings.TrimSpace(cells[1])
	if !hasHangul(key) {
		return "", nil // header row or english-key row — both skipped
	}
	if value == "" || strings.HasPrefix(value, "---") {
		return "", nil // separator row that somehow had hangul on the left
	}
	return key, extractEnglishTokens(value)
}

// inlineGloss is one (key, value) pair extracted from a parenthetical.
type inlineGloss struct{ key, value string }

// extractInlineGlosses scans the line for `<korean> (<english>)`
// patterns. Multiple per line are supported. The english side is
// constrained to "looks like a phrase or identifier" — alphanumeric +
// space + hyphen + dot/underscore, no nested parens — so we don't
// match attribution like "(see #43)".
func extractInlineGlosses(line string) []inlineGloss {
	var out []inlineGloss
	for i := 0; i < len(line); {
		open := strings.Index(line[i:], "(")
		if open < 0 {
			break
		}
		open += i
		close := strings.Index(line[open+1:], ")")
		if close < 0 {
			break
		}
		close += open + 1
		inner := strings.TrimSpace(line[open+1 : close])
		// Pre-paren text — find the trailing token that the parens annotate.
		pre := strings.TrimRightFunc(line[:open], unicode.IsSpace)
		key := lastKoreanPhrase(pre)
		if key != "" && isEnglishGloss(inner) {
			out = append(out, inlineGloss{key: key, value: inner})
		}
		i = close + 1
	}
	return out
}

// lastKoreanPhrase returns the trailing hangul phrase preceding a
// parenthetical gloss, with three normalizations:
//
//  1. Backwards scan stops at the closest sentence boundary
//     (`)` / `.` / `,` / `;` / `:` / `?` / `!` / newline). Everything
//     after that boundary is the candidate phrase.
//  2. Leading single-syllable particles (은/는/이/가/을/를/의/와/과/도)
//     are dropped — they're attached to the previous noun by Korean
//     grammar, not to the alias key.
//  3. The phrase is capped at its last 3 whitespace-delimited tokens
//     to keep multi-word compounds (e.g., "합의 알고리즘") while
//     preventing runaway capture across "본 시스템에서" etc.
//
// Returns empty when the result has no hangul.
func lastKoreanPhrase(s string) string {
	// 1. Trim back to the nearest sentence boundary.
	start := 0
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c == ')' || c == '.' || c == ',' || c == ';' || c == ':' || c == '?' || c == '!' || c == '\n' {
			start = i + 1
			break
		}
	}
	phrase := strings.TrimSpace(s[start:])
	if phrase == "" || !hasHangul(phrase) {
		return ""
	}
	// 2/3. Tokenize, drop leading particles, cap at 3 tokens.
	tokens := strings.Fields(phrase)
	for len(tokens) > 0 && isKoreanParticle(tokens[0]) {
		tokens = tokens[1:]
	}
	if len(tokens) > 3 {
		tokens = tokens[len(tokens)-3:]
	}
	out := strings.Join(tokens, " ")
	if !hasHangul(out) {
		return ""
	}
	return out
}

// isKoreanParticle reports whether tok is a stand-alone single-syllable
// Korean particle. Particles attached *inside* a noun (e.g., 거버넌스는)
// don't reach this function — they're part of one whitespace-delimited
// token already and we accept that approximation for v1.
func isKoreanParticle(tok string) bool {
	switch tok {
	case "은", "는", "이", "가", "을", "를", "의", "와", "과", "도":
		return true
	}
	return false
}

// isEnglishGloss reports whether s looks like an english phrase /
// identifier suitable as an alias value: at least one latin letter,
// no nested punctuation that would suggest it's not a gloss.
func isEnglishGloss(s string) bool {
	if s == "" || len(s) > 80 {
		return false
	}
	hasLatin := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
			if r < 128 {
				hasLatin = true
			}
		case unicode.IsDigit(r), r == ' ', r == '-', r == '_', r == '.', r == '/':
			// allowed
		default:
			return false
		}
	}
	return hasLatin
}

// extractEnglishTokens pulls every plausible english identifier /
// phrase fragment from a markdown table value cell. Three sources:
//
//   - backtick-quoted text   `gstable`           → "gstable"
//   - parenthetical phrase   (Weemix Byzantine)  → "Weemix Byzantine"
//   - standalone latin words capitalized or all-caps ≥3 chars
//
// Sources are merged + deduped at the caller. Numbers alone (8282)
// are skipped — they aren't useful as semantic keywords.
func extractEnglishTokens(cell string) []string {
	seen := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		// Drop pure-digit tokens; not useful as glossary entries.
		if isAllDigits(s) {
			return
		}
		seen[s] = struct{}{}
	}

	// Pass 1: backtick-quoted tokens.
	for {
		open := strings.Index(cell, "`")
		if open < 0 {
			break
		}
		close := strings.Index(cell[open+1:], "`")
		if close < 0 {
			break
		}
		add(cell[open+1 : open+1+close])
		cell = cell[open+1+close+1:]
	}

	// Pass 2: parenthetical phrases on the original line. The earlier
	// pass mutated cell, so we work on a copy. Inline-gloss extractor
	// upstream already handles `key (english)` pairs — here we
	// specifically harvest the inner phrase even when no korean key
	// precedes (e.g., table cells of the form
	// `WBFT (Weemix Byzantine Fault Tolerance)`).
	scan := cell
	for {
		open := strings.Index(scan, "(")
		if open < 0 {
			break
		}
		close := strings.Index(scan[open+1:], ")")
		if close < 0 {
			break
		}
		inner := strings.TrimSpace(scan[open+1 : open+1+close])
		if isEnglishGloss(inner) {
			add(inner)
		}
		scan = scan[open+1+close+1:]
	}

	// Pass 3: bare latin tokens that look like identifiers.
	for _, tok := range fieldsForGlossary(cell) {
		if isIdentifierLike(tok) {
			add(tok)
		}
	}

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// fieldsForGlossary splits on whitespace plus markdown decorations
// (asterisks for bold, commas, parens).
func fieldsForGlossary(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ' ', '\t', ',', '*', '(', ')', '`':
			return true
		}
		return false
	})
}

// isIdentifierLike reports whether t looks like an english identifier
// worth keeping as an alias keyword. Heuristic: at least 3 chars, at
// least one latin letter, no embedded hangul.
func isIdentifierLike(t string) bool {
	t = strings.TrimSpace(t)
	if len(t) < 3 {
		return false
	}
	if hasHangul(t) {
		return false
	}
	hasLetter := false
	for _, r := range t {
		if unicode.IsLetter(r) && r < 128 {
			hasLetter = true
		}
	}
	return hasLetter
}

// isAllDigits reports whether s consists entirely of decimal digits
// (after trimming).
func isAllDigits(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// hasHangul reports whether s contains any hangul syllable / jamo.
func hasHangul(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0xAC00 && r <= 0xD7A3: // syllables
			return true
		case r >= 0x1100 && r <= 0x11FF: // jamo
			return true
		case r >= 0x3130 && r <= 0x318F: // compatibility jamo
			return true
		}
	}
	return false
}

// mergeAliases unions one (key, []value) into the accumulator after
// key/value sanity filters. Filters dropped here keep the v1 output
// small enough to hand-review:
//
//   - Key starting with a markdown decoration (`#`, `>`, `**`, `//`,
//     `<`, `[`, `(`, `-` followed by space, `=`) — these are heading,
//     code-comment, or quote artifacts, not noun aliases.
//   - Value > 60 chars — long values are usually full sentences that
//     leaked through the inline-gloss extractor; an alias should be
//     a phrase, not prose.
func mergeAliases(accum map[string]map[string]struct{}, key string, vals []string) {
	key = strings.TrimSpace(key)
	if key == "" || isMarkdownDecorationKey(key) {
		return
	}
	set, ok := accum[key]
	if !ok {
		set = map[string]struct{}{}
		accum[key] = set
	}
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" || len(v) > 60 {
			continue
		}
		set[v] = struct{}{}
	}
}

// isMarkdownDecorationKey reports whether key starts with characters
// that indicate it was scraped from a heading / code comment / list
// marker rather than from prose.
func isMarkdownDecorationKey(key string) bool {
	if key == "" {
		return true
	}
	// Single-char first-rune checks.
	first := rune(key[0])
	switch first {
	case '#', '>', '<', '[', '(', '=':
		return true
	}
	// 2-char prefixes for common decorations.
	prefixes := []string{"**", "//", "- ", "* ", "+ ", "```"}
	for _, p := range prefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// WriteYAML serializes the extracted AliasMap to w. Keys + values are
// sorted so reruns produce byte-identical output (good for diffs and
// for "ckv query --alias" fingerprint stability).
func WriteYAML(w io.Writer, aliases map[string][]string) error {
	keys := make([]string, 0, len(aliases))
	for k := range aliases {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if _, err := fmt.Fprintln(w, "aliases:"); err != nil {
		return err
	}
	for _, k := range keys {
		vals := aliases[k]
		if len(vals) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(w, "  %q:\n", k); err != nil {
			return err
		}
		for _, v := range vals {
			if _, err := fmt.Fprintf(w, "    - %q\n", v); err != nil {
				return err
			}
		}
	}
	return nil
}
