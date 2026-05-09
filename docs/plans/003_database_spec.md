# 003: Database — SQLite schema and session lifecycle

## Section 1: Context & Constraints

### Milestone Entry
> **003: Database — SQLite schema and session lifecycle**
> - Initialize `hilt.sqlite` at `~/.config/hilt/`
> - Create `sessions` and `turns` tables (journal-style schema)
> - Load or create active session on startup
> - Archive stale sessions (TTL > `session_ttl_days`) on startup
> - Prune old archived sessions on startup
> - Implement `GetActiveSession`, `ArchiveSession`, `CreateSession`, `PruneOldSessions`

### Codebase State
Milestone 002 (Config) is complete. The repository now contains:
- `go.mod` with `github.com/BurntSushi/toml v1.6.0`
- `internal/config/config.go` fully implemented — `Config` struct, `Load()`, validation, auto-creation, path expansion, env var fallback
- `cmd/hilt/main.go` loads config and logs results; `cfg` is available for downstream use
- `internal/session/session.go` containing only a package documentation comment — no types, no functions, no schema
- `internal/memory/memory.go`, `internal/telegram/telegram.go`, and `internal/server/server.go` as empty stubs with package docs only

### Decisions Already Made
| Decision | Resolution | Rationale |
|----------|------------|-----------|
| SQLite driver | **`modernc.org/sqlite`** | Pure-Go, no CGO. Locked in. |
| Message persistence | **Journal-style `turns` table + `sessions` metadata table** | Delta-only new messages per turn. O(N) storage per session instead of O(N²). |
| Session structure | **Separate `sessions` and `turns` tables** | `sessions` holds metadata (timestamps, token totals). `turns` holds per-turn deltas. No `turns_json` blob on `sessions`. |
| Single active session | **Exactly one `archived_at IS NULL` row at any time** | Enforced by application logic, not a DB-level CHECK. |
| Session title | **Generated from first user message** | Truncated to ~40 runes. Not an LLM call. |
| First-run behavior | **Go binary auto-creates DB and schema** | Consistent with config auto-creation. No install script required. |
| TTL pruning | **Delete archived sessions older than `session_ttl_days`** | Runs on every startup. Non-blocking to user. |
| Stale session on startup | **Archive, then create fresh** | If last activity > `session_ttl_days`, archive the stale session and start new. |

### Approaches Ruled Out
| Approach | Why Rejected |
|----------|-------------|
| Store full `[]runner.Message` snapshot per turn (Option A) | Rejected in favor of delta-only (Option B) for O(N) storage. |
| `turns_json` blob on `sessions` table | Rejected. The design doc originally showed this, but the journal-style `turns` table was chosen for delta persistence and cleaner querying. |
| Multi-user schema | Rejected. Single-user is a permanent design constraint. |
| Background goroutine for maintenance on archive in MVP | Rejected. No LLM-driven maintenance pass in MVP. `/new` archives and starts fresh without maintenance. |

### Constraints & Assumptions
- **`modernc.org/sqlite`** is the only database dependency.
- **DB path:** `~/.config/hilt/hilt.sqlite` (resolved via `os.UserHomeDir()`).
- **Schema versioning:** No formal migration framework for MVP. Schema is created if tables do not exist via `CREATE TABLE IF NOT EXISTS`.
- **Single goroutine:** No concurrent DB access concerns for MVP. No connection pooling beyond `sql.DB` defaults.
- **`session_ttl_days`** comes from `config.Config.SessionTTLDays`.
- **Token fields are counters, not ground truth from Axe:** `total_input_tokens` and `total_output_tokens` on `sessions` are updated incrementally per turn (milestones 006–007). They are not re-derived from `turns`.
- **Turn schema includes `user_message`:** Stored as plain text for session title derivation and debugging. The `new_messages_json` field stores only the delta (messages added this turn).
- **Session resume:** The active session ID is loaded on startup. Full turn reconstruction happens in milestone 007 when assembling `[]runner.Message`.
- **Title is nullable/empty on creation:** `CreateSession` leaves `title` empty. Title is set on the first user message in milestone 006.

