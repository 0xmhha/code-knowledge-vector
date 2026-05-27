// Package projectcfg loads <src>/ckv.yaml — the per-project hook for
// customizing how CKV indexes a repository. The file is optional; an
// absent file resolves to a zero-value Config that the build layer
// treats as "use defaults everywhere".
//
// Today's fields:
//
//	schema_version: "1"
//	languages: [go, typescript, solidity, markdown]  # subset to index; empty → all
//	ignore: ["vendor/**", "**/*_test.go"]    # extra .ckvignore patterns
//	chunking:
//	  file_header_lines: 30                  # override default 50
//	build_roots: [./cmd/ckv]                 # Go entry packages; index
//	                                          # only files reachable from these
//	                                          # via `go list -deps`. Empty →
//	                                          # walk the whole srcRoot.
//
// Reserved-for-future (no-op today; documented to stabilize the schema):
//
//	important_symbols: ["^Handle.*"]          # regex tags on chunks
//	skills_dir: ".claude/skills"              # per-project skills hook
//
// Reserving these now means projects can adopt them without a schema
// bump later. Unknown future fields decode into IgnoredFields so we
// can surface a "your ckv.yaml has unknown fields X,Y" warning instead
// of silently dropping configuration.
package projectcfg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersionCurrent is the version this binary writes/accepts.
const SchemaVersionCurrent = "1"

// FileName is the file name we look for at <src>/.
const FileName = "ckv.yaml"

// Config is the parsed project hook. All fields are optional; zero
// values fall back to global defaults.
type Config struct {
	SchemaVersion    string          `yaml:"schema_version"`
	Languages        []string        `yaml:"languages,omitempty"`
	Ignore           []string        `yaml:"ignore,omitempty"`
	Chunking         ChunkingOptions `yaml:"chunking,omitempty"`
	ImportantSymbols []string        `yaml:"important_symbols,omitempty"`
	SkillsDir        string          `yaml:"skills_dir,omitempty"`

	// BuildRoots is a list of Go entry packages (e.g. ./cmd/ckv) whose
	// transitive dependency closure defines the corpus to index. Empty
	// means "walk the whole srcRoot" — the original behavior. The build
	// layer resolves each entry via `go list -json -deps`, collects the
	// .go files those packages own, and uses that file set as a filter
	// when walking. Entries must be repo-relative paths so ckv.yaml
	// stays portable across checkouts.
	//
	// Today BuildRoots filters Go files only; other languages (TS, Sol)
	// fall through unaffected. A multi-language analog (TS via tsconfig,
	// etc.) is a future-work item.
	BuildRoots []string `yaml:"build_roots,omitempty"`

	// importantRE is the compiled regex form of ImportantSymbols.
	// Populated by Load after schema validation. Not yaml-tagged.
	importantRE []*regexp.Regexp
}

// ChunkingOptions overrides specific chunker knobs. Pointer-valued
// would have been more correct ("nil = inherit default") but the YAML
// authoring story is cleaner with omitempty + zero-value-means-default.
// The build layer treats FileHeaderLines == 0 as "use default".
type ChunkingOptions struct {
	FileHeaderLines int `yaml:"file_header_lines,omitempty"`
}

// ErrNotFound is returned when <src>/ckv.yaml does not exist. Callers
// usually swallow this and continue with a zero-value Config.
var ErrNotFound = errors.New("projectcfg: not found")

// Load reads <srcRoot>/ckv.yaml. Returns ErrNotFound when the file is
// absent. Other errors (yaml parse, schema mismatch, regex compile)
// are returned wrapped so callers can choose to fail-fast or fall
// back to defaults.
func Load(srcRoot string) (*Config, error) {
	path := filepath.Join(srcRoot, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parse(data, path)
}

// LoadOrDefault returns Load's result, or a zero-value Config (and no
// error) when ErrNotFound. Convenience for the build path where
// "no ckv.yaml" is a perfectly valid state.
func LoadOrDefault(srcRoot string) (*Config, error) {
	cfg, err := Load(srcRoot)
	if errors.Is(err, ErrNotFound) {
		return &Config{SchemaVersion: SchemaVersionCurrent}, nil
	}
	return cfg, err
}

func parse(data []byte, path string) (*Config, error) {
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.SchemaVersion == "" {
		return nil, fmt.Errorf("%s: missing schema_version (expected %q)", path, SchemaVersionCurrent)
	}
	if c.SchemaVersion != SchemaVersionCurrent {
		return nil, fmt.Errorf("%s: schema_version %q not supported (this binary: %q)",
			path, c.SchemaVersion, SchemaVersionCurrent)
	}
	// Validate languages are known. We can't import internal/build's
	// parser registry here without a cycle, so this is a syntactic
	// check against the documented allow-list.
	for _, lang := range c.Languages {
		if _, ok := knownLanguages[lang]; !ok {
			return nil, fmt.Errorf("%s: unknown language %q (supported: go, typescript, solidity, markdown)", path, lang)
		}
	}
	if c.Chunking.FileHeaderLines < 0 {
		return nil, fmt.Errorf("%s: chunking.file_header_lines must be ≥ 0", path)
	}
	for i, root := range c.BuildRoots {
		trimmed := strings.TrimSpace(root)
		if trimmed == "" {
			return nil, fmt.Errorf("%s: build_roots[%d] is empty — remove the entry or fill in a package path", path, i)
		}
		if filepath.IsAbs(trimmed) {
			return nil, fmt.Errorf("%s: build_roots[%d] = %q must be repo-relative (e.g. ./cmd/ckv), not absolute", path, i, trimmed)
		}
		c.BuildRoots[i] = trimmed
	}
	// Compile importantSymbols up front so a bad regex fails the load.
	c.importantRE = make([]*regexp.Regexp, 0, len(c.ImportantSymbols))
	for _, pat := range c.ImportantSymbols {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid important_symbols regex %q: %w", path, pat, err)
		}
		c.importantRE = append(c.importantRE, re)
	}
	return &c, nil
}

// LanguageAllowed reports whether the given language is enabled by
// this config. Empty Languages list means "all languages allowed".
func (c *Config) LanguageAllowed(lang string) bool {
	if c == nil || len(c.Languages) == 0 {
		return true
	}
	return slices.Contains(c.Languages, lang)
}

// MatchesImportant reports whether the given symbol name matches any
// of the important_symbols patterns. Returns false on nil receiver or
// when no patterns are configured.
func (c *Config) MatchesImportant(symbolName string) bool {
	if c == nil || symbolName == "" {
		return false
	}
	for _, re := range c.importantRE {
		if re.MatchString(symbolName) {
			return true
		}
	}
	return false
}

// knownLanguages is the validation allow-list. Kept in sync with the
// parsers registered in internal/build/builder.go. Adding a new
// language requires updating BOTH so misspellings are caught early.
var knownLanguages = map[string]struct{}{
	"go":         {},
	"typescript": {},
	"solidity":   {},
	"markdown":   {},
}
