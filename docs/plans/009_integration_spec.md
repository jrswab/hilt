# Spec 009: Integration — End-to-End Smoke Test

## Section 1: Context & Constraints

### Milestone Entry

> **009: Integration — End-to-end smoke test**
>
> - Start server with valid config
> - Send first Telegram message → verify context assembly, Axe call, reply received
> - Send second message → verify history is loaded, turn number increments
> - Send `/new` → verify session archives, fresh session starts
> - Send `/sessions` → verify archived session appears in list
> - Verify all turns persisted in SQLite
> - Restart server → verify active session resumes correctly

### Research Findings

#### Codebase State
The project is a Go module at `github.com/jrswab/hilt` with the following packages and interfaces already implemented:

- `cmd/hilt/main.go` — entry point that wires config, session manager, Telegram bot, memory reader, main agent processor, and router.
- `internal/config` — `Config` struct, `Load(path)`, `ExpandPath`, `createDefaults`.
- `internal/session` — `Manager` with SQLite schema (`sessions`, `turns` tables), `GetActiveSession`, `CreateSession`, `ArchiveSession`, `PruneOldSessions`, `GetTurnCount`, `GetTurns`, `RecordTurn`, `UpdateSessionTokens`, `SetSessionTitle`, `UpdateSessionActivity`.
- `internal/telegram` — `Bot` with `Start(ctx, MessageHandler)` and `SendMessage(ctx, chatID, text)`.
- `internal/memory` — `Reader` with `ReadAGENTSMD`, `ReadCriticalMD`, `ReadDailyNote`, `EnsureDailyNoteSkeleton`.
- `internal/mainagent` — `Processor` with `ProcessTurn(ctx, chatID, text)`, `assembleContext`, `processTurn2Plus`, delta computation, history building, error mapping.
- `internal/server` — `Router` with `HandleMessage(ctx, chatID, text)`, dispatching `/new`, `/sessions`, and normal messages to the processor.

All packages already have comprehensive unit tests. The interfaces (`Messenger`, `SessionManager`, `TurnProcessor`, `Runner`, `FileReader`, `ActiveSessionProvider`, `TurnStore`, `SessionStore`, `HistoryBuilder`, `ErrorMapper`) are already defined and consumed via constructor injection.

#### Decisions Already Made & Why

| Decision | Resolution | Rationale |
|----------|------------|-----------|
| Smoke test format | **Go test (`go test`)** | Deterministic, CI-friendly, leverages existing test infrastructure. |
| External API boundaries | **Fakes/mocks for Telegram and Axe** | Avoids network dependencies, API keys, and provider rate limits in tests. The real integration surface is the wiring between Hilt's own packages, not the external APIs. |
| Verification scope | **All 7 milestone checks in a single automated test** | The user explicitly requested all verifications be covered. |
| Database for smoke test | **Temp-file SQLite DB** | Required to simulate server restart (new `sql.DB` connection to the same file). In-memory (`:memory:`) would not survive connection close. |
| Server "restart" semantics | **Re-initialize in-process components** (close and reopen DB, re-create router/processor) | Spawning a new OS process from within `go test` is fragile and platform-dependent. Re-initializing the component graph exercises the same startup code paths. |
| Test location | **`cmd/hilt/` or a dedicated `internal/integration/` package** | TBD by implementer; must be discoverable by `go test ./...`. |

#### Approaches Considered and Rejected

| Approach | Why Rejected |
|----------|--------------|
| Shell script smoke test | Not a Go test; harder to assert on internal state, harder to run in CI. |
| Real Telegram Bot API | Requires valid bot token, network, and a second Telegram client to send messages; non-deterministic and slow. |
| Real LLM provider (Axe) | Requires API keys and consumes tokens; defeats the purpose of a fast, deterministic smoke test. |
| In-memory (`:memory:`) SQLite for restart test | Cannot be reopened by a new `sql.DB` handle; temp file is required. |
| Spawn new OS process for restart | Platform-dependent, requires compiled binary, harder to inject fakes. |

#### Constraints & Assumptions
- **Single-user, single-goroutine** — no concurrent message processing to simulate.
- **All existing interfaces remain stable** — the smoke test composes the same constructors used in `main()`.
- **`runner.Options.Messages` and `runner.Result.Messages`** — the delta-only persistence scheme (journal-style `turns` table) is already implemented and assumed correct per prior milestones.
- **Axe Issue #82 is merged** — `runner.Options.Messages` works for multi-turn history.
- **Temp directories** — the test must use `t.TempDir()` for config, workspace, and database to avoid cross-test contamination.
- **Environment isolation** — the test must not read the user's real `~/.config/hilt/config.toml` or `~/.hilt/` workspace.

#### Open Questions — All Answered
1. **Test format?** → Go test (`go test`).
2. **All 7 verifications automated?** → Yes.
3. **Real or fake external APIs?** → Fakes for Telegram bot and Axe `runner.Run`.
4. **Restart semantics?** → Re-initialize in-process components with a fresh DB connection to the same file.
5. **Database type for test?** → Temp-file SQLite.

---

## Section 2: Requirements

### R1: Test Harness Construction

The smoke test must construct and start the full Hilt component graph using only in-memory or temporary-file resources.

