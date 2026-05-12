// Package eval scores ckv against a known-query fixture. It produces
// recall@k, MRR, and citation-accuracy metrics so model/chunker changes
// can be detected as regressions.
//
// Fixture format (testdata/queries.yaml):
//
//	schema_version: "1"
//	queries:
//	  - id: q1
//	    intent: "..."
//	    expected:
//	      file: server.go
//	      symbol: Server.Listen
//	      kind: Method
//	      line_range: [22, 29]
//
// A query passes when at least one hit in top-K (default K=5) cites
// the expected file and the hit's line range overlaps the expected one.
package eval

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// FixtureSchemaVersion is the version this binary writes/expects.
// Bump on breaking changes; consumers should refuse loading newer
// majors and warn on newer minors.
const FixtureSchemaVersion = "1"

// Fixture is the parsed top-level document.
type Fixture struct {
	SchemaVersion string  `yaml:"schema_version"`
	Queries       []Query `yaml:"queries"`
}

// Query is one ground-truth entry.
type Query struct {
	ID       string   `yaml:"id"`
	Intent   string   `yaml:"intent"`
	Expected Expected `yaml:"expected"`
	Notes    string   `yaml:"notes,omitempty"`
}

// Expected describes the correct retrieval target. LineRange is
// [start, end] inclusive; the scorer treats any hit overlapping this
// range (and matching File) as correct.
type Expected struct {
	File      string           `yaml:"file"`
	Symbol    string           `yaml:"symbol,omitempty"`
	Kind      types.SymbolKind `yaml:"kind,omitempty"`
	LineRange [2]int           `yaml:"line_range"`
}

// LoadFixture reads and validates a YAML fixture from path. Validation
// checks: non-empty schema_version, every query has id + intent +
// expected.file + a sane line_range.
func LoadFixture(path string) (*Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fixture: %w", err)
	}
	var f Fixture
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse fixture: %w", err)
	}
	if f.SchemaVersion == "" {
		return nil, fmt.Errorf("fixture: missing schema_version")
	}
	if f.SchemaVersion != FixtureSchemaVersion {
		// Major-version mismatch is fatal; minor we let through with a
		// warning at the runner layer.
		return nil, fmt.Errorf("fixture: schema_version %q not supported (this binary: %q)",
			f.SchemaVersion, FixtureSchemaVersion)
	}
	seen := map[string]bool{}
	for i, q := range f.Queries {
		if q.ID == "" {
			return nil, fmt.Errorf("fixture: query %d missing id", i)
		}
		if seen[q.ID] {
			return nil, fmt.Errorf("fixture: duplicate query id %q", q.ID)
		}
		seen[q.ID] = true
		if q.Intent == "" {
			return nil, fmt.Errorf("fixture: query %q missing intent", q.ID)
		}
		if q.Expected.File == "" {
			return nil, fmt.Errorf("fixture: query %q missing expected.file", q.ID)
		}
		if q.Expected.LineRange[0] < 1 || q.Expected.LineRange[1] < q.Expected.LineRange[0] {
			return nil, fmt.Errorf("fixture: query %q has invalid line_range %v",
				q.ID, q.Expected.LineRange)
		}
	}
	return &f, nil
}
