package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/bgeonnx"
)

func newModelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage embedding models (fetch, verify, list)",
	}

	cmd.AddCommand(newModelFetchCmd(), newModelListCmd())
	return cmd
}

func newModelFetchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fetch <name>",
		Short: "Download an embedding model into the local cache",
		Long: `Downloads the ONNX model and tokenizer from HuggingFace into
~/.cache/ckv/models/<name>/. Existing files are skipped.

Supported models:
  bge-large-en-v1.5      1024-dim, ~1.34 GB (default)
  embeddinggemma-300m     768-dim, ~1.2 GB`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			fmt.Printf("ckv model fetch: %s\n", name)

			destDir, err := bgeonnx.FetchModel(name, globalFlags.modelDir, func(msg string) {
				fmt.Println(msg)
			})
			if err != nil {
				return fmt.Errorf("ckv model fetch: %w", err)
			}
			fmt.Printf("ckv model fetch: done → %s\n", destDir)
			return nil
		},
	}
}

func newModelListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List cached embedding models",
		RunE: func(cmd *cobra.Command, args []string) error {
			models := bgeonnx.RegisteredModels()

			fmt.Printf("%-25s %-6s %-8s %s\n", "MODEL", "DIM", "STATUS", "PATH")
			for _, cfg := range models {
				dir, err := cfg.DefaultModelDir()
				if err != nil {
					continue
				}

				status := "missing"
				for _, f := range cfg.FetchFiles() {
					if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
						status = "missing"
						break
					}
					status = "ready"
				}

				fmt.Printf("%-25s %-6d %-8s %s\n", cfg.Name, cfg.Dim, status, dir)
			}
			return nil
		},
	}
}
