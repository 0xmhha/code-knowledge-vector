package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/chunk"
	"github.com/0xmhha/code-knowledge-vector/internal/discover"
	"github.com/0xmhha/code-knowledge-vector/internal/footprint"
	"github.com/0xmhha/code-knowledge-vector/internal/manifest"
	cparse "github.com/0xmhha/code-knowledge-vector/internal/parse"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/javascript"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/markdown"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-vector/internal/projectcfg"
	"github.com/0xmhha/code-knowledge-vector/internal/store/sqlitevec"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// ErrNoManifest signals that ReindexOptions.OutDir has no prior index.
// Reindex needs a baseline IndexedHead to diff against; the caller
// should run `ckv build` first.
var ErrNoManifest = errors.New("reindex: no manifest at OutDir — run `ckv build` first")

// ErrEmbedderMismatch signals that the embedder passed to Reindex does
// not match the embedder recorded in the manifest. Reindex would mix
// embeddings from two different models in the same store, which breaks
// retrieval. The caller must either use the original embedder or run a
// full `ckv build` to replace the index.
var ErrEmbedderMismatch = errors.New("reindex: embedder identity does not match manifest")

// ReindexOptions configures a partial rebuild. SrcRoot, OutDir, and
// Embedder are required; everything else has a documented default.
type ReindexOptions struct {
	SrcRoot   string
	OutDir    string
	Embedder  types.Embedder // must match the embedder identity in the manifest
	CKVIgnore []string       // extra ignore patterns from --ckvignore CLI flag

	// Since is the commit the diff is computed against. Empty means
	// "use manifest.IndexedHead" (the common case). Pass a specific
	// SHA to override (e.g., "main~5") for catch-up reindex.
	Since string

	// Files, when non-empty, bypasses the git diff and forces reindex
	// of exactly these src-relative paths. Useful when the caller
	// already knows the change set (CI hook, fsnotify watcher) or when
	// reindexing files that aren't yet committed.
	Files []string

	BatchSize int               // 0 → defaultBatch (32)
	Now       func() time.Time  // 0 → time.Now
	Footprint *footprint.Logger // optional; nil → no logging

	// ProgressOut receives human-readable per-file progress lines.
	// nil disables progress entirely (library-mode default).
	ProgressOut io.Writer

	// DisableContextualPrefix mirrors Options.DisableContextualPrefix
	// for the reindex path so partial rebuilds match what the original
	// build produced. Keep both at the same value across build+reindex
	// — mixing prefixed and raw embeddings in one store would degrade
	// retrieval.
	DisableContextualPrefix bool
}

// ReindexResult is what Reindex returns to the caller.
type ReindexResult struct {
	// FilesProcessed is the count of files actually re-embedded
	// (added + modified). Deletions don't count here.
	FilesProcessed int
	// FilesAdded / FilesModified / FilesDeleted partition the changed
	// set by git diff status so callers can report a useful summary.
	FilesAdded    int
	FilesModified int
	FilesDeleted  int
	// FilesSkipped is files in the diff that didn't match any parser
	// (e.g., changed README.txt is in the diff but ckv doesn't index
	// .txt today). Surfaced so users know the diff size != reindex size.
	FilesSkipped int
	// Chunks aggregates chunk.Stats across every file processed.
	Chunks chunk.Stats
	// PrevHead and NewHead bracket the reindex range.
	PrevHead string
	NewHead  string
	BuiltAt  string
	DBPath   string
}

