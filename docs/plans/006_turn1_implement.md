# Implementation Guide: 006 — Turn 1: Context Assembly for First Message

## Section 1: Context Summary

**Associated milestone document:** `docs/plans/000_hilt_milestones.md`

This milestone transforms the `server.Router` message stub into a working end-to-end Turn 1 pipeline. When a user sends a normal (non-command) message to the Telegram bot and the active session has zero persisted turns, Hilt must assemble a hierarchical Markdown context from workspace files, invoke Axe via `runner.Options.Prompt` (adapted from the spec's `Messages` to match the actual Axe 1.10 API), persist the turn delta to SQLite, and send the LLM reply back to the user. The work is spread across four packages: `session` (new DB methods), `memory` (file I/O for workspace context), `mainagent` (new package for context assembly and Axe orchestration), and `server`/`cmd/hilt` (wiring).

---

## Section 2: Implementation Checklist

### Phase 1: Foundation — Database, File I/O, and Dependencies

- [x] **`go.mod`**: Add Axe dependency: `go get github.com/jrswab/axe/pkg/runner`.
  - Axe v1.10.0 installed. The API uses `runner.Run(ctx, opts)` returning `*Result`, with `Options.Prompt` for inline prompts.

- [x] **`internal/session/session.go`: `GetTurnCount()`** — Add method to count turns for a session.
  - Query: `SELECT COUNT(*) FROM turns WHERE session_id = ?`
  - Returns `(int, error)`.
  - Needed for: Turn 1 detection (zero turns = Turn 1).

- [x] **`internal/session/session.go`: `RecordTurn()`** — Add method to insert a turn row.
  - Signature: `func (m *Manager) RecordTurn(ctx context.Context, sessionID int64, turnNum int, userMessage string, newMessagesJSON string) error`
  - Execute: `INSERT INTO turns (session_id, turn_number, user_message, new_messages_json) VALUES (?, ?, ?, ?)`
  - Needed for: R013 (persist turn delta).

- [x] **`internal/session/session.go`: `SetSessionTitle()`** — Add method to update session title.
  - Signature: `func (m *Manager) SetSessionTitle(ctx context.Context, sessionID int64, title string) error`
  - Execute: `UPDATE sessions SET title = ? WHERE id = ?`
  - Returns `ErrNotFound` via `RowsAffected` check if the session does not exist.
  - Needed for: R015 (auto-generate title from first user message).

- [x] **`internal/session/session_test.go`: Test `GetTurnCount`/`RecordTurn`/`SetSessionTitle`** — Use `:memory:` DB via `session.NewManager(":memory:")`.
  - Test that `GetTurnCount` returns 0 for a new session.
  - Test that `RecordTurn` inserts a row and `GetTurnCount` returns 1.
  - Test that `SetSessionTitle` updates the title and returns `ErrNotFound` for a nonexistent session ID.

- [x] **`internal/memory/memory.go`: Implement `Reader` struct** — Replace the empty stub.
  - Add constructor: `func NewReader(workspace string) *Reader`
  - The `workspace` field stores the workspace path.

- [x] **`internal/memory/memory.go`: `ReadAGENTSMD()`** — Read `AGENTS.md` from the workspace root.
  - Returns `(string, error)` where `error` is only for I/O failures.
  - Returns `"", nil` if the file does not exist or is empty (per R004 and E001).

- [x] **`internal/memory/memory.go`: `ReadCriticalMD()`** — Read `memory/critical.md`.
  - Creates `memory/` subdirectory if it does not exist (E003).
  - Returns `"", nil` if the file does not exist or is empty (E002).

- [x] **`internal/memory/memory.go`: `ReadDailyNote(date string)`** — Read `memory/YYYY-MM-DD.md`.
  - `date` is expected in `YYYY-MM-DD` format.
  - Returns `("", nil)` for missing or empty file.
  - Returns `(content, nil)` if file exists.
  - Returns error only for I/O failures other than "not found".
  - Needed for: R005 and R006.

- [x] **`internal/memory/memory.go`: `EnsureDailyNoteSkeleton(date string)`** — Create today's daily note if missing.
  - Creates `memory/` subdirectory if needed (E003).
  - If `memory/YYYY-MM-DD.md` does not exist, write the skeleton template exactly as specified in R005.
  - If the file already exists, do nothing and return nil.
  - Needed for: R005.

- [x] **`internal/memory/memory_test.go`: Test `Reader` methods** — Use `t.TempDir()` as workspace.
  - Test `ReadAGENTSMD` returns content when file exists, `""` when missing.
  - Test `ReadCriticalMD` creates `memory/` subdirectory when missing.
  - Test `EnsureDailyNoteSkeleton` creates the file with exact template content.
  - Test `ReadDailyNote` returns content for an existing file.

---

### Phase 2: Core — `internal/mainagent/` Package

- [x] **`internal/mainagent/processor.go`: Define dependency interfaces** — These are the minimal interfaces the `Processor` depends on.
  - `ActiveSessionProvider`, `TurnStore`, `SessionStore`, `FileReader`, `Runner`, `Messenger`
  - `Runner` interface adapted to match actual Axe API: `Run(ctx context.Context, opts runner.Options) (*runner.Result, error)`

- [x] **`internal/mainagent/processor.go`: Implement context assembly** — Private helper function `assembleContext`.
  - Gets today's and yesterday's UTC dates.
  - Calls `EnsureDailyNoteSkeleton`, reads all memory files.
  - Builds Markdown string per R003–R007 with correct section ordering.
  - Omits yesterday's note subsection entirely if file does not exist (R006).

- [x] **`internal/mainagent/processor.go`: Define `Processor` struct and constructor** —
  - `NewProcessor` validates all dependencies non-nil; panics with clear message if nil.
  - Stores `agentsDir`, `model`, `logger` in struct.

- [x] **`internal/mainagent/processor.go`: Implement `ProcessTurn`** — The single public method.
  - Trims whitespace, gets active session, counts turns.
  - If turns > 0: sends "not yet online for turn 2+" message.
  - If turns == 0 (Turn 1):
    - Assembles context, builds `runner.Options` with `AgentName: "main"`, `AgentsDirs`, `Model`, `Prompt`.
    - Calls `runner.Run()`, handles errors with fallback messages.
    - Persists delta JSON (assistant message) to turns table.
    - Updates session tokens and activity.
    - Sets session title if empty (truncated to 40 runes).
    - Sends reply via Telegram; only Telegram dispatch errors propagate.

- [x] **`internal/mainagent/processor_test.go`: Test `assembleContext`** —
  - All sections present in correct order.
  - Missing AGENTS.md/critical.md still show headers.
  - Missing yesterday note omitted entirely.
  - Skeleton creation verified.
  - Whitespace trimming on user message.

- [x] **`internal/mainagent/processor_test.go`: Test `ProcessTurn` success path** —
  - Fake runner returns known result.
  - Assert: Telegram receives result.Content.
  - Assert: Turn recorded with correct turn_number=1, user_message, delta JSON.
  - Assert: Session tokens updated.
  - Assert: Session title set (if empty).

- [x] **`internal/mainagent/processor_test.go`: Test `ProcessTurn` error paths**
  - **Axe failure (E005):** Fake runner returns error. Fallback message sent, no turn recorded.
  - **Nil result:** Fallback message sent, no turn recorded.
  - **Turn 2+:** Stub message sent.
  - **DB failure after Axe success (E007):** Telegram reply STILL sent with result.Content.
  - **Messenger error:** Propagates to caller.

- [x] **`internal/mainagent/processor_test.go`: Test title truncation** —
  - 50 ASCII chars → 40-char title.
  - 30 ASCII chars → full string.
  - Multi-byte runes (emojis) → truncation at exactly 40 runes.

- [x] **`internal/mainagent/processor_test.go`: Test `NewProcessor` panics on nil dependencies** —
  - All six dependency nil cases verified.

---

### Phase 3: Integration — Wiring

- [x] **`internal/server/server.go`: Add `TurnProcessor` interface and `processor` field to `Router`** —
  - Defined small `TurnProcessor` interface to avoid circular imports.
  - `processor` field is nil-safe; nil means stub behavior.

- [x] **`internal/server/server.go`: Update `NewRouter`** — Accepts `processor TurnProcessor` parameter (can be nil).

- [x] **`internal/server/server.go`: Update `handleNormalMessage`** —
  - When processor is non-nil, calls `ProcessTurn` and handles errors.
  - When processor is nil, sends old stub message for backward compatibility.

- [x] **`internal/server/server_test.go`: Update all tests** —
  - `testRouter` helper updated to accept processor parameter.
  - All existing tests pass with nil processor.

- [x] **`cmd/hilt/main.go`: Wire dependencies** —
  - Creates `memory.NewReader(cfg.WorkspaceDir)`.
  - Creates `axeRunner` wrapper around `runner.Run`.
  - Creates `mainagent.NewProcessor` with all dependencies.
  - Passes processor to `server.NewRouter`.

- [x] **`cmd/hilt/main.go`: Import new packages** —
  - `github.com/jrswab/axe/pkg/runner`
  - `github.com/jrswab/hilt/internal/mainagent`
  - `github.com/jrswab/hilt/internal/memory`

---

### Phase 4: Validation

- [x] **Compile check:** `go build ./...` — no import cycles or compilation errors.
- [x] **Unit tests:** `go test ./...` — all new and existing tests pass (8 packages).
- [x] **Verify stub replacement:** `handleNormalMessage` calls processor when non-nil.
- [x] **Cross-reference with spec:** All R001–R018 and E001–E010 traceable to implemented code.
