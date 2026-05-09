# 007: Turn 2+ — Conversation History via `runner.Options.Messages`

## Section 1: Context & Constraints

### Milestone Entry

> - [ ] **007: Turn 2+ — Conversation history via `runner.Options.Messages`**
>   - Load all prior turns from `turns` table
>   - Reconstruct `[]runner.Message` by iterating turns in order
>   - Append current user message
>   - Pass via `opts.Messages` to `runner.Run()`
>   - Receive `result.Messages` from Axe
>   - Compute delta (new messages added this turn)
>   - Store delta in new `turns` row
>   - Update session `last_activity`, `total_input_tokens`, `total_output_tokens`

### Research Findings

**Axe `pkg/runner` API (confirmed working in local source):**
- `runner.Options.Messages` allows callers to seed the conversation with a pre-built message history. When non-empty, `Prompt` and `Stdin` are ignored.
- `runner.Result.Messages` returns the **full** conversation history (all messages including assistant responses and tool results) **only** when `opts.Messages` was initially non-empty. If `opts.Messages` was empty, `Result.Messages` is nil.
- Message validation is strict: user messages cannot have tool calls or tool results; assistant messages cannot have tool results; tool messages must have empty content and reference valid preceding assistant call IDs.
- Axe provides `runner.Message` with fields: `Role`, `Content`, `ToolCalls []ToolCall`, `ToolResults []ToolResult`.

**Current codebase state:**
- `internal/mainagent/processor.go` handles turn processing. Turn 1 is fully implemented using `runner.Options.Prompt` with assembled Markdown context. Turn 2+ is stubbed with a Telegram reply: `"Message received. The main agent is not yet online for turn 2+."`.
- `internal/session/session.go` defines the `TurnStore` interface: `GetTurnCount` and `RecordTurn`.
- `turns` table schema: `session_id`, `turn_number`, `user_message`, `new_messages_json`.
- Current `new_messages_json` persistence uses a minimal struct with only `role` and `content`. Tool call data from Axe's tool loop is silently lost.
- `SessionStore` provides `UpdateSessionTokens` and `SetSessionTitle`. `ActiveSessionProvider` provides `GetActiveSession` and `UpdateSessionActivity`.

**Decisions already made:**
- Single-user, single active session globally.
- Delta-only storage: each turn row stores only the messages added in that turn, not a full snapshot.
- Turn numbering starts at 1.
- `user_message` column stores the trimmed raw user text (not the assembled context). For Turn 1, Axe sees the assembled Markdown context via `opts.Prompt`; Turn 2+ sees the raw user text via the reconstructed conversation history. This is acceptable because the conversation history is a chat transcript, not a verbatim replay of prompts.
- Separate read concern: a distinct component reconstructs `[]runner.Message` from raw turn rows, rather than adding reconstruction methods to `TurnStore`.
- Hilt-agnostic persistence format: define an internal representation for `new_messages_json` that maps cleanly to/from `runner.Message`, rather than marshaling `runner.Message` directly.

