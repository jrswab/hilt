# 005: Routing — Built-in commands and main agent dispatch

## Section 1: Context & Constraints

### Milestone Entry

> **005: Routing — Built-in commands and main agent dispatch**
> - Parse incoming text: `/command` vs. normal message
> - Built-in `/new`: archive current session, create fresh empty session
> - Built-in `/sessions`: list archived sessions within TTL window
> - Normal message → main agent pipeline
> - Unknown command → "Command not found. Use /skills to see available commands." (hardcoded for MVP)

### Research Findings

**Codebase State**
The project has completed milestones 001–004. The following packages exist and are functional:
- `cmd/hilt/main.go` — entry point that wires config, session manager, and Telegram bot
- `internal/config` — loads and validates `config.toml`
- `internal/session` — SQLite-backed session lifecycle (`GetActiveSession`, `ArchiveSession`, `CreateSession`, `PruneOldSessions`)
- `internal/telegram` — `gotgbot/v2` long-polling loop with `MessageHandler` callback
- `internal/server` — currently an empty package (was intended for webhook mode); **repurposed for routing logic**
- `internal/memory` — empty package (deferred)

**Design Decisions Already Made**
- Single-user permanently. No multi-user isolation needed.
- Single goroutine concurrency model (`MaxRoutines: 1` in dispatcher).
- Command resolution order: built-ins → workflows → agents. For MVP, only built-ins exist.
- Session titles are stored in the `sessions` table and are nullable.
- Session TTL is configured in `config.toml` (`session_ttl_days`).

**Constraints and Assumptions**
- The `telegram` package exposes `MessageHandler` as `func(ctx context.Context, chatID int64, text string) error`.
- The main agent pipeline (context assembly, Axe integration) is milestone 006; milestone 005 only needs a stub.
- `internal/server` shall house the `Router` type and its tests. The webhook-related design doc language is outdated and should be corrected in documentation.
- `session.Manager` currently lacks a method to list archived sessions; this capability must be added to satisfy `/sessions`.

**Approaches Ruled Out**
- Keeping routing logic in `cmd/hilt/main.go` as closures (rejected in favor of a testable package).
- Having the Telegram bot auto-send handler return values (rejected in favor of injected `Messenger` dependency).

---

## Section 2: Requirements

### Behaviors

1. **Command Detection**
   - Any message whose text, after trimming leading/trailing whitespace, begins with `/` is treated as a command.
   - Command names are case-insensitive (`/new`, `/NEW`, and `/New` are equivalent).
   - A bare `/` (no command name after the slash) is treated as an unknown command.

2. **Normal Message Routing**
   - Non-command text messages are routed to the main agent handler.
   - For this milestone, the main agent handler is a stub that replies with a placeholder message.

3. **`/new` — Start Fresh Session**
   - Archives the currently active session.
   - Creates a new empty active session.
   - Sends the user a confirmation reply.
   - If no active session exists when `/new` is invoked, a new session is created defensively (no error surfaced to the user).
   - The reply must include the new session ID.

4. **`/sessions` — List Archived Sessions**
   - Queries archived sessions whose `archived_at` is within the configured TTL window (from now back `session_ttl_days`).
   - Formats the result as a numbered list containing, per session:
     - Session ID
     - Title (or a fallback label if the title is nil/empty)
     - Creation timestamp
     - Archive timestamp
   - If no archived sessions exist within the TTL window, sends a single message stating so (not an empty list).

5. **Unknown Command**
   - Any command not matching a built-in produces the hardcoded reply:  
     `"Command not found. Use /skills to see available commands."`

6. **Error Handling**
   - If session operations fail during command processing, the error is logged and a user-friendly message is sent via Telegram. The server must not crash.
   - If the reply message cannot be sent, the error is logged. No retry is required in this milestone.

### Interfaces

The Router depends on small, interface-based abstractions:

- **Messenger** — anything that can send text replies to Telegram.  
  Required method: `SendMessage(ctx context.Context, chatID int64, text string) error`

- **SessionManager** — anything that can perform session lifecycle operations needed by routing.  
  Required methods:
  - `GetActiveSession(ctx context.Context) (*Session, error)`
  - `ArchiveSession(ctx context.Context, id int64) error`
  - `CreateSession(ctx context.Context) (*Session, error)`
  - `ListArchivedSessions(ctx context.Context, cutoff time.Time) ([]Session, error)`  
    *(new capability; cutoff is derived from `session_ttl_days`)*

The **Router** struct holds these dependencies and exposes:
- `HandleMessage(ctx context.Context, chatID int64, text string) error` — the callback wired into `telegram.Bot.Start()`.

### Data Flow

```
Telegram update
    │
    ▼
telegram.Bot.handleUpdate
    │
    ▼
Router.HandleMessage(chatID, text)
    │
    ├── /new ──────┐
    │              ▼
    │    SessionManager.GetActiveSession
    │              │
    │              ▼
    │    SessionManager.ArchiveSession
    │              │
    │              ▼
    │    SessionManager.CreateSession
    │              │
    │              ▼
    │    Messenger.SendMessage(confirmation)
    │
    ├── /sessions ─┐
    │              ▼
    │    SessionManager.ListArchivedSessions(cutoff)
    │              │
    │              ▼
    │    format numbered list
    │              │
    │              ▼
    │    Messenger.SendMessage(list or "no sessions" notice)
    │
    ├── unknown cmd ─┐
    │                ▼
    │    Messenger.SendMessage("Command not found...")
    │
    └── normal msg ──┐
                     ▼
          Messenger.SendMessage(stub reply)
```

### Edge Cases

| Scenario | Expected Behavior |
|----------|-------------------|
| `/new` with no prior active session | Create new session; send confirmation with new ID |
| `/new` when active session has nil title | Archive normally; new session starts untitled |
| `/sessions` with zero archived sessions in TTL window | Reply: "No archived sessions found." |
| `/sessions` when some sessions have nil titles | Display fallback label (e.g., "Untitled") |
| Whitespace-only message (`"   "`) | Treated as normal message (stub reply) |
| Message `/` (bare slash) | Treated as unknown command |
| Message `/ unknown` | Treated as unknown command (space after slash means no valid command name) |
| `SendMessage` fails | Log the error; do not propagate to Telegram layer |
| `ArchiveSession` returns `ErrNotFound` | Log and treat as "no active session"; proceed to create new session |
| Mixed-case command (`/SeSsIoNs`) | Normalized and matched to `/sessions` |

### Testing Priority

The following behaviors must be tested:

1. **Command parsing** — `/` prefix detection, case-insensitive matching, whitespace trimming, bare `/` handling.
2. **`/new`** — archives active session, creates new session, sends confirmation with correct session ID, handles missing active session defensively.
3. **Edge cases** — nil session titles, empty archived list, failed messenger calls (errors logged, not propagated).

Secondary (still tested, lower priority):
- `/sessions` list formatting and ordering.
- Unknown command reply text.
- Normal message stub routing.
