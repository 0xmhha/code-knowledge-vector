package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-vector/internal/freshness"
	"github.com/0xmhha/code-knowledge-vector/internal/manifest"
)

type freshnessOpts struct {
	out     string
	srcRoot string
	jsonOut bool
}

func newFreshnessCmd() *cobra.Command {
	opts := &freshnessOpts{}
	cmd := &cobra.Command{
		Use:   "freshness",
		Short: "Show index freshness (indexed_head vs git HEAD)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFreshness(opts)
		},
	}
	cmd.Flags().StringVar(&opts.out, "out", "./ckv-data", "data directory")
	cmd.Flags().StringVar(&opts.srcRoot, "src", "", "source root (default: manifest.src_root)")
	cmd.Flags().BoolVar(&opts.jsonOut, "json", false, "machine-readable output")
	return cmd
}

func runFreshness(opts *freshnessOpts) error {
	m, err := manifest.Load(opts.out)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}
	src := opts.srcRoot
	if src == "" {
		src = m.SrcRoot
	}
	report, err := freshness.Check(src, m.IndexedHead)
	if err != nil {
		return err
	}

	if opts.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	status := "fresh"
	if report.Stale {
		status = "STALE"
	}
	fmt.Printf("ckv: %s  indexed=%s  current=%s\n", status, short(report.IndexedHead), short(report.CurrentHead))
	if len(report.ChangedFiles) > 0 {
		fmt.Printf("ckv: %d changed file(s) since indexing:\n", len(report.ChangedFiles))
		for _, f := range report.ChangedFiles {
			fmt.Println("  -", f)
		}
	}
	for _, w := range report.Warnings {
		fmt.Fprintln(os.Stderr, "ckv: warning:", w)
	}
	return nil
}

func short(sha string) string {
	if len(sha) < 12 {
		return sha
	}
	return sha[:12]
}
