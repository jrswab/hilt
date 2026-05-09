# Hilt Milestones

This document tracks the tracer-bullet milestones for building Hilt, a lightweight LLM assistant triggered from Telegram. All milestones represent vertical, end-to-end slices of functionality.

---

## Section 1: Research Findings

### Codebase State
The project is a **greenfield Go repo** at `/Users/jaronswab/go/src/github.com/jrswab/hilt`. It currently contains only documentation — no Go source files, no `go.mod`, no package structure.

Existing files:
- `docs/plans/000_hilt_design.md` — comprehensive architecture design (~600 lines)
- `docs/plans/memory-system-walkthrough.md` — Memory Palace design guide (~800 lines)
- `README.md`, `LICENSE`, `.gitignore`

### Axe `pkg/runner` API (confirmed working)
- `runner.Options.Messages` accepts `[]runner.Message` for pre-built conversation history. When non-empty, Axe skips stdin/prompt resolution entirely.
- `runner.Result.Messages` returns the **full** conversation history (including assistant responses and tool results) **only** when `opts.Messages` was initially non-empty.
- Message validation is strict: user messages can't have tool calls/results; assistant messages can't have tool results; tool messages must have empty content and reference valid preceding assistant call IDs.
- This means Hilt can seed history → Axe extends it → Hilt persists the extended result. No translation layer needed for messages.

### Decisions Made & Why

| Decision | Resolution | Rationale |
|----------|------------|-----------|
| User model | **Single-user permanently** | Personal assistant, not SaaS. Keeps schema, concurrency, and auth simple. |
| Development style | **Vertical traces** | Build MVP as a complete end-to-end slice, then layer features. Prevents horizontal half-finished work. |
| Message persistence | **Hybrid (C) + Delta-only (B)** | Journal-style `turns` table + separate `sessions` metadata table. Each turn row stores `new_messages_json` (delta) — only the messages added in that turn. Reconstruct by iterating all turns in order. O(N) storage. |
| Install script | **Shell script (`install.sh`) built early and updated incrementally** | Trace-bullet ethos: enables real manual testing at every milestone instead of retrofitting setup into an existing system. |
| First-run bootstrap | **Go binary auto-creates everything** | Server auto-creates `~/.config/hilt/` directories, SQLite DB, default `main.toml`, stub `AGENTS.md`. `config.toml` is auto-generated with empty values; user fills it in (or uses env vars). |
| Telegram integration | **`gotgbot/v2`** | Modern, actively maintained, better `context.Context` support than `telegram-bot-api/v5`. |
| HTTP vs library | **Telegram library** | Full-featured, handles edge cases. Zero risk for our simple needs (text, voice, file download). |
| Package layout | **Minimal MVP** | Create only packages needed for MVP: `config`, `telegram`, `session`, `memory`, `server`. Defer `workflow`, `voice`, `agents` (will be thin wrappers during MVP). |
| Token estimation | **Reactive only** | No `tiktoken-go`. Use `result.InputTokens` from Axe as ground truth. |
| Error handling | **Map Axe typed errors to Telegram replies** | `runner.ConfigError`, `runner.BudgetExceededError`, `runner.RuntimeError` → user-friendly strings. |

### Approaches Considered and Rejected

| Approach | Why Rejected |
|----------|--------------|
| Multi-user schema | Single-user is a permanent design constraint. Multi-user would explode complexity (per-user memory isolation, sessions, workflows). |
| Store full `[]runner.Message` snapshot per turn (Option A) | Chosen delta-only (Option B) for O(N) storage instead of O(N²). |
| Raw HTTP for Telegram | Rejected in favor of `gotgbot/v2` for cleaner code and better context support. |
| Full package layout on day one | Premature abstraction. Only create packages the MVP needs. |
| Implement install.sh before Go binary | Install.sh depends on finalized paths and schemas. Go binary defines the truth. |

### Constraints & Assumptions
- **Axe Issue #82 is merged** — `runner.Options.Messages` works for multi-turn history.
- **SQLite via `modernc.org/sqlite`** — pure-Go, no CGO.
- **Single goroutine** — single-user means no concurrent message processing needed for MVP.
- **No voice transcription in MVP** — whisper.cpp + ffmpeg deferred to post-MVP.
- **No workflows in MVP** — `/flow` command deferred.
- **No memory maintenance background agent in MVP** — `/new` archives the session but does not run an LLM-driven maintenance pass. Just archive and start fresh.
- **No token budget warnings in MVP** — can be added later; just log for now.
- **Axe's config and env var convention** — Hilt does not manage API keys. User sets `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, etc. via their preferred secret manager.