- **R1.1** — Create a temporary directory for the test.
- **R1.2** — Write a valid `config.toml` into the temp directory with a fake bot token, temp workspace path, and sensible defaults.
- **R1.3** — Create the workspace directory with `AGENTS.md`, `memory/critical.md`, and at least one daily note file so that `assembleContext` has real content to load.
- **R1.4** — Initialize a `session.Manager` pointing to a temp-file SQLite database.
- **R1.5** — Initialize a `memory.Reader` pointing to the temp workspace.
- **R1.6** — Provide a fake `Runner` that returns deterministic `runner.Result` values.
- **R1.7** — Provide a fake `Messenger` that records all sent messages.
- **R1.8** — Wire all components together exactly as `main()` does: processor → router → Telegram handler.

### R2: First Telegram Message (Turn 1)

Simulate the first user message and verify the full Turn 1 pipeline.

- **R2.1** — Invoke the router's `HandleMessage` with a non-command text message.
- **R2.2** — Verify the fake `Runner` was called with `runner.Options` where `Prompt` is non-empty and contains the assembled Markdown context (workspace context, critical state, recent memory, current task).
- **R2.3** — Verify `Messages` is empty on Turn 1 (history is not used for the first message).
- **R2.4** — Verify the fake `Messenger` received a reply matching the content returned by the fake `Runner`.
- **R2.5** — Verify a single turn row was inserted into the `turns` table with `turn_number = 1`, the original user message, and `new_messages_json` containing the assistant response delta.
- **R2.6** — Verify the active session's `total_input_tokens` and `total_output_tokens` were incremented by the values from the fake `Result`.
- **R2.7** — Verify the active session's `title` was set to a truncated version of the first user message.

### R3: Second Telegram Message (Turn 2+)

Simulate a follow-up message and verify multi-turn history behavior.

- **R3.1** — Invoke `HandleMessage` again with a second non-command text message.
- **R3.2** — Verify the fake `Runner` was called with `runner.Options.Messages` non-empty, containing the reconstructed conversation history from Turn 1 plus the new user message appended.
- **R3.3** — Verify `Prompt` is empty on Turn 2+ (the prompt field is only used for Turn 1).
- **R3.4** — Verify the fake `Messenger` received the second reply.
- **R3.5** — Verify a second turn row was inserted with `turn_number = 2`.
- **R3.6** — Verify session token counters were incremented again.

### R4: `/new` Command

Simulate the `/new` built-in command and verify session lifecycle transition.

- **R4.1** — Invoke `HandleMessage` with `/new`.
- **R4.2** — Verify the original active session now has `archived_at` populated.
- **R4.3** — Verify a new active session exists with a different `id`, zero turns, zero tokens, and a nil title.
- **R4.4** — Verify the fake `Messenger` received a confirmation message containing the new session ID.

### R5: `/sessions` Command

Simulate the `/sessions` built-in command and verify archive listing.

- **R5.1** — Invoke `HandleMessage` with `/sessions`.
- **R5.2** — Verify the fake `Messenger` received a message containing the previously archived session (from R4).
- **R5.3** — Verify the archived session's title appears in the listing.

### R6: Turn Persistence Verification

Directly query the database to confirm the full turn journal is intact.

- **R6.1** — Read all rows from the `turns` table for the archived session and assert there are exactly 2 rows (`turn_number` 1 and 2).
- **R6.2** — Assert each row's `new_messages_json` is valid JSON and contains the expected assistant response for that turn.
- **R6.3** — Assert the new active session has zero rows in the `turns` table.

### R7: Server Restart

Simulate a server restart by re-initializing the component graph with a fresh DB connection to the same SQLite file.

- **R7.1** — Close the original `session.Manager`.
- **R7.2** — Create a new `session.Manager` pointing to the same temp-file database.
- **R7.3** — Call `GetActiveSession` on the new manager.
- **R7.4** — Verify the returned session is the same one created in R4 (the post-`/new` active session), not the archived one.
- **R7.5** — Verify the session has zero turns and is ready to resume.

### Edge Cases

- **E1 — Empty workspace files** — If `AGENTS.md` or `critical.md` are empty, the assembled context must still be well-formed (headers present, empty sections allowed). The smoke test should include content to exercise the normal path; edge-case coverage of empty files belongs in unit tests.
- **E2 — Runner returns error on Turn 1** — The smoke test covers the happy path. Error-path smoke testing is out of scope for this milestone; error mapping is already covered by milestone 008 unit tests.
- **E3 — Concurrent messages** — Out of scope. Single-user, single-goroutine is a permanent constraint for MVP.
- **E4 — Missing `agents/main.toml`** — The fake `Runner` does not read the filesystem, so this is not a smoke-test concern. Config default creation is covered by milestone 002.
- **E5 — Session manager `GetActiveSession` auto-creates on empty DB** — Already covered by R1 startup and R7 restart.

---

## Planning Checklist (Pre-Implementation)

The following items were confirmed with the user before writing this spec:

- [x] **Public interface** — A Go test runnable with `go test`, likely in `cmd/hilt/` or `internal/integration/`.
- [x] **Behaviors to test** — All 7 milestone verifications (Turn 1, Turn 2+, `/new`, `/sessions`, persistence, restart).
- [x] **Deep modules** — Reuse existing `fakeMessenger`, `fakeRunner`, and `fakeSessionManager` patterns from unit tests; the smoke test is a shallow orchestration layer that composes deep modules already built.
- [x] **Testability** — All dependencies are interface-based; no new interfaces are needed.
- [x] **Behaviors list** — See R1–R7 above.
