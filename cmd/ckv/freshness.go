package main

import (
	"errors"

	"github.com/spf13/cobra"
)

func newFreshnessCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "freshness",
		Short: "Show index freshness (indexed_head vs git HEAD)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("freshness: not yet implemented (S1-W3: ops surface)")
		},
	}
	cmd.Flags().StringVar(&out, "out", "./ckv-data", "data directory")
	return cmd
}
