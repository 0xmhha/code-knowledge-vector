package query

import (
	"path/filepath"
	"strings"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// BoostOptions controls score boosting after vector + BM25 ranking.
// All multipliers are applied independently and combined multiplicatively.
type BoostOptions struct {
	// SignatureMatch boosts hits whose symbol name or signature contains
	// query keywords. Default multiplier: 1.5
	SignatureMatch      bool
	SignatureMultiplier float64

	// DocMatch boosts hits whose leading comment block contains query
	// keywords. Default multiplier: 1.3
	DocMatch      bool
	DocMultiplier float64

	// RecentModified boosts hits whose commit_hash equals the indexed
	// HEAD (treated as "recently modified"). Default multiplier: 1.1
	// For real git log-based recency, the caller can pre-compute and
	// pass via a custom Score override.
	RecentModified   bool
	RecentMultiplier float64

	// PackageProximity boosts hits whose file path contains the given
	// package keyword. Default multiplier: 1.2
	PackageProximity  bool
	PackageKeyword    string
	PackageMultiplier float64

	// IndexedHead is used by RecentModified to compare chunk.CommitHash.
	// Set by the engine before calling Run.
	IndexedHead string
}

// Defaults returns the recommended boost multipliers.
func DefaultBoostOptions() BoostOptions {
	return BoostOptions{
		SignatureMultiplier: 1.5,
		DocMultiplier:       1.3,
		RecentMultiplier:    1.1,
		PackageMultiplier:   1.2,
	}
}

// BoostService applies score boosting rules to candidate hits.
type BoostService struct{}

// Run reorders hits by boosted scores. The hit's Score.Normalized is
// updated with the boosted value; the original raw distance is preserved.
// Stable sort: hits with equal boosted scores keep their input order.
func (s *BoostService) Run(hits []types.Hit, intent string, opts BoostOptions) []types.Hit {
	if len(hits) == 0 {
		return hits
	}
	keywords := extractKeywords(intent)
	boosted := make([]types.Hit, len(hits))
	for i, h := range hits {
		mult := 1.0
		if opts.SignatureMatch && matchesSignature(h, keywords) {
			mult *= multiplierOr(opts.SignatureMultiplier, 1.5)
		}
		if opts.DocMatch && matchesDoc(h, keywords) {
			mult *= multiplierOr(opts.DocMultiplier, 1.3)
		}
		if opts.RecentModified && isRecent(h, opts.IndexedHead) {
			mult *= multiplierOr(opts.RecentMultiplier, 1.1)
		}
		if opts.PackageProximity && matchesPackage(h, opts.PackageKeyword) {
			mult *= multiplierOr(opts.PackageMultiplier, 1.2)
		}
		h.Score.Normalized = h.Score.Normalized * mult
		boosted[i] = h
	}
	// Stable sort by Score.Normalized descending
	for i := 1; i < len(boosted); i++ {
		for j := i; j > 0 && boosted[j].Score.Normalized > boosted[j-1].Score.Normalized; j-- {
			boosted[j], boosted[j-1] = boosted[j-1], boosted[j]
		}
	}
	return boosted
}

// RunContext applies boost to sc.RawHits when Options.EnableScoreBoost.
func (s *BoostService) RunContext(sc *SearchContext, indexedHead string) {
	if !sc.Options.EnableScoreBoost || len(sc.RawHits) == 0 {
		return
	}
	bopts := sc.Options.Boost
	if bopts.SignatureMultiplier == 0 {
		bopts = mergeWithDefaults(bopts)
	}
	bopts.IndexedHead = indexedHead
	sc.RawHits = s.Run(sc.RawHits, sc.Intent, bopts)
}

func mergeWithDefaults(opts BoostOptions) BoostOptions {
	d := DefaultBoostOptions()
	if opts.SignatureMultiplier == 0 {
		opts.SignatureMultiplier = d.SignatureMultiplier
	}
	if opts.DocMultiplier == 0 {
		opts.DocMultiplier = d.DocMultiplier
	}
	if opts.RecentMultiplier == 0 {
		opts.RecentMultiplier = d.RecentMultiplier
	}
	if opts.PackageMultiplier == 0 {
		opts.PackageMultiplier = d.PackageMultiplier
	}
	return opts
}

func multiplierOr(v, fallback float64) float64 {
	if v <= 0 {
		return fallback
	}
	return v
}

// extractKeywords tokenizes the intent into lowercase alphanumeric words.
// Filters out very short tokens (<3 chars) and common stopwords.
func extractKeywords(intent string) []string {
	if intent == "" {
		return nil
	}
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		w := strings.ToLower(cur.String())
		cur.Reset()
		if len(w) < 3 {
			return
		}
		if isStopword(w) {
			return
		}
		tokens = append(tokens, w)
	}
	for _, r := range intent {
		if isAlphaNum(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

func isAlphaNum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

var stopwords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "this": {}, "that": {},
	"how": {}, "what": {}, "why": {}, "when": {}, "where": {}, "which": {},
	"from": {}, "into": {}, "onto": {}, "out": {}, "off": {}, "all": {},
}

func isStopword(w string) bool {
	_, ok := stopwords[w]
	return ok
}

func matchesSignature(h types.Hit, keywords []string) bool {
	if len(keywords) == 0 {
		return false
	}
	target := strings.ToLower(h.Chunk.SymbolName)
	// Signature is the first non-empty line of the chunk text
	for _, line := range strings.SplitN(h.Chunk.Text, "\n", 5) {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		target = target + " " + strings.ToLower(s)
		break
	}
	return containsAny(target, keywords)
}

func matchesDoc(h types.Hit, keywords []string) bool {
	if len(keywords) == 0 {
		return false
	}
	// Extract leading comment block (Go //, JS /** */, Solidity ///).
	var doc strings.Builder
	for _, line := range strings.Split(h.Chunk.Text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") ||
			strings.HasPrefix(t, "*") || strings.HasPrefix(t, "*/") ||
			strings.HasPrefix(t, "#") {
			doc.WriteString(strings.ToLower(t))
			doc.WriteByte(' ')
			continue
		}
		break // first non-comment line ends the doc block
	}
	if doc.Len() == 0 {
		return false
	}
	return containsAny(doc.String(), keywords)
}

func isRecent(h types.Hit, indexedHead string) bool {
	if indexedHead == "" || h.Chunk.CommitHash == "" {
		return false
	}
	return h.Chunk.CommitHash == indexedHead
}

func matchesPackage(h types.Hit, pkg string) bool {
	if pkg == "" {
		return false
	}
	dir := strings.ToLower(filepath.ToSlash(filepath.Dir(h.Chunk.File)))
	return strings.Contains(dir, strings.ToLower(pkg))
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
