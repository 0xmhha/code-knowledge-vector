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
	"github.com/0xmhha/code-knowledge-vector/internal/ckgalign"
	"github.com/0xmhha/code-knowledge-vector/internal/discover"
	"github.com/0xmhha/code-knowledge-vector/internal/flowcorpus"
	"github.com/0xmhha/code-knowledge-vector/internal/footprint"
	"github.com/0xmhha/code-knowledge-vector/internal/manifest"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/prdoc"
	"github.com/0xmhha/code-knowledge-vector/internal/policy"
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

// ErrSchemaCascade signals the recorded CKG schema_version differs from the
// graph's current schema_version. A CKG cache schema bump cold-rebuilds the
// graph and can change canonical_id semantics wholesale, so a partial reindex
// (even with re-alignment) is unsafe — the caller must run a full `ckv build`
// (reindex-migration-design §3.2).
var ErrSchemaCascade = errors.New("reindex: CKG schema_version changed — run `ckv build` for a full rebuild")

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

	// PolicyPath mirrors Options.PolicyPath. Reindexed chunks pass
	// through the policy loader so category/guidance stay current with
	// the yaml even when only some files change.
	PolicyPath string

	// PRFetch, when non-nil, enables incremental PR-corpus ingest: reindex
	// fetches merged PRs after the manifest's recorded cutoff
	// (sources.prs.last_pr_number / last_merged_at) and indexes only the new
	// ones. Nil leaves the PR corpus untouched (the common code-only reindex).
	PRFetch *PRFetchOptions
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
	// FilesResumed is the count of files skipped because a prior interrupted
	// reindex toward the same head already re-embedded them (resume checkpoint).
	FilesResumed int
	// Chunks aggregates chunk.Stats across every file processed.
	Chunks chunk.Stats
	// PrevHead and NewHead bracket the reindex range.
	PrevHead string
	NewHead  string
	BuiltAt  string
	DBPath   string
	// Validation is the post-reindex integrity report (§5.1): authoritative
	// counts, orphan detection, and canonical coverage.
	Validation sqlitevec.Validation
	// PRsIndexed is the number of PR-corpus chunks added by an incremental PR
	// ingest this run (0 when PRFetch is nil or no new PRs were found).
	PRsIndexed int
	// FlowReindexed is the number of flow-corpus chunks re-indexed this run
	// because the corpus content hash changed (0 when unchanged or absent).
	FlowReindexed int
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
	// Reject a reindex whose embedder produces a different embedding space
	// than the one that built the index, even when name+dim coincide (e.g.
	// Ollama bge-m3 vs ONNX bge-m3). Otherwise the new chunks would be
	// embedded in an incompatible space and silently corrupt similarity.
	// Empty checksum = index predates identity recording → name+dim guards
	// above are the best we can do (no regression).
	if man.EmbeddingChecksum != "" {
		if got := o.Embedder.Identity().Checksum(); got != man.EmbeddingChecksum {
			return nil, fmt.Errorf("%w: index=%q embedder=%q",
				ErrEmbedderMismatch, man.EmbeddingChecksum, got)
		}
	}

	// Serialize concurrent writers on this dataset (§5.3). Released on return.
	lock, err := acquireDatasetLock(o.OutDir)
	if err != nil {
		return nil, err
	}
	defer lock.release()

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

	parsers := newParsers()
	chunker := newChunker(o.Embedder, cfg)
	embedTextFn := resolveEmbedTextFn(o.DisableContextualPrefix)
	pol, err := policy.Load(o.PolicyPath)
	if err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}

	// P4c — resume ledger: files already re-embedded by an interrupted prior
	// reindex toward this same head are skipped below (matched by content hash);
	// the ledger is cleared once this run completes (reindex-migration §4.4).
	ckpt := loadCheckpoint(o.OutDir, newHead)

	// CKG re-alignment (P2a): re-run ckgalign against the same graph this
	// index was built against — recorded in manifest.Sources.CKG.Path — so
	// re-embedded chunks keep their canonical_id join key. Without this,
	// reindexed files silently lose canonical_id (reindex-migration-design
	// §0.2 gap1). Best-effort: a recorded-but-unloadable graph warns and
	// continues rather than blocking a routine reindex on ckg availability;
	// the digest assert at Open/health (P1) owns fail-loud for a genuinely
	// mismatched graph.
	var ckgIx *ckgalign.Index
	if man.Sources != nil && man.Sources.CKG != nil && man.Sources.CKG.Path != "" {
		ix, alignErr := ckgalign.Load(man.Sources.CKG.Path)
		if alignErr != nil {
			fmt.Fprintf(os.Stderr, "ckv: warning: reindex could not load ckg graph at %s (%v); "+
				"re-embedded chunks lose canonical_id until the next aligned build\n",
				man.Sources.CKG.Path, alignErr)
			fp.Emit("reindex.ckg_align_unavailable", "ckg_path", man.Sources.CKG.Path, "err", alignErr.Error())
		} else {
			ckgIx = ix
			fp.Emit("reindex.ckg_align_loaded", "ckg_path", man.Sources.CKG.Path,
				"entries", ix.EntryCount(), "canonical_available", ix.CanonicalAvailable())
		}
	}

	// The aligned CKG graph's current coordinates (schema + digest), read once
	// for the schema-cascade (P2b-3) and graph-regeneration (P2b-1) checks.
	if man.Sources != nil && man.Sources.CKG != nil && man.Sources.CKG.Path != "" {
		if coords, cerr := ckgalign.ReadCoords(man.Sources.CKG.Path); cerr == nil {
			// P2b-3 — schema cascade: a CKG cache schema bump (recorded vs
			// current schema_version) cold-rebuilds the graph and can change
			// canonical_id semantics wholesale. Re-alignment is not enough;
			// refuse the partial reindex and direct the caller to a full
			// `ckv build` (reindex-migration-design §3.2).
			if rec := man.Sources.CKG.SchemaVersion; rec != "" && coords.SchemaVersion != "" && coords.SchemaVersion != rec {
				return nil, fmt.Errorf("%w: recorded=%s current=%s", ErrSchemaCascade, rec, coords.SchemaVersion)
			}

			// P2b-1 — graph regeneration: if the graph changed under the same
			// source commit (its logical digest differs from what this index
			// recorded), the git diff is empty but canonical_id may be stale
			// across the whole index. Re-align every chunk against the new
			// graph (no re-embed — only the join key changes) and record the
			// new digest so the next reindex is a no-op. Gated on
			// CanonicalAvailable so an unpopulated graph never wipes good join
			// keys (ADR-007).
			if ckgIx != nil && ckgIx.CanonicalAvailable() {
				recorded := man.Sources.CKG.GraphDigest
				current := coords.GraphDigest
				if recorded != "" && current != "" && recorded != current {
					n, rerr := realignAllCanonical(ctx, store, ckgIx)
					if rerr != nil {
						return nil, fmt.Errorf("reindex canonical realign: %w", rerr)
					}
					man.Sources.CKG.GraphDigest = current
					fp.Emit("reindex.canonical_realigned",
						"recorded_digest", recorded, "current_digest", current, "chunks_realigned", n)
					if o.ProgressOut != nil {
						fmt.Fprintf(o.ProgressOut, "ckv: ckg graph regenerated (digest %s→%s); re-aligned %d chunks\n",
							recorded, current, n)
					}
				}
			}
		}
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
		// File listed by diff but missing on disk: treat as a
		// late delete. Keeps reindex robust against races.
		if _, err := os.Stat(abs); errors.Is(err, os.ErrNotExist) {
			if err := store.DeleteByFile(ctx, rel); err != nil {
				return nil, fmt.Errorf("delete vanished %s: %w", rel, err)
			}
			result.FilesDeleted++
			continue
		}
		if discover.IsProbablyBinary(abs) {
			result.FilesSkipped++
			continue
		}
		// Resume: an interrupted prior run already re-embedded this file (same
		// content) toward this head — skip it.
		sha := fileSHA(abs)
		if ckpt.isDone(rel, sha) {
			result.FilesResumed++
			continue
		}
		// Always purge existing rows first so a file that lost symbols
		// doesn't leave orphan chunks.
		if err := store.DeleteByFile(ctx, rel); err != nil {
			return nil, fmt.Errorf("purge before reindex %s: %w", rel, err)
		}
		chunks, err := processFile(abs, rel, lang, newHead, parsers, cfg, chunker)
		if err != nil {
			return nil, err
		}
		if len(chunks) == 0 {
			continue
		}
		stampCanonicalIDs(ckgIx, chunks)
		pol.Apply(chunks)
		if err := embedAndUpsert(ctx, store, o.Embedder, chunks, o.BatchSize, nil, embedTextFn); err != nil {
			return nil, fmt.Errorf("embed/upsert %s: %w", rel, err)
		}
		if err := ckpt.markDone(rel, sha); err != nil {
			return nil, fmt.Errorf("reindex checkpoint %s: %w", rel, err)
		}
		if containsString(changes.added, rel) {
			result.FilesAdded++
		} else {
			result.FilesModified++
		}
		result.FilesProcessed++
		accumulateStats(&result.Chunks, chunks)
		languageCounts[lang] += chunk.Summarize(chunks).Total
	}

	// P3 — incremental PR ingest: when PR fetching is enabled, fetch merged
	// PRs after the recorded cutoff and index only the new ones (dedup by
	// number), then advance the cutoff. gh access is best-effort — a fetch
	// failure warns and leaves the PR corpus unchanged rather than failing the
	// code reindex. Runs before validation so the new PR chunks are counted.
	if o.PRFetch != nil {
		sinceNumber := 0
		fetchOpts := *o.PRFetch
		if man.Sources != nil && man.Sources.PRs != nil {
			sinceNumber = man.Sources.PRs.LastPRNumber
			if fetchOpts.Since.IsZero() && man.Sources.PRs.LastMergedAt != "" {
				if ts, e := time.Parse(time.RFC3339, man.Sources.PRs.LastMergedAt); e == nil {
					fetchOpts.Since = ts
				}
			}
		}
		metas, ferr := FetchMergedPRs(ctx, o.SrcRoot, fetchOpts)
		if ferr != nil {
			fmt.Fprintf(os.Stderr, "ckv: pr-fetch warning: %v\n", ferr)
		} else if cutoff, n, ierr := ingestPRs(ctx, store, o.Embedder, o.BatchSize, embedTextFn, metas, sinceNumber); ierr != nil {
			return nil, fmt.Errorf("reindex pr ingest: %w", ierr)
		} else if cutoff != nil {
			if man.Sources == nil {
				man.Sources = &manifest.Sources{}
			}
			if man.Sources.PRs == nil {
				man.Sources.PRs = cutoff
			} else {
				man.Sources.PRs.LastPRNumber = cutoff.LastPRNumber
				man.Sources.PRs.LastMergedAt = cutoff.LastMergedAt
				if man.Sources.PRs.Repo == "" {
					man.Sources.PRs.Repo = cutoff.Repo
				}
			}
			result.PRsIndexed = n
			if o.ProgressOut != nil {
				fmt.Fprintf(o.ProgressOut, "ckv: incremental PR ingest: %d new PR chunks (cutoff #%d)\n",
					n, cutoff.LastPRNumber)
			}
		}
	}

	// P3b — incremental flow-corpus re-index: when the recorded flow corpus
	// content hash differs from the file's current hash, replace the flow layer
	// wholesale (delete + reload) so corpus edits and removals are reflected.
	// Best-effort: a load failure warns and leaves the layer unchanged.
	if man.Sources != nil && man.Sources.Flow != nil && man.Sources.Flow.Path != "" {
		if current := contentHash(man.Sources.Flow.Path); current != "" && current != man.Sources.Flow.ContentHash {
			flowChunks, _, ferr := flowcorpus.Load(man.Sources.Flow.Path, filepath.Base(man.Sources.Flow.Path))
			if ferr != nil {
				fmt.Fprintf(os.Stderr, "ckv: flow re-index warning: %v\n", ferr)
			} else {
				if _, derr := store.DeleteFlowChunks(ctx); derr != nil {
					return nil, fmt.Errorf("reindex flow delete: %w", derr)
				}
				if len(flowChunks) > 0 {
					if err := embedAndUpsert(ctx, store, o.Embedder, flowChunks, o.BatchSize, nil, embedTextFn); err != nil {
						return nil, fmt.Errorf("reindex flow embed: %w", err)
					}
				}
				man.Sources.Flow.ContentHash = current
				result.FlowReindexed = len(flowChunks)
				if o.ProgressOut != nil {
					fmt.Fprintf(o.ProgressOut, "ckv: flow corpus changed → re-indexed %d flow chunks\n", len(flowChunks))
				}
			}
		}
	}

	// Step 3: refresh manifest. Even with zero changes we update
	// BuiltAt so freshness reflects the most recent verification pass.
	builtAt := o.Now().UTC().Format(time.RFC3339)
	result.BuiltAt = builtAt

	man.BuiltAt = builtAt
	man.SrcCommit = newHead
	man.IndexedHead = newHead
	man.Languages = languageCounts

	// P2b-2 — validation gate (reindex-migration-design §5.1): reconcile
	// ChunkCount to the authoritative COUNT(*) (replacing the drift-prone
	// arithmetic that mistook a re-embedded file's chunk total for a net
	// delta) and surface store integrity. Orphan chunks/vectors are a hard
	// integrity violation → fail loud. Low canonical coverage against a
	// populated graph is a warning: the graph, not this index, owns whether a
	// join key exists.
	val, verr := store.Validate(ctx)
	if verr != nil {
		return nil, fmt.Errorf("reindex validate: %w", verr)
	}
	if !val.OK() {
		return nil, fmt.Errorf("reindex integrity check failed: %d orphan chunks, %d orphan vectors",
			val.OrphanChunks, val.OrphanVectors)
	}
	man.ChunkCount = val.Chunks
	result.Validation = val
	fp.Emit("reindex.validated",
		"chunks", val.Chunks, "vectors", val.Vectors,
		"orphan_chunks", val.OrphanChunks, "orphan_vectors", val.OrphanVectors,
		"symbol_chunks", val.SymbolChunks, "canonical_rate", val.CanonicalRate())
	if ckgIx != nil && ckgIx.CanonicalAvailable() && val.SymbolChunks > 0 && val.CanonicalRate() < 0.90 {
		fmt.Fprintf(os.Stderr, "ckv: warning: canonical_id coverage %.1f%% below 90%% after reindex (ckg-aligned)\n",
			val.CanonicalRate()*100)
	}

	if err := store.SetManifest(ctx, map[string]string{
		"embedding_model":     o.Embedder.Name(),
		"embedding_dim":       fmt.Sprintf("%d", o.Embedder.Dimension()),
		"embedding_normalize": man.EmbeddingNormalize,
		"embedding_checksum":  man.EmbeddingChecksum,
		"indexed_head":        newHead,
		"built_at":            builtAt,
	}); err != nil {
		return nil, fmt.Errorf("write db manifest: %w", err)
	}
	if err := manifest.Save(o.OutDir, man); err != nil {
		return nil, fmt.Errorf("save manifest.json: %w", err)
	}
	// The manifest is now the durable record — the resume ledger is spent.
	ckpt.clear()

	doneReindex(
		"files_processed", result.FilesProcessed,
		"files_added", result.FilesAdded,
		"files_modified", result.FilesModified,
		"files_deleted", result.FilesDeleted,
		"files_skipped", result.FilesSkipped,
		"files_resumed", result.FilesResumed,
		"chunks_total", result.Chunks.Total,
		"new_head", newHead,
	)
	return result, nil
}

