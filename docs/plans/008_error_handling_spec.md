# 008: Error Handling — Map Axe Errors to Telegram Replies

## Section 1: Context & Constraints

### Milestone Entry

> - [ ] **008: Error Handling — Map Axe errors to Telegram replies**
>   - Type-switch on `runner.Run()` error returns
>   - `ConfigError` → "Configuration issue: ..."
>   - `BudgetExceededError` → "⚠️ Token budget exceeded ..."
>   - `RuntimeError` + provider category (auth, rate limit, timeout, server) → appropriate user message
>   - Generic fallback → "I couldn't process that request: ..."
>   - Ensure all errors send a Telegram reply; never swallow silently

### Research Findings

**Codebase State:**
Milestone 007 (Turn 2+) is complete. The `Processor` in `internal/mainagent/processor.go` currently handles errors ad-hoc with inconsistent patterns:

- **Pre-flight errors** (`GetActiveSession`, `GetTurnCount`, `assembleContext`, `BuildMessages`): sends a hardcoded context-specific message (e.g., `"Something went wrong preparing your session."`, `"Something went wrong loading conversation history."`) and logs the raw error.
- **Turn 1 runner errors**: sends `"I couldn't process that request: "+err.Error()` — raw error text reaches the user.
- **Turn 2+ runner errors**: same raw error exposure.
- **Post-runner failures** (`marshal delta`, `RecordTurn`, `UpdateSessionTokens`, `UpdateSessionActivity`): logs the error but still sends the LLM reply to the user (non-blocking).
- **Nil result**: sends `"I couldn't process that request: unexpected empty result"`.

All error paths currently log raw errors. The gap is: **raw error text leaks to users**, and Axe-specific error types are not distinguished.

**Axe Typed Error API (from `pkg/runner/errors.go`):**
- `runner.ConfigError` — configuration problems (missing agent, invalid TOML, missing API key, invalid messages slice, etc.). Exposes `Msg string` and `Err error` (unwraps).
- `runner.BudgetExceededError` — token budget exceeded. Exposes `Used int`, `Max int`.
- `runner.RuntimeError` — runtime failures (provider call failure, tool execution failure, JSON marshal failure, etc.). Exposes `Msg string` and `Err error` (unwraps).
- `runner.IsConfigError(err)`, `runner.IsRuntimeError(err)`, `runner.IsBudgetExceededError(err)` — type assertions.
- `runner.ProviderCategory(err)` — returns `(provider.ErrorCategory, bool)` by unwrapping through `RuntimeError` to find a `*provider.ProviderError`.

**Provider Error Categories (from `internal/provider/provider.go`):**
- `ErrCategoryAuth` = `"auth"`
- `ErrCategoryRateLimit` = `"rate_limit"`
- `ErrCategoryTimeout` = `"timeout"`
- `ErrCategoryOverloaded` = `"overloaded"`
- `ErrCategoryBadRequest` = `"bad_request"`
- `ErrCategoryServer` = `"server"`

**Decisions Already Made:**
- Single-user personal assistant — exposing some error detail to the user is acceptable, but the user has explicitly requested sanitization (raw errors logged, clean messages sent).
- `Processor` already depends on `Messenger` to send replies; the error handling path must continue to use it.
- All errors in `Processor` are handled synchronously (no retry, no background queue).

**Approaches Ruled Out:**
- Middleware / decorator pattern for error handling — rejected because `Processor` is already a flat orchestrator; a dedicated mapper interface is simpler.
- Per-error-type Telegram formatting (Markdown, HTML) — out of scope for MVP; plain text only.
- Retry logic for transient errors — deferred to post-MVP.

**Constraints:**
- The `Processor` constructor already accepts many dependencies; adding `ErrorMapper` as an injected interface maintains testability without bloating the constructor signature unreasonably.
- Must be backward compatible: if `ErrorMapper` returns an empty string, the caller must fall back to a safe generic message.
- The mapper must never panic on `nil` input.
- Post-runner persistence errors (`RecordTurn`, `UpdateSessionTokens`, etc.) must continue to send the LLM reply to the user; the error is logged but the user already received their answer. Only the runner invocation itself blocks the reply.

