package sqlitevec

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// embeddedMigrations is the default migration source. New .sql files
// dropped into ./migrations/ are compiled in automatically.
//
//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// ErrMigrationRequired is returned by Open when the DB has pending
// migrations. Callers can offer the user an actionable hint.
var ErrMigrationRequired = errors.New("sqlitevec: pending schema migrations (run `ckv migrate --out PATH`)")

// ErrMigrationTampered indicates an applied migration's source SQL has
// changed since it was first applied. Refuse to continue: silently
// re-running a different SQL would corrupt the DB invisibly.
var ErrMigrationTampered = errors.New("sqlitevec: applied migration content has changed (file edited after apply)")

// migrationName matches "NNN_description.sql". The numeric prefix is
// the migration version; the description is informational.
var migrationName = regexp.MustCompile(`^(\d{3})_([a-z0-9_]+)\.sql$`)

// Migration is one applied or pending schema change.
type Migration struct {
	Version string // "000", "001", ...
	Name    string // "baseline", "add_category_guidance", ...
	File    string // "000_baseline.sql"
	SQL     string // file content
	SHA256  string // hex sha256 of SQL
}

// MigrationStatus is what `ckv migrate --dry-run` (or Status()) prints.
type MigrationStatus struct {
	Current  string      // latest applied version, "" if none
	Applied  []Migration // already applied, in order
	Pending  []Migration // yet to apply, in order
	Tampered []Migration // applied versions whose SQL changed (block)
}

// MigrationRunner applies SQL migrations from an fs.FS to a database
// and tracks the applied versions in the schema_migrations table.
type MigrationRunner struct {
	db     *sql.DB
	dbPath string // for auto-backup. Empty disables backup.
	source fs.FS
	dir    string // subdirectory within source ("migrations")
	backup bool
}

// MigrationOption configures a MigrationRunner.
type MigrationOption func(*MigrationRunner)

// WithBackup enables (default) or disables auto-backup before Apply.
func WithBackup(enabled bool) MigrationOption {
	return func(r *MigrationRunner) { r.backup = enabled }
}

// WithSource overrides the embedded migration FS. Useful for tests.
// dir is the subdirectory containing *.sql files (e.g. "migrations").
func WithSource(source fs.FS, dir string) MigrationOption {
	return func(r *MigrationRunner) {
		r.source = source
		r.dir = dir
	}
}

// NewMigrationRunner constructs a runner. dbPath is used for auto-backup
// (pass "" to disable backup regardless of WithBackup).
func NewMigrationRunner(db *sql.DB, dbPath string, opts ...MigrationOption) *MigrationRunner {
	r := &MigrationRunner{
		db:     db,
		dbPath: dbPath,
		source: embeddedMigrations,
		dir:    "migrations",
		backup: true,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Status reports which migrations are applied vs pending. Read-only.
func (r *MigrationRunner) Status(ctx context.Context) (*MigrationStatus, error) {
	if err := r.ensureTable(ctx); err != nil {
		return nil, err
	}

	available, err := r.loadAvailable()
	if err != nil {
		return nil, err
	}

	applied, err := r.loadApplied(ctx)
	if err != nil {
		return nil, err
	}

	status := &MigrationStatus{}
	for _, m := range available {
		if rec, ok := applied[m.Version]; ok {
			if rec.SHA256 != m.SHA256 {
				status.Tampered = append(status.Tampered, m)
				continue
			}
			status.Applied = append(status.Applied, m)
			status.Current = m.Version
		} else {
			status.Pending = append(status.Pending, m)
		}
	}

	for version := range applied {
		// Applied but no longer in source — flag as tampered (removed file).
		if !containsVersion(available, version) {
			status.Tampered = append(status.Tampered, Migration{
				Version: version,
				Name:    applied[version].Name,
				File:    applied[version].File,
			})
		}
	}
	return status, nil
}

// Apply runs all pending migrations in version order. Each migration
// runs in its own transaction. On the first failure, the runner stops
// (later migrations are not attempted).
//
// If backup is enabled and dbPath is set, a copy of the DB file is
// written to "<dbPath>.bak.<unix-ts>" before any migration runs.
func (r *MigrationRunner) Apply(ctx context.Context) error {
	status, err := r.Status(ctx)
	if err != nil {
		return err
	}
	if len(status.Tampered) > 0 {
		return fmt.Errorf("%w: %s", ErrMigrationTampered, formatTampered(status.Tampered))
	}
	if len(status.Pending) == 0 {
		return nil
	}

	if r.backup && r.dbPath != "" {
		if err := r.backupDB(); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	}

	for _, m := range status.Pending {
		if err := r.applyOne(ctx, m); err != nil {
			return fmt.Errorf("migration %s (%s): %w", m.Version, m.Name, err)
		}
	}
	return nil
}

// DryRun returns the SQL that Apply would execute, without touching the DB.
func (r *MigrationRunner) DryRun(ctx context.Context) ([]Migration, error) {
	status, err := r.Status(ctx)
	if err != nil {
		return nil, err
	}
	if len(status.Tampered) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrMigrationTampered, formatTampered(status.Tampered))
	}
	return status.Pending, nil
}

