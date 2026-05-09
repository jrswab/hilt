# 007: Turn 2+ — Conversation History via `runner.Options.Messages` — Implementation Guide

## Section 1: Context Summary

**Milestone:** `docs/plans/000_hilt_milestones.md` — 007: Turn 2+ — Conversation history via `runner.Options.Messages`

When a user sends a normal message and the active session already has one or more recorded turns, Hilt must switch from the Turn 1 Markdown-context path (`opts.Prompt`) to a conversation-history path (`opts.Messages`). The full message history is reconstructed from prior `turns` rows, the current user message is appended, and the slice is passed to `runner.Run()`. Axe returns the complete history in `result.Messages`; the delta (new suffix) is persisted as a new `turns` row, session metadata is updated, and the reply text is extracted and sent to Telegram. Tool calls and tool results must round-trip through JSON losslessly. The session layer gains a read method (`GetTurns`) that is deliberately not added to the `TurnStore` interface, keeping write and read concerns separate.

## Section 2: Implementation Checklist

**Pre-requisite**
- [x] `go.mod`: Update `github.com/jrswab/axe` to a version that exposes `runner.Options.Messages` and `runner.Result.Messages` (run `go get github.com/jrswab/axe@latest` and verify the fields exist).

**Session read capability**
- [x] `internal/session/session.go`: Add `TurnRow` struct with fields `TurnNumber int`, `UserMessage string`, `NewMessagesJSON string`.
- [x] `internal/session/session.go`: Add `GetTurns(ctx context.Context, sessionID int64) ([]TurnRow, error)` method on `*Manager` that queries all turns ordered by `turn_number ASC` and returns an empty slice for zero turns.
- [x] `internal/session/session_test.go`: Test `GetTurns` returns correct rows in chronological order for a session with multiple turns, returns empty slice for zero turns, and returns error on invalid session ID.

**Internal persistence format**
- [x] `internal/mainagent/processor.go`: Replace the existing `message` struct with `persistedMessage`, `persistedToolCall`, and `persistedToolResult` structs that match the required JSON shape (`role`, `content`, `tool_calls` with `id`/`name`/`arguments`, `tool_results` with `call_id`/`content`/`is_error`). Use `omitempty` for optional arrays.
- [x] `internal/mainagent/processor.go`: Add `toRunnerMessages([]persistedMessage) []runner.Message` and `fromRunnerMessages([]runner.Message) []persistedMessage` conversion helpers (lossless round-trip).
- [x] `internal/mainagent/processor.go`: Update the Turn 1 delta creation in `ProcessTurn()` to use `[]persistedMessage` so the persistence format is unified.
- [x] `internal/mainagent/persist_test.go`: Test JSON round-trip of a plain assistant message, a message with `tool_calls`, a message with `tool_results`, and backward compatibility unmarshaling old `{"role":"assistant","content":"..."}` JSON.

**History builder**
- [x] `internal/mainagent/history.go`: Define `TurnReader` interface with `GetTurns(ctx context.Context, sessionID int64) ([]session.TurnRow, error)`.
- [x] `internal/mainagent/history.go`: Define `HistoryBuilder` struct with a `TurnReader` field and constructor `NewHistoryBuilder(reader TurnReader) *HistoryBuilder`.
- [x] `internal/mainagent/history.go`: Implement `BuildMessages(ctx context.Context, sessionID int64) ([]runner.Message, error)` on `*HistoryBuilder` that interleaves user messages with deserialized persisted messages, validates roles, and returns a non-nil empty slice for zero turns.
- [x] `internal/mainagent/history_test.go`: Test `BuildMessages` reconstructs history correctly for 0 turns, 1 turn, and 2 turns with interleaved assistant/tool messages.
- [x] `internal/mainagent/history_test.go`: Test `BuildMessages` returns an error for invalid JSON in `new_messages_json` and for an unrecognized `role` value.

**Delta computation and reply extraction**
- [x] `internal/mainagent/delta.go`: Implement `computeDelta(sent []runner.Message, result *runner.Result) []runner.Message` that returns `result.Messages[len(sent):]` when valid, logs and falls back to a single assistant message with `result.Content` when `result.Messages` is nil/empty or shorter than `sent`.
- [x] `internal/mainagent/delta.go`: Implement `extractReply(result *runner.Result) string` that returns the `Content` of the last assistant message in `result.Messages`, walking backward past tool messages, or falling back to `result.Content`.
- [x] `internal/mainagent/delta_test.go`: Test `computeDelta` with exact suffix match, nil `Messages` fallback, and `len(returned) < len(sent)` fallback.
- [x] `internal/mainagent/delta_test.go`: Test `extractReply` when last message is assistant, when last message is tool (walks back), and when `Messages` is empty.

**Processor Turn 2+ integration**
- [x] `internal/mainagent/processor.go`: Add `history HistoryBuilder` field to the `Processor` struct.
- [x] `internal/mainagent/processor.go`: Update `NewProcessor()` signature to accept a `HistoryBuilder` parameter (placed after `Messenger`) and store it in the struct; update panic validation.
- [x] `internal/mainagent/processor.go`: Replace the Turn 2+ stub in `ProcessTurn()` with full execution: call `history.BuildMessages()`, append current user message, set `opts.Messages` and clear `opts.Prompt`, call `runner.Run()`, compute delta with `computeDelta()`, persist via `RecordTurn()`, update tokens and activity, extract reply with `extractReply()`, and send to Telegram.
- [x] `internal/mainagent/processor.go`: Handle `BuildMessages` error by sending `"Something went wrong loading conversation history."` to Telegram, logging, and returning nil without calling the LLM.
- [x] `internal/mainagent/processor_test.go`: Update all `NewProcessor` calls to include a `fakeHistoryBuilder`; add `fakeHistoryBuilder` test helper with `messages []runner.Message` and `err error`.
- [x] `internal/mainagent/processor_test.go`: Test Turn 2+ success path: `GetTurnCount` returns 1, fake history returns prior messages, runner receives `opts.Messages` with history + current user message, delta is recorded as turn 2, tokens and activity are updated, correct reply is sent.
- [x] `internal/mainagent/processor_test.go`: Test Turn 2+ with tool call round-trip: fake history returns prior turns, runner returns `result.Messages` containing assistant-with-tool-calls and tool-with-tool-results; verify the persisted JSON contains `tool_calls` and `tool_results`.
- [x] `internal/mainagent/processor_test.go`: Test Turn 2+ graceful degradation when `result.Messages` is nil: verify fallback delta is persisted as a single assistant message and `result.Content` is sent as the reply.
- [x] `internal/mainagent/processor_test.go`: Test Turn 2+ when `BuildMessages` returns an error: verify Telegram receives the history error message, no LLM call is made, and no turn is recorded.
- [x] `internal/mainagent/processor_test.go`: Test Turn 2+ `turn_number` increments sequentially and token counters accumulate correctly.

**Application wiring**
- [x] `cmd/hilt/main.go`: Instantiate `historyBuilder := mainagent.NewHistoryBuilder(mgr)` and pass it as the new argument to `mainagent.NewProcessor()`.
