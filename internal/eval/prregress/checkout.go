package prregress

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree is a detached `git worktree` pointing at base_sha. The
// original clone's checkout / branch / HEAD is not touched, so the
// user can keep working on whatever they had checked out.
type Worktree struct {
	Path    string // absolute path of the worktree directory
	BaseSHA string // resolved full SHA (in case the fixture used a prefix)

	sourcePath string // original clone path — used to clean up on Close()
}

// CreateWorktree spins up a detached worktree at base_sha. Returns a
// Worktree handle whose Close() removes the worktree (the underlying
// commits stay in the clone's objects/).
//
// The worktree path is deterministic per (sourcePath, baseSHA), so
// reruns reuse the same directory and avoid unbounded /tmp growth.
// We do NOT cache across runs to avoid "stale index" surprises — the
// caller deletes it via Close() on every eval.
func CreateWorktree(ctx context.Context, e Entry) (*Worktree, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("git not found in PATH")
	}
	abs, err := filepath.Abs(e.SourcePath)
	if err != nil {
		return nil, fmt.Errorf("resolve source_path: %w", err)
	}
	if _, err := os.Stat(filepath.Join(abs, ".git")); err != nil {
		return nil, fmt.Errorf("source_path %s is not a git checkout: %w", abs, err)
	}

	// Resolve base_sha to its full form via `git rev-parse`. Prefix
	// matches in the fixture are convenient; we still want the full
	// SHA in logs/metadata.
	fullSHA, err := gitOutput(ctx, abs, "rev-parse", "--verify", e.BaseSHA+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("base_sha %q not found in %s: %w", e.BaseSHA, abs, err)
	}
	fullSHA = strings.TrimSpace(fullSHA)

	// Worktree path: deterministic so reruns overwrite cleanly.
	wtPath := filepath.Join(os.TempDir(), fmt.Sprintf("ckv-prregress-%s-%s", e.ID, fullSHA[:12]))
	// If a leftover worktree exists (previous run crashed), drop it
	// before creating a new one.
	if _, err := os.Stat(wtPath); err == nil {
		_ = gitRun(ctx, abs, "worktree", "remove", "--force", wtPath)
		_ = os.RemoveAll(wtPath) // belt + suspenders
	}

	// Detached worktree → no branch created, no HEAD mutation in the
	// original clone.
	if err := gitRun(ctx, abs, "worktree", "add", "--detach", wtPath, fullSHA); err != nil {
		return nil, fmt.Errorf("git worktree add at %s: %w", fullSHA, err)
	}

	return &Worktree{Path: wtPath, BaseSHA: fullSHA, sourcePath: abs}, nil
}

// Close removes the worktree. Idempotent.
func (w *Worktree) Close() error {
	if w == nil || w.Path == "" {
		return nil
	}
	ctx := context.Background()
	// Best effort: `git worktree remove` is the right path, but if it
	// fails (e.g. dirty state from the build process), fall through to
	// rm -rf so we don't leak /tmp space.
	_ = gitRun(ctx, w.sourcePath, "worktree", "remove", "--force", w.Path)
	err := os.RemoveAll(w.Path)
	w.Path = ""
	return err
}

// gitRun executes git with stdout/stderr suppressed except on failure.
// Output noise from `git worktree add` (progress lines) is unwanted in
// the eval report; we surface stderr only when something breaks.
func gitRun(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitOutput returns stdout (trimmed) from git.
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return "", fmt.Errorf("git %s: %w (stderr: %s)", strings.Join(args, " "), err, stderr)
	}
	return string(out), nil
}
