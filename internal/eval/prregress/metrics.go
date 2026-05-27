package prregress

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// Multi-stage evaluation metrics.
//
// The legacy JudgeScore combines plan-vs-diff approach and code
// generation into one LLM-graded number. These metrics decompose the
// signal so a regression report can answer "which stage did the agent
// stumble on?" rather than "the score dropped, somewhere."
//
//	E1 — IntentScore:     did the plan capture WHAT to do?
//	E2 — SymbolF1:        did it identify WHERE to look (at symbol granularity)?
//	E3 — PlanStepsScore:  did it decompose HOW to do it into commit-shaped steps?
//
// All three are pure-Go, deterministic across reruns. An optional cosine
// variant of E1 (IntentCosine) lives in this file and runs only when a
// real embedder is wired in via RunOptions.Embedder.

// IntentScore measures how well a plan's intent narrative overlaps with a
// reference intent statement (Entry.IntentGroundTruth when present, else
// Meta.Title). Unigram token-F1 after lowercase + alphanum/Hangul split
// + minimal stopword strip. Returns 0..1.
//
// Empty plan or empty reference → 0 (the metric requires both sides).
// Identical token sets → 1. Disjoint → 0.
//
// Paraphrase invariance is intentionally NOT a goal here: the determinism
// of token-F1 is the point — reruns produce identical numbers, which is
// what regression detection needs. The cosine variant (IntentCosine)
// handles paraphrase when an embedder is available.
func IntentScore(plan, reference string) float64 {
	return tokenF1(plan, reference)
}

// IntentCosine returns the embedding cosine similarity between two
// strings. Optional companion to IntentScore — runs only when the caller
// supplies an embedder (typically the same instance the runner uses for
// index build, reused for free).
//
// Mock embedders satisfy the interface but their vectors are random
// hashes of the input; cosine over those is essentially noise. Callers
// who want a meaningful number must pass a real embedder (e.g. bgeonnx).
//
// Returns NaN-safe value clamped to [-1, 1]. Empty inputs → 0.
func IntentCosine(ctx context.Context, plan, reference string, e types.Embedder) (float64, error) {
	if e == nil {
		return 0, fmt.Errorf("IntentCosine: nil embedder")
	}
	if strings.TrimSpace(plan) == "" || strings.TrimSpace(reference) == "" {
		return 0, nil
	}
	vecs, err := e.Embed(ctx, []string{plan, reference})
	if err != nil {
		return 0, fmt.Errorf("IntentCosine: embed: %w", err)
	}
	if len(vecs) != 2 {
		return 0, fmt.Errorf("IntentCosine: embedder returned %d vectors, want 2", len(vecs))
	}
	return cosine(vecs[0], vecs[1]), nil
}

// SymbolF1 mirrors FileSetF1 for symbol identifiers extracted from the
// plan vs Entry.ChangedSymbols. Normalization: lowercase, runs of
// non-alphanum collapse to a single `_`, leading/trailing `_` trimmed.
//
// Empty truthSymbols → 0/0/0 (matches FileSetF1's contract). The
// descriptive truth entries in the fixture ("secp256r1 precompile
// registration", "systemcontracts init parsers — members") will not match
// PascalCase plan symbols — that low recall is the intended signal until
// AST-diff symbol extraction lands.
func SymbolF1(planSymbols, truthSymbols []string) (precision, recall, f1 float64) {
	if len(truthSymbols) == 0 {
		return 0, 0, 0
	}
	truthSet := normalizeSymbolSet(truthSymbols)
	planSet := normalizeSymbolSet(planSymbols)
	if len(truthSet) == 0 {
		return 0, 0, 0
	}
	var tp int
	for s := range planSet {
		if _, ok := truthSet[s]; ok {
			tp++
		}
	}
	if len(planSet) > 0 {
		precision = float64(tp) / float64(len(planSet))
	}
	recall = float64(tp) / float64(len(truthSet))
	if precision+recall > 0 {
		f1 = 2 * precision * recall / (precision + recall)
	}
	return precision, recall, f1
}

// PlanStepsScore compares the plan's step narrative (markdown body sans
// the "Expected Changes" file list) against the PR's commit-message log.
// Same token-F1 algorithm as IntentScore but applied to longer strings.
//
// Empty commitMessages → 0; the metric requires both sides. The caller
// is responsible for joining the multi-commit messages into one string
// before passing it in (the runner does this once).
func PlanStepsScore(planSteps, commitMessages string) float64 {
	return tokenF1(planSteps, commitMessages)
}

// ExtractPlanSteps returns the plan narrative with the "Expected Changes"
// tail removed so the file list does not dilute step-level token overlap
// (FileSetF1 already covers that axis). When no "Expected Changes"
// header is present, the whole markdown is returned unchanged.
func ExtractPlanSteps(markdown string) string {
	if markdown == "" {
		return ""
	}
	loc := expectedHeaderRE.FindStringIndex(markdown)
	if loc == nil {
		return strings.TrimSpace(markdown)
	}
	return strings.TrimSpace(markdown[:loc[0]])
}

