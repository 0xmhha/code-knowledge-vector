package sqlitevec

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	_ "github.com/mattn/go-sqlite3"
)

// openTestDB returns a sql.DB at a temp file and a cleanup func. We use
// a real file (not :memory:) so backup tests have something to copy.
func openTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, dbPath
}

// fakeFS builds an in-memory fs.FS containing migrations/*.sql with the
// given (name, content) pairs.
func fakeFS(files map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for name, content := range files {
		out["migrations/"+name] = &fstest.MapFile{Data: []byte(content)}
	}
	return out
}

func TestMigrationRunner_FreshDB_StatusBeforeApply(t *testing.T) {
	db, dbPath := openTestDB(t)
	files := fakeFS(map[string]string{
		"000_baseline.sql": "SELECT 1;",
		"001_add_col.sql":  "CREATE TABLE t (x INT);",
	})
	r := NewMigrationRunner(db, dbPath,
		WithSource(files, "migrations"),
		WithBackup(false),
	)

	status, err := r.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Current != "" {
		t.Errorf("Current=%q, want empty", status.Current)
	}
	if len(status.Pending) != 2 {
		t.Fatalf("Pending=%d, want 2", len(status.Pending))
	}
	if len(status.Applied) != 0 {
		t.Errorf("Applied=%d, want 0", len(status.Applied))
	}
	if status.Pending[0].Version != "000" || status.Pending[1].Version != "001" {
		t.Errorf("Pending order wrong: %v", status.Pending)
	}
}

func TestMigrationRunner_Apply_PendingThenIdempotent(t *testing.T) {
	db, dbPath := openTestDB(t)
	files := fakeFS(map[string]string{
		"000_baseline.sql": "SELECT 1;",
		"001_add_t.sql":    "CREATE TABLE t (x INT);",
	})
	r := NewMigrationRunner(db, dbPath,
		WithSource(files, "migrations"),
		WithBackup(false),
	)

	if err := r.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Table created?
	if _, err := db.Exec(`INSERT INTO t (x) VALUES (1)`); err != nil {
		t.Fatalf("insert into t: %v", err)
	}

	// Status reflects all applied.
	status, _ := r.Status(context.Background())
	if status.Current != "001" {
		t.Errorf("Current=%q, want 001", status.Current)
	}
	if len(status.Pending) != 0 {
		t.Errorf("Pending=%d, want 0", len(status.Pending))
	}
	if len(status.Applied) != 2 {
		t.Errorf("Applied=%d, want 2", len(status.Applied))
	}

	// Re-apply is a no-op.
	if err := r.Apply(context.Background()); err != nil {
		t.Fatalf("re-Apply: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("schema_migrations count=%d, want 2", count)
	}
}

func TestMigrationRunner_FailedMigration_DoesNotPartiallyApply(t *testing.T) {
	db, dbPath := openTestDB(t)
	files := fakeFS(map[string]string{
		"000_baseline.sql": "SELECT 1;",
		"001_bad.sql":      "THIS IS NOT VALID SQL;",
		"002_good.sql":     "CREATE TABLE t (x INT);",
	})
	r := NewMigrationRunner(db, dbPath,
		WithSource(files, "migrations"),
		WithBackup(false),
	)

	err := r.Apply(context.Background())
	if err == nil {
		t.Fatal("Apply: expected error from 001_bad.sql, got nil")
	}
	if !strings.Contains(err.Error(), "001") {
		t.Errorf("error should reference failing migration 001: %v", err)
	}

	// 000 succeeded; 001 failed → 002 must not have run.
	status, _ := r.Status(context.Background())
	if status.Current != "000" {
		t.Errorf("Current=%q after partial failure, want 000", status.Current)
	}
	// `t` table must not exist (it was in 002).
	if _, err := db.Exec(`SELECT * FROM t`); err == nil {
		t.Error("table t should not exist after partial failure")
	}
}

func TestMigrationRunner_DryRun_ListsPendingWithoutApply(t *testing.T) {
	db, dbPath := openTestDB(t)
	files := fakeFS(map[string]string{
		"000_baseline.sql": "SELECT 1;",
		"001_add.sql":      "CREATE TABLE t (x INT);",
	})
	r := NewMigrationRunner(db, dbPath,
		WithSource(files, "migrations"),
		WithBackup(false),
	)

	pending, err := r.DryRun(context.Background())
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("pending=%d, want 2", len(pending))
	}

	// DB must remain untouched (no schema_migrations rows, no `t` table).
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("schema_migrations count after DryRun=%d, want 0", count)
	}
	if _, err := db.Exec(`SELECT * FROM t`); err == nil {
		t.Error("table t must not exist after DryRun")
	}
}

