package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-vector/internal/store/sqlitevec"
)

type migrateOpts struct {
	out      string
	dryRun   bool
	noBackup bool
	status   bool
	jsonOut  bool
}

func newMigrateCmd() *cobra.Command {
	opts := &migrateOpts{}
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending SQLite schema migrations",
		Long: `Applies any schema migrations not yet recorded in schema_migrations.

A backup of the DB file is written to "<dbpath>.bak.<unix-ts>" before
the first migration runs (disable with --no-backup).

Examples:
  ckv migrate --out ./ckv-data                 # apply pending migrations
  ckv migrate --out ./ckv-data --dry-run       # preview what would run
  ckv migrate --out ./ckv-data --status        # show applied vs pending
  ckv migrate --out ./ckv-data --no-backup     # skip the .bak copy`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrate(cmd.Context(), opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.out, "out", "./ckv-data", "data directory containing vector.db")
	f.BoolVar(&opts.dryRun, "dry-run", false, "report pending migrations without applying")
	f.BoolVar(&opts.noBackup, "no-backup", false, "skip the .bak file before applying")
	f.BoolVar(&opts.status, "status", false, "show applied vs pending and exit (no writes)")
	f.BoolVar(&opts.jsonOut, "json", false, "machine-readable output")

	return cmd
}

func runMigrate(ctx context.Context, opts *migrateOpts) error {
	if ctx == nil {
		ctx = context.Background()
	}
	dbPath := filepath.Join(opts.out, "vector.db")
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("vector.db not found at %s (run `ckv build` first)", dbPath)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	defer db.Close()

	runner := sqlitevec.NewMigrationRunner(db, dbPath,
		sqlitevec.WithBackup(!opts.noBackup),
	)

	if opts.status || opts.dryRun {
		return reportMigrationStatus(ctx, runner, opts)
	}

	if err := runner.Apply(ctx); err != nil {
		return err
	}

	status, err := runner.Status(ctx)
	if err != nil {
		return err
	}
	if opts.jsonOut {
		return json.NewEncoder(os.Stdout).Encode(migrationSummary(status))
	}
	fmt.Printf("ckv: migrate ok. current=%s applied=%d pending=%d\n",
		emptyDash(status.Current), len(status.Applied), len(status.Pending))
	return nil
}

func reportMigrationStatus(ctx context.Context, runner *sqlitevec.MigrationRunner, opts *migrateOpts) error {
	status, err := runner.Status(ctx)
	if err != nil {
		return err
	}
	if opts.jsonOut {
		return json.NewEncoder(os.Stdout).Encode(migrationSummary(status))
	}
	fmt.Printf("ckv: current=%s applied=%d pending=%d tampered=%d\n",
		emptyDash(status.Current), len(status.Applied), len(status.Pending), len(status.Tampered))
	if len(status.Applied) > 0 {
		fmt.Println("applied:")
		for _, m := range status.Applied {
			fmt.Printf("  %s  %s\n", m.Version, m.File)
		}
	}
	if len(status.Pending) > 0 {
		fmt.Println("pending:")
		for _, m := range status.Pending {
			fmt.Printf("  %s  %s\n", m.Version, m.File)
		}
	}
	if len(status.Tampered) > 0 {
		fmt.Fprintln(os.Stderr, "tampered (applied but source SHA differs):")
		for _, m := range status.Tampered {
			fmt.Fprintf(os.Stderr, "  %s  %s\n", m.Version, m.File)
		}
		return fmt.Errorf("refusing to migrate: tampered migrations present")
	}
	return nil
}

type migrationSummaryJSON struct {
	Current  string   `json:"current"`
	Applied  []string `json:"applied"`
	Pending  []string `json:"pending"`
	Tampered []string `json:"tampered,omitempty"`
}

func migrationSummary(s *sqlitevec.MigrationStatus) migrationSummaryJSON {
	names := func(ms []sqlitevec.Migration) []string {
		out := make([]string, 0, len(ms))
		for _, m := range ms {
			out = append(out, m.File)
		}
		return out
	}
	return migrationSummaryJSON{
		Current:  s.Current,
		Applied:  names(s.Applied),
		Pending:  names(s.Pending),
		Tampered: names(s.Tampered),
	}
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