// stampCanonicalIDs copies canonical_id from the aligned ckg node onto each
// source chunk that has a real source span and no canonical_id yet. Mirrors
// the build-path alignment (internal/build/builder.go) so a reindex keeps the
// CKG↔CKV join key on re-embedded chunks. No-op when ix is nil (index built
// without --ckg, or the recorded graph was unloadable).
func stampCanonicalIDs(ix *ckgalign.Index, chunks []types.Chunk) {
	if ix == nil {
		return
	}
	for i := range chunks {
		if chunks[i].CanonicalID == "" && chunks[i].StartLine > 0 {
			if e := ix.LookupEntry(chunks[i].File, chunks[i].StartLine, chunks[i].EndLine); e != nil {
				chunks[i].CanonicalID = e.CanonicalID
			}
		}
	}
}

// realignAllCanonical re-derives every chunk's canonical_id from ix and writes
// back only the ones that changed to a new non-empty value. Used when the
// aligned CKG graph is regenerated (digest changed) under the same source
// commit: the git diff is empty but canonical_id may be stale across the whole
// index. Vectors are untouched — only the join key is refreshed. Never wipes a
// canonical to empty (a chunk the new graph no longer matches keeps its prior
// key rather than losing the join entirely).
func realignAllCanonical(ctx context.Context, store *sqlitevec.Store, ix *ckgalign.Index) (int, error) {
	files, err := store.AllFiles(ctx)
	if err != nil {
		return 0, err
	}
	updates := make(map[string]string)
	for _, f := range files {
		chunks, err := store.LookupByFileOrdered(ctx, f)
		if err != nil {
			return 0, err
		}
		for _, c := range chunks {
			if c.StartLine <= 0 {
				continue
			}
			if e := ix.LookupEntry(c.File, c.StartLine, c.EndLine); e != nil {
				if e.CanonicalID != "" && e.CanonicalID != c.CanonicalID {
					updates[c.ID] = e.CanonicalID
				}
			}
		}
	}
	return store.RealignCanonical(ctx, updates)
}

