// Package build is the indexer orchestrator: discover → parse → chunk →
// embed → store. Pulled out of cmd/ckv so it stays testable as a library
// (`ckv build` becomes a thin Cobra wrapper) and so the future CKS
// integration can call Run() directly.
package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/chunk"
	"github.com/0xmhha/code-knowledge-vector/internal/ckgalign"
	"github.com/0xmhha/code-knowledge-vector/internal/convention"
	"github.com/0xmhha/code-knowledge-vector/internal/discover"
	"github.com/0xmhha/code-knowledge-vector/internal/filterlist"
	"github.com/0xmhha/code-knowledge-vector/internal/flowcorpus"
	"github.com/0xmhha/code-knowledge-vector/internal/footprint"
	"github.com/0xmhha/code-knowledge-vector/internal/manifest"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/prdoc"
	"github.com/0xmhha/code-knowledge-vector/internal/policy"
	"github.com/0xmhha/code-knowledge-vector/internal/projectcfg"
	"github.com/0xmhha/code-knowledge-vector/internal/store/sqlitevec"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// Options carry the CLI/programmatic configuration. SrcRoot and OutDir
// are required; everything else has a documented default.
type Options struct {
	SrcRoot   string
	OutDir    string
	Embedder  types.Embedder // required
	CKVIgnore []string       // extra ignore patterns from --ckvignore CLI flag
	BatchSize int            // embedding batch size; 0 → 32
	// Version is the ckv build version recorded in the manifest. The CLI sets
	// it from the ldflags-injected cmd/ckv.Version; empty falls back to "dev".
	Version   string
	Now       func() time.Time
	Footprint *footprint.Logger // optional; nil → no logging
	// ProgressOut receives human-readable per-file progress lines.
	// nil disables progress entirely (the library-mode default so
	// embedded callers don't get surprise stderr writes). The CLI
	// sets this to os.Stderr; tests can inject a bytes.Buffer.
	ProgressOut io.Writer

	// DisableContextualPrefix turns off the rule-based contextual prefix.
	// The default (zero value, prefix on) prepends a one-line
	// "language: X. file: Y. symbol: Z." sentence to each chunk's embed
	// text — improving recall@1 on natural-language queries at ~5%
	// throughput cost. Disable for A/B measurement against the raw-text
	// baseline. Chunk IDs and the stored Text are unaffected either way.
	DisableContextualPrefix bool

	// PR corpus. When non-nil, the build fetches merged PRs
	// via `gh` CLI and indexes their descriptions + commit messages
	// as additional chunks alongside the source code.
	PRFetch *PRFetchOptions

	// PolicyPath is the path to a policy yaml (e.g. policy/stablenet.yaml).
	// When set and the file exists, every emitted chunk is annotated with
	// Category + ModificationGuidance based on its path. Empty disables
	// classification — chunks ship with Category="" and Guidance=nil.
	PolicyPath string

	// DocsRoots are extra directories walked for markdown AFTER SrcRoot.
	// Files found here are tagged Category="domain" and cited by their
	// path relative to the docs root. Used to embed an out-of-tree curated
	// corpus (the cks domain-knowledge entries + authoritative docs) in
	// the same index. These roots are not git repos, so chunks carry no
	// commit hash.
	DocsRoots []string

	// FlowCorpus is the path to a curated flow corpus JSONL (corpus.jsonl).
	// When set, the builder parses it (internal/flowcorpus) into flow_step /
	// flow_spine / curated-invariant chunks and embeds them in the same index.
	// Step chunks cite their real file:line; flow/invariant chunks are fileless
	// and cite the corpus path, whose directory is added to the manifest's docs
	// roots so query-time citation enforcement resolves them. Empty disables it.
	FlowCorpus string

	// CKGPath is the path to a CKG data directory (containing graph.db).
	// When set, the builder loads an in-memory (file_path, start_line)
	// index from ckg and resolves each emitted source chunk's CanonicalID
	// via ckgalign.LookupEntry — the stable, import-path-qualified key cks
	// composer uses to disambiguate same-named symbols across packages.
	// Empty disables alignment (CanonicalID stays ""). Docs-corpus chunks
	// are NOT aligned (ckg has no node for curated markdown). Open failures
	// abort the build with a clear error rather than silently skipping alignment.
	CKGPath string

	// FilesFromPath is the path to a JSON file with include/exclude glob
	// patterns. When set, only files whose repo-relative path passes the
	// filterlist.FilterList.Allow check are sent to the embedder — for
	// ALL languages (Go, Solidity, TypeScript, JavaScript, Markdown).
	// Empty string (the default) disables the allowlist: all discovered
	// files are eligible as before. See internal/filterlist for the JSON
	// schema: {"include": [...globs...], "exclude": [...globs...]}.
	FilesFromPath string
}

