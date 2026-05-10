# 008: Error Handling — Implementation Guide

## Section 1: Context Summary

**Associated milestone:** `docs/plans/008_error_handling_spec.md`

The `Processor` currently leaks raw error text directly to Telegram users (e.g., `"I couldn't process that request: "+err.Error()`). This milestone introduces a typed `ErrorMapper` that translates Axe's structured errors (`ConfigError`, `BudgetExceededError`, `RuntimeError` with provider categories) into clean, user-facing messages, while always logging the raw error. A unified `reportError` method on `Processor` replaces all ad-hoc error handling paths so that no error is swallowed silently and no raw error text reaches the user. Post-runner persistence errors continue to send the LLM reply to the user (non-blocking) as established behavior.

## Section 2: Implementation Checklist

### ErrorMapper Interface & Implementation

- [x] **Create `internal/mainagent/errormapper.go`**
  - Define `ErrorMapper` interface with `Map(err error) string`
  - Define `AxeErrorMapper` struct (stateless, safe for concurrent use)
  - Implement `(*AxeErrorMapper) Map(err error) string` with full type-switch using `runner.IsConfigError`, `runner.IsBudgetExceededError`, `runner.IsRuntimeError`, and `runner.ProviderCategory` / `errors.As`
  - Map each error type to the exact user-facing string from the spec table
  - Return generic fallback `"I couldn't process that request. Please try again."` for `nil`, unrecognized errors, and empty results
  - Handle `ConfigError` precedence over `RuntimeError` for wrapped errors
  - Handle unknown provider categories as generic RuntimeError fallback
  - Import `"github.com/jrswab/axe/pkg/runner"` and `"github.com/jrswab/axe/internal/provider"`

- [x] **Create `internal/mainagent/errormapper_test.go`**
  - `TestMapConfigError`: assert `Map` returns config message for `*runner.ConfigError`
  - `TestMapBudgetExceededError`: assert budget-exceeded message with zero and non-zero `Used`/`Max`
  - `TestMapRuntimeErrorAuth`: assert auth message when `runner.ProviderCategory` returns `"auth"`
  - `TestMapRuntimeErrorRateLimit`: assert rate-limit message for `"rate_limit"`
  - `TestMapRuntimeErrorTimeout`: assert timeout message for `"timeout"`
  - `TestMapRuntimeErrorServer`: assert server message for `"server"`
  - `TestMapRuntimeErrorOverloaded`: assert server message for `"overloaded"`
  - `TestMapRuntimeErrorBadRequest`: assert bad-request message for `"bad_request"`
  - `TestMapRuntimeErrorUnknownCategory`: assert generic `"Something went wrong. Please try again."` for unlisted category
  - `TestMapRuntimeErrorWithoutProviderError`: assert generic runtime fallback when no provider error is wrapped
  - `TestMapNilError`: assert generic fallback for `nil`
  - `TestMapPlainError`: assert generic fallback for non-Axe `errors.New("foo")`
  - `TestMapWrappedConfigError`: assert `errors.As` finds `ConfigError` through `fmt.Errorf("...: %w", configErr)`
  - Verify the mapped message does NOT contain the raw `err.Error()` text in any test

### Processor Integration

- [x] **Update `internal/mainagent/processor.go: Processor` struct**
  - Add `mapper ErrorMapper` field

- [x] **Update `internal/mainagent/processor.go: NewProcessor()`**
  - Add `mapper ErrorMapper` parameter
  - Add `nil` panic check for `mapper`
  - Assign `mapper` to `p.mapper`

- [x] **Add `internal/mainagent/processor.go: reportError()`**
  - Signature: `(p *Processor) reportError(ctx context.Context, chatID int64, err error, fallbackMsg string)`
  - Log raw `err` at Error level with `slog.Any("error", err)`
  - Resolve user-facing message: `p.mapper.Map(err)`
  - If mapper returns empty or `err` is `nil`, use `fallbackMsg` if non-empty; otherwise generic fallback `"I couldn't process that request. Please try again."`
  - Call `p.messenger.SendMessage(ctx, chatID, msg)` and log any messenger failure at Error level (do not propagate)
  - Return nothing (errors surfaced to user, not propagated)

- [x] **Update `internal/mainagent/processor.go: ProcessTurn()` — pre-flight errors**
  - Replace all ad-hoc `p.messenger.SendMessage` + `p.logger.Error` blocks for `GetActiveSession`, `GetTurnCount`, and `assembleContext` errors with `p.reportError(ctx, chatID, err, "Something went wrong preparing your session.")`

- [x] **Update `internal/mainagent/processor.go: ProcessTurn()` — runner error (Turn 1)**
  - Replace `"I couldn't process that request: "+err.Error()` with `p.reportError(ctx, chatID, err, "")`

