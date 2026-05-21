package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xmhha/code-knowledge-vector/internal/footprint"
)

// newFootprint creates the per-command logger. It writes JSONL to
// <out>/footprint.jsonl unless --no-footprint is set, and always emits
// human-readable slog output to stderr.
//
// Respects the persistent root flags (B8):
//   - --log-level (or $CKV_LOG_LEVEL) sets the slog minimum level.
//   - --profile <path> tells the logger to aggregate per-event
//     latencies and dump them as profile.json on Close.
//
// The footprint file is the seed of the future read-write working-memory
// MCP: every build/query/mcp event is preserved so memory recall +
// retrieval-quality feedback loops can read it later.
func newFootprint(outDir, runID string) *footprint.Logger {
	if globalFlags.noFootprint {
		return footprint.Discard()
	}
	jsonl := filepath.Join(outDir, "footprint.jsonl")
	level := resolveLogLevel(globalFlags.logLevel)
	l, err := footprint.New(footprint.Options{
		JSONLPath:   jsonl,
		Stderr:      true,
		RunID:       runID,
		Level:       level,
		ProfilePath: globalFlags.profile,
	})
	if err != nil {
		// Footprint failure is non-fatal — degrade to stderr-only so the
		// build/query work proceeds. Surface the cause so the user can fix.
		fmt.Fprintf(os.Stderr, "ckv: footprint disabled (%v)\n", err)
		l, _ = footprint.New(footprint.Options{Stderr: true, RunID: runID, Level: level})
	}
	return l
}

// resolveLogLevel parses --log-level / $CKV_LOG_LEVEL into a slog.Level.
// Empty input or unknown values fall back to LevelInfo (the documented
// default). Accepted spellings: debug, info, warn / warning, error.
func resolveLogLevel(flagValue string) slog.Level {
	raw := flagValue
	if raw == "" {
		raw = os.Getenv("CKV_LOG_LEVEL")
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
