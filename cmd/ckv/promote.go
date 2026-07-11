package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
)

type promoteOpts struct {
	dataset string
	version string
}

func newPromoteCmd() *cobra.Command {
	opts := &promoteOpts{}

	cmd := &cobra.Command{
		Use:   "promote",
		Short: "Atomically point <dataset>/current at a built version directory",
		Long: `Repoints <dataset>/current at <dataset>/<version> after an integrity
gate (no orphan chunks/vectors). The swap is atomic (temp symlink + rename),
so a concurrent reader resolving 'current' sees either the old or the new
target, never a missing link.

This is the CKV-side atomic-promote primitive for blue-green serving; the
orchestrator (CKS) decides when to promote and owns version naming/retention.

Example:
  ckv promote --dataset ./knowledge-data/pr-77 --version 0bf2f4d1b-4be26516`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPromote(cmd.Context(), opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.dataset, "dataset", "", "dataset root directory that holds version dirs + the 'current' pointer")
	f.StringVar(&opts.version, "version", "", "version directory name under --dataset to promote")
	_ = cmd.MarkFlagRequired("dataset")
	_ = cmd.MarkFlagRequired("version")

	return cmd
}

func runPromote(ctx context.Context, opts *promoteOpts) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := build.PromoteVersion(opts.dataset, opts.version); err != nil {
		return err
	}
	fmt.Printf("ckv: promoted %s → %s/current\n", opts.version, opts.dataset)
	return nil
}
