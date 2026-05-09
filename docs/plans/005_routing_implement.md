# 005: Routing — Implementation Guide

**Milestone:** `000_hilt_milestones.md`

## Section 1: Context Summary

Milestone 004 wired the Telegram long-polling loop, session manager, and config loader. Milestone 005 introduces message routing: parsing `/command` text into built-in actions (`/new`, `/sessions`) or a main-agent stub, while keeping the Telegram and session packages decoupled. The existing `internal/server` package (left empty after the webhook-to-polling pivot) is repurposed for this. The key dependency for the routing layer is `session.Manager`, which currently lacks a way to list archived sessions *within* the TTL window — this must be added first.

---

## Section 2: Implementation Checklist

### Phase 1: Sessions Package

- [x] **`internal/session/session.go: ListArchivedSessions()`**
  - Add `ListArchivedSessions(ctx context.Context, cutoff time.Time) ([]Session, error)` to the Manager type.
  - Query `SELECT id, title, created_at, last_activity, archived_at, total_input_tokens, total_output_tokens FROM sessions WHERE archived_at IS NOT NULL AND archived_at >= ? ORDER BY archived_at DESC`, passing `cutoff` formatted as UTC `2006-01-02 15:04:05` for consistent SQLite text comparison.
  - Build and return a `[]Session` slice, nil on no results.

- [x] **`internal/session/session_test.go` — Test `ListArchivedSessions`**
  - Subtest "no archived sessions returns empty slice": verify `[]Session{}` and nil error.
  - Subtest "filters outside TTL": archive one session with a recent timestamp and one with an old timestamp; pass a cutoff that excludes the old one; assert only the recent session is returned.
  - Subtest "orders most-recent-first": create two archived sessions with different `archived_at` values; assert the slice is ordered by `archived_at DESC`.

### Phase 2: Server/Router Package

- [x] **`internal/server/server.go` — New package comment`**
  - Replace the outdated webhook comment with: `// Package server routes incoming Telegram messages to built-in commands or the main agent stub.`

- [x] **`internal/server/server.go` — Define interfaces`**
  - Add exported `Messenger` interface with `SendMessage(ctx context.Context, chatID int64, text string) error`.
  - Add exported `SessionManager` interface with:
    - `GetActiveSession(ctx context.Context) (*session.Session, error)`
    - `ArchiveSession(ctx context.Context, id int64) error`
    - `CreateSession(ctx context.Context) (*session.Session, error)`
    - `ListArchivedSessions(ctx context.Context, cutoff time.Time) ([]session.Session, error)`
  - Note: references the concrete `session.Session` type rather than redefining it, keeping the interface small but typed.

- [x] **`internal/server/server.go: Router struct & NewRouter()`**
  - Define exported `Router` struct holding `messenger Messenger`, `sessions SessionManager`, `ttlDays int`, `logger *slog.Logger`.
  - Define exported `NewRouter(messenger Messenger, sessions SessionManager, ttlDays int, logger *slog.Logger) *Router` constructor.
  - Validate that `messenger` and `sessions` are non-nil; return error if nil (or panic with clear message; the spec does not dictate, but fail-fast is preferred).

- [x] **`internal/server/server.go: Router.HandleMessage()`**
  - Signature: `func (r *Router) HandleMessage(ctx context.Context, chatID int64, text string) error`.
  - Trim leading/trailing whitespace from `text`.
  - If trimmed text starts with `/`:
    - Extract the command name: substring after `/` up to first space or end of string.
    - Normalize to lower-case.
    - Switch on command name:
      - `"new"` → `r.handleNewCmd(ctx, chatID)`
      - `"sessions"` → `r.handleSessionsCmd(ctx, chatID)`
      - anything else (including empty string) → `r.sendUnknownCommand(ctx, chatID)`
    - Return nil (any internal errors are logged and sent inside handlers).
  - Else: `r.handleNormalMessage(ctx, chatID, trimmedText)`.

- [x] **`internal/server/server.go: Router.handleNewCmd()`**
  - Private method: `func (r *Router) handleNewCmd(ctx context.Context, chatID int64)`.
  - Call `r.sessions.GetActiveSession(ctx)`.
  - If session returns non-nil and no error: call `r.sessions.ArchiveSession(ctx, session.ID)`.
  - If `ArchiveSession` returns `internal.ErrNotFound` or `GetActiveSession` returns `internal.ErrNotFound`: log and proceed to create new session.
  - Call `r.sessions.CreateSession(ctx)`.
  - Call `r.messenger.SendMessage(ctx, chatID, fmt.Sprintf("New session started (ID: %d).", newSession.ID))`.
  - On any unexpected error: log it and send `"Something went wrong starting a new session."`.

- [x] **`internal/server/server.go: Router.handleSessionsCmd()`**
  - Private method: `func (r *Router) handleSessionsCmd(ctx context.Context, chatID int64)`.
  - Compute `cutoff := time.Now().AddDate(0, 0, -r.ttlDays)`.
  - Call `r.sessions.ListArchivedSessions(ctx, cutoff)`.
  - If len(sessions) == 0: `r.messenger.SendMessage(ctx, chatID, "No archived sessions found.")`.
  - Else: format as numbered list. Per line: `N. ID: {id}, Title: {title}, Created: {created_at.Format(time.RFC3339)}, Archived: {archived_at.Format(time.RFC3339)}`. Use `"Untitled"` if `Title` is nil or empty string. Join with `\n`.
  - On error: log and send `"Could not list archived sessions."`.

- [x] **`internal/server/server.go: Router.sendUnknownCommand()`**
  - Private method: `func (r *Router) sendUnknownCommand(ctx context.Context, chatID int64)`.
  - Send `r.messenger.SendMessage(ctx, chatID, "Command not found. Use /skills to see available commands.")`.

- [x] **`internal/server/server.go: Router.handleNormalMessage()`**
  - Private method: `func (r *Router) handleNormalMessage(ctx context.Context, chatID int64, text string)`.
  - Stub implementation: send `"Message received. The main agent is not yet online."` (or similar placeholder).

- [x] **`internal/server/server_test.go` — Router command parsing tests`**
  - Create test doubles: `fakeMessenger` (records SendMessage calls) and `fakeSessionManager` (returns canned sessions).
  - Subtest "routes /new to handleNewCmd": pass `"  /new  "` (with spaces), assert messenger receives confirmation with session ID.
  - Subtest "case insensitive": `"/NEW"` and `"/New"` both trigger `/new` behavior.
  - Subtest "bare slash is unknown command": `"/"` triggers unknown command reply.
  - Subtest "slash with space is unknown": `"/ unknown"` triggers unknown command (command name is empty after extraction).
  - Subtest "normal message routes to stub": `"hello world"` triggers stub reply.
  - Subtest "whitespace-only is normal message": `"   "` triggers stub reply.