// Reindex re-embeds only the files that changed between the manifest's
// IndexedHead (or ReindexOptions.Since) and the source tree's current
// git HEAD. Idempotent: re-running with no changes is a no-op except
// the manifest's BuiltAt timestamp.
//
// Pipeline:
//  1. Load manifest → get PrevHead + verify Embedder identity.
//  2. Compute the change set:
//     - if ReindexOptions.Files is set, use it verbatim;
//     - else `git diff --name-status PrevHead..HEAD` partitions paths
//     into added / modified / deleted (renames split into delete+add).
//  3. For deletions: store.DeleteByFile.
//  4. For adds + modifications: parse → chunk → DeleteByFile (idempotent
//     for adds) → embed → upsert.
//  5. Update manifest IndexedHead and BuiltAt.
//
// Files that fall outside the supported language set are silently
// skipped (reported in FilesSkipped) so a diff that touches docs/
// markdown + go files works without manual filtering.
func Reindex(ctx context.Context, o ReindexOptions) (*ReindexResult, error) {
	if o.SrcRoot == "" || o.OutDir == "" {
		return nil, fmt.Errorf("reindex: SrcRoot and OutDir are required")
	}
	if o.Embedder == nil {
		return nil, fmt.Errorf("reindex: Embedder is required")
	}
	if o.BatchSize <= 0 {
		o.BatchSize = defaultBatch
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	fp := o.Footprint
	if fp == nil {
		fp = footprint.Discard()
	}

	man, err := manifest.Load(o.OutDir)
	if err != nil {
		if errors.Is(err, manifest.ErrNotFound) {
			return nil, ErrNoManifest
		}
		return nil, fmt.Errorf("load manifest: %w", err)
	}
	if man.EmbeddingModel != "" && man.EmbeddingModel != o.Embedder.Name() {
		return nil, fmt.Errorf("%w: index=%q embedder=%q",
			ErrEmbedderMismatch, man.EmbeddingModel, o.Embedder.Name())
	}
	if man.EmbeddingDim != o.Embedder.Dimension() {
		return nil, fmt.Errorf("%w: index_dim=%d embedder_dim=%d",
			ErrEmbedderMismatch, man.EmbeddingDim, o.Embedder.Dimension())
	}

	prevHead := o.Since
	if prevHead == "" {
		prevHead = man.IndexedHead
	}

	newHead, _ := detectCommit(o.SrcRoot)

	changes, err := resolveChangeSet(o.SrcRoot, prevHead, newHead, o.Files)
	if err != nil {
		return nil, err
	}

	doneReindex := fp.Span("reindex",
		"src_root", o.SrcRoot,
		"out_dir", o.OutDir,
		"prev_head", prevHead,
		"new_head", newHead,
		"diff_size", len(changes.added)+len(changes.modified)+len(changes.deleted),
	)

	cfg, cfgErr := projectcfg.LoadOrDefault(o.SrcRoot)
	if cfgErr != nil {
		return nil, fmt.Errorf("project config: %w", cfgErr)
	}

	dbPath := filepath.Join(o.OutDir, "vector.db")
	store, err := sqlitevec.Open(dbPath, o.Embedder.Dimension())
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	parsers := map[string]cparse.Parser{
		"go":         golang.New(),
		"typescript": typescript.New(),
		"javascript": javascript.New(),
		"solidity":   solidity.New(),
		"markdown":   markdown.New(),
	}

	chunker := chunk.New(chunk.Options{
		MaxInputTokens:  o.Embedder.MaxInputTokens(),
		FileHeaderLines: cfg.Chunking.FileHeaderLines,
	})

	embedTextFn := chunk.BuildEmbedText
	if o.DisableContextualPrefix {
		embedTextFn = chunk.RawEmbedText
	}

	result := &ReindexResult{
		PrevHead: prevHead,
		NewHead:  newHead,
		DBPath:   dbPath,
	}
	languageCounts := make(map[string]int, len(man.Languages))
	maps.Copy(languageCounts, man.Languages)

	// Step 1: deletions. The store happily accepts paths that don't
	// exist — DeleteByFile is idempotent — so we don't need to gate it.
	for _, rel := range changes.deleted {
		if err := store.DeleteByFile(ctx, rel); err != nil {
			return nil, fmt.Errorf("delete %s: %w", rel, err)
		}
		result.FilesDeleted++
	}

	// Step 2: re-embed adds + modifications. We don't distinguish them
	// at the store layer — DeleteByFile is a no-op when there are no
	// existing rows, so the same code path handles both.
	mergedIgnore := append([]string{}, cfg.Ignore...)
	mergedIgnore = append(mergedIgnore, o.CKVIgnore...)

	for _, rel := range concat(changes.added, changes.modified) {
		abs := filepath.Join(o.SrcRoot, rel)
		// Skip paths the discover layer would have rejected (binary,
		// secret pattern, oversized, ignored). Mirrors Walk's classify
		// + isIgnored checks so reindex stays consistent with build.
		lang := classifyLanguageRel(rel)
		if lang == "" {
			result.FilesSkipped++
			continue
		}
		if _, ok := parsers[lang]; !ok {
			result.FilesSkipped++
			continue
		}
		if !cfg.LanguageAllowed(lang) {
			result.FilesSkipped++
			continue
		}
		if discoverIgnored(rel, mergedIgnore) {
			result.FilesSkipped++
			// File was indexed before but is now ignored — drop its
			// stale chunks. Skipped count still increments since we
			// didn't process it; the deletion happens silently here.
			if err := store.DeleteByFile(ctx, rel); err != nil {
				return nil, fmt.Errorf("drop newly-ignored %s: %w", rel, err)
			}
			continue
		}
		// Read the file as it exists *now*. A renamed file shows up in
		// the diff under its new name; an added file may not yet be in
		// git history but is on disk.
		src, err := os.ReadFile(abs)
		if err != nil {
			// File listed by diff but missing on disk: treat as a
			// late delete. Keeps reindex robust against races.
			if errors.Is(err, os.ErrNotExist) {
				if err := store.DeleteByFile(ctx, rel); err != nil {
					return nil, fmt.Errorf("delete vanished %s: %w", rel, err)
				}
				result.FilesDeleted++
				continue
			}
			return nil, fmt.Errorf("read %s: %w", rel, err)
		}
		if discover.IsProbablyBinary(abs) {
			result.FilesSkipped++
			continue
		}
		p := parsers[lang]
		spans, err := p.Parse(rel, src)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", rel, err)
		}
		chunks := chunker.Chunk(chunk.Input{
			File:       rel,
			Language:   lang,
			CommitHash: newHead,
			Source:     src,
			Spans:      spans,
		})
		// Always purge existing rows first so a file that lost symbols
		// (e.g., a deleted function) doesn't leave orphan chunks. This
		// is the difference between idempotent rebuild and silent stale.
		if err := store.DeleteByFile(ctx, rel); err != nil {
			return nil, fmt.Errorf("purge before reindex %s: %w", rel, err)
		}
		if len(chunks) == 0 {
			continue // empty file or all parse spans dropped
		}
		if err := embedAndUpsert(ctx, store, o.Embedder, chunks, o.BatchSize, nil, embedTextFn); err != nil {
			return nil, fmt.Errorf("embed/upsert %s: %w", rel, err)
		}
		if containsString(changes.added, rel) {
			result.FilesAdded++
		} else {
			result.FilesModified++
		}
		result.FilesProcessed++
		s := chunk.Summarize(chunks)
		result.Chunks.Total += s.Total
		result.Chunks.Symbol += s.Symbol
		result.Chunks.FileHeader += s.FileHeader
		result.Chunks.Doc += s.Doc
		result.Chunks.FunctionSplit += s.FunctionSplit
		result.Chunks.Truncated += s.Truncated
		languageCounts[lang] += s.Total
	}

	// Step 3: refresh manifest. Even with zero changes we update
	// BuiltAt so freshness reflects the most recent verification pass.
	builtAt := o.Now().UTC().Format(time.RFC3339)
	result.BuiltAt = builtAt

	man.BuiltAt = builtAt
	man.SrcCommit = newHead
	man.IndexedHead = newHead
	man.Languages = languageCounts
	// ChunkCount in the manifest is best-effort: we don't recompute it
	// from a SELECT COUNT after every reindex (expensive on large DBs).
	// Callers who need an authoritative count should query the store.
	man.ChunkCount += result.Chunks.Total - (result.FilesDeleted + result.FilesModified)

	if err := store.SetManifest(ctx, map[string]string{
		"embedding_model":     o.Embedder.Name(),
		"embedding_dim":       fmt.Sprintf("%d", o.Embedder.Dimension()),
		"embedding_normalize": man.EmbeddingNormalize,
		"indexed_head":        newHead,
		"built_at":            builtAt,
	}); err != nil {
		return nil, fmt.Errorf("write db manifest: %w", err)
	}
	if err := manifest.Save(o.OutDir, man); err != nil {
		return nil, fmt.Errorf("save manifest.json: %w", err)
	}

	doneReindex(
		"files_processed", result.FilesProcessed,
		"files_added", result.FilesAdded,
		"files_modified", result.FilesModified,
		"files_deleted", result.FilesDeleted,
		"files_skipped", result.FilesSkipped,
		"chunks_total", result.Chunks.Total,
		"new_head", newHead,
	)
	return result, nil
}