---

## Section 2: Requirements

### 2.1 Interface: ErrorMapper

A new interface **MUST** be provided for mapping errors to user-facing Telegram messages.

```go
type ErrorMapper interface {
    Map(err error) string
}
```

**Required behavior:**
- Accept any `error` value, including `nil`.
- Return a non-empty, sanitized string suitable for Telegram display.
- The returned string **MUST NOT** contain raw error text, stack traces, or internal paths.
- When `err` is `nil`, return the generic fallback message (see 2.4).
- The implementation **MUST** be safe for concurrent use (no mutable state).

### 2.2 Interface: ErrorReporter (or Wrapper)

A unified error reporting routine **MUST** wrap all error paths in `Processor`.

**Required behavior:**
- Accept `context.Context`, `chatID int64`, `err error`, and an optional `fallbackMsg string`.
- Log the raw `err` at **Error** level with structured logging (`slog.Any("error", err)`).
- Resolve the user-facing message by calling `ErrorMapper.Map(err)`.
- If `Map` returns a non-empty string, use it.
- If `Map` returns empty or `err` is `nil`, use the provided `fallbackMsg`.
- If `fallbackMsg` is also empty, use the generic fallback: `"I couldn't process that request. Please try again."`
- Send the resolved message via `Messenger.SendMessage`.
- If `SendMessage` itself fails, log that failure at Error level and do not propagate (never panic, never swallow silently).

### 2.3 Axe Error Type Mapping

