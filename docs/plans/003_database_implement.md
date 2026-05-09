# 003: Database — Implementation Guide

## Section 1: Context Summary

Milestone 003 introduces the SQLite-backed session persistence layer using `modernc.org/sqlite`. Hilt is single-user, so exactly one `sessions` row may be active (`archived_at IS NULL`) at any time. A companion `turns` table stores per-turn deltas but is not written to in this milestone — only its schema is created. The `Manager` type in `internal/session/` encapsulates all DB access: initializing the schema, managing the active session lifecycle, archiving, pruning by TTL, and token bookkeeping. All methods accept `context.Context` so cancellation propagates through SQLite queries. The entrypoint in `cmd/hilt/main.go` wires the manager into startup: load or create the active session, archive it if stale, then prune old archives.

---

## Section 2: Implementation Checklist

### Dependencies & Shared Utilities

- [x] **Add `modernc.org/sqlite` dependency**
  - Run `go get modernc.org/sqlite` in repo root. This updates `go.mod` and `go.sum`.

- [x] **Export `ExpandPath` from `internal/config/config.go`**
  - Rename `expandPath` → `ExpandPath` and export it. It is already unit-tested; no test changes needed. `internal/session` depends on this for `~` expansion.

### Schema & Types

- [x] **Define `Session` struct in `internal/session/session.go`**
  - Fields: `ID int64`, `Title *string`, `CreatedAt time.Time`, `LastActivity time.Time`, `ArchivedAt *time.Time`, `TotalInputTokens int64`, `TotalOutputTokens int64`. Include `sql.NullString`/`sql.NullTime` scanning helpers if needed, or scan in the query methods.

- [x] **Define `Manager` struct in `internal/session/session.go`**
  - Single field: `db *sql.DB`.

- [x] **Write schema SQL constant in `internal/session/session.go`**
  - Define a `const schemaSQL` or `var schemaStmts []string` containing the exact `CREATE TABLE IF NOT EXISTS` and `CREATE INDEX IF NOT EXISTS` statements from the spec (R1). No logic; just strings.

### Manager Lifecycle

- [x] **Implement `internal/session/session.go: NewManager(dbPath string) (*Manager, error)`**
  - Reject empty `dbPath`. Expand `~` via `config.ExpandPath`. Create parent directories with `0755`. Open SQLite database (`modernc.org/sqlite`). Execute schema statements. Return `*Manager` wrapping the open `*sql.DB`.

- **Test `NewManager`** in `internal/session/session_test.go`:
  - [x] Creates database file and tables on first run.
  - [x] Returns non-nil `*Manager` on valid path.
  - [x] Returns error when parent directory is not writable.
  - [x] Returns error when `HOME` is unset and path contains `~`.

### Session CRUD

- [x] **Implement `internal/session/session.go: (m *Manager) CreateSession(ctx context.Context) (*Session, error)`**
  - Insert row with defaults (`archived_at = NULL`). Return the created `*Session` including generated `ID` (use `sql.Result.LastInsertId`). `Title` must be nil.

- **Test `CreateSession`** in `internal/session/session_test.go`:
  - [x] Returns `ID > 0`.
  - [x] `ArchivedAt` is nil, `Title` is nil, `CreatedAt` and `LastActivity` are within last second.
  - [x] Multiple calls produce multiple rows (allows caller to archive first).

- [x] **Implement `internal/session/session.go: (m *Manager) GetActiveSession(ctx context.Context) (*Session, error)`**
  - Query `archived_at IS NULL`. If 0 rows → call `CreateSession` and return result. If 1 row → scan and return. If >1 row → archive all but the one with max `last_activity` (call `ArchiveSession`), then return the survivor.

- **Test `GetActiveSession`** in `internal/session/session_test.go`:
  - [x] Empty DB auto-creates a session and returns it.
  - [x] Single active session returned unchanged.
  - [x] Two active sessions: older one archived, newer returned.
  - [x] After `ArchiveSession`, subsequent call auto-creates new session.