// changeSet partitions paths by what the index should do with them.
// Adds and modifications take the same code path at runtime (delete-
// then-upsert), but we track them separately for reporting.
type changeSet struct {
	added    []string
	modified []string
	deleted  []string
}

// resolveChangeSet returns the set of paths affected by the reindex.
// If forceFiles is non-empty it overrides the git diff entirely —
// every entry is treated as a modification (the safe default; if the
// file doesn't exist on disk we promote it to a deletion downstream).
func resolveChangeSet(srcRoot, prevHead, newHead string, forceFiles []string) (changeSet, error) {
	if len(forceFiles) > 0 {
		return changeSet{modified: forceFiles}, nil
	}
	if prevHead == "" {
		return changeSet{}, errors.New("reindex: no previous head — manifest has no IndexedHead and no --since override")
	}
	if newHead == "" {
		return changeSet{}, errors.New("reindex: source tree has no git HEAD — pass --files or commit first")
	}
	if prevHead == newHead {
		return changeSet{}, nil // already fresh
	}
	out, err := exec.Command("git", "-C", srcRoot, "diff", "--name-status", prevHead, newHead).Output()
	if err != nil {
		return changeSet{}, fmt.Errorf("git diff %s..%s: %w", prevHead, newHead, err)
	}
	return parseDiffNameStatus(string(out)), nil
}

