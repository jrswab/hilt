# Implementation Guide 009: Integration — End-to-End Smoke Test

## Section 1: Context Summary

**Associated milestone document:** `docs/plans/000_hilt_milestones.md`

This milestone verifies that all previously built Hilt packages — config, session, memory, mainagent, server, and telegram — wire together correctly into a functioning request pipeline. The smoke test is a single Go test that exercises the real component graph with fakes only at the external boundaries (Axe LLM runner and Telegram Bot API). It confirms Turn 1 context assembly, Turn 2+ history reconstruction, built-in command dispatch (`/new`, `/sessions`), delta persistence to SQLite, and server restart behavior using a temp-file database.

---

## Section 2: Implementation Checklist

### Test Fakes

- [x] `cmd/hilt/smoke_test.go`: Define `fakeSmokeRunner` struct implementing `mainagent.Runner`  
  - `Run(ctx, opts)` records every `runner.Options` call in a slice and returns pre-configured `*runner.Result` values in order.
  - Turn 1 result: `Content`, `InputTokens`, `OutputTokens` only (no `Messages`).
  - Turn 2 result: `Content`, `InputTokens`, `OutputTokens`, and `Messages` containing the full conversation history (sent messages plus the new assistant response) so `computeDelta` extracts the correct suffix.

- [x] `cmd/hilt/smoke_test.go`: Define `fakeSmokeMessenger` struct implementing `server.Messenger` and `mainagent.Messenger`  
  - `SendMessage(ctx, chatID, text)` appends to an internal slice of `{chatID, text}` records and returns a configurable error (default nil).

### Test Harness (R1)

- [x] `cmd/hilt/smoke_test.go`: Implement `TestSmoke` — temp directory and workspace setup  
  - Use `t.TempDir()` for the test root.
  - Create `AGENTS.md`, `memory/critical.md`, and `memory/YYYY-MM-DD.md` (today and yesterday) with deterministic content so `assembleContext` produces non-empty Markdown.
  - Construct a `*config.Config` directly (reusing `makeTestConfig` from `main_test.go`) with `WorkspaceDir` pointing to the temp directory.
  - Call `startSessionManager(ctx, dbPath, cfg, logger)` to create a real `*session.Manager` backed by a temp-file SQLite database and obtain the initial active session.
  - Create the `agents/` subdirectory so `agentsDir` exists (the fake runner ignores it, but the processor passes it through).
  - Wire the real component graph exactly as `main()` does:
    - `memoryReader := memory.NewReader(cfg.WorkspaceDir)`
    - `historyBuilder := mainagent.NewHistoryBuilder(mgr)`
    - `processor := mainagent.NewProcessor(mgr, mgr, mgr, memoryReader, fakeRunner, fakeMessenger, agentsDir, cfg.MainAgentModel, logger, historyBuilder, &mainagent.AxeErrorMapper{})`
    - `router := server.NewRouter(fakeMessenger, mgr, processor, cfg.SessionTTLDays, logger)`

### Turn 1 Verification (R2)

- [x] `cmd/hilt/smoke_test.go`: Add Turn 1 assertions to `TestSmoke`  
  - Call `router.HandleMessage(ctx, chatID, "first user message")`.
  - Assert `fakeRunner.calls[0].Prompt` is non-empty and contains the expected Markdown headers (`## Workspace Context`, `### Critical State`, `## Recent Memory`, `## Current Task`) and the file contents written during setup.
  - Assert `fakeRunner.calls[0].Messages` is empty (Turn 1 does not use history).
  - Assert `fakeMessenger` received a reply with the `Content` from the Turn 1 fake result.
  - Query the active session via `mgr.GetTurns(ctx, sessionID)` and assert exactly 1 turn with `turn_number == 1`, `user_message == "first user message"`, and `new_messages_json` containing a valid JSON array with one assistant message matching the fake result content.
  - Assert session `total_input_tokens` and `total_output_tokens` match the fake result values.
  - Assert the session `title` was set to a truncated version of the first user message.

### Turn 2+ Verification (R3)

- [x] `cmd/hilt/smoke_test.go`: Add Turn 2+ assertions to `TestSmoke`  
  - Call `router.HandleMessage(ctx, chatID, "second user message")`.
  - Assert `fakeRunner.calls[1].Prompt` is empty (Turn 2+ uses `Messages`, not `Prompt`).
  - Assert `fakeRunner.calls[1].Messages` is non-empty and contains, in order: the user message from Turn 1, the assistant response from Turn 1, and the new user message from Turn 2.
  - Assert `fakeMessenger` received a second reply with the Turn 2 fake result content.
  - Assert `mgr.GetTurns` now returns 2 turns (`turn_number` 1 and 2).
  - Assert session token counters equal the sum of Turn 1 and Turn 2 fake result values.

### `/new` Command Verification (R4)

- [x] `cmd/hilt/smoke_test.go`: Add `/new` assertions to `TestSmoke`  
  - Call `router.HandleMessage(ctx, chatID, "/new")`.
  - Assert the old session is no longer returned by `mgr.GetActiveSession`.
  - Assert `mgr.ListArchivedSessions` with a past cutoff returns the old session with a non-nil `archived_at`.
  - Assert a new active session exists with a different `id`, zero turns, zero tokens, and nil title.
  - Assert `fakeMessenger` received a confirmation containing the new session ID.

### `/sessions` Command Verification (R5)

- [x] `cmd/hilt/smoke_test.go`: Add `/sessions` assertions to `TestSmoke`  
  - Call `router.HandleMessage(ctx, chatID, "/sessions")`.
  - Assert `fakeMessenger` received a listing that contains the archived session's ID and its title (set during Turn 1).

### Persistence Verification (R6)

- [x] `cmd/hilt/smoke_test.go`: Add persistence assertions to `TestSmoke`  
  - Use `mgr.GetTurns(ctx, archivedSessionID)` and assert exactly 2 rows with `turn_number` 1 and 2.
  - For each row, unmarshal `new_messages_json` and assert it contains the expected assistant response for that turn.
  - Use `mgr.GetTurns(ctx, newActiveSessionID)` and assert zero rows.

### Restart Verification (R7)

- [x] `cmd/hilt/smoke_test.go`: Add restart assertions to `TestSmoke`  
  - Call `mgr.Close()` to release the database connection.
  - Call `startSessionManager(ctx, dbPath, cfg, logger)` again with the same `dbPath` to simulate a server restart.
  - Assert the returned active session has the same `id` as the post-`/new` session (not the original archived session).
  - Assert the restarted session has zero turns via `mgr.GetTurnCount` or `mgr.GetTurns`.
  - Close the restarted manager.

### Verification

- [x] Run `go test ./cmd/hilt -run TestSmoke -v` and confirm all assertions pass.
