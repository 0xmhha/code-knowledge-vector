# SQLite Schema Migrations

Each migration is a single `.sql` file. They are applied in lexical order by
the migration runner (`migrate.go`) and recorded in the `schema_migrations`
table.

## Naming

```
NNN_short_description.sql
```

- `NNN` — zero-padded three-digit version. Strict lexical ordering.
- `short_description` — lowercase, underscore-separated. Brief enough to fit
  in a `git log` line.

Existing:

| Version | File | Purpose |
|---------|------|---------|
| 000 | `000_baseline.sql` | No-op baseline (records framework init) |

## Authoring rules

1. **Idempotent SQL only.** Use `CREATE TABLE IF NOT EXISTS`, `CREATE INDEX
   IF NOT EXISTS`. For `ALTER TABLE ADD COLUMN`, the runner wraps the
   statement in a check that compares to `PRAGMA table_info`.
2. **One concern per file.** A migration that adds a column + an index +
   backfills data should be split into multiple files.
3. **No data backfill in migrations.** Migrations alter schema only.
   Backfilling existing rows requires running `ckv build` or `ckv reindex`
   again with the new extractor.
4. **Never edit an applied migration.** The runner records a SHA-256 of
   each migration's content; a tampered migration is detected and
   refused. If a migration is wrong, write a new one that fixes it.
5. **No CGO-dependent SQL.** vec0 virtual table operations are fragile
   in migrations — keep vec0 changes minimal and well-tested.

## Workflow

To add a migration:

1. Pick the next version number (look at the highest existing file).
2. Create `migrations/NNN_xxx.sql` with idempotent SQL.
3. Test it via `go test ./internal/store/sqlitevec/...`.
4. Operators run `ckv migrate --out PATH` (or it runs automatically on
   `ckv build`/`reindex`/`query` via `Open()`).

## Backup

The runner creates `<dbpath>.bak.<unix-timestamp>` before applying
migrations unless `--no-backup` is set.