// applyOne runs a single migration in a transaction + records it.
func (r *MigrationRunner) applyOne(ctx context.Context, m Migration) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, file, applied_at, sha256)
		 VALUES (?, ?, ?, ?, ?)`,
		m.Version, m.Name, m.File, time.Now().UTC().Format(time.RFC3339Nano), m.SHA256,
	); err != nil {
		return fmt.Errorf("record: %w", err)
	}
	return tx.Commit()
}

// ensureTable creates the tracking table on first use. Idempotent.
func (r *MigrationRunner) ensureTable(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		name       TEXT NOT NULL,
		file       TEXT NOT NULL,
		applied_at TEXT NOT NULL,
		sha256     TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	return nil
}

// loadAvailable reads all NNN_*.sql files from the source FS.
func (r *MigrationRunner) loadAvailable() ([]Migration, error) {
	entries, err := fs.ReadDir(r.source, r.dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", r.dir, err)
	}

	var out []Migration
	seen := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		match := migrationName.FindStringSubmatch(e.Name())
		if match == nil {
			continue
		}
		version, name := match[1], match[2]
		if prev, ok := seen[version]; ok {
			return nil, fmt.Errorf("duplicate migration version %s: %s and %s", version, prev, e.Name())
		}
		seen[version] = e.Name()

		path := r.dir + "/" + e.Name()
		raw, err := fs.ReadFile(r.source, path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		sum := sha256.Sum256(raw)
		out = append(out, Migration{
			Version: version,
			Name:    name,
			File:    e.Name(),
			SQL:     string(raw),
			SHA256:  hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// loadApplied returns version → record map from schema_migrations.
func (r *MigrationRunner) loadApplied(ctx context.Context) (map[string]Migration, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT version, name, file, sha256 FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	out := map[string]Migration{}
	for rows.Next() {
		var m Migration
		if err := rows.Scan(&m.Version, &m.Name, &m.File, &m.SHA256); err != nil {
			return nil, err
		}
		out[m.Version] = m
	}
	return out, rows.Err()
}

// backupDB copies the live SQLite file to "<dbPath>.bak.<ts>".
// SQLite WAL mode means we should run a checkpoint first so the .db
// file alone is a complete snapshot.
func (r *MigrationRunner) backupDB() error {
	if _, err := r.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("wal_checkpoint: %w", err)
	}

	src, err := os.Open(r.dbPath)
	if err != nil {
		return err
	}
	defer src.Close()

	bakPath := fmt.Sprintf("%s.bak.%d", r.dbPath, time.Now().Unix())
	dst, err := os.OpenFile(bakPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(bakPath)
		return err
	}
	if err := dst.Sync(); err != nil {
		return err
	}
	return nil
}

// BackupPath returns the conventional backup filename for the given
// DB path + timestamp. Useful for tests and CLI hint messages.
func BackupPath(dbPath string, ts time.Time) string {
	return fmt.Sprintf("%s.bak.%d", dbPath, ts.Unix())
}

// containsVersion is a small helper for "is this version still present
// in the source FS?". O(n) is fine — migrations count stays in 2-digits.
func containsVersion(ms []Migration, version string) bool {
	for _, m := range ms {
		if m.Version == version {
			return true
		}
	}
	return false
}

func formatTampered(ms []Migration) string {
	names := make([]string, 0, len(ms))
	for _, m := range ms {
		names = append(names, m.Version+" "+m.File)
	}
	return strings.Join(names, ", ")
}

// MigrationsDir returns the absolute path to the embedded migrations
// directory relative to the running binary. Used for `ckv migrate --help`
// to display where migrations live for inspection.
//
// Returns empty string when running from binary (embedded only).
func MigrationsDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(wd, "internal", "store", "sqlitevec", "migrations")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}