- [x] **Implement `internal/session/session.go: (m *Manager) ArchiveSession(ctx context.Context, id int64) error`**
  - Query existing row for `id`. If not found → return `ErrNotFound`. If `archived_at` already non-nil → return `ErrSessionExpired`. Otherwise update `archived_at = CURRENT_TIMESTAMP`.

- **Test `ArchiveSession`** in `internal/session/session_test.go`:
  - [x] Success: row updated, `ArchivedAt` is non-nil.
  - [x] Missing ID returns `ErrNotFound`.
  - [x] Already-archived ID returns `ErrSessionExpired`.

### Maintenance Operations

- [x] **Implement `internal/session/session.go: (m *Manager) PruneOldSessions(ctx context.Context, cutoff time.Time) (int64, error)`**
  - Execute `DELETE FROM sessions WHERE archived_at IS NOT NULL AND archived_at < ?`. Return `RowsAffected()`. No error when 0 rows deleted.

- **Test `PruneOldSessions`** in `internal/session/session_test.go`:
  - [x] Deletes only archived sessions older than cutoff.
  - [x] Leaves active sessions untouched.
  - [x] Leaves recently archived sessions untouched.
  - [x] Verifies cascade: turns for deleted session are also removed (query `turns` table after prune).

- [x] **Implement `internal/session/session.go: (m *Manager) UpdateSessionActivity(ctx context.Context) error`**
  - Update `last_activity = CURRENT_TIMESTAMP` where `archived_at IS NULL`. If no rows affected → return `ErrNotFound`.

- **Test `UpdateSessionActivity`** in `internal/session/session_test.go`:
  - [x] Active session's `LastActivity` is updated.
  - [x] Returns `ErrNotFound` when no active session exists.

- [x] **Implement `internal/session/session.go: (m *Manager) UpdateSessionTokens(ctx context.Context, sessionID, inputTokens, outputTokens int64) error`**
  - Execute `UPDATE sessions SET total_input_tokens = total_input_tokens + ?, total_output_tokens = total_output_tokens + ? WHERE id = ?`. If no rows affected → return `ErrNotFound`. Allow negative values.

- **Test `UpdateSessionTokens`** in `internal/session/session_test.go`:
  - [x] Increments existing counters correctly.
  - [x] Returns `ErrNotFound` for non-existent session ID.
  - [x] Works on archived sessions (no restriction on `archived_at`).
  - [x] Negative values decrement counters correctly.

### Startup Wiring

- [x] **Wire session manager into `cmd/hilt/main.go: main()`**
  - After `config.Load` succeeds, resolve DB path: `filepath.Join(config.ExpandPath("~/.config/hilt"), "hilt.sqlite")`.
  - Call `session.NewManager(dbPath)`; on error log and exit 1.
  - Call `manager.GetActiveSession(ctx)`.
  - Check staleness: `time.Now().Sub(session.LastActivity) > time.Duration(cfg.SessionTTLDays) * 24 * time.Hour`. If stale:
    - `manager.ArchiveSession(ctx, session.ID)` → on error log and exit 1.
    - `manager.CreateSession(ctx)` → new active session.
  - Call `manager.PruneOldSessions(ctx, time.Now().AddDate(0, 0, -cfg.SessionTTLDays))`.
  - Log active session `ID` at info level.

- **Test startup flow** in `cmd/hilt/main_test.go`:
  - [x] First run: creates DB, creates active session, logs session ID, exits 0.
  - [x] Restart with existing active session: loads same session, does not archive.
  - [x] Stale session (manually set `last_activity` to past): archives old, creates new, prunes.
  - [x] Manager creation failure: exits non-zero.

### Error Handling & Helpers

- [x] **Implement `mapSQLError(err error) error` helper in `internal/session/session.go`**
  - If `err == sql.ErrNoRows` → return `internal.ErrNotFound`. Otherwise return `err` (or wrapped with fmt). Used by `ArchiveSession`, `UpdateSessionActivity`, `UpdateSessionTokens`.

- [x] **Add `Close() error` method on `Manager`**
  - Delegates to `m.db.Close()`. Call this from `main()` via `defer` after `NewManager` succeeds.
