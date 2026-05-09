# Spec: 006 — Turn 1: Context Assembly for First Message in a Session

## Document Info
| Field | Value |
|---|---|
| Spec Number | 006 |
| Milestone | Turn 1 — Context assembly for first message in a session |
| Status | Draft |

---

## Section 1: Context & Constraints

### Milestone Entry

> **006: Turn 1 — Context assembly for first message in a session**
> - Assemble hierarchical Markdown context:
>   - `## Workspace Context` → AGENTS.md content
>   - `### Critical State` → `memory/critical.md`
>   - `## Recent Memory` → today's + yesterday's daily notes (L2)
>   - `## Current Task` → user's message
> - Create today's daily note skeleton if missing
> - Pass assembled prompt to Axe `runner.Run()`
> - Persist turn 1 to `turns` table with `new_messages_json`

### Research Findings Relevant to This Milestone

**Codebase Structure and Patterns**
- The project is a Go module with packages: `internal/config`, `internal/session`, `internal/telegram`, `internal/server`, `internal/memory` (stub), and `internal/errors.go`.
- The `server` package owns routing logic. Built-in commands (`/new`, `/sessions`) are already implemented. Normal messages are routed to `handleNormalMessage`, which currently replies with a stub.
- The `session.Manager` owns all SQLite database access but currently has no `turns` table methods — only the schema definition.
- The `memory` package is an empty stub; memory-related file operations currently have no owner.
- Configuration is loaded via `config.Load`, which expands paths, creates defaults, and validates. The workspace directory path is resolved to an absolute path in the returned `Config`.

**Decisions Already Made and Why**
1. **New package `internal/mainagent/`** — The main agent execution loop (context assembly, Axe invocation, turn persistence, reply dispatch) lives in a dedicated package with a small public interface. This is a deep module: callers interact with a single `ProcessTurn` method; the complexity of Markdown assembly, file I/O, Axe options construction, and delta computation is hidden inside.
2. **Turn 1 uses `runner.Options.Messages`** (not `Prompt`) — For consistency with Turn 2+, the assembled context is placed as a single user message in a `[]runner.Message` slice passed via `opts.Messages`. Axe loads `main.toml` as the system prompt on every turn regardless.
3. **Delta-only persistence** — Each `turns` row stores only the messages *newly added* in that turn (`new_messages_json`). Reconstruction requires iterating all turns in order.
4. **No token budget warnings in MVP** — Token counts are tracked per session but warnings at 80% are deferred.
5. **No voice, no workflows, no background maintenance** — These are permanently out of scope for MVP milestones.
6. **Single-user, single goroutine** — No concurrent message processing; no per-user isolation.

**Approaches Already Ruled Out**
- Using `runner.Options.Prompt` for Turn 1 (would create divergent code paths between Turn 1 and Turn 2+).
- Storing full `[]runner.Message` snapshot per turn (rejected for O(N²) storage; delta-only is O(N)).
- Embedding context assembly logic inside `server.Router` (would bloat the router with memory-file I/O and Axe concerns).

**Constraints and Assumptions**
- Axe Issue #82 is merged: `runner.Options.Messages` works and `runner.Result.Messages` returns the full conversation history (including assistant responses and tool results) when `opts.Messages` was initially non-empty.
- Axe message validation is strict: user messages cannot have tool calls/results; assistant messages cannot have tool results; tool messages must have empty content and reference valid preceding assistant call IDs. Hilt does not construct tool messages directly; only user messages are constructed by Hilt.
- The `main.toml` system prompt is loaded by Axe automatically from the agents directory on every `runner.Run()` call.
- SQLite via `modernc.org/sqlite` (pure-Go, no CGO).
- Hilt runs as a single OS process; memory files are local filesystem files.
- The workspace directory and `~/.config/hilt/` directory are created and valid by the time the server starts (enforced by `config.Load`).
- `AGENTS.md` stub is created by `config.Load` if missing, but may be empty.
- `critical.md` may not exist yet; daily notes may not exist yet.

**Open Questions Resolved During Research**
1. **Where does main agent logic live?** → New `internal/mainagent/` package.
2. **Turn 1 Axe API: `Prompt` or `Messages`?** → `Messages` for consistency.
3. **Which behaviors to test?** → (a) Context assembly produces correct Markdown; (b) Daily note skeleton creation; (c) Runner invocation with correct options; (d) Turn delta persistence; (e) Telegram reply dispatch; (f) Error paths.

---

## Section 2: Requirements

### 2.1 Trigger Condition

**R001 — Turn 1 is detected when a normal (non-command) message arrives and the active session has zero persisted turns.**
- The system queries the `turns` table for the active session.
- If no rows exist, this is Turn 1.
- If rows exist, this is a subsequent turn (out of scope for this milestone; see milestone 007).

**R002 — The user message text is trimmed of leading and trailing whitespace before processing.**

---

### 2.2 Context Assembly

**R003 — Hilt assembles a single hierarchical Markdown document from memory files and the user's message.**
The assembled document has the following top-level sections, in this exact order:

