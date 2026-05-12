package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
)

type buildOpts struct {
	src       string
	out       string
	ckgPath   string
	languages []string
	configPth string
	jsonOut   bool
}

func newBuildCmd() *cobra.Command {
	opts := &buildOpts{}

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the vector index from a code repository",
		Long: `Walks --src, parses supported languages (Go in W2; TS/Solidity W3),
chunks function/method/type spans, embeds each chunk, and persists to
<out>/vector.db + <out>/manifest.json.

Re-running on a populated --out updates chunks in place (Upsert).
Incremental indexing (--since) lands in S2.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(cmd.Context(), opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.src, "src", ".", "source repository path")
	f.StringVar(&opts.out, "out", "./ckv-data", "output data directory (vector.db, manifest.json)")
	f.StringVar(&opts.ckgPath, "ckg", "", "CKG data directory for symbol alignment (optional; W3)")
	f.StringSliceVar(&opts.languages, "lang", nil, "languages to index (default: auto-detect; supported: go)")
	f.StringVar(&opts.configPth, "config", "", "path to ckv.yaml (optional; W3)")
	f.BoolVar(&opts.jsonOut, "json", false, "machine-readable summary output")

	return cmd
}

func runBuild(ctx context.Context, opts *buildOpts) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// W2 ships with the mock embedder so the pipeline works without an
	// ONNX runtime. Swap to bgeonnx in W3 once the runtime decision lands.
	emb := mock.Default()

	fp := newFootprint(opts.out, "")
	defer fp.Close()

	res, err := build.Run(ctx, build.Options{
		SrcRoot:   opts.src,
		OutDir:    opts.out,
		Embedder:  emb,
		Footprint: fp,
	})
	if err != nil {
		return err
	}

	if opts.jsonOut {
		return json.NewEncoder(cmdOutput()).Encode(res)
	}
	fmt.Printf("ckv: indexed %d files → %d chunks (%d symbol, %d header, %d truncated)\n",
		res.FilesIndexed, res.Chunks.Total, res.Chunks.Symbol, res.Chunks.FileHeader, res.Chunks.Truncated)
	fmt.Printf("ckv: indexed_head=%s built_at=%s db=%s\n", res.IndexedHead, res.BuiltAt, res.DBPath)
	return nil
}

// cmdOutput is a hook so tests can capture writes. Today returns os.Stdout.
func cmdOutput() interface{ Write(p []byte) (int, error) } {
	return stdoutWriter{}
}

type stdoutWriter struct{}

func (stdoutWriter) Write(p []byte) (int, error) {
	return fmt.Print(string(p))
}
