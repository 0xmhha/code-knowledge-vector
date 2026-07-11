package build

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// checkpointFile is the reindex resume ledger written under the data dir.
const checkpointFile = ".ckv-reindex.ckpt"

// resumeCheckpoint is an append-only ledger of files already re-embedded during
// a reindex toward a specific target head. It lets an interrupted reindex
// resume — skipping files it already finished — instead of re-processing the
// whole change set (reindex-migration-design §4.4). The file's first line is the
// target head; each subsequent line is "<content_sha>\t<relpath>". A crash may
// leave a partial final line, which is simply ignored on reload (that file is
// re-processed). The ledger is removed once the reindex completes.
type resumeCheckpoint struct {
	path string
	head string
	done map[string]string // relpath → content sha256
	f    *os.File          // lazily-opened append handle
}

// loadCheckpoint reads any existing ledger under outDir. A ledger written for a
// different target head is stale — it is discarded (removed) and an empty
// checkpoint returned, so a reindex to a new head never skips the wrong files.
func loadCheckpoint(outDir, targetHead string) *resumeCheckpoint {
	path := filepath.Join(outDir, checkpointFile)
	c := &resumeCheckpoint{path: path, head: targetHead, done: map[string]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return c // absent → fresh
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != targetHead {
		_ = os.Remove(path) // stale (different head) → discard
		return c
	}
	for _, ln := range lines[1:] {
		ln = strings.TrimRight(ln, "\r")
		if ln == "" {
			continue
		}
		if parts := strings.SplitN(ln, "\t", 2); len(parts) == 2 {
			c.done[parts[1]] = parts[0]
		}
	}
	return c
}

// isDone reports whether rel was already re-embedded at content hash sha.
func (c *resumeCheckpoint) isDone(rel, sha string) bool {
	return sha != "" && c.done[rel] == sha
}

// markDone appends a completed file to the ledger (writing the head header when
// the ledger is first created) and records it in memory.
func (c *resumeCheckpoint) markDone(rel, sha string) error {
	if c.f == nil {
		newFile := false
		if _, err := os.Stat(c.path); errors.Is(err, os.ErrNotExist) {
			newFile = true
		}
		f, err := os.OpenFile(c.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("checkpoint open: %w", err)
		}
		c.f = f
		if newFile {
			if _, err := fmt.Fprintf(c.f, "%s\n", c.head); err != nil {
				return fmt.Errorf("checkpoint header: %w", err)
			}
		}
	}
	if _, err := fmt.Fprintf(c.f, "%s\t%s\n", sha, rel); err != nil {
		return fmt.Errorf("checkpoint append: %w", err)
	}
	c.done[rel] = sha
	return nil
}

// clear closes and removes the ledger — called once the reindex completes, so
// the manifest (now advanced) becomes the durable record.
func (c *resumeCheckpoint) clear() {
	if c.f != nil {
		_ = c.f.Close()
		c.f = nil
	}
	_ = os.Remove(c.path)
}

// fileSHA returns the hex sha256 of the file's bytes, or "" on read error (a
// file that can't be hashed is simply never treated as already-done).
func fileSHA(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