### Open Questions Resolved
1. **Package location?** → `internal/session/` owns schema, business logic, and the `Manager` type. No separate `internal/db/` package needed for MVP.
2. **`context.Context` on methods?** → Yes. All Manager methods accept `ctx context.Context` for testability and cancellation propagation.
3. **Turn insertion in this milestone?** → No. The `turns` table schema is created, but `InsertTurn` is deferred to milestone 006/007.
4. **Should `GetActiveSession` auto-create if none exists?** → Yes. Startup behavior requires an active session. If no active session exists (including after archiving a stale one), `GetActiveSession` creates a fresh empty session and returns it.
5. **What does "archive" mean?** → Set `archived_at = CURRENT_TIMESTAMP`. The session row remains in the DB but `archived_at IS NOT NULL` excludes it from active-session queries.

---

## Section 2: Requirements

### R1: SQLite Database Initialization
The `Manager` must ensure the SQLite database file and schema exist before any other operation.

**Behaviors:**
- On `Manager` creation (constructor/factory), open `~/.config/hilt/hilt.sqlite` with `modernc.org/sqlite`.
- If the database file does not exist, create it implicitly via the driver.
- Execute `CREATE TABLE IF NOT EXISTS` for `sessions` and `turns`.
- Return an error if the DB cannot be opened or schema cannot be created.

**Schema:**
```sql
CREATE TABLE IF NOT EXISTS sessions (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    title               TEXT,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_activity       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    archived_at         DATETIME,
    total_input_tokens  INTEGER NOT NULL DEFAULT 0,
    total_output_tokens INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_sessions_archived_at ON sessions(archived_at);

CREATE TABLE IF NOT EXISTS turns (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id          INTEGER NOT NULL,
    turn_number         INTEGER NOT NULL,
    timestamp           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_message        TEXT NOT NULL DEFAULT '',
    new_messages_json   TEXT NOT NULL DEFAULT '[]',
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_turns_session_id ON turns(session_id);
```

**Edge cases:**
- R1-E1: `~/.config/hilt/` directory does not exist — create it with `0755` (consistent with config package behavior).
- R1-E2: Database file exists but is not a valid SQLite file — return an error describing corruption.
- R1-E3: Schema creation partially fails (e.g., table exists but index fails) — return error; do not claim success.

---

### R2: Session Struct Definition
A `Session` struct must represent a row from the `sessions` table.

**Fields:**
- `ID` — `int64`
- `Title` — `*string` (nullable; nil means unset)
- `CreatedAt` — `time.Time`
- `LastActivity` — `time.Time`
- `ArchivedAt` — `*time.Time` (nil means active)
- `TotalInputTokens` — `int64`
- `TotalOutputTokens` — `int64`

**Edge cases:**
- R2-E1: `Title` may be nil or empty on a newly created session.
- R2-E2: `ArchivedAt` must be nil for an active session and non-nil for an archived session.

---

### R3: Manager Constructor
Expose a constructor function that returns a ready-to-use `*Manager`.

**Signature (illustrative):**
```go
func NewManager(dbPath string) (*Manager, error)
```

**Behaviors:**
- Expand `~` in `dbPath` using the same semantics as `internal/config.expandPath`.
- Create parent directories if missing.
- Open the database and create schema (R1).
- Return a `*Manager` that holds the open `*sql.DB`.

**Edge cases:**
- R3-E1: `dbPath` is empty — return an error (caller must resolve the default).
- R3-E2: Parent directory exists but is not writable — return a clear permission error.
- R3-E3: Home directory expansion fails (`HOME` unset) — return error.

---

### R4: GetActiveSession
Load the single active session, creating one if none exists.

**Behaviors:**
- Query `sessions` where `archived_at IS NULL`.
- If exactly one row exists, return it as `*Session`.
- If zero rows exist, call `CreateSession` and return the newly created session.
- If more than one row has `archived_at IS NULL`, archive all but the most recently active (max `last_activity`) and return that one.

**Edge cases:**
- R4-E1: Multiple active sessions detected (e.g., manual DB tampering) — archive all but the latest.
- R4-E2: DB is empty (first run) — auto-create and return a fresh session.

---

### R5: CreateSession
Insert a new empty active session.

**Behaviors:**
- Insert a row into `sessions` with `archived_at = NULL` and all other fields at defaults.
- Return the newly created `*Session` including its generated `ID`.
- Before inserting, if an active session already exists, it must remain untouched (callers are responsible for archiving first).

