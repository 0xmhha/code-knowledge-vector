// Package discover — Go build-root resolution.
//
// FU-9 lets ckv.yaml say "index only the files reachable from these
// Go entry packages." This file is the bridge: given entry packages
// (./cmd/ckv, ./cmd/server, ...), it asks `go list -json -deps` what
// the transitive dependency closure looks like and returns the absolute
// paths of every .go file those packages own.
//
// The returned set is then passed to Walk() as a filter — files outside
// the set are simply skipped during the walk, even if they sit inside
// srcRoot. Non-Go files (TS, Solidity) are not affected by this filter.

package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// goListPackage is the subset of `go list -json` output we care about.
// We deliberately don't decode every field — the JSON is large (~50KB
// per package) and the rest is not load-bearing for our filter.
type goListPackage struct {
	Dir            string   `json:"Dir"`
	ImportPath     string   `json:"ImportPath"`
	Standard       bool     `json:"Standard"`
	GoFiles        []string `json:"GoFiles"`
	CgoFiles       []string `json:"CgoFiles"`
	TestGoFiles    []string `json:"TestGoFiles"`
	XTestGoFiles   []string `json:"XTestGoFiles"`
	IgnoredGoFiles []string `json:"IgnoredGoFiles"`
}

// GoListOptions tunes ResolveGoBuildRoots' behavior.
type GoListOptions struct {
	// IncludeTests adds *_test.go files (TestGoFiles + XTestGoFiles)
	// to the returned set. Defaults true — tests are valuable as
	// usage examples and live in the same packages as the code under
	// test, so including them mirrors how an agent would search.
	IncludeTests bool

	// SkipStandardLib drops Go's stdlib packages (fmt, os, ...) from
	// the result. Defaults true — stdlib sources live outside srcRoot
	// anyway, so including them would just inflate the set with paths
	// the walker can't reach.
	SkipStandardLib bool
}

// DefaultGoListOptions are the sensible defaults: include tests, skip
// stdlib. Callers can construct a custom GoListOptions when they need
// to deviate.
func DefaultGoListOptions() GoListOptions {
	return GoListOptions{IncludeTests: true, SkipStandardLib: true}
}

// ResolveGoBuildRoots walks the dependency closure of `entryPackages`
// using `go list -json -deps` and returns the absolute paths of every
// .go file owned by the reachable packages.
//
// `srcRoot` is the directory `go list` runs in — it must be inside the
// module that owns the entry packages. The function returns a set
// (map[string]struct{}) instead of a slice because the walker uses
// O(1) lookups; the conversion is cheap.
//
// Failures are returned wrapped so the caller can surface "go list
// failed at <pkg>" instead of a raw subprocess error.
func ResolveGoBuildRoots(ctx context.Context, srcRoot string, entryPackages []string, opts GoListOptions) (map[string]struct{}, error) {
	if len(entryPackages) == 0 {
		return nil, errors.New("discover: ResolveGoBuildRoots called with no entry packages")
	}
	if _, err := exec.LookPath("go"); err != nil {
		return nil, fmt.Errorf("discover: `go` not in PATH — required for build_roots resolution")
	}

	args := []string{"list", "-json", "-deps"}
	args = append(args, entryPackages...)

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = srcRoot
	stdout, err := cmd.Output()
	if err != nil {
		stderr := ""
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return nil, fmt.Errorf("discover: go list -deps failed in %s: %w (stderr: %s)", srcRoot, err, stderr)
	}

	files := make(map[string]struct{})
	// `go list -json` emits a stream of concatenated JSON objects, not
	// a JSON array. The standard library's decoder handles this if we
	// loop over Decode() calls.
	dec := json.NewDecoder(strings.NewReader(string(stdout)))
	for dec.More() {
		var pkg goListPackage
		if err := dec.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("discover: parse go list output: %w", err)
		}
		if opts.SkipStandardLib && pkg.Standard {
			continue
		}
		if pkg.Dir == "" {
			// Synthetic / unresolved packages (e.g. test main wrappers)
			// have no Dir. Nothing to add to the file set.
			continue
		}
		addFiles(files, pkg.Dir, pkg.GoFiles)
		addFiles(files, pkg.Dir, pkg.CgoFiles)
		if opts.IncludeTests {
			addFiles(files, pkg.Dir, pkg.TestGoFiles)
			addFiles(files, pkg.Dir, pkg.XTestGoFiles)
		}
		// IgnoredGoFiles are files excluded by build tags; we honor
		// the build tag exclusion and do NOT index them, matching
		// what `make all` actually compiles.
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("discover: go list returned no files for entries %v — check that the paths are valid Go packages", entryPackages)
	}
	return files, nil
}

func addFiles(set map[string]struct{}, dir string, names []string) {
	for _, name := range names {
		set[filepath.Join(dir, name)] = struct{}{}
	}
}
