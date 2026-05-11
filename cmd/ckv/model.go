package main

import (
	"errors"

	"github.com/spf13/cobra"
)

func newModelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage embedding models (fetch, verify, list)",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "fetch <name>",
			Short: "Download an embedding model into the local cache",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return errors.New("model fetch: not yet implemented (S1-W2: embedder)")
			},
		},
		&cobra.Command{
			Use:   "list",
			Short: "List cached embedding models",
			RunE: func(cmd *cobra.Command, args []string) error {
				return errors.New("model list: not yet implemented (S1-W2: embedder)")
			},
		},
	)

	return cmd
}