// Result is what Run returns to the CLI for the summary log.
type Result struct {
	FilesIndexed int
	Chunks       chunk.Stats
	IndexedHead  string
	BuiltAt      string
	DBPath       string
}

const defaultBatch = 32

// Run executes the full indexing pipeline once. Idempotent: re-running
// against the same OutDir updates chunks in place (Upsert semantics).
//
// Pipeline:
//  1. Detect git HEAD of SrcRoot (for citation.commit_hash).
//  2. Walk SrcRoot via discover; skip non-source / oversized / ignored.
//  3. For each Go file: parse → chunk → embed → upsert.
//  4. Write manifest.json + DB-side manifest table.
func Run(ctx context.Context, o Options) (*Result, error) {
	if o.SrcRoot == "" || o.OutDir == "" {
		return nil, fmt.Errorf("build: SrcRoot and OutDir are required")
	}
	if o.Embedder == nil {
		return nil, fmt.Errorf("build: Embedder is required")
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

	// Memory guard: refuse to start the build when host RAM is below the
	// embedder's documented headroom. Returns nil for embedders without
	// an estimate (mock) or when CKV_MEM_GUARD=off. See memory.go.
	if err := preCheckMemory(o.Embedder, o.ProgressOut); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(o.OutDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir out: %w", err)
	}

	// Serialize concurrent writers on this dataset (§5.3). Released on return.
	lock, err := acquireDatasetLock(o.OutDir)
	if err != nil {
		return nil, err
	}
	defer lock.release()

	commit, _ := detectCommit(o.SrcRoot) // empty string when not a git repo; acceptable

	// Load per-project hook (<src>/ckv.yaml). Absence is OK — Load
	// returns a zero-value Config that the rest of the pipeline
	// treats as "use defaults".
	cfg, cfgErr := projectcfg.LoadOrDefault(o.SrcRoot)
	if cfgErr != nil {
		// Malformed config is fatal: silently ignoring would leak
		// surprises into indexing. Fail-fast with a clear message.
		return nil, fmt.Errorf("project config: %w", cfgErr)
	}
	fp.Emit("project_config.loaded",
		"path", o.SrcRoot,
		"has_ckv_yaml", len(cfg.Ignore)+len(cfg.Languages)+cfg.Chunking.FileHeaderLines > 0,
		"languages_filter", cfg.Languages,
		"extra_ignore_count", len(cfg.Ignore),
		"file_header_lines", cfg.Chunking.FileHeaderLines,
		"important_symbol_count", len(cfg.ImportantSymbols),
	)

	// embedderProvider is optional metadata: bgeonnx exposes Provider()
	// so we can record whether CoreML or CPU ran the workload; the mock
	// embedder doesn't implement it and falls through to "" — kept off
	// the structured log when empty so noise doesn't accumulate.
	embedderProvider := ""
	if p, ok := o.Embedder.(interface{ Provider() string }); ok {
		embedderProvider = p.Provider()
	}
	doneBuild := fp.Span("build",
		"src_root", o.SrcRoot,
		"out_dir", o.OutDir,
		"embedder", o.Embedder.Name(),
		"embedder_provider", embedderProvider,
	)

	// Merge config-supplied ignore patterns under CLI-supplied ones so
	// the CLI flag wins when there is overlap (CLI is more proximate).
	mergedIgnore := append([]string{}, cfg.Ignore...)
	mergedIgnore = append(mergedIgnore, o.CKVIgnore...)

	// --files-from: load the JSON allowlist. nil when path is empty
	// (Load returns nil, nil for empty path) — discover.Walk treats nil
	// AllowList as "no allowlist" and applies current behavior.
	allowList, err := filterlist.Load(o.FilesFromPath)
	if err != nil {
		return nil, fmt.Errorf("files-from: %w", err)
	}
	if allowList != nil {
		fp.Emit("filterlist.loaded",
			"path", o.FilesFromPath,
			"include_count", len(allowList.Include),
			"exclude_count", len(allowList.Exclude),
		)
	}

	// Resolve `build_roots` (ckv.yaml): turn the listed Go entry
	// packages into a file-set the walker uses as a filter. When
	// build_roots is empty, the filter stays nil and the walk yields
	// every Go file under srcRoot, just like before.
	var goBuildFiles map[string]struct{}
	if len(cfg.BuildRoots) > 0 {
		resolved, resolveErr := discover.ResolveGoBuildRoots(ctx, o.SrcRoot, cfg.BuildRoots, discover.DefaultGoListOptions())
		if resolveErr != nil {
			return nil, fmt.Errorf("build_roots: %w", resolveErr)
		}
		goBuildFiles = resolved
		fp.Emit("build_roots.resolved",
			"roots", cfg.BuildRoots,
			"file_count", len(resolved),
		)
	}
	files, walkErrs, err := discover.Walk(o.SrcRoot, discover.Options{
		Extra:        mergedIgnore,
		GoBuildFiles: goBuildFiles,
		AllowList:    allowList,
	})
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	for _, e := range walkErrs {
		fmt.Fprintf(os.Stderr, "ckv: walk warning: %v\n", e)
	}

	dbPath := filepath.Join(o.OutDir, "vector.db")
	store, err := sqlitevec.Open(dbPath, o.Embedder.Dimension())
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	// Treat manifest.json as the build's commit marker. From here on the
	// vector.db is mutated in place, so drop any prior manifest before the
	// first upsert: while the build runs (or if it fails partway) the index
	// reads as "not ready" (query.Open returns ErrNotFound → "run ckv build")
	// instead of pairing a stale manifest with a partially-written index. A
	// successful run re-commits it via manifest.Save below.
	if err := manifest.Remove(o.OutDir); err != nil {
		return nil, fmt.Errorf("clear stale manifest: %w", err)
	}

	parsers := newParsers()
	totalStats := chunk.Stats{}
	languageCounts := make(map[string]int)
	indexedFiles := 0
	chunker := newChunker(o.Embedder, cfg)
	embedTextFn := resolveEmbedTextFn(o.DisableContextualPrefix)

	// Policy is optional. Absence is silent so existing callers without
	// a policy file behave unchanged. Malformed yaml is fatal — same
	// rationale as projectcfg above (better to fail-fast than ship
	// chunks with surprise classifications).
	pol, err := policy.Load(o.PolicyPath)
	if err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}
	categoryCounts := map[string]int{}

	// Convention aggregator accumulates per-package AST statistics
	// across all observed Go files. We emit ChunkConvention chunks at
	// build end (one per package) so the agent can query "what idioms
	// does this package follow?" without rebuilding the AST.
	convAgg := convention.NewAggregator()

	// progress writes a throttled stderr-side status line so the user
	// can watch ckv build advance. Library callers leave ProgressOut
	// nil and get a silent no-op.
	prog := newProgress(o.ProgressOut, len(files), o.Now)

	// Load the ckg alignment index once, before the file loop. Open
	// failure aborts: callers who don't want alignment must leave
	// CKGPath empty rather than rely on silent skip.
	var ckgIx *ckgalign.Index
	if o.CKGPath != "" {
		var alignErr error
		ckgIx, alignErr = ckgalign.Load(o.CKGPath)
		if alignErr != nil {
			return nil, fmt.Errorf("ckg alignment: %w", alignErr)
		}
		fp.Emit("ckg_align.loaded",
			"ckg_path", o.CKGPath,
			"files_indexed", ckgIx.FileCount(),
			"entries", ckgIx.EntryCount(),
			"canonical_available", ckgIx.CanonicalAvailable(),
		)
		if !ckgIx.CanonicalAvailable() {
			// Column-present-but-empty (pre-1.19 ckg cache) or column absent:
			// chunks will inherit empty canonical_ids and cks FindByCanonicalID
			// joins won't resolve. Surface it loudly instead of silently
			// shipping an index that looks aligned but isn't (ADR-007).
			fmt.Fprintf(os.Stderr, "ckv: warning: ckg graph at %s has no populated canonical_id "+
				"(pre-1.19 cache or missing column); chunks inherit empty join keys "+
				"and cks FindByCanonicalID is unavailable\n", o.CKGPath)
			fp.Emit("ckg_align.canonical_unavailable", "ckg_path", o.CKGPath)
		}
	}

	// Memory watchdog runs while the file loop progresses and flips a
	// shared flag when free RAM drops below CKV_MEM_GUARD_LOW_MB.
	// embedAndUpsert reads the flag and halves its working batch on
	// the next iteration. nil sig means the guard is off — both calls
	// are safe via underPressure()'s nil receiver check.
	wdCtx, wdCancel := context.WithCancel(ctx)
	defer wdCancel()
	memSig := startMemWatchdog(wdCtx, o.ProgressOut)

	for i, f := range files {
		var perFileErr error
		func() {
			defer prog.Tick(i + 1)
			chunks, err := processFile(f.AbsPath, f.RelPath, f.Language, commit, parsers, cfg, chunker)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ckv: %v\n", err)
				return
			}
			if len(chunks) == 0 {
				return
			}
			// CKG alignment: stamp each chunk's CanonicalID by matching
			// (file_path, start_line) into the in-memory ckg index.
			// Skipped when ckgIx is nil (no --ckg) or when a chunk has
			// no source span (StartLine == 0, e.g. file-header chunks).
			// Lookup is called only when CanonicalID is still empty so a
			// future producer that already populated it (none today) is
			// respected.
			if ckgIx != nil {
				for i := range chunks {
					if chunks[i].CanonicalID == "" && chunks[i].StartLine > 0 {
						if e := ckgIx.LookupEntry(
							chunks[i].File, chunks[i].StartLine, chunks[i].EndLine,
						); e != nil {
							// copy ckg's canonical_id verbatim so a CKV chunk
							// inherits the exact key ckg resolves on.
							chunks[i].CanonicalID = e.CanonicalID
						}
					}
				}
			}
			// Convention stats: feed Go source through the aggregator
			// before chunks are upserted. Convention emission happens
			// at build end so per-package summaries see all files.
			if f.Language == "go" {
				if src, rerr := os.ReadFile(f.AbsPath); rerr == nil {
					if cerr := convAgg.ObserveFile(f.RelPath, src); cerr != nil {
						fmt.Fprintf(os.Stderr, "ckv: convention skipped %s: %v\n", f.RelPath, cerr)
					}
				}
			}
			for cat, n := range pol.Apply(chunks) {
				categoryCounts[cat] += n
			}
			if err := embedAndUpsert(ctx, store, o.Embedder, chunks, o.BatchSize, memSig, embedTextFn); err != nil {
				perFileErr = fmt.Errorf("embed/upsert %s: %w", f.RelPath, err)
				return
			}
			indexedFiles++
			languageCounts[f.Language] += len(chunks)
			accumulateStats(&totalStats, chunks)
		}()
		if perFileErr != nil {
			return nil, perFileErr
		}
	}

	// PR corpus: fetch merged PRs, index as chunks, and tag source
	// chunks with file→PR breadcrumbs. prSource captures the cutoff
	// (newest PR indexed) for the manifest ledger / incremental ingest.
	var prSource *manifest.PRSource
	if o.PRFetch != nil {
		prMetas, err := FetchMergedPRs(ctx, o.SrcRoot, *o.PRFetch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ckv: pr-fetch warning: %v\n", err)
		} else if len(prMetas) > 0 {
			prSource = prCutoff(prMetas)
			// Index PR description + commit message chunks.
			var prChunks []types.Chunk
			for _, meta := range prMetas {
				prChunks = append(prChunks, prdoc.Parse(meta)...)
			}
			if len(prChunks) > 0 {
				if err := embedAndUpsert(ctx, store, o.Embedder, prChunks, o.BatchSize, memSig, embedTextFn); err != nil {
					return nil, fmt.Errorf("embed/upsert PR chunks: %w", err)
				}
				s := chunk.Summarize(prChunks)
				totalStats.Total += s.Total
				totalStats.PRDoc += s.PRDoc
			}

			// Build file→PRRef map, then re-upsert source chunks
			// that have matching files so they carry PR breadcrumbs.
			filePRs := buildFilePRMap(prMetas)
			if len(filePRs) > 0 {
				tagged, err := tagSourceChunksWithPRs(ctx, store, filePRs)
				if err != nil {
					fmt.Fprintf(os.Stderr, "ckv: pr-tag warning: %v\n", err)
				} else if tagged > 0 {
					if o.ProgressOut != nil {
						fmt.Fprintf(o.ProgressOut, "ckv: tagged %d source chunks with PR breadcrumbs\n", tagged)
					}
				}
			}

			if o.ProgressOut != nil {
				fmt.Fprintf(o.ProgressOut, "ckv: indexed %d PRs → %d PR chunks\n", len(prMetas), len(prChunks))
			}
		}
	}

	// Emit convention chunks (one per Go package observed). Runs after
	// the file loop so every file has contributed to its package's stats.
	if convChunks := emitConventionChunks(convAgg, commit); len(convChunks) > 0 {
		// Convention chunks pass through the policy loader too so they
		// inherit the same category metadata as source chunks in their
		// package directory. Useful for narrow_candidates(category=...).
		pol.Apply(convChunks)
		if err := embedAndUpsert(ctx, store, o.Embedder, convChunks, o.BatchSize, memSig, embedTextFn); err != nil {
			return nil, fmt.Errorf("embed/upsert convention chunks: %w", err)
		}
		accumulateStats(&totalStats, convChunks)
	}

	// --docs: index additional markdown corpora living outside SrcRoot.
	// Not a git repo (commit=""); tagged Category="domain" so callers can
	// tell curated knowledge from code. processFile handles markdown the
	// same as in-tree docs.
	for _, docsRoot := range o.DocsRoots {
		docFiles, docWalkErrs, werr := discover.Walk(docsRoot, discover.Options{})
		if werr != nil {
			return nil, fmt.Errorf("walk docs %q: %w", docsRoot, werr)
		}
		for _, e := range docWalkErrs {
			fmt.Fprintf(os.Stderr, "ckv: docs walk warning: %v\n", e)
		}
		for _, f := range docFiles {
			chunks, perr := processFile(f.AbsPath, f.RelPath, f.Language, "", parsers, cfg, chunker)
			if perr != nil {
				fmt.Fprintf(os.Stderr, "ckv: %v\n", perr)
				continue
			}
			if len(chunks) == 0 {
				continue
			}
			// Do not call pol.Apply here: docs chunks always carry the
			// "domain" category regardless of PolicyPath (a source-tree
			// policy must not reclassify the curated corpus).
			for i := range chunks {
				chunks[i].Category = "domain"
			}
			categoryCounts["domain"] += len(chunks)
			if err := embedAndUpsert(ctx, store, o.Embedder, chunks, o.BatchSize, memSig, embedTextFn); err != nil {
				return nil, fmt.Errorf("embed/upsert docs %s: %w", f.RelPath, err)
			}
			indexedFiles++
			languageCounts[f.Language] += len(chunks)
			accumulateStats(&totalStats, chunks)
		}
	}

	// --flow-corpus: embed a curated flow corpus (corpus.jsonl). Step chunks
	// cite real code file:line (resolvable under SrcRoot); flow/invariant
	// chunks are fileless and cite the corpus file by basename, so the corpus
	// directory is added to the manifest docs roots below for citation
	// resolution. Embedding the prose is the human-wording → code-keyword bridge.
	manifestDocsRoots := append([]string{}, o.DocsRoots...)
	if o.FlowCorpus != "" {
		corpusRel := filepath.Base(o.FlowCorpus)
		flowChunks, fcStats, ferr := flowcorpus.Load(o.FlowCorpus, corpusRel)
		if ferr != nil {
			return nil, fmt.Errorf("flow corpus: %w", ferr)
		}
		for _, w := range fcStats.Warnings {
			fmt.Fprintf(os.Stderr, "ckv: flow-corpus warning: %s\n", w)
		}
		fp.Emit("flow_corpus.loaded",
			"path", o.FlowCorpus,
			"flows", fcStats.Flows, "steps", fcStats.Steps,
			"invariants", fcStats.Invariants, "edges_skipped", fcStats.Edges,
			"records_skipped", fcStats.Skipped,
		)
		if len(flowChunks) > 0 {
			if err := embedAndUpsert(ctx, store, o.Embedder, flowChunks, o.BatchSize, memSig, embedTextFn); err != nil {
				return nil, fmt.Errorf("embed/upsert flow corpus: %w", err)
			}
			categoryCounts["domain"] += len(flowChunks)
			indexedFiles++
			accumulateStats(&totalStats, flowChunks)
		}
		// Record the corpus dir as a docs root so fileless flow/invariant
		// citations (basename) resolve at query time (PR #10 path).
		if d := filepath.Dir(o.FlowCorpus); d != "" {
			manifestDocsRoots = append(manifestDocsRoots, d)
		}
	}

	builtAt := o.Now().UTC().Format(time.RFC3339)

	// Embedding-space identity is derived from the embedder (which sources
	// it from the model registry) rather than hardcoded, so swapping the
	// model changes the recorded normalize/checksum automatically and a
	// later Open with a different embedding space is rejected.
	embID := o.Embedder.Identity()
	embChecksum := embID.Checksum()

	// Persist identity into both the JSON sidecar and the DB manifest
	// table so /freshness can read either without coordinating opens.
	if err := store.SetManifest(ctx, map[string]string{
		"embedding_model":     o.Embedder.Name(),
		"embedding_dim":       fmt.Sprintf("%d", o.Embedder.Dimension()),
		"embedding_normalize": embID.Normalize,
		"embedding_checksum":  embChecksum,
		"indexed_head":        commit,
		"built_at":            builtAt,
	}); err != nil {
		return nil, fmt.Errorf("write db manifest: %w", err)
	}

	ckvVersion := o.Version
	if ckvVersion == "" {
		ckvVersion = "dev"
	}
	man := &manifest.Manifest{
		SchemaVersion:      manifest.SchemaVersionCurrent,
		CKVVersion:         ckvVersion,
		BuiltAt:            builtAt,
		SrcRoot:            absOrEmpty(o.SrcRoot),
		SrcCommit:          commit,
		IndexedHead:        commit,
		EmbeddingModel:     o.Embedder.Name(),
		EmbeddingDim:       o.Embedder.Dimension(),
		EmbeddingNormalize: embID.Normalize,
		EmbeddingChecksum:  embChecksum,
		ChunkCount:         totalStats.Total,
		Languages:          languageCounts,
		CKVIgnore:          o.CKVIgnore,
		DocsRoots:          absRoots(manifestDocsRoots),
	}
	man.Sources = buildSourcesLedger(o, commit, builtAt, prSource)
	if err := manifest.Save(o.OutDir, man); err != nil {
		return nil, fmt.Errorf("save manifest.json: %w", err)
	}

	doneBuild(
		"files_indexed", indexedFiles,
		"chunks_total", totalStats.Total,
		"chunks_symbol", totalStats.Symbol,
		"chunks_file_header", totalStats.FileHeader,
		"chunks_doc", totalStats.Doc,
		"chunks_truncated", totalStats.Truncated,
		"indexed_head", commit,
		"languages", languageCounts,
		"policy_loaded", o.PolicyPath != "",
		"category_counts", categoryCounts,
	)

	return &Result{
		FilesIndexed: indexedFiles,
		Chunks:       totalStats,
		IndexedHead:  commit,
		BuiltAt:      builtAt,
		DBPath:       dbPath,
	}, nil
}