```markdown
## Workspace Context

### Rules (AGENTS.md)
[content of ~/.hilt/AGENTS.md, or empty string if file is missing/empty]

### Critical State
[content of ~/.hilt/memory/critical.md, or empty string if file is missing/empty]

## Recent Memory

### Note: YYYY-MM-DD
[content of today's daily note, or skeleton if newly created]

### Note: YYYY-MM-DD
[content of yesterday's daily note, or omitted if file does not exist]

## Current Task

[user's trimmed message]
```

**R004 — The `## Workspace Context` section always contains two subsections:** `### Rules (AGENTS.md)` and `### Critical State`.**
- If the source file does not exist or is empty, the subsection header is still present and the content area is empty.

**R005 — The `## Recent Memory` section contains today's daily note as the first subsection.**
- The subsection header is `### Note: YYYY-MM-DD` where the date is the current UTC date.
- If the file `memory/YYYY-MM-DD.md` does not exist, Hilt creates it with the Wing/Hall skeleton template before reading it.
- The skeleton template:
  ```markdown
  # YYYY-MM-DD

  ## Wing: Work

  ### Hall: decisions

  ### Hall: events

  ### Hall: discoveries

  ### Hall: tasks

  ### Hall: blockers

  ## Wing: Health

  ### Hall: metrics
  ```

**R006 — The `## Recent Memory` section contains yesterday's daily note as the second subsection if and only if the file exists.**
- The subsection header is `### Note: YYYY-MM-DD` where the date is UTC yesterday.
- If the file does not exist, the entire subsection is omitted (no creation, no skeleton, no header).
- Yesterday is defined as the calendar day before today in UTC.

**R007 — The `## Current Task` section is always last and contains only the user's trimmed message.**
- No additional headers, no wrapping, no prefix.
- This placement ensures the model treats it as the primary instruction.

**R008 — The assembled Markdown document becomes the `Content` of a single `runner.Message` with `Role: "user"`.**
- This message is the sole element in the `Messages` slice passed to Axe for Turn 1.
- If the assembled document is empty (e.g., all sources missing and user sent an empty string), the content is the empty string.

---

### 2.3 Axe Invocation

**R009 — Hilt invokes Axe `runner.Run()` with the following options set:**
- `AgentsDir`: `~/.config/hilt/agents/`
- `Workspace`: resolved absolute path of `config.WorkspaceDir`
- `Model`: `config.MainAgentModel`
- `Messages`: `[]runner.Message{ {Role: "user", Content: assembledMarkdown} }`

**R010 — Hilt does not set `Prompt` on `runner.Options` for Turn 1.**
- The assembled context is delivered exclusively via `Messages`.

**R011 — If `runner.Run()` returns a non-error result, Hilt extracts `result.Content` for the Telegram reply.**
- `result.Content` is the assistant's text response.
- Tool calls, stop reasons, and token counts are read from the result but only token counts (input and output) are used for session bookkeeping in this milestone.

**R012 — `result.Messages` is used to compute the delta for persistence.**
- Delta = `result.Messages[len(opts.Messages):]` (all messages after the input messages).
- For Turn 1, `len(opts.Messages)` is 1, so the delta includes the assistant response and any intermediate tool/result messages.
- The delta is serialized to JSON and stored in `new_messages_json`.

---

### 2.4 Turn Persistence

**R013 — After a successful Axe call, Hilt writes a new row to the `turns` table.**
- `session_id`: the active session's ID.
- `turn_number`: 1 for Turn 1.
- `user_message`: the user's trimmed raw message (not the assembled Markdown).
- `new_messages_json`: JSON array of the delta messages (see R012).

**R014 — After writing the turn, Hilt updates the active session's token counters.**
- `total_input_tokens` is incremented by `result.InputTokens`.
- `total_output_tokens` is incremented by `result.OutputTokens`.
- `last_activity` is updated to the current timestamp.

**R015 — If the session has no title, Hilt sets the title from the first user message after Turn 1 completes successfully.**
- The title is the user message trimmed to 40 runes. If the message is longer than 40 runes, it is truncated after the 40th rune with no ellipsis added.
- An empty user message results in a null/empty title.
- The title is written to the `sessions` table.

---

### 2.5 Telegram Reply Dispatch

**R016 — On success, Hilt sends `result.Content` to the user's Telegram chat.**
- The reply is sent via the existing `Messenger.SendMessage` interface.
- If `result.Content` is empty, an empty message is sent (behavior delegated to Telegram API).

**R017 — On Axe error, Hilt sends a generic fallback message:** "I couldn't process that request: [error details]".
- Detailed error mapping (`ConfigError`, `BudgetExceededError`, provider categories) is milestone 008. This milestone provides only the generic fallback to ensure the user is never left without a reply.
- The error details string is the `Error()` string of the Go error.

**R018 — On any internal error before or during Axe invocation (missing files, DB failure, assembly failure), Hilt logs the error and sends a user-friendly Telegram message.**
- Example: "Something went wrong assembling context."
- The error is never swallowed silently.

---

