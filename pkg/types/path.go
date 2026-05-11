package types

import "path/filepath"

// matchPath is the single matcher used by Filter.PathGlob. Today: plain
// filepath.Match (single '*' globs, no '**'). When/if we swap to doublestar
// or another implementation, this is the one seam to change.
func matchPath(pattern, path string) (bool, error) {
	return filepath.Match(pattern, path)
}