// embedAndUpsert batches the chunks through the embedder and upserts
// the resulting (chunk, vector) pairs into the store. embedTextFn picks
// what gets sent to the embedder per chunk; callers pass chunk.BuildEmbedText
// for the rule-based contextual prefix or chunk.RawEmbedText for the
// raw-baseline behavior. The persisted chunk Text is unchanged either
// way so snippet display and chunk IDs stay stable.
//
// Adaptive batching: when sig reports memory pressure, the working
// batch halves (floor 1) before the next embed call. Recovery is
// implicit — effectiveBatch resets to `batch` on the next file via
// fresh embedAndUpsert call, so transient pressure doesn't permanently
// throttle the build. sig may be nil (guard disabled); underPressure()
// handles that.
func embedAndUpsert(ctx context.Context, store *sqlitevec.Store, emb types.Embedder, chunks []types.Chunk, batch int, sig *memSignal, embedTextFn func(types.Chunk) string) error {
	if embedTextFn == nil {
		embedTextFn = chunk.RawEmbedText
	}
	effectiveBatch := batch
	for i := 0; i < len(chunks); {
		if sig.underPressure() && effectiveBatch > 1 {
			effectiveBatch /= 2
			if effectiveBatch < 1 {
				effectiveBatch = 1
			}
		}
		end := min(i+effectiveBatch, len(chunks))
		texts := make([]string, end-i)
		for j, c := range chunks[i:end] {
			texts[j] = embedTextFn(c)
		}
		okChunks, vecs, err := embedResilient(ctx, emb, chunks[i:end], texts)
		if err != nil {
			return fmt.Errorf("embed batch: %w", err)
		}
		if len(okChunks) > 0 {
			if err := store.Upsert(ctx, okChunks, vecs); err != nil {
				return fmt.Errorf("upsert batch: %w", err)
			}
		}
		i = end
	}
	return nil
}