### 2.6 Error Handling and Edge Cases

**E001 — Missing AGENTS.md.**
- If `~/.hilt/AGENTS.md` does not exist at assembly time, the `### Rules (AGENTS.md)` subsection is present with empty content.
- This is a non-fatal condition; processing continues.

**E002 — Missing critical.md.**
- If `~/.hilt/memory/critical.md` does not exist, the `### Critical State` subsection is present with empty content.
- This is a non-fatal condition; processing continues.

**E003 — Missing memory/ directory.**
- If `memory/` does not exist inside the workspace, Hilt creates it before attempting to write the daily note skeleton.

**E004 — Daily note skeleton creation failure.**
- If creating the skeleton file fails (permissions, disk full, etc.), the error is logged, a Telegram error message is sent, and the turn does not proceed.
- The user receives: "Something went wrong preparing your session."

**E005 — Axe invocation failure (any error from `runner.Run()`).**
- The error is logged.
- The generic fallback message (R017) is sent to Telegram.
- **No turn is persisted** because there is no valid delta to store.
- Session token counters are not updated.

**E006 — Axe success but empty `result.Messages`.**
- If `result.Messages` is nil or shorter than `opts.Messages`, this is treated as an unexpected Axe result.
- The error is logged.
- The user receives the fallback error message.
- No turn is persisted.

**E007 — DB write failure after successful Axe call.**
- If writing the `turns` row or updating session counters fails after Axe has already returned a result:
  - The error is logged at `Error` level.
  - The Telegram reply **is still sent** (the user already waited for the LLM; denying the reply does not recover the DB).
  - The system continues operating; the turn is lost from persistence.

**E008 — Concurrent Turn 1 messages.**
- Out of scope. The single-goroutine design (Telegram dispatcher `MaxRoutines: 1`) serializes all message processing. No explicit locking is required for MVP.

**E009 — Very long assembled context.**
- No proactive token estimation is performed. The assembled Markdown is sent to Axe as-is.
- If the context exceeds the model's window, Axe returns a `BudgetExceededError` or provider-side error (handled as E005).

**E010 — Session has no active session at Turn 1 time.**
- This cannot happen under normal operation because the server creates/loads an active session at startup.
- If it does happen (DB corrupted, deleted mid-run), `GetActiveSession` returns an error, which is logged and surfaced to the user.

---

### 2.7 Interface Summary

**`mainagent.Processor`** is the public interface of the new package. Its dependency interfaces are:

```go
// TurnStore manages the turns table.
type TurnStore interface {
    GetTurnCount(ctx context.Context, sessionID int64) (int, error)
    RecordTurn(ctx context.Context, sessionID int64, turnNum int, userMessage string, newMessagesJSON string) error
}

// SessionStore manages session metadata updates.
type SessionStore interface {
    UpdateSessionTokens(ctx context.Context, sessionID, inputTokens, outputTokens int64) error
    SetSessionTitle(ctx context.Context, sessionID int64, title string) error
}

// FileReader reads workspace memory files.
type FileReader interface {
    ReadAGENTSMD() (string, error)
    ReadCriticalMD() (string, error)
    ReadDailyNote(date string) (string, error)
    EnsureDailyNoteSkeleton(date string) error
}

// Runner invokes the LLM.
type Runner interface {
    Run(opts runner.Options) (runner.Result, error)
}

// Messenger sends replies to Telegram.
type Messenger interface {
    SendMessage(ctx context.Context, chatID int64, text string) error
}
```

The `Processor` itself exposes a single method:

```go
func (p *Processor) ProcessTurn(ctx context.Context, chatID int64, text string) error
```

**Invariants:**
- `ProcessTurn` always returns an error to the caller if Telegram dispatch fails; internal errors (Axe, DB) are surfaced to the user via Telegram and the method returns nil to avoid double-handling.
- `ProcessTurn` never panics; all dependencies are validated at construction time.

---

### 2.8 Behaviors to Test (Priority Order)

| Priority | Behavior | Why |
|---|---|---|
| 1 | Context assembly produces correct hierarchical Markdown with all sections in order | Core deliverable of the milestone; complex string-building logic |
| 2 | Daily note skeleton is created when today's file is missing | Side effect that modifies the filesystem; must be correct for future sessions |
| 3 | Runner is invoked with correct `AgentsDir`, `Workspace`, `Model`, and `Messages` | Integration with Axe; wrong options mean silent failures or wrong model |
| 4 | Turn delta (all messages after input) is persisted as JSON in `turns` table | Data durability; incorrect delta corrupts Turn 2+ reconstruction |
| 5 | Telegram reply is sent with `result.Content` on success | End-to-end completion; user-visible output |
| 6 | Error paths: Axe failure, DB failure, file read failure, empty Axe result | Resilience; user must always receive a reply, system must not crash |

**Non-goals for testing:**
- Exact token count accuracy (delegated to Axe).
- Content of Axe's assistant response (depends on model and external API).
- Filesystem race conditions (single-threaded by design).
- Memory maintenance agent behavior (post-MVP).
