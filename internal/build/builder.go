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

// Options carry the CLI/programmatic configuration. SrcRoot and OutDir
// are required; everything else has a documented default.
type Options struct {
	SrcRoot   string
	OutDir    string
	Embedder  types.Embedder     // required
	CKVIgnore []string           // extra ignore patterns from --ckvignore CLI flag
	BatchSize int                // embedding batch size; 0 → 32
	Now       func() time.Time
	Footprint *footprint.Logger // optional; nil → no logging
	// ProgressOut receives human-readable per-file progress lines.
	// nil disables progress entirely (the library-mode default so
	// embedded callers don't get surprise stderr writes). The CLI
	// sets this to os.Stderr; tests can inject a bytes.Buffer.
	ProgressOut io.Writer
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
//   1. Detect git HEAD of SrcRoot (for citation.commit_hash).
//   2. Walk SrcRoot via discover; skip non-source / oversized / ignored.
//   3. For each Go file: parse → chunk → embed → upsert.
//   4. Write manifest.json + DB-side manifest table.
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

	// Resolve `build_roots` (ckv.yaml FU-9): turn the listed Go entry
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

	parsers := map[string]cparse.Parser{
		"go":         golang.New(),
		"typescript": typescript.New(),
		"javascript": javascript.New(),
		"solidity":   solidity.New(),
		"markdown":   markdown.New(),
	}

	totalStats := chunk.Stats{}
	languageCounts := make(map[string]int)
	indexedFiles := 0

	chunkOpts := chunk.Options{
		MaxInputTokens: o.Embedder.MaxInputTokens(),
	}
	if cfg.Chunking.FileHeaderLines > 0 {
		chunkOpts.FileHeaderLines = cfg.Chunking.FileHeaderLines
	}
	chunker := chunk.New(chunkOpts)

	// progress writes a throttled stderr-side status line so the user
	// can watch ckv build advance. Library callers leave ProgressOut
	// nil and get a silent no-op.
	prog := newProgress(o.ProgressOut, len(files), o.Now)

	// Memory watchdog runs while the file loop progresses and flips a
	// shared flag when free RAM drops below CKV_MEM_GUARD_LOW_MB.
	// embedAndUpsert reads the flag and halves its working batch on
	// the next iteration. nil sig means the guard is off — both calls
	// are safe via underPressure()'s nil receiver check.
	wdCtx, wdCancel := context.WithCancel(ctx)
	defer wdCancel()
	memSig := startMemWatchdog(wdCtx, o.ProgressOut)

	for i, f := range files {
		// Closure scope so `defer prog.Tick(i+1)` fires once per
		// iteration regardless of which `continue` was taken — keeps
		// the progress denominator honest even when files are skipped
		// for parser/language/read/parse reasons.
		var perFileErr error
		func() {
			defer prog.Tick(i + 1)
			p, ok := parsers[f.Language]
			if !ok {
				return // language parser not implemented yet
			}
			if !cfg.LanguageAllowed(f.Language) {
				return // project ckv.yaml disabled this language
			}
			src, err := os.ReadFile(f.AbsPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ckv: read %s: %v\n", f.RelPath, err)
				return
			}
			spans, err := p.Parse(f.RelPath, src)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ckv: parse %s: %v\n", f.RelPath, err)
				return
			}
			chunks := chunker.Chunk(chunk.Input{
				File:       f.RelPath,
				Language:   f.Language,
				CommitHash: commit,
				Source:     src,
				Spans:      spans,
			})
			if len(chunks) == 0 {
				return
			}
			if err := embedAndUpsert(ctx, store, o.Embedder, chunks, o.BatchSize, memSig); err != nil {
				perFileErr = fmt.Errorf("embed/upsert %s: %w", f.RelPath, err)
				return
			}
			indexedFiles++
			languageCounts[f.Language] += len(chunks)
			s := chunk.Summarize(chunks)
			totalStats.Total += s.Total
			totalStats.Symbol += s.Symbol
			totalStats.FileHeader += s.FileHeader
			totalStats.Doc += s.Doc
			totalStats.Truncated += s.Truncated
		}()
		if perFileErr != nil {
			return nil, perFileErr
		}
	}

	builtAt := o.Now().UTC().Format(time.RFC3339)

	// Persist identity into both the JSON sidecar and the DB manifest
	// table so /freshness can read either without coordinating opens.
	if err := store.SetManifest(ctx, map[string]string{
		"embedding_model":     o.Embedder.Name(),
		"embedding_dim":       fmt.Sprintf("%d", o.Embedder.Dimension()),
		"embedding_normalize": "l2",
		"indexed_head":        commit,
		"built_at":            builtAt,
	}); err != nil {
		return nil, fmt.Errorf("write db manifest: %w", err)
	}

	man := &manifest.Manifest{
		SchemaVersion:      manifest.SchemaVersionCurrent,
		CKVVersion:         "dev",
		BuiltAt:            builtAt,
		SrcRoot:            absOrEmpty(o.SrcRoot),
		SrcCommit:          commit,
		IndexedHead:        commit,
		EmbeddingModel:     o.Embedder.Name(),
		EmbeddingDim:       o.Embedder.Dimension(),
		EmbeddingNormalize: "l2",
		ChunkCount:         totalStats.Total,
		Languages:          languageCounts,
		CKVIgnore:          o.CKVIgnore,
	}
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
	)

	return &Result{
		FilesIndexed: indexedFiles,
		Chunks:       totalStats,
		IndexedHead:  commit,
		BuiltAt:      builtAt,
		DBPath:       dbPath,
	}, nil
}

// embedAndUpsert batches the chunks' Text through the embedder and
// upserts the resulting (chunk, vector) pairs into the store.
//
// Adaptive batching: when sig reports memory pressure, the working
// batch halves (floor 1) before the next embed call. Recovery is
// implicit — effectiveBatch resets to `batch` on the next file via
// fresh embedAndUpsert call, so transient pressure doesn't permanently
// throttle the build. sig may be nil (guard disabled); underPressure()
// handles that.
func embedAndUpsert(ctx context.Context, store *sqlitevec.Store, emb types.Embedder, chunks []types.Chunk, batch int, sig *memSignal) error {
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
			texts[j] = c.Text
		}
		vecs, err := emb.Embed(ctx, texts)
		if err != nil {
			return fmt.Errorf("embed batch: %w", err)
		}
		if err := store.Upsert(ctx, chunks[i:end], vecs); err != nil {
			return fmt.Errorf("upsert batch: %w", err)
		}
		i = end
	}
	return nil
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