**Edge cases:**
- R5-E1: Insert fails due to DB constraint — return error.
- R5-E2: Title is not set by this method; it remains nil.

---

### R6: ArchiveSession
Mark a specific session as archived.

**Behaviors:**
- Update the row with the given `id`, setting `archived_at = CURRENT_TIMESTAMP`.
- Return `ErrNotFound` (from `internal/errors.go`) if no row with that `id` exists.
- Return `ErrSessionExpired` (from `internal/errors.go`) if the session is already archived (idempotent? no — return error to indicate no-op).

**Edge cases:**
- R6-E1: Session is already archived — return `ErrSessionExpired`.
- R6-E2: `id` does not exist — return `ErrNotFound`.

---

### R7: PruneOldSessions
Delete archived sessions (and their turns via `ON DELETE CASCADE`) older than a TTL.

**Behaviors:**
- Accept a `cutoff time.Time` parameter.
- Delete all rows from `sessions` where `archived_at IS NOT NULL AND archived_at < cutoff`.
- Return the number of sessions deleted.
- Return an error if the delete query fails.

**Edge cases:**
- R7-E1: No archived sessions exist — return 0 deleted, nil error.
- R7-E2: Cutoff is in the future — deletes nothing (valid, not an error).
- R7-E3: Cascading delete of turns fails (foreign key constraint without CASCADE) — return error.

---

### R8: UpdateSessionActivity
Refresh `last_activity` for the active session.

**Behaviors:**
- Update `last_activity = CURRENT_TIMESTAMP` for the active session (`archived_at IS NULL`).
- Return error if no active session exists.

**Edge cases:**
- R8-E1: No active session — return `ErrNotFound`.

---

### R9: UpdateSessionTokens
Increment the token counters for a session.

**Behaviors:**
- Accept `sessionID`, `inputTokens`, and `outputTokens`.
- Atomically increment `total_input_tokens += inputTokens` and `total_output_tokens += outputTokens` for the given session ID.
- Return `ErrNotFound` if the session ID does not exist.

**Edge cases:**
- R9-E1: Negative increments — allow negative values (for corrections) or reject? **Decision:** allow; treat as signed addition. The caller (milestone 007) is responsible for passing sensible values.
- R9-E2: Session is archived — still update it. Token tracking is independent of archival state.

---

### R10: Startup Lifecycle Orchestration
`cmd/hilt/main.go` must wire the session manager into startup.

**Behaviors (after config is loaded):**
1. Create the `Manager` with the resolved DB path (`~/.config/hilt/hilt.sqlite`).
2. Call `GetActiveSession` to load or create the active session.
3. Check if the active session's `last_activity` is older than `cfg.SessionTTLDays`.
4. If stale: call `ArchiveSession`, then call `CreateSession` to get a fresh active session.
5. Call `PruneOldSessions` with cutoff = `now - cfg.SessionTTLDays`.
6. Log the active session ID at `info` level.

**Edge cases:**
- R10-E1: Session manager creation fails — log error and exit with code 1.
- R10-E2: Stale session archive fails — log error and exit with code 1.
- R10-E3: `SessionTTLDays` is 0 or negative — this should have been caught by config validation in milestone 002, but if it reaches here, treat as "no pruning" (defensive).

---

### R11: Error Handling
All database errors must be handled consistently.

**Behaviors:**
- `sql.ErrNoRows` must be mapped to `internal.ErrNotFound` where appropriate.
- Schema creation errors, query execution errors, and transaction errors must be returned unwrapped or wrapped with context.
- The caller (`main.go`) logs errors and exits; the `session` package does not log.

**Edge cases:**
- R11-E1: SQLite is locked by another process — return the driver's error; `main.go` will handle it.

---

## Behaviors to Prioritize for Testing

1. **Schema creation on first run** — verify tables and indexes are created.
2. **GetActiveSession** — returns existing active session; creates one if none; handles multiple active sessions.
3. **CreateSession + ArchiveSession** — archive followed by create produces exactly one active session.
4. **PruneOldSessions** — deletes only archived sessions older than cutoff; respects cascade.
5. **UpdateSessionTokens** — increments values correctly; handles missing session.
6. **Startup flow in main** — manager creation, stale detection, archive+create, prune all wired together.
