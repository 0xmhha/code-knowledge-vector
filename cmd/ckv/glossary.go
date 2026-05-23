package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-vector/internal/glossary"
)

type glossaryOpts struct {
	src string // root of markdown tree to scan
	out string // output yaml path; empty -> stdout
}

func newGlossaryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "glossary",
		Short: "Tools for the vocabulary-bridge glossary",
		Long: `The glossary subcommand group manages the YAML file consumed by
'ckv query --alias' / MCP 'alias_path'. The vocabulary bridge widens
intent strings (typically Korean / domain-vague) with curated code
keywords before embedding so retrieval works against an English
codebase.`,
	}
	cmd.AddCommand(newGlossaryExtractCmd())
	return cmd
}

func newGlossaryExtractCmd() *cobra.Command {
	opts := &glossaryOpts{}
	cmd := &cobra.Command{
		Use:   "extract",
		Short: "Auto-extract a korean → english keyword glossary from markdown docs",
		Long: `Walks --src for *.md / *.markdown files and extracts korean → english
keyword aliases using two patterns:

  1. Markdown table rows:    | <한국어 키> | <영문 값> |
  2. Inline parentheticals:  <한국어 키> (<English gloss>)

Only entries whose key contains hangul are emitted (so english phrases
are never aliased to themselves). Values are deduplicated and sorted.

Output is the YAML shape 'ckv query --alias' consumes:

  aliases:
    "합의 알고리즘":
      - WBFT
      - Weemix Byzantine Fault Tolerance

The extractor is intentionally cautious — review the output before
shipping it into production query pipelines. v1 misses heading-level
mappings and structural code references; iterate by hand.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGlossaryExtract(opts)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.src, "src", "", "root directory to scan for markdown (required)")
	f.StringVar(&opts.out, "out", "", "output YAML path (empty = stdout)")
	_ = cmd.MarkFlagRequired("src")
	return cmd
}

func runGlossaryExtract(opts *glossaryOpts) error {
	if opts.src == "" {
		return fmt.Errorf("glossary extract: --src is required")
	}
	aliases, err := glossary.Extract(opts.src)
	if err != nil {
		return fmt.Errorf("glossary extract: %w", err)
	}
	if len(aliases) == 0 {
		fmt.Fprintln(os.Stderr, "ckv glossary: no aliases extracted (no markdown files with hangul keys found)")
		// Still write an empty 'aliases:' block so downstream pipelines
		// don't break on missing files.
	}
	if opts.out == "" {
		return glossary.WriteYAML(os.Stdout, aliases)
	}
	f, err := os.Create(opts.out)
	if err != nil {
		return fmt.Errorf("create %s: %w", opts.out, err)
	}
	defer f.Close()
	if err := glossary.WriteYAML(f, aliases); err != nil {
		return fmt.Errorf("write %s: %w", opts.out, err)
	}
	fmt.Fprintf(os.Stderr, "ckv glossary: wrote %d aliases to %s\n", len(aliases), opts.out)
	return nil
}