func TestMigrationRunner_DetectsTampering(t *testing.T) {
	db, dbPath := openTestDB(t)

	original := fakeFS(map[string]string{
		"000_baseline.sql": "SELECT 1;",
	})
	r1 := NewMigrationRunner(db, dbPath,
		WithSource(original, "migrations"),
		WithBackup(false),
	)
	if err := r1.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Re-open with a tampered migration of the same version.
	tampered := fakeFS(map[string]string{
		"000_baseline.sql": "SELECT 99;", // different SHA
	})
	r2 := NewMigrationRunner(db, dbPath,
		WithSource(tampered, "migrations"),
		WithBackup(false),
	)
	status, err := r2.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Tampered) != 1 {
		t.Errorf("Tampered=%d, want 1: %v", len(status.Tampered), status)
	}
	if err := r2.Apply(context.Background()); !errors.Is(err, ErrMigrationTampered) {
		t.Errorf("Apply with tampered should return ErrMigrationTampered, got %v", err)
	}
}

func TestMigrationRunner_AutoBackup_WritesBakFile(t *testing.T) {
	db, dbPath := openTestDB(t)

	// Make sure the DB file exists with some content so backup is meaningful.
	if _, err := db.Exec(`CREATE TABLE seed (x INT); INSERT INTO seed VALUES (1)`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	files := fakeFS(map[string]string{
		"000_baseline.sql": "SELECT 1;",
	})
	r := NewMigrationRunner(db, dbPath,
		WithSource(files, "migrations"),
		WithBackup(true),
	)
	if err := r.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	dir := filepath.Dir(dbPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "test.db.bak.") {
			found = true
			info, _ := e.Info()
			if info.Size() == 0 {
				t.Errorf("backup file %s is empty", e.Name())
			}
			break
		}
	}
	if !found {
		t.Errorf("no .bak file written; entries: %v", entries)
	}
}

func TestMigrationRunner_BackupDisabled_NoBakFile(t *testing.T) {
	db, dbPath := openTestDB(t)
	if _, err := db.Exec(`CREATE TABLE seed (x INT)`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	files := fakeFS(map[string]string{
		"000_baseline.sql": "SELECT 1;",
	})
	r := NewMigrationRunner(db, dbPath,
		WithSource(files, "migrations"),
		WithBackup(false),
	)
	if err := r.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	dir := filepath.Dir(dbPath)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "test.db.bak.") {
			t.Errorf("backup file %s written despite WithBackup(false)", e.Name())
		}
	}
}

func TestMigrationRunner_RejectsDuplicateVersion(t *testing.T) {
	db, dbPath := openTestDB(t)
	files := fakeFS(map[string]string{
		"000_baseline.sql":  "SELECT 1;",
		"000_duplicate.sql": "SELECT 2;",
	})
	r := NewMigrationRunner(db, dbPath,
		WithSource(files, "migrations"),
		WithBackup(false),
	)
	if _, err := r.Status(context.Background()); err == nil {
		t.Fatal("expected duplicate version error, got nil")
	}
}

func TestMigrationRunner_IgnoresNonMatchingFiles(t *testing.T) {
	db, dbPath := openTestDB(t)
	files := fakeFS(map[string]string{
		"000_baseline.sql": "SELECT 1;",
		"README.md":        "docs",
		"notes.txt":        "text",
		"01_short.sql":     "SELECT 2;", // version prefix wrong length
	})
	r := NewMigrationRunner(db, dbPath,
		WithSource(files, "migrations"),
		WithBackup(false),
	)
	status, err := r.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Pending) != 1 {
		t.Errorf("Pending=%d, want only the valid 000_baseline.sql", len(status.Pending))
	}
}

func TestOpen_AutoMigrate_AppliesAllPending(t *testing.T) {
	// Uses the real embedded migrations: at minimum 000_baseline must
	// auto-apply, and the latest version on disk must be reached.
	dbPath := filepath.Join(t.TempDir(), "v.db")
	s, err := Open(dbPath, testDim)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	runner := NewMigrationRunner(s.db, dbPath, WithBackup(false))
	status, err := runner.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Pending) != 0 {
		t.Errorf("Open should have applied all migrations, %d still pending: %v",
			len(status.Pending), status.Pending)
	}
	if len(status.Applied) == 0 {
		t.Fatal("at least 000_baseline should be applied after Open")
	}
}

func TestOpen_DisableAutoMigrate_ReturnsErrMigrationRequired(t *testing.T) {
	t.Setenv("CKV_DISABLE_AUTO_MIGRATE", "1")
	dbPath := filepath.Join(t.TempDir(), "v.db")
	_, err := Open(dbPath, testDim)
	if !errors.Is(err, ErrMigrationRequired) {
		t.Errorf("Open with CKV_DISABLE_AUTO_MIGRATE=1 should return ErrMigrationRequired, got %v", err)
	}
}