The `ErrorMapper` implementation **MUST** type-switch using `errors.As` (or Axe's helpers) and produce the following user-facing messages:

| Axe Error Type | Condition | User-Facing Message |
|----------------|-----------|---------------------|
| `runner.ConfigError` | Always | `"Configuration issue: please check your agent files, API keys, and model settings."` |
| `runner.BudgetExceededError` | Always | `"⚠️ Token budget exceeded. Try simplifying your request or increase the budget in your config."` |
| `runner.RuntimeError` wrapping `provider.ProviderError` with category `"auth"` | `runner.IsRuntimeError(err) && category == "auth"` | `"Authentication failed. Please check your API key configuration."` |
| `runner.RuntimeError` wrapping provider category `"rate_limit"` | category == `"rate_limit"` | `"Rate limited. Please wait a moment and try again."` |
| `runner.RuntimeError` wrapping provider category `"timeout"` | category == `"timeout"` | `"Request timed out. Please try again."` |
| `runner.RuntimeError` wrapping provider category `"server"` or `"overloaded"` | category in `{"server", "overloaded"}` | `"The AI service is experiencing issues. Please try again later."` |
| `runner.RuntimeError` wrapping provider category `"bad_request"` | category == `"bad_request"` | `"Invalid request. Please check your configuration."` |
| `runner.RuntimeError` without recognized provider category | `runner.IsRuntimeError(err)` but category not matched | `"Something went wrong. Please try again."` |
| Any other non-nil error | Fallback | `"I couldn't process that request. Please try again."` |
| `nil` | — | `"I couldn't process that request. Please try again."` |

**Additional rules:**
- The mapper **MUST** use `errors.As` (or Axe's `AsProviderError`) to unwrap through `RuntimeError` to find `*provider.ProviderError`.
- If a `RuntimeError` wraps both a provider error and another nested error, only the provider category determines the user message.
- The mapper **MUST NOT** leak the underlying `err.Error()` string into the user message.

### 2.4 Behavior: Runner Error Paths (Blocking)

When `runner.Run()` returns an error in either Turn 1 or Turn 2+:

1. Log the raw error at Error level.
2. Call `ErrorMapper.Map(err)`.
3. Send the mapped message to Telegram via `Messenger`.
4. **Do not** record a turn, update tokens, or update session activity.
5. Return `nil` from `ProcessTurn` (errors are surfaced to the user, not propagated to the router).

### 2.5 Behavior: Pre-Flight Error Paths (Blocking)

When an error occurs before `runner.Run()` (session lookup, turn count, context assembly, history building):

1. Log the raw error at Error level.
2. Send an appropriate sanitized message to Telegram.
3. The message **MAY** be context-specific (e.g., `"Something went wrong loading conversation history."`) provided by the caller, or the generic mapper output.
4. **Do not** call the runner.
5. Return `nil` from `ProcessTurn`.

Because all errors flow through the reporter, the raw error is always logged even when a context-specific fallback is used.

### 2.6 Behavior: Post-Runner Persistence Error Paths (Non-Blocking)

When `runner.Run()` succeeds but a subsequent step fails (`json.Marshal` delta, `RecordTurn`, `UpdateSessionTokens`, `UpdateSessionActivity`):

1. Log the raw error at Error level.
2. Still send the LLM reply to the user (the assistant's response is already available).
3. Do **not** send an additional Telegram error message for these cases; the user already has their answer.
4. The `ProcessTurn` may return the messenger error if Telegram dispatch itself fails (existing behavior preserved).

### 2.7 Behavior: Nil Result from Runner

When `runner.Run()` returns `nil, nil` or a `*runner.Result` that is `nil`:

1. Log at Error level: `"runner returned nil result"`.
2. Send the generic fallback message to Telegram.
3. Do not record a turn.

### 2.8 Behavior: Messenger Dispatch Failure

When `Messenger.SendMessage` fails while attempting to send an error reply:

1. Log the messenger error at Error level with the original error attached.
2. Do **not** attempt to send a second message (no cascading failures).
3. Return `nil` from `ProcessTurn`.

### 2.9 Edge Cases

**Wrapped Axe errors:**
If a `runner.RuntimeError` wraps a `runner.ConfigError` (unusual but possible via `fmt.Errorf` chains), `errors.As` for `ConfigError` **MUST** take precedence over `RuntimeError` because configuration issues are more actionable.

**Unknown provider category:**
If `runner.ProviderCategory` returns a category string not in the table (e.g., a future Axe category), treat it as a generic `RuntimeError` fallback: `"Something went wrong. Please try again."`.

**Empty ErrorMapper.Map return:**
If a custom `ErrorMapper` implementation returns `""`, the reporter **MUST** fall back to the generic message. This defends against buggy mapper implementations.

**`BudgetExceededError` with zero values:**
If `Used` and `Max` are both zero, the same budget-exceeded message is sent (do not attempt to format empty numbers).

**Concurrent sessions (theoretical):**
`ErrorMapper` must be stateless, so concurrent `ProcessTurn` calls share the same mapper safely.

**Non-Axe, non-DB errors:**
`context.Canceled`, `context.DeadlineExceeded`, and generic `fmt.Errorf` errors all fall through to the generic fallback message. The raw error ("context deadline exceeded") is logged, and the user sees "I couldn't process that request. Please try again."

---

## Test Behaviors

The following behaviors are prioritized for testing:

1. **Type-switch coverage**: Each Axe error type (`ConfigError`, `BudgetExceededError`, `RuntimeError` with each provider category, `RuntimeError` without category) produces the correct user-facing string, and the raw error text does not appear in the mapped message.
2. **Provider category unwrapping**: A `RuntimeError` wrapping a `*provider.ProviderError` with category `"rate_limit"` correctly resolves to the rate-limit message, verified via `errors.As` depth.
3. **Never silent**: When `runner.Run` returns any error, a Telegram message is always sent. When the messenger itself fails, the failure is logged.
4. **Fallback safety**: `ErrorMapper.Map(nil)` and `Map` on an unrecognized error type both return the generic fallback. An empty-string mapper result triggers the fallback.
5. **Pre-flight vs post-runner distinction**: A runner error blocks the turn and sends an error reply. A post-runner `RecordTurn` error logs but still sends the LLM reply.
6. **Wrapped errors**: A `ConfigError` wrapped in `fmt.Errorf` is still detected by `errors.As` and maps to the config message.