- [x] **Update `internal/mainagent/processor.go: ProcessTurn()` — nil result (Turn 1)**
  - Replace ad-hoc logging + messenger call with `p.reportError(ctx, chatID, nil, "")` (mapper handles nil → generic fallback)

- [x] **Update `internal/mainagent/processor.go: processTurn2Plus()` — pre-flight error (`BuildMessages`)**
  - Replace ad-hoc block with `p.reportError(ctx, chatID, err, "Something went wrong loading conversation history.")`

- [x] **Update `internal/mainagent/processor.go: processTurn2Plus()` — runner error (Turn 2+)**
  - Replace `"I couldn't process that request: "+err.Error()` with `p.reportError(ctx, chatID, err, "")`

- [x] **Update `internal/mainagent/processor.go: processTurn2Plus()` — nil result (Turn 2+)**
  - Replace ad-hoc block with `p.reportError(ctx, chatID, nil, "")`

- [x] **Verify post-runner persistence paths remain unchanged**
  - `json.Marshal` delta, `RecordTurn`, `UpdateSessionTokens`, `UpdateSessionActivity` errors must continue to log and send `result.Content` / `extractReply(result)` to the user (no `reportError` call)
  - These should NOT send a Telegram error message; only the LLM reply

### Processor Tests

- [x] **Update `internal/mainagent/processor_test.go: TestProcessTurnRunnerError`**
  - Change `fakeRunner.err` to a plain `fmt.Errorf("API rate limit")`
  - Assert `messenger.lastText` equals generic fallback `"I couldn't process that request. Please try again."` (sanitized, no raw error text)
  - Assert no turn recorded

- [x] **Add `internal/mainagent/processor_test.go: TestProcessTurnRunnerConfigError`**
  - Set `fakeRunner.err` to `&runner.ConfigError{Msg: "missing agent"}`
  - Assert messenger text is `"Configuration issue: please check your agent files, API keys, and model settings."`
  - Assert no turn recorded

- [x] **Add `internal/mainagent/processor_test.go: TestProcessTurnRunnerBudgetExceededError`**
  - Set `fakeRunner.err` to `&runner.BudgetExceededError{Used: 150, Max: 100}`
  - Assert messenger text contains `"⚠️ Token budget exceeded"`
  - Assert no turn recorded

- [x] **Add `internal/mainagent/processor_test.go: TestProcessTurnRunnerRuntimeErrorRateLimit`** (covered by TestProcessTurnRunnerRuntimeError)
  - Set `fakeRunner.err` to `&runner.RuntimeError{Msg: "rate limited", Err: &provider.ProviderError{Category: provider.ErrCategoryRateLimit}}`
  - Assert messenger text is `"Rate limited. Please wait a moment and try again."`
  - Assert no turn recorded

- [x] **Add `internal/mainagent/processor_test.go: TestProcessTurnRunnerRuntimeErrorAuth`** (covered by errormapper_test TestMapRuntimeErrorAuth)
  - Set `fakeRunner.err` to runtime error wrapping provider auth error
  - Assert appropriate auth message

- [x] **Add `internal/mainagent/processor_test.go: TestProcessTurnNilResult`**
  - Assert messenger text is generic fallback (no `"unexpected empty result"` raw text)
  - Assert no turn recorded

- [x] **Update `internal/mainagent/processor_test.go: TestProcessTurnTurn2PlusHistoryError`**
  - Assert messenger text still contains history-specific fallback or generic message
  - Verify the error is sanitized (no raw DB error text exposed)

- [x] **Update `internal/mainagent/processor_test.go: TestProcessTurnSuccess` and all Turn 2+ tests**
  - Update `NewProcessor` calls to pass an `AxeErrorMapper{}` as the new parameter

- [x] **Add `internal/mainagent/processor_test.go: TestReportErrorMessengerFailureLogged`**
  - Set `fakeMessenger.err = fmt.Errorf("network down")`
  - Trigger a runner error via `ProcessTurn`
  - Assert `ProcessTurn` returns `nil` (no propagation)
  - Assert the messenger failure is handled silently (no panic, no second message attempt)

- [x] **Add `internal/mainagent/processor_test.go: TestNewProcessorPanicsOnNilMapper`** (covered by TestNewProcessorPanicsOnNil)
  - Pass `nil` for `mapper` in `NewProcessor`
  - Assert panic

- [x] **Update `internal/mainagent/processor_test.go: TestNewProcessorPanicsOnNil`**
  - Add mapper nil case to the existing panic table

### Wire-up (if applicable)

- [x] **Update callsite that constructs `Processor`** (if any outside tests)
  - Pass `&mainagent.AxeErrorMapper{}` as the mapper argument to `NewProcessor`
  - Check `cmd/hilt/main.go` or equivalent bootstrap location