// planSymbolRE matches PascalCase identifiers (with optional dotted
// method/field) commonly used in plan narratives. Lowercase-leading
// identifiers are excluded to keep English nouns ("the function", "this
// type") out of the candidate set.
var planSymbolRE = regexp.MustCompile(`\b[A-Z][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)?\b`)

// ExtractPlanSymbols pulls likely code-symbol references out of a plan
// markdown. Order-preserving, dedup. Feeds SymbolF1 alongside the
// hand-curated Entry.ChangedSymbols truth.
//
// Limitation: regex is heuristic — it picks up Type names mentioned in
// prose (e.g. "modify ServerConfig"), missed by AST-diff but useful as a
// first-pass signal. A future iteration could swap to a tree-sitter
// pass over fenced code blocks for precision.
func ExtractPlanSymbols(markdown string) []string {
	if markdown == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, m := range planSymbolRE.FindAllString(markdown, -1) {
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

// tokenF1 returns the symmetric F1 of two tokenized strings' sets.
func tokenF1(a, b string) float64 {
	setA := tokenSet(a)
	setB := tokenSet(b)
	if len(setA) == 0 || len(setB) == 0 {
		return 0
	}
	var tp int
	for t := range setA {
		if _, ok := setB[t]; ok {
			tp++
		}
	}
	if tp == 0 {
		return 0
	}
	precision := float64(tp) / float64(len(setA))
	recall := float64(tp) / float64(len(setB))
	return 2 * precision * recall / (precision + recall)
}

func tokenSet(s string) map[string]struct{} {
	tokens := tokenize(s)
	if len(tokens) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		out[t] = struct{}{}
	}
	return out
}

// tokenize splits text on non-token-rune boundaries (ASCII letter/digit
// or Hangul syllable), lowercases, and drops length-1 tokens and the
// minimal stopword set. Order-preserving for debuggability — callers
// that need a set must deduplicate.
func tokenize(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ToLower(s)
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		t := cur.String()
		cur.Reset()
		if len([]rune(t)) <= 1 {
			return
		}
		if _, stop := stopwords[t]; stop {
			return
		}
		out = append(out, t)
	}
	for _, r := range s {
		if isTokenRune(r) {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func isTokenRune(r rune) bool {
	if r >= '0' && r <= '9' {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	// Hangul script: syllables (AC00–D7A3) + Jamo blocks.
	return unicode.Is(unicode.Hangul, r)
}

// stopwords — minimal English + Korean set to drop the most common
// connectives that dominate short PR narratives. Conservative on
// purpose; token-F1 already normalizes by set size, so over-pruning
// hurts more than under-pruning.
var stopwords = map[string]struct{}{
	// English
	"the": {}, "and": {}, "of": {}, "to": {}, "in": {}, "for": {},
	"on": {}, "with": {}, "by": {}, "is": {}, "be": {}, "are": {},
	"this": {}, "that": {}, "as": {}, "an": {}, "or": {}, "if": {},
	"it": {}, "at": {}, "from": {}, "we": {}, "into": {}, "was": {},
	"were": {}, "has": {}, "have": {}, "had": {}, "but": {}, "not": {},
	// Korean particles + common conjunctions
	"그리고": {}, "또는": {}, "또한": {}, "그러나": {}, "하지만": {},
	"및": {}, "에서": {}, "에게": {}, "에는": {}, "에서는": {},
}

// normalizeSymbolSet folds a slice of symbol strings into a set keyed
// by their canonical form.
func normalizeSymbolSet(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, s := range in {
		n := normalizeSymbol(s)
		if n != "" {
			out[n] = struct{}{}
		}
	}
	return out
}

// normalizeSymbol collapses any non-alphanum into a single `_`, lowercases,
// trims leading/trailing `_`. So `GovMinter._cleanupBurnDeposit` and
// `GovMinter cleanupBurnDeposit` both fold to `govminter_cleanupburndeposit`.
func normalizeSymbol(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	prevSep := true // suppress leading _
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevSep = false
			continue
		}
		if !prevSep {
			b.WriteByte('_')
			prevSep = true
		}
	}
	out := strings.TrimRight(b.String(), "_")
	return out
}

// cosine computes the cosine similarity of two equal-length vectors.
// Returns 0 on zero-magnitude input (no orientation to compare).
// Clamped to [-1, 1] to absorb floating-point drift.
func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		fa := float64(a[i])
		fb := float64(b[i])
		dot += fa * fb
		na += fa * fa
		nb += fb * fb
	}
	if na == 0 || nb == 0 {
		return 0
	}
	c := dot / (math.Sqrt(na) * math.Sqrt(nb))
	if c > 1 {
		c = 1
	} else if c < -1 {
		c = -1
	}
	return c
}