### Open Questions — All Answered
1. **Single-user permanent?** → Yes.
2. **MVP scope?** → Telegram loop + config + SQLite sessions + turn history + main agent routing + `/new` + `/sessions`.
3. **Vertical traces?** → Yes. Build end-to-end, not layer by layer.
4. **Message persistence format?** → Hybrid journal table. Delta-only new messages per turn.
5. **Auto-generation of files?** → Go binary creates everything on first run.
6. **Install script timing?** → Build early, update incrementally.
7. **Telegram library?** → `gotgbot/v2`.
8. **Package layout?** → Minimal MVP: `config`, `telegram`, `session`, `memory`, `server`.
9. **Go binary or install.sh first?** → Build install.sh early, update incrementally.

---

## Section 2: Milestones

All milestones are **tracer bullets** — vertical, end-to-end slices that prove the full stack works.

---

- [x] **001: Bootstrap — Initialize Go module and core packages**
  - Initialize `go.mod` with correct Axe and SQLite dependencies
  - Create `cmd/hilt/main.go` entry point
  - Scaffold `internal/config/`, `internal/db/`, `internal/session/`, `internal/memory/`, `internal/telegram/`, `internal/server/` packages
  - Create empty `errors.go` in `internal/` for shared error types
  - Establish logging (`log/slog`) and basic CLI flag/env var wiring

- [x] **002: Config — Load and validate `config.toml` on startup**
  - Define `Config` struct matching design doc schema (bot token, workspace dir, model, context window, etc.)
  - Parse `~/.config/hilt/config.toml` via `BurntSushi/toml`
  - Fall back to environment variables for bot token and API keys
  - Validate required fields; fail fast with clear errors
  - Create `agents/main.toml` and `AGENTS.md` stubs if missing

- [x] **003: Database — SQLite schema and session lifecycle**
  - Initialize `hilt.sqlite` at `~/.config/hilt/`
  - Create `sessions` and `turns` tables (journal-style schema)
  - Load or create active session on startup
  - Archive stale sessions (TTL > `session_ttl_days`) on startup
  - Prune old archived sessions on startup
  - Implement `GetActiveSession`, `ArchiveSession`, `CreateSession`, `PruneOldSessions`

- [x] **004: Telegram — Long-polling loop with `gotgbot/v2`**
  - Initialize bot with token from config
  - Start polling dispatcher with 60s timeout
  - Handle text messages: route to message processor
  - Handle unsupported message types: auto-reply "Hilt only processes text and voice messages."
  - Implement `SendMessage` wrapper
  - Graceful shutdown on SIGINT/SIGTERM

- [x] **005: Routing — Built-in commands and main agent dispatch**
  - Parse incoming text: `/command` vs. normal message
  - Built-in `/new`: archive current session, create fresh empty session
  - Built-in `/sessions`: list archived sessions within TTL window
  - Normal message → main agent pipeline
  - Unknown command → "Command not found. Use /skills to see available commands." (hardcoded for MVP)

- [x] **006: Turn 1 — Context assembly for first message in a session**
  - Assemble hierarchical Markdown context:
    - `## Workspace Context` → AGENTS.md content
    - `### Critical State` → `memory/critical.md`
    - `## Recent Memory` → today's + yesterday's daily notes (L2)
    - `## Current Task` → user's message
  - Create today's daily note skeleton if missing
  - Pass assembled prompt to Axe `runner.Run()` as `Prompt`
  - Persist turn 1 to `turns` table with `new_messages_json`

- [x] **007: Turn 2+ — Conversation history via `runner.Options.Messages`**
  - Load all prior turns from `turns` table
  - Reconstruct `[]runner.Message` by iterating turns in order
  - Append current user message
  - Pass via `opts.Messages` to `runner.Run()`
  - Receive `result.Messages` from Axe
  - Compute delta (new messages added this turn)
  - Store delta in new `turns` row
  - Update session `last_activity`, `total_input_tokens`, `total_output_tokens`

- [ ] **008: Error Handling — Map Axe errors to Telegram replies**
  - Type-switch on `runner.Run()` error returns
  - `ConfigError` → "Configuration issue: ..."
  - `BudgetExceededError` → "⚠️ Token budget exceeded ..."
  - `RuntimeError` + provider category (auth, rate limit, timeout, server) → appropriate user message
  - Generic fallback → "I couldn't process that request: ..."
  - Ensure all errors send a Telegram reply; never swallow silently

- [ ] **009: Integration — End-to-end smoke test**
  - Start server with valid config
  - Send first Telegram message → verify context assembly, Axe call, reply received
  - Send second message → verify history is loaded, turn number increments
  - Send `/new` → verify session archives, fresh session starts
  - Send `/sessions` → verify archived session appears in list
  - Verify all turns persisted in SQLite
  - Restart server → verify active session resumes correctly

- [ ] **010: Cleanup — Code review, logging polish, and repository hygiene**
  - Review all error paths and logging levels
  - Ensure no hardcoded secrets or tokens in code
  - Add `README.md` with build and run instructions
  - Tag initial version or create release notes
  - Prepare groundwork for post-MVP features (voice, workflows, skills, memory maintenance)