// embedResilient embeds texts for the given chunks and returns the chunks that
// embedded successfully, paired with their vectors. On an Embed error it
// bisects the batch and retries each half; a single chunk that still fails is
// skipped (warned) rather than aborting the whole build. This absorbs embedder
// backends that reject specific inputs — e.g. ollama's Qwen3 embedding endpoint
// crashing on a very large chunk (docs/qwen3-dimension-ab-2026-07-12.md).
// A cancelled or timed-out context is propagated, not skipped, so a genuine
// outage still fails loudly instead of silently dropping every chunk.
func embedResilient(ctx context.Context, emb types.Embedder, chunks []types.Chunk, texts []string) ([]types.Chunk, [][]float32, error) {
	vecs, err := emb.Embed(ctx, texts)
	if err == nil {
		return chunks, vecs, nil
	}
	if ctx.Err() != nil {
		return nil, nil, err
	}
	if len(chunks) <= 1 {
		if len(chunks) == 1 {
			// Distinguish a per-input rejection from a broken embedder: if a
			// tiny known-good probe also fails, the embedder is down — propagate
			// the original error rather than silently dropping every chunk to an
			// empty index.
			if _, perr := emb.Embed(ctx, []string{"ok"}); perr != nil {
				return nil, nil, err
			}
			fmt.Fprintf(os.Stderr, "ckv: warning: skipping chunk %s (%s): embedder rejected it: %v\n",
				chunks[0].ID, chunks[0].File, err)
		}
		return nil, nil, nil
	}
	mid := len(chunks) / 2
	lc, lv, lerr := embedResilient(ctx, emb, chunks[:mid], texts[:mid])
	if lerr != nil {
		return nil, nil, lerr
	}
	rc, rv, rerr := embedResilient(ctx, emb, chunks[mid:], texts[mid:])
	if rerr != nil {
		return nil, nil, rerr
	}
	outC := append(make([]types.Chunk, 0, len(lc)+len(rc)), lc...)
	outC = append(outC, rc...)
	outV := append(make([][]float32, 0, len(lv)+len(rv)), lv...)
	outV = append(outV, rv...)
	return outC, outV, nil
}

// detectCommit returns `git rev-parse HEAD` at srcRoot, or empty if the
// directory is not a git repo or git is unavailable.
func detectCommit(srcRoot string) (string, error) {
	cmd := exec.Command("git", "-C", srcRoot, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func absOrEmpty(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// absRoots returns the absolute form of each root, preserving order.
// nil in → nil out so the manifest field stays omitted when unused.
func absRoots(roots []string) []string {
	if len(roots) == 0 {
		return nil
	}
	out := make([]string, len(roots))
	for i, r := range roots {
		out[i] = absOrEmpty(r)
	}
	return out
}