**Approaches ruled out:**
- Storing full `[]runner.Message` snapshot per turn (rejected in milestone 001 in favor of delta-only for O(N) storage).
- Re-assembling full Markdown context for every turn (not required; conversation history plus Axe's static system prompt carries forward agent identity).
- Extending `TurnStore` with history reconstruction methods (rejected per user confirmation of option B).

**Constraints:**
- Axe v1.10.0+ with Messages API is required.
- SQLite via `modernc.org/sqlite`, pure Go.
- Existing `turns` table schema must be used as-is (no migration needed for this milestone).
- Backward compatibility: Turn 1 rows already in the database must be readable by the new reconstruction logic.
- The `user_message` column in the database is a raw string; Turn 1 rows may have been persisted with a minimal `new_messages_json` (only `role` and `content`). The new persistence format must remain compatible with these rows.

---

## Section 2: Requirements

### 2.1 Behavior: Turn 2+ Detection

When a normal (non-command) message arrives and `GetTurnCount` returns a value greater than zero, the processor **MUST** follow the Turn 2+ execution path.

- The Turn 1 path (Markdown context assembly + `opts.Prompt`) **MUST NOT** be used.
- The Turn 2+ path **MUST** rely on `opts.Messages` for conversation history.

### 2.2 Interface: TurnReader

A new read capability **MUST** be provided for loading all turns of a session in chronological order.

**Required behavior:**
- Accept a `session_id`.
- Return all `turns` rows for that session ordered by `turn_number` ascending.
- Each row exposes at minimum: `turn_number`, `user_message`, `new_messages_json`.
- Return an empty slice (not nil) when the session has zero turns.
- Return an error only for database query failures.

**Placement:** This read capability **SHOULD** be exposed by the same type that implements `TurnStore` (`session.Manager`), because it already owns the database connection and session schema knowledge. It **MUST NOT** be added to the `TurnStore` interface itself.

### 2.3 Interface: HistoryBuilder

A new component **MUST** reconstruct `[]runner.Message` from persisted turns.

**Required behavior:**
- Accept a `session_id` and produce a complete `[]runner.Message` representing the conversation so far.
- For each turn returned by `TurnReader`, in order:
  1. Append a user message with `Role: "user"` and `Content` set to the turn's `user_message` value.
  2. Append all messages deserialized from that turn's `new_messages_json`, preserving their order.
- Return an empty slice (not nil) if the session has zero turns.
- Return an error if any `new_messages_json` contains invalid JSON or a message with an unrecognized `role` value.

**Data mapping from persistence to `runner.Message`:**
The internal persistence format (the shape stored in `new_messages_json`) **MUST** support:
- `role` (string): `"user"`, `"assistant"`, or `"tool"`
- `content` (string): message text
- `tool_calls` (array of objects, optional): each with `id` (string), `name` (string), `arguments` (map of string to string)
- `tool_results` (array of objects, optional): each with `call_id` (string), `content` (string), `is_error` (boolean)

The mapping from persistence format to `runner.Message` **MUST** be lossless: every field in the persisted type round-trips to the equivalent field in `runner.Message`, `runner.ToolCall`, and `runner.ToolResult`.

### 2.4 Behavior: Turn 2+ Execution

When processing Turn 2+:

1. Reconstruct full conversation history via `HistoryBuilder`.
2. Append the current user message (trimmed raw text) to the reconstructed slice.
3. Call `runner.Run(ctx, opts)` where:
   - `opts.Messages` is set to the full slice from step 2.
   - `opts.Prompt` is empty.
   - All other options (`AgentName`, `AgentsDirs`, `Model`) remain the same as Turn 1.
4. Receive `result` from Axe.
5. Compute the delta — the suffix of `result.Messages` that was not in `opts.Messages`.
6. Convert the delta messages to the internal persistence format and marshal to JSON.
7. Record the turn: `RecordTurn(sessionID, turnCount+1, trimmedUserText, deltaJSON)`.
8. Update session tokens: `UpdateSessionTokens(sessionID, int64(result.InputTokens), int64(result.OutputTokens))`.
9. Update session activity: `UpdateSessionActivity()`.
10. Extract the reply text from `result` and send it to Telegram.

### 2.5 Behavior: Delta Computation

The delta is the set of messages added by Axe during the current turn.

**Computation rules:**
- Let `sent` be the slice passed in `opts.Messages`.
- Let `returned` be `result.Messages`.
- The delta **MUST** be `returned[len(sent):]` (the suffix starting at `len(sent)`).
- If `returned` is nil or empty (unexpected when `sent` was non-empty), the delta **MUST** fall back to a single assistant message with `Content` set to `result.Content`.
- If `len(returned) < len(sent)` (data corruption or unexpected Axe behavior), log an error and fall back to a single assistant message with `Content` set to `result.Content`.
- The delta **MUST NOT** include any messages that were already in `sent`.

### 2.6 Behavior: Tool Call Persistence

When a turn involves tool calls (Axe's underlying conversation loop produces them), the full tool round-trip **MUST** be persisted in the delta:

- The assistant message containing `tool_calls` **MUST** appear in the delta.
- The tool message(s) containing `tool_results` **MUST** appear in the delta, in the correct order after the assistant message that requested them.
- The final assistant message after tool results are processed **MUST** appear in the delta.
- All `tool_calls` entries in the persisted JSON **MUST** include `id`, `name`, and `arguments`.
- All `tool_results` entries in the persisted JSON **MUST** include `call_id`, `content`, and `is_error`.

### 2.7 Behavior: Telegram Reply Extraction

The text sent to Telegram **MUST** be derived from `result.Messages` or `result.Content`.

**Extraction rules:**
- If `result.Messages` is non-empty and the last message has `Role: "assistant"`, use its `Content`.
- If `result.Messages` is non-empty and the last message has `Role: "tool"`, walk backward to the most recent assistant message and use its `Content`.
- If `result.Messages` is empty/nil, use `result.Content`.
- The reply **MUST NOT** be constructed from tool call or tool result text.

### 2.8 Behavior: Session Metadata Updates

After every successful Turn 2+ (including tool-using turns):

- `UpdateSessionActivity()` **MUST** be called to refresh `last_activity`.
- `UpdateSessionTokens()` **MUST** be called with `result.InputTokens` and `result.OutputTokens`.
- The session title **MUST NOT** be modified if it is already set. If unset and this is the first turn, the title may be set from the raw user text (this is existing Turn 1 behavior and continues to apply).

### 2.9 Edge Cases

**Empty session (zero turns):**
- `HistoryBuilder` returns an empty slice.
- Execution follows the existing Turn 1 path. This behavior **MUST NOT** change.

**Single prior turn (Turn 1 in the database):**
- `HistoryBuilder` returns Turn 1's user message plus Turn 1's persisted assistant message.
- Turn 2 appends the new user message and proceeds normally.

**Database corruption (invalid JSON in `new_messages_json`):**
- `HistoryBuilder` returns an error.
- The processor logs the error.
- A Telegram message **MUST** be sent: `"Something went wrong loading conversation history."`
- No LLM call is made; no turn is recorded.

**Axe returns nil `Messages` (unexpected):**
- Log a warning.
- Fall back to `result.Content` as the delta (single assistant message).
- Send `result.Content` as the Telegram reply.

**User message empty after trimming:**
- An empty string **MUST** be persisted as `user_message`.
- The reconstructed user message has empty `Content`.
- The LLM call proceeds with an empty final user message.

**Large conversation history:**
- The full `[]runner.Message` is passed to Axe every turn. Token costs grow linearly with turn count.
- No truncation, summarization, or sliding window is performed in this milestone.

**Tool-using turn where Axe budget is exceeded mid-loop:**
- Axe returns `BudgetExceededError`. The error handling milestone (008) will map this to a Telegram reply.
- No turn is recorded because the run did not complete successfully.

---

## Test Behaviors

The following behaviors are prioritized for testing:

1. **History reconstruction from prior turns:** `HistoryBuilder` produces `[]runner.Message` in correct chronological order, with user messages interleaved with assistant/tool messages, matching the turn rows in the database.
2. **Delta computation:** Given a known `sent` slice and a mock `result.Messages`, the delta is exactly the suffix. Fallback to `result.Content` works when `result.Messages` is nil or shorter than `sent`.
3. **Tool call round-trip:** A turn containing assistant-with-tool-calls and tool-with-tool-results round-trips through JSON persistence and reconstruction without data loss (all IDs, names, arguments, call IDs, and `is_error` flags preserved).
4. **Turn numbering and metadata:** After Turn 2+, `turn_number` increments sequentially, `total_input_tokens` and `total_output_tokens` accumulate, and `last_activity` is updated.
5. **Graceful degradation:** When `result.Messages` is nil/empty, Turn 2+ falls back to `result.Content`, sends a Telegram reply, and persists a single-assistant delta.