// parseDiffNameStatus turns `git diff --name-status` output into a
// changeSet. The format is "<status>\t<path>" for A/M/D and
// "R<NN>\t<old>\t<new>" for renames (NN is the similarity score).
// Renames are split into a deletion of the old path plus an add of
// the new path so the indexer doesn't have to special-case them.
func parseDiffNameStatus(diff string) changeSet {
	cs := changeSet{}
	for line := range strings.SplitSeq(diff, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		switch {
		case strings.HasPrefix(status, "A"):
			cs.added = append(cs.added, fields[1])
		case strings.HasPrefix(status, "M"):
			cs.modified = append(cs.modified, fields[1])
		case strings.HasPrefix(status, "D"):
			cs.deleted = append(cs.deleted, fields[1])
		case strings.HasPrefix(status, "R"):
			// Rename: D(old) + A(new). Both paths are present.
			if len(fields) >= 3 {
				cs.deleted = append(cs.deleted, fields[1])
				cs.added = append(cs.added, fields[2])
			}
		case strings.HasPrefix(status, "C"):
			// Copy: A(new) only — the source is unchanged.
			if len(fields) >= 3 {
				cs.added = append(cs.added, fields[2])
			}
		case strings.HasPrefix(status, "T"):
			// Type change (mode bits, symlink↔file): re-read as a
			// modification so the chunker decides based on new content.
			cs.modified = append(cs.modified, fields[1])
		}
	}
	return cs
}

// classifyLanguageRel maps a src-relative path to the language tag the
// indexer uses, or empty when ckv doesn't index this extension.
// Mirrors discover.classifyLanguage without re-exporting it.
func classifyLanguageRel(rel string) string {
	ext := strings.ToLower(filepath.Ext(rel))
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

// discoverIgnored checks whether rel matches any of the supplied
// patterns OR the default secret patterns. It does not consult
// DefaultIgnore (.git/, node_modules/) because git diff already
// excludes those — they're untracked or gitignored.
func discoverIgnored(rel string, extra []string) bool {
	patterns := append([]string{}, extra...)
	if os.Getenv("CKV_DISABLE_SECRET_FILTER") != "1" {
		patterns = append(patterns, discover.DefaultSecretPatterns...)
	}
	return discover.IsIgnored(rel, patterns)
}

func concat(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

func containsString(s []string, v string) bool {
	return slices.Contains(s, v)
}
