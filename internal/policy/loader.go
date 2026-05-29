// Package policy loads project-specific category + ModificationGuidance
// rules from a YAML file and applies them to chunks during build/reindex.
//
// The yaml format is intentionally small: a list of categories, each
// with a name, a set of path globs, and three guidance fields
// (also_review, required_tests, watch_out). First-match-by-glob wins;
// unmatched chunks keep an empty Category and nil Guidance.
//
// Glob support: '*' matches a single path segment; '**' matches zero or
// more segments. Matching is left-anchored to the chunk's File field as
// already stored — keep paths repo-relative (e.g. "core/state/**").
package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// Policy is the loaded ruleset. Categories are evaluated in order.
type Policy struct {
	Version    int            `yaml:"version" json:"version"`
	Categories []CategoryRule `yaml:"categories" json:"categories"`
}

// CategoryRule is one classification entry. A chunk's File is tested
// against each path glob; the first hit determines the category.
type CategoryRule struct {
	Name          string   `yaml:"name" json:"name"`
	Paths         []string `yaml:"paths" json:"paths"`
	AlsoReview    []string `yaml:"also_review" json:"also_review"`
	RequiredTests []string `yaml:"required_tests" json:"required_tests"`
	WatchOut      []string `yaml:"watch_out" json:"watch_out"`
}

// Load parses a policy yaml from disk. Empty path returns an empty
// policy (matches nothing) so callers can treat "no policy file" the
// same as "policy with no rules".
func Load(path string) (*Policy, error) {
	if path == "" {
		return &Policy{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy %s: %w", path, err)
	}
	return Parse(raw)
}

// Parse decodes the yaml bytes into a Policy. Returned errors include
// the path-level context the caller provided (file path or "inline").
func Parse(raw []byte) (*Policy, error) {
	p := &Policy{}
	if err := yaml.Unmarshal(raw, p); err != nil {
		return nil, fmt.Errorf("parse policy yaml: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// Validate checks for obvious authoring mistakes — duplicate category
// names, missing required fields. Glob syntax is not pre-validated
// because filepath.Match is lenient; bad globs simply never match.
func (p *Policy) Validate() error {
	seen := map[string]bool{}
	for i, c := range p.Categories {
		if c.Name == "" {
			return fmt.Errorf("policy: category[%d] has empty name", i)
		}
		if seen[c.Name] {
			return fmt.Errorf("policy: duplicate category %q", c.Name)
		}
		seen[c.Name] = true
		if len(c.Paths) == 0 {
			return fmt.Errorf("policy: category %q has no paths", c.Name)
		}
	}
	return nil
}

// Apply annotates each chunk with Category + Guidance based on the
// first matching CategoryRule. Mutates chunks in place. Idempotent:
// re-applying the same policy is a no-op for chunks that already match
// the same rule.
//
// Returns counts per category (including "" for unmatched) so callers
// can print a coverage summary.
func (p *Policy) Apply(chunks []types.Chunk) map[string]int {
	counts := map[string]int{}
	for i := range chunks {
		cat, guide := p.match(chunks[i].File)
		chunks[i].Category = cat
		chunks[i].Guidance = guide
		counts[cat]++
	}
	return counts
}

// match returns the first matching rule's category and guidance.
// Empty string + nil means no match.
func (p *Policy) match(file string) (string, *types.ModificationGuidance) {
	for _, c := range p.Categories {
		for _, pat := range c.Paths {
			if matchGlob(pat, file) {
				return c.Name, &types.ModificationGuidance{
					AlsoReview:    append([]string(nil), c.AlsoReview...),
					RequiredTests: append([]string(nil), c.RequiredTests...),
					WatchOut:      append([]string(nil), c.WatchOut...),
				}
			}
		}
	}
	return "", nil
}

// matchGlob is a minimal '**'-aware path matcher built on top of
// filepath.Match. '*' matches one segment (no '/'); '**' matches zero
// or more segments. Matching is left-anchored: pattern "core/state/**"
// matches "core/state/journal.go" but not "x/core/state/journal.go".
func matchGlob(pattern, path string) bool {
	// Normalize separators so windows-style paths (if ever) work too.
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)
	patSegs := strings.Split(pattern, "/")
	pathSegs := strings.Split(path, "/")
	return matchSegments(patSegs, pathSegs)
}

func matchSegments(pat, path []string) bool {
	for len(pat) > 0 && len(path) > 0 {
		if pat[0] == "**" {
			// '**' can match zero or more segments. Recurse on each
			// suffix of path to find one that satisfies the rest.
			for i := 0; i <= len(path); i++ {
				if matchSegments(pat[1:], path[i:]) {
					return true
				}
			}
			return false
		}
		ok, _ := filepath.Match(pat[0], path[0])
		if !ok {
			return false
		}
		pat = pat[1:]
		path = path[1:]
	}
	// Trailing '**' in the pattern can match nothing.
	for len(pat) > 0 && pat[0] == "**" {
		pat = pat[1:]
	}
	return len(pat) == 0 && len(path) == 0
}

// GuidanceJSON is the round-trip helper used by the store to persist
// the Guidance struct as a TEXT column. Returns "" for nil so the
// column NULL-or-empty semantics stay consistent.
func GuidanceJSON(g *types.ModificationGuidance) (string, error) {
	if g == nil {
		return "", nil
	}
	b, err := json.Marshal(g)
	if err != nil {
		return "", fmt.Errorf("marshal guidance: %w", err)
	}
	return string(b), nil
}

// GuidanceFromJSON is the inverse of GuidanceJSON. Empty string returns
// nil (treat "no guidance" and "missing column" the same).
func GuidanceFromJSON(raw string) (*types.ModificationGuidance, error) {
	if raw == "" {
		return nil, nil
	}
	var g types.ModificationGuidance
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return nil, fmt.Errorf("unmarshal guidance: %w", err)
	}
	return &g, nil
}