- [x] **`internal/server/server_test.go` — Router /new tests`**
  - Subtest "archives existing and creates new": fake session manager returns active session with ID=1; after routing, assert ArchiveSession(ID=1) was called, then CreateSession was called, then messenger sent confirmation with new ID.
  - Subtest "no active session creates new defensively": fake session manager returns `ErrNotFound` from `GetActiveSession`; assert `CreateSession` is called, archive is skipped, confirmation is sent.
  - Subtest "archive error logs but still creates new": fake session manager returns an error from `ArchiveSession`; assert `CreateSession` still runs and user sees confirmation.

- [x] **`internal/server/server_test.go` — Router /sessions tests`**
  - Subtest "empty archived list": fake returns empty slice; assert messenger received `"No archived sessions found."`.
  - Subtest "lists sessions with nil titles": fake returns sessions with nil Title; assert message contains `"Untitled"`.
  - Subtest "formats numbered list": fake returns 2 archived sessions; assert message contains `"1."` and `"2."` with correct fields.

- [x] **`internal/server/server_test.go` — Router edge case tests`**
  - Subtest "messenger error is logged not propagated": make fake messenger return error; assert `HandleMessage` itself returns nil (not error) and does not panic.
  - Subtest "all handlers swallow their own errors": verify that even if both session manager and messenger fail, `HandleMessage` completes without returning an error.

### Phase 3: Main Wiring

- [x] **`cmd/hilt/main.go` — Wire Router into Telegram bot`**
  - Replace the existing `messageHandler` closure with `router := server.NewRouter(bot, mgr, cfg.SessionTTLDays, logger)`.
  - Pass `router.HandleMessage` as the handler to `bot.Start(ctx, router.HandleMessage)`.
  - Remove the dummy closure and its debug-only log line (or move it into `handleNormalMessage` as the placeholder).
  - Add `import "github.com/jrswab/hilt/internal/server"`.

### Phase 4: Documentation Cleanup

- [x] **`docs/plans/000_hilt_design.md` — Correct webhook references`
  - Find and remove or rewrite any sentences that describe `internal/server` as "HTTP server for webhook mode". Update to reflect long-polling and the routing role. (Minimal change: update the package description line in the server section.)
