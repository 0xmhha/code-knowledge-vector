// Package build is the indexer orchestrator: discover → parse → chunk →
// embed → store. Pulled out of cmd/ckv so it stays testable as a library
// (`ckv build` becomes a thin Cobra wrapper) and so the future CKS
// integration can call Run() directly.
package build

import (
	"context"
	"fmt"
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
	"github.com/0xmhha/code-knowledge-vector/internal/parse/typescript"
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

	if err := os.MkdirAll(o.OutDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir out: %w", err)
	}

	commit, _ := detectCommit(o.SrcRoot) // empty string when not a git repo; acceptable

	doneBuild := fp.Span("build", "src_root", o.SrcRoot, "out_dir", o.OutDir, "embedder", o.Embedder.Name())

	files, walkErrs, err := discover.Walk(o.SrcRoot, discover.Options{Extra: o.CKVIgnore})
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
		// "solidity":   solParser.New(), // W3-T10
	}

	totalStats := chunk.Stats{}
	languageCounts := make(map[string]int)
	indexedFiles := 0

	chunker := chunk.New(chunk.Options{
		MaxInputTokens: o.Embedder.MaxInputTokens(),
	})

	for _, f := range files {
		p, ok := parsers[f.Language]
		if !ok {
			continue // language parser not implemented yet (TS/Sol)
		}
		src, err := os.ReadFile(f.AbsPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ckv: read %s: %v\n", f.RelPath, err)
			continue
		}
		spans, err := p.Parse(f.RelPath, src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ckv: parse %s: %v\n", f.RelPath, err)
			continue
		}
		chunks := chunker.Chunk(chunk.Input{
			File:       f.RelPath,
			Language:   f.Language,
			CommitHash: commit,
			Source:     src,
			Spans:      spans,
		})
		if len(chunks) == 0 {
			continue
		}
		if err := embedAndUpsert(ctx, store, o.Embedder, chunks, o.BatchSize); err != nil {
			return nil, fmt.Errorf("embed/upsert %s: %w", f.RelPath, err)
		}
		indexedFiles++
		languageCounts[f.Language] += len(chunks)
		s := chunk.Summarize(chunks)
		totalStats.Total += s.Total
		totalStats.Symbol += s.Symbol
		totalStats.FileHeader += s.FileHeader
		totalStats.Truncated += s.Truncated
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
func embedAndUpsert(ctx context.Context, store *sqlitevec.Store, emb types.Embedder, chunks []types.Chunk, batch int) error {
	for i := 0; i < len(chunks); i += batch {
		end := min(i+batch, len(chunks))
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