// ingestPRs indexes PR-corpus chunks for the merged PRs in metas whose number
// is newer than sinceNumber (dedup against the recorded cutoff), tags matching
// source chunks with PR breadcrumbs, and returns the advanced cutoff plus the
// number of PR chunks indexed. gh access lives in the caller (FetchMergedPRs);
// this pure ingest+dedup step is unit-testable without gh.
func ingestPRs(ctx context.Context, store *sqlitevec.Store, emb types.Embedder, batch int, embedTextFn func(types.Chunk) string, metas []prdoc.PRMeta, sinceNumber int) (*manifest.PRSource, int, error) {
	var fresh []prdoc.PRMeta
	for _, m := range metas {
		if m.PRNumber > sinceNumber {
			fresh = append(fresh, m)
		}
	}
	if len(fresh) == 0 {
		return nil, 0, nil
	}
	var prChunks []types.Chunk
	for _, m := range fresh {
		prChunks = append(prChunks, prdoc.Parse(m)...)
	}
	if len(prChunks) > 0 {
		if err := embedAndUpsert(ctx, store, emb, prChunks, batch, nil, embedTextFn); err != nil {
			return nil, 0, fmt.Errorf("embed/upsert PR chunks: %w", err)
		}
	}
	if filePRs := buildFilePRMap(fresh); len(filePRs) > 0 {
		if _, err := tagSourceChunksWithPRs(ctx, store, filePRs); err != nil {
			return nil, 0, fmt.Errorf("tag source chunks with PRs: %w", err)
		}
	}
	return prCutoff(fresh), len(prChunks), nil
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
