package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xmhha/code-knowledge-vector/internal/footprint"
)

// newFootprint creates the per-command logger. It writes JSONL to
// <out>/footprint.jsonl unless --no-footprint is set, and always emits
// human-readable slog output to stderr.
//
// The footprint file is the seed of the future read-write working-memory
// MCP: every build/query/mcp event is preserved so memory recall +
// retrieval-quality feedback loops can read it later.
func newFootprint(outDir, runID string) *footprint.Logger {
	if globalFlags.noFootprint {
		return footprint.Discard()
	}
	jsonl := filepath.Join(outDir, "footprint.jsonl")
	l, err := footprint.New(footprint.Options{
		JSONLPath: jsonl,
		Stderr:    true,
		RunID:     runID,
	})
	if err != nil {
		// Footprint failure is non-fatal — degrade to stderr-only so the
		// build/query work proceeds. Surface the cause so the user can fix.
		fmt.Fprintf(os.Stderr, "ckv: footprint disabled (%v)\n", err)
		l, _ = footprint.New(footprint.Options{Stderr: true, RunID: runID})
	}
	return l
}
