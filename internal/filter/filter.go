// Package filter implements the Sensitive Filter engine. It scans text
// for secrets, credentials, and sensitive patterns before data leaves
// the MCP server. Three-tier decision: CLEAN (pass), REDACTED (mask
// sensitive parts), BLOCKED (reject entirely).
//
// Pattern definitions come from a JSON config file (patterns.json).
// The filter is applied as the last step in every MCP tool that
// returns code or text content.
package filter

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Result is the outcome of filtering a text.
type Result struct {
	Status        Status  `json:"status"`
	FilteredText  string  `json:"filtered_text"`
	RedactedCount int     `json:"redacted_count"`
	BlockedBy     string  `json:"blocked_by,omitempty"`
	Matches       []Match `json:"matches,omitempty"`
}

// Status is the filter decision.
type Status string

const (
	StatusClean    Status = "CLEAN"
	StatusRedacted Status = "REDACTED"
	StatusBlocked  Status = "BLOCKED"
)

// Match records one pattern hit.
type Match struct {
	PatternID string `json:"pattern_id"`
	Name      string `json:"name"`
	Severity  string `json:"severity"`
	Action    string `json:"action"`
}

// Pattern is one entry in the pattern config.
type Pattern struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Regex       string `json:"regex"`
	Severity    string `json:"severity"`
	Action      string `json:"action"`
	Description string `json:"description,omitempty"`
	compiled    *regexp.Regexp
}

// Config is the patterns.json structure.
type Config struct {
	Version  string    `json:"version"`
	Patterns []Pattern `json:"patterns"`
}

// Filter scans text against a set of patterns.
type Filter struct {
	patterns []Pattern
}

// New creates a filter from the given config. Patterns with invalid
// regex are silently skipped.
func New(cfg Config) *Filter {
	var compiled []Pattern
	for _, p := range cfg.Patterns {
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			continue
		}
		p.compiled = re
		compiled = append(compiled, p)
	}
	return &Filter{patterns: compiled}
}

// LoadConfig reads a patterns.json file and returns the parsed config.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("filter: read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("filter: parse config: %w", err)
	}
	return cfg, nil
}

// Scan checks text against all patterns and returns the filter result.
func (f *Filter) Scan(text string) Result {
	if f == nil || len(f.patterns) == 0 {
		return Result{Status: StatusClean, FilteredText: text}
	}

	var matches []Match
	filtered := text
	blocked := false
	blockedBy := ""

	for _, p := range f.patterns {
		if !p.compiled.MatchString(text) {
			continue
		}
		matches = append(matches, Match{
			PatternID: p.ID,
			Name:      p.Name,
			Severity:  p.Severity,
			Action:    p.Action,
		})
		switch p.Action {
		case "block":
			blocked = true
			blockedBy = p.ID
		case "redact":
			replacement := fmt.Sprintf("[REDACTED:%s]", p.ID)
			filtered = p.compiled.ReplaceAllString(filtered, replacement)
		}
	}

	if blocked {
		return Result{
			Status:    StatusBlocked,
			BlockedBy: blockedBy,
			Matches:   matches,
		}
	}
	if len(matches) > 0 {
		return Result{
			Status:        StatusRedacted,
			FilteredText:  filtered,
			RedactedCount: countRedactions(filtered),
			Matches:       matches,
		}
	}
	return Result{Status: StatusClean, FilteredText: text}
}

// PassThrough returns a no-op filter that passes all text unchanged.
// Used when no patterns.json is configured.
func PassThrough() *Filter {
	return &Filter{}
}

func countRedactions(text string) int {
	return strings.Count(text, "[REDACTED:")
}
