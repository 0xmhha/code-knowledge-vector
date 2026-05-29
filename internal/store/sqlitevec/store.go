// Package sqlitevec is the default CKV VectorStore implementation —
// SQLite + the sqlite-vec extension's vec0 virtual table.
//
// Why SQLite? Life-cycle and idiom parity with CKG.
// Why CGO? vec0 ships as a C amalgamation; the cgo binding embeds it so
// there is no separate shared library to install on the user's machine.
package sqlitevec

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3" // sqlite3 driver

	"github.com/0xmhha/code-knowledge-vector/internal/policy"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// SchemaVersion is stamped into the manifest table on first init. Bump
// when the SQL schema changes in a way old binaries cannot read.
const SchemaVersion = "1.0"

// Store implements types.VectorStore over SQLite + vec0.
type Store struct {
	db  *sql.DB
	dim int
}

var registerOnce = func() {
	// sqlite-vec is registered globally for every future sqlite3 conn
	// in this process. Idempotent under repeated calls.
	sqlitevec.Auto()
}

func init() {
	registerOnce()
}

// Open opens (or creates) the DB at path. dim must match the stored
// dimension if the DB already has data. Pass dim from the Embedder's
// Dimension() — that gives us a single source of truth for what the
// embeddings look like.
//
// After schema init, pending migrations from the embedded migrations FS
// are applied automatically. Set CKV_DISABLE_AUTO_MIGRATE=1 to refuse
// rather than apply (Open then returns ErrMigrationRequired so callers
// can surface the recommended `ckv migrate` hint).
func Open(path string, dim int) (*Store, error) {
	if dim <= 0 {
		return nil, fmt.Errorf("sqlitevec: invalid dim %d", dim)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// vec0 + foreign key + WAL improves read concurrency vs the indexer
	// writing in the background. NORMAL synchronous is the WAL-friendly
	// default; FULL would force fsync on every commit.
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}

	s := &Store{db: db, dim: dim}
	if err := s.initSchema(dim); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.runPendingMigrations(path); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// runPendingMigrations applies migrations recorded in the embedded
// migrations FS that have not yet been recorded in schema_migrations.
// In auto mode (default) backups are taken and migrations applied. In
// manual mode (CKV_DISABLE_AUTO_MIGRATE=1) any pending migration causes
// Open to return ErrMigrationRequired so callers can ask the user to
// run `ckv migrate`.
func (s *Store) runPendingMigrations(dbPath string) error {
	autoOff := os.Getenv("CKV_DISABLE_AUTO_MIGRATE") == "1"
	runner := NewMigrationRunner(s.db, dbPath, WithBackup(!autoOff))
	if autoOff {
		status, err := runner.Status(context.Background())
		if err != nil {
			return err
		}
		if len(status.Pending) > 0 {
			return ErrMigrationRequired
		}
		return nil
	}
	return runner.Apply(context.Background())
}

// initSchema creates tables and the vec0 virtual table on first run.
// If the DB already exists, validates the on-disk dim against the
// caller's request — a mismatch means the user changed embedding
// models and must rebuild from scratch.
func (s *Store) initSchema(dim int) error {
	// manifest holds embedding_dim and other identity keys mirrored
	// from manifest.json. JSON file is authoritative for inspection;
	// the table is the authoritative gate inside DB-level transactions.
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS manifest (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}

	// Already-built DB: validate dim before touching vec0.
	storedDim, err := s.getStoredDim()
	if err != nil {
		return err
	}
	if storedDim != 0 && storedDim != dim {
		return fmt.Errorf("sqlitevec: embedding dim mismatch: db=%d, caller=%d (rebuild required)", storedDim, dim)
	}

	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS chunks (
		id              TEXT PRIMARY KEY,
		file            TEXT NOT NULL,
		start_line      INTEGER NOT NULL,
		end_line        INTEGER NOT NULL,
		language        TEXT NOT NULL,
		is_test         INTEGER NOT NULL DEFAULT 0,
		symbol_name     TEXT,
		symbol_kind     TEXT,
		chunk_kind      TEXT NOT NULL,
		commit_hash     TEXT NOT NULL,
		content_sha256  TEXT NOT NULL,
		ckg_node_id     TEXT,
		text            TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create chunks: %w", err)
	}

	// Migration: older indexes may lack the is_test column. SQLite
	// doesn't support ADD COLUMN IF NOT EXISTS, so we probe and ALTER
	// only on miss. Failure to migrate is fatal — silently running
	// without is_test would return wrong is_test=false for every chunk.
	if err := s.ensureColumn("chunks", "is_test", `ALTER TABLE chunks ADD COLUMN is_test INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("migrate chunks.is_test: %w", err)
	}
	if err := s.ensureColumn("chunks", "recent_prs", `ALTER TABLE chunks ADD COLUMN recent_prs TEXT DEFAULT ''`); err != nil {
		return fmt.Errorf("migrate chunks.recent_prs: %w", err)
	}

	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_chunks_file     ON chunks(file)`,
		`CREATE INDEX IF NOT EXISTS idx_chunks_lang     ON chunks(language)`,
		`CREATE INDEX IF NOT EXISTS idx_chunks_symbol   ON chunks(symbol_name)`,
		`CREATE INDEX IF NOT EXISTS idx_chunks_ckg_node ON chunks(ckg_node_id)`,
		`CREATE INDEX IF NOT EXISTS idx_chunks_is_test  ON chunks(is_test)`,
	} {
		if _, err := s.db.Exec(idx); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	// vec0 requires the dimension baked into DDL. We interpolate it
	// directly (it's our own int, not user input) — using ? would be
	// rejected by the virtual table parser.
	createVec := fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS chunk_vec USING vec0(
		chunk_id TEXT PRIMARY KEY,
		embedding FLOAT[%d]
	)`, dim)
	if _, err := s.db.Exec(createVec); err != nil {
		return fmt.Errorf("create chunk_vec: %w", err)
	}

	// Stamp dim + schema version on first init.
	if storedDim == 0 {
		if err := s.setManifestKVs(map[string]string{
			"schema_version": SchemaVersion,
			"embedding_dim":  fmt.Sprintf("%d", dim),
		}); err != nil {
			return err
		}
	}
	return nil
}

// ensureColumn runs alterDDL only if the column is missing. Idempotent:
// safe to call on every Open() so older databases get migrated in
// place without a separate "ckv migrate" step.
func (s *Store) ensureColumn(table, column, alterDDL string) error {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return fmt.Errorf("pragma table_info(%s): %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info: %w", err)
		}
		if name == column {
			return nil // column already present
		}
	}
	if _, err := s.db.Exec(alterDDL); err != nil {
		return fmt.Errorf("alter %s: %w", table, err)
	}
	return nil
}

func (s *Store) getStoredDim() (int, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM manifest WHERE key = 'embedding_dim'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read embedding_dim: %w", err)
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return 0, fmt.Errorf("parse embedding_dim %q: %w", v, err)
	}
	return n, nil
}

func (s *Store) setManifestKVs(kv map[string]string) error {
	stmt, err := s.db.Prepare(`INSERT INTO manifest (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for k, v := range kv {
		if _, err := stmt.Exec(k, v); err != nil {
			return fmt.Errorf("write manifest[%s]: %w", k, err)
		}
	}
	return nil
}

// SetManifest persists arbitrary identity keys (embedding_model, etc).
// Called by the indexer after a successful build so the DB carries its
// own copy of the manifest in addition to the JSON sidecar.
func (s *Store) SetManifest(_ context.Context, kv map[string]string) error {
	return s.setManifestKVs(kv)
}

// Upsert inserts or replaces chunks + their embeddings. Vectors and
// chunks are paired positionally; len mismatch is a programmer error.
func (s *Store) Upsert(ctx context.Context, chunks []types.Chunk, embeddings [][]float32) error {
	if len(chunks) != len(embeddings) {
		return fmt.Errorf("sqlitevec: chunks(%d) and embeddings(%d) length mismatch", len(chunks), len(embeddings))
	}
	if len(chunks) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	insChunk, err := tx.PrepareContext(ctx, `INSERT INTO chunks (
		id, file, start_line, end_line, language, is_test,
		symbol_name, symbol_kind, chunk_kind,
		commit_hash, content_sha256, ckg_node_id, recent_prs,
		category, guidance, text
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		file = excluded.file,
		start_line = excluded.start_line,
		end_line = excluded.end_line,
		language = excluded.language,
		is_test = excluded.is_test,
		symbol_name = excluded.symbol_name,
		symbol_kind = excluded.symbol_kind,
		chunk_kind = excluded.chunk_kind,
		commit_hash = excluded.commit_hash,
		content_sha256 = excluded.content_sha256,
		ckg_node_id = excluded.ckg_node_id,
		recent_prs = excluded.recent_prs,
		category = excluded.category,
		guidance = excluded.guidance,
		text = excluded.text`)
	if err != nil {
		return fmt.Errorf("prepare chunk insert: %w", err)
	}
	defer insChunk.Close()

	// vec0 does not support ON CONFLICT, so we DELETE+INSERT per chunk.
	delVec, err := tx.PrepareContext(ctx, `DELETE FROM chunk_vec WHERE chunk_id = ?`)
	if err != nil {
		return fmt.Errorf("prepare vec delete: %w", err)
	}
	defer delVec.Close()
	insVec, err := tx.PrepareContext(ctx, `INSERT INTO chunk_vec (chunk_id, embedding) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare vec insert: %w", err)
	}
	defer insVec.Close()

	for i, c := range chunks {
		if got := len(embeddings[i]); got != s.dim {
			return fmt.Errorf("sqlitevec: chunk %s embedding dim %d != store dim %d", c.ID, got, s.dim)
		}
		prJSON := marshalPRRefs(c.RecentPRs)
		guideJSON, err := policy.GuidanceJSON(c.Guidance)
		if err != nil {
			return fmt.Errorf("marshal guidance for %s: %w", c.ID, err)
		}
		if _, err := insChunk.ExecContext(ctx,
			c.ID, c.File, c.StartLine, c.EndLine, c.Language, boolToInt(c.IsTest),
			c.SymbolName, string(c.SymbolKind), string(c.ChunkKind),
			c.CommitHash, c.ContentSHA256, c.CKGNodeID, prJSON,
			c.Category, guideJSON, c.Text,
		); err != nil {
			return fmt.Errorf("insert chunk %s: %w", c.ID, err)
		}
		if _, err := delVec.ExecContext(ctx, c.ID); err != nil {
			return fmt.Errorf("delete vec %s: %w", c.ID, err)
		}
		blob, err := sqlitevec.SerializeFloat32(embeddings[i])
		if err != nil {
			return fmt.Errorf("serialize vec %s: %w", c.ID, err)
		}
		if _, err := insVec.ExecContext(ctx, c.ID, blob); err != nil {
			return fmt.Errorf("insert vec %s: %w", c.ID, err)
		}
	}
	return tx.Commit()
}

// DeleteByFile removes every chunk + vector belonging to the given path.
// Used by the incremental indexer and the rename safety path.
func (s *Store) DeleteByFile(ctx context.Context, path string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id FROM chunks WHERE file = ?`, path)
	if err != nil {
		return fmt.Errorf("select chunks by file: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()

	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM chunk_vec WHERE chunk_id = ?`, id); err != nil {
			return fmt.Errorf("delete vec %s: %w", id, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE file = ?`, path); err != nil {
		return fmt.Errorf("delete chunks by file: %w", err)
	}
	return tx.Commit()
}

// Search runs a vec0 KNN over query, then JOINs to chunks for metadata
// and applies the Filter as a post-step. We over-fetch by 3x when a
// filter is set so the post-filter has enough candidates to satisfy k.
func (s *Store) Search(ctx context.Context, query []float32, k int, filter types.Filter) ([]types.Hit, error) {
	if got := len(query); got != s.dim {
		return nil, fmt.Errorf("sqlitevec: query dim %d != store dim %d", got, s.dim)
	}
	if k <= 0 {
		return nil, nil
	}
	fetch := k
	if !filter.IsZero() {
		fetch = k * 3
	}
	blob, err := sqlitevec.SerializeFloat32(query)
	if err != nil {
		return nil, fmt.Errorf("serialize query: %w", err)
	}

	// vec0 KNN: WHERE embedding MATCH ? AND k = N. The result includes
	// a `distance` column. Join to chunks for metadata.
	stmt := `SELECT
			c.id, c.file, c.start_line, c.end_line, c.language, c.is_test,
			c.symbol_name, c.symbol_kind, c.chunk_kind,
			c.commit_hash, c.content_sha256, c.ckg_node_id, c.recent_prs,
			c.category, c.guidance, c.text,
			v.distance
		FROM chunk_vec v
		JOIN chunks c ON c.id = v.chunk_id
		WHERE v.embedding MATCH ? AND k = ?
		ORDER BY v.distance`
	rows, err := s.db.QueryContext(ctx, stmt, blob, fetch)
	if err != nil {
		return nil, fmt.Errorf("vec0 search: %w", err)
	}
	defer rows.Close()

	out := make([]types.Hit, 0, k)
	rank := 0
	for rows.Next() {
		var (
			c         types.Chunk
			isTest    int
			symKind   sql.NullString
			chKind    string
			ckgID     sql.NullString
			prJSON    sql.NullString
			catCol    sql.NullString
			guideJSON sql.NullString
			distance  float64
		)
		if err := rows.Scan(
			&c.ID, &c.File, &c.StartLine, &c.EndLine, &c.Language, &isTest,
			&c.SymbolName, &symKind, &chKind,
			&c.CommitHash, &c.ContentSHA256, &ckgID, &prJSON,
			&catCol, &guideJSON, &c.Text,
			&distance,
		); err != nil {
			return nil, fmt.Errorf("scan hit: %w", err)
		}
		c.IsTest = isTest != 0
		c.SymbolKind = types.SymbolKind(strings.TrimSpace(symKind.String))
		c.ChunkKind = types.ChunkKind(chKind)
		c.CKGNodeID = ckgID.String
		c.RecentPRs = unmarshalPRRefs(prJSON.String)
		c.Category = catCol.String
		guide, err := policy.GuidanceFromJSON(guideJSON.String)
		if err != nil {
			return nil, fmt.Errorf("scan guidance for %s: %w", c.ID, err)
		}
		c.Guidance = guide

		if !filter.Matches(c) {
			continue
		}
		rank++
		out = append(out, types.Hit{
			Chunk: c,
			Score: types.HitScore{
				Normalized:     normalize(distance),
				VectorDistance: distance,
				VectorRank:     rank,
			},
		})
		if len(out) >= k {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// boolToInt converts a Go bool to SQLite's 0/1 integer convention.
// SQLite has no native BOOLEAN type — integer columns + 0/1 values are
// the universal pattern.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// normalize maps cosine distance [0,2] to a similarity score [0,1]
// where higher is better.
func normalize(distance float64) float64 {
	s := 1 - distance/2
	if s < 0 {
		return 0
	}
	if s > 1 {
		return 1
	}
	return s
}

// Stats returns aggregate index counts + manifest identity. The
// manifest fields come from the in-DB manifest table written by
// SetManifest at build time.
func (s *Store) Stats(ctx context.Context) (types.Stats, error) {
	var st types.Stats
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&st.ChunkCount); err != nil {
		return st, fmt.Errorf("count chunks: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM manifest`)
	if err != nil {
		return st, fmt.Errorf("read manifest table: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return st, err
		}
		switch k {
		case "embedding_model":
			st.EmbeddingModel = v
		case "embedding_dim":
			fmt.Sscanf(v, "%d", &st.EmbeddingDim)
		case "indexed_head":
			st.IndexedHead = v
		case "built_at":
			st.BuiltAt = v
		case "schema_version":
			st.SchemaVersion = v
		}
	}
	return st, rows.Err()
}

// Close releases the underlying *sql.DB handle. Idempotent.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// Dim reports the embedding dimension this store was opened with.
func (s *Store) Dim() int { return s.dim }

// UpdateRecentPRs sets the recent_prs JSON column for source chunks
// whose file matches entries in filePRs. Only updates rows where
// recent_prs is currently empty (avoids overwriting prior tagging).
// Returns the total number of rows updated.
func (s *Store) UpdateRecentPRs(ctx context.Context, filePRs map[string][]types.PRRef) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `UPDATE chunks SET recent_prs = ? WHERE file = ? AND (recent_prs IS NULL OR recent_prs = '')`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	tagged := 0
	for file, refs := range filePRs {
		prJSON := marshalPRRefs(refs)
		res, err := stmt.ExecContext(ctx, prJSON, file)
		if err != nil {
			continue
		}
		n, _ := res.RowsAffected()
		tagged += int(n)
	}
	return tagged, tx.Commit()
}

// LookupPRsByFile returns the merged PRRef lists for all chunks matching
// the given file path. Deduplicates by PR number.
func (s *Store) LookupPRsByFile(ctx context.Context, file string) ([]types.PRRef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT recent_prs FROM chunks WHERE file = ? AND recent_prs != '' AND recent_prs IS NOT NULL`, file)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := map[int]bool{}
	var out []types.PRRef
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		refs := unmarshalPRRefs(raw)
		for _, r := range refs {
			if !seen[r.Number] {
				seen[r.Number] = true
				out = append(out, r)
			}
		}
	}
	return out, rows.Err()
}

func marshalPRRefs(refs []types.PRRef) string {
	if len(refs) == 0 {
		return ""
	}
	b, _ := json.Marshal(refs)
	return string(b)
}

func unmarshalPRRefs(s string) []types.PRRef {
	if s == "" {
		return nil
	}
	var refs []types.PRRef
	_ = json.Unmarshal([]byte(s), &refs)
	return refs
}
