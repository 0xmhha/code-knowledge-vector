// Package discover walks --src and yields the source files CKV should
// index. It respects a .ckvignore file (same line-based syntax as
// .gitignore: comments, empty lines, glob patterns; '/' suffix means
// directory). Symlinks, oversized files, and detected binaries are
// always skipped, regardless of ignore rules.
//
// Limitations vs full gitignore semantics:
//   - No negation ("!pattern" — not supported)
//   - No "**" globs (we use filepath.Match; doublestar may land in W3)
//   - Patterns match against the source-relative path AND the basename
//
// These cover the common cases (node_modules/, vendor/, *.log, build/)
// without pulling in a heavyweight gitignore parser. Power users with
// complex rules can pass --files-from once we add it (planned: W3).
package discover

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DefaultMaxBytes caps individual file size to avoid OOM on accidental
// large blobs. 1MiB is generous for source code (largest typical Go
// file in stdlib is ~150KB).
const DefaultMaxBytes = 1 << 20 // 1 MiB

// DefaultIgnore patterns are applied on top of .ckvignore. They are
// the directories every realistic indexer wants to skip and are listed
// explicitly so users can see them in `ckv build --json` output.
var DefaultIgnore = []string{
	".git/",
	"node_modules/",
	"vendor/",
	".next/",
	"out/",
	"dist/",
	"build/",
	"target/",
	".venv/",
	"__pycache__/",
}

// Options control the walk. All fields are optional; the zero value is
// the documented default.
type Options struct {
	MaxBytes int64    // size cap; 0 → DefaultMaxBytes
	Extra    []string // additional ignore patterns from CLI

	// GoBuildFiles, when non-nil, restricts the walk's Go-language
	// output to absolute paths that appear as keys in the map. Other
	// languages (TypeScript, Solidity, etc.) are unaffected — they
	// continue through the regular ignore-pattern path. Use this
	// to honor `build_roots` from ckv.yaml (resolved upstream via
	// ResolveGoBuildRoots). Nil/empty map means "no filter, walk
	// every Go file" — the original behavior.
	GoBuildFiles map[string]struct{}
}

// File is the result record. RelPath is forward-slash, repo-relative.
type File struct {
	AbsPath  string
	RelPath  string
	Size     int64
	Language string // "go" | "typescript" | "solidity" | "markdown" | "" (unknown)
}

// Walk scans srcRoot and returns the list of files CKV should process.
// Errors during walk are logged into errs (one per file) so a single
// bad file doesn't abort the whole indexing pass.
func Walk(srcRoot string, opts Options) (files []File, errs []error, err error) {
	srcRoot, err = filepath.Abs(srcRoot)
	if err != nil {
		return nil, nil, err
	}
	max := opts.MaxBytes
	if max <= 0 {
		max = DefaultMaxBytes
	}

	patterns := append([]string{}, DefaultIgnore...)
	if extra, err := loadCKVIgnore(srcRoot); err == nil {
		patterns = append(patterns, extra...)
	} else if !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	patterns = append(patterns, opts.Extra...)

	walkErr := filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			errs = append(errs, walkErr)
			return nil
		}
		rel, _ := filepath.Rel(srcRoot, path)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		// Directory pruning saves the bulk of work (node_modules, .git).
		if d.IsDir() {
			if isIgnored(rel+"/", patterns) {
				return filepath.SkipDir
			}
			return nil
		}
		// Symlinks: skip to keep the index decoupled from user FS layout.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if isIgnored(rel, patterns) {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			errs = append(errs, infoErr)
			return nil
		}
		if info.Size() > max {
			return nil
		}
		lang := classifyLanguage(rel)
		if lang == "" {
			return nil // unknown language → not indexable today
		}
		// build_roots filter: when GoBuildFiles is set, Go files must
		// be in the resolved dependency closure. Non-Go files pass
		// through — the filter is Go-only by design.
		if lang == "go" && len(opts.GoBuildFiles) > 0 {
			if _, ok := opts.GoBuildFiles[path]; !ok {
				return nil
			}
		}
		if isProbablyBinary(path) {
			return nil
		}
		files = append(files, File{
			AbsPath:  path,
			RelPath:  rel,
			Size:     info.Size(),
			Language: lang,
		})
		return nil
	})
	return files, errs, walkErr
}

// loadCKVIgnore reads <srcRoot>/.ckvignore as line-based patterns.
// Missing file returns os.ErrNotExist (callers usually ignore that).
func loadCKVIgnore(srcRoot string) ([]string, error) {
	f, err := os.Open(filepath.Join(srcRoot, ".ckvignore"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []string
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, scan.Err()
}

// isIgnored matches rel against patterns. Directory patterns end in '/'
// and match any path whose first segment(s) equal the pattern (without
// trailing slash). Non-directory patterns use filepath.Match against
// both the full relative path and the basename.
func isIgnored(rel string, patterns []string) bool {
	base := filepath.Base(strings.TrimSuffix(rel, "/"))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "/") {
			// Directory pattern: matches when rel begins with that dir.
			dir := strings.TrimSuffix(p, "/")
			if rel == dir+"/" || strings.HasPrefix(rel, dir+"/") || rel == dir {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
		if ok, _ := filepath.Match(p, base); ok {
			return true
		}
	}
	return false
}

// classifyLanguage maps file extension to the CKV language tag. Empty
// string means "we don't index this file type today" (W1+W2 ships
// Go-first; TS/Solidity parsers land in W2 too but discovery has the
// classification ready).
//
// Markdown (`*.md`, `*.markdown`) is indexed as "markdown" so docs/ADR
// corpora (plan-S1, featurelist, ADR-*.md) become searchable — see
// review-direction-2026-05-18.md Appendix B for the design rationale.
// Extension-less doc files (README, CHANGELOG) are deferred to S2.
func classifyLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".sol":
		return "solidity"
	case ".md", ".markdown":
		return "markdown"
	}
	return ""
}

// isProbablyBinary returns true if the first 8KiB of the file contains
// a NUL byte. Cheap, false-negative-safe heuristic — every common binary
// format (PNG, ELF, zip) has NUL bytes in its header; source code does not.
func isProbablyBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		// Treat unreadable as binary so we skip rather than panic later.
		return true
	}
	defer f.Close()
	var head [8192]byte
	n, _ := f.Read(head[:])
	return bytes.IndexByte(head[:n], 0) >= 0
}
