# 004: Telegram — Long-polling loop with `gotgbot/v2`

## Section 1: Context & Constraints

### Milestone Entry
> **004: Telegram — Long-polling loop with `gotgbot/v2`**
> - Initialize bot with token from config
> - Start polling dispatcher with 60s timeout
> - Handle text messages: route to message processor
> - Handle unsupported message types: auto-reply "Hilt only processes text and voice messages."
> - Implement `SendMessage` wrapper
> - Graceful shutdown on SIGINT/SIGTERM

### Codebase State
Milestone 003 (Database) is complete. The repository now contains:
- `cmd/hilt/main.go` — loads config, initializes session manager, starts active session, wires SIGINT/SIGTERM cancellation
- `internal/config/config.go` — `Config` struct with `TelegramBotToken` (string) and `AllowedUserIDs []int64`
- `internal/session/session.go` — `Manager` and `Session` types; DB schema initialized
- `internal/telegram/telegram.go` — empty stub with package doc only
- `internal/server/server.go` — empty stub with package doc only (webhook mode; not used in MVP)

### Decisions Already Made
| Decision | Resolution | Rationale |
|----------|------------|-----------|
| Telegram library | **`gotgbot/v2`** | Modern, active maintenance, good `context.Context` support. Locked in. |
| Polling mode | **Long-polling** (60s timeout) | No public IP/TLS required. Default for MVP. |
| Voice transcription | **Deferred post-MVP** | whisper.cpp + ffmpeg not included in MVP. |
| Single-user concurrency | **Single goroutine** | No concurrent message processing needed for MVP. |
| Allowed users | **`config.AllowedUserIDs`** | Empty slice = no restriction (development). |
| Error mapping | **Telegram replies for Axe errors** | Decided in 000; 004 implements the generic `SendMessage` primitive used later. |

### Approaches Ruled Out
| Approach | Why Rejected |
|----------|-------------|
| Raw HTTP for Telegram | Rejected in favor of `gotgbot/v2`. |
| Webhook mode in MVP | Deferred; no TLS/public IP assumed. |
| Multi-user filtering | Single-user design; `AllowedUserIDs` is a coarse gate, not per-session auth. |

### Constraints & Assumptions
- **`gotgbot/v2`** is the chosen library; the spec describes what it must expose, not how to wire internal types.
- **Bot token** comes from `cfg.TelegramBotToken`, already validated as non-empty by config loading.
- **Allowed user list** comes from `cfg.AllowedUserIDs`. Empty means unrestricted.
- **No Markdown or HTML parsing** for outgoing messages in MVP. Plain text only.
- **Context cancellation** originates from `signal.NotifyContext` in `main.go` (SIGINT/SIGTERM). The Telegram layer must respect it.
- **Session manager is already initialized** before the Telegram loop starts.
- **Voice messages are unsupported in MVP** even though the auto-reply text mentions voice aspirationally (transcription is post-MVP).

### Open Questions Resolved
1. **Should 004 parse `/command`?** → No. 004 routes all text to a single handler callback. Command parsing is milestone 005.
2. **Where does allowed-user filtering happen?** → In 004, before invoking the handler.
3. **Does 004 handle voice?** → No; voice is treated as an unsupported message type for MVP.
4. **Should `server` package own polling?** → No. `internal/telegram` owns the Bot API connection. `main.go` wires it directly. The `server` package remains a webhook-mode placeholder.

---

## Section 2: Requirements

### R1: Bot Initialization
Create a Telegram bot client using the configured token.

**Behaviors:**
- Accept a bot token string and produce an initialized bot handle.
- Validate connectivity with the Telegram API (e.g., fetch bot identity). Return an error if the token is invalid or the API is unreachable.
- Return an error if the token is empty.

**Edge cases:**
- R1-E1: Token is empty — return error immediately (defensive; config should already prevent this).
- R1-E2: Token valid but Telegram API temporarily unreachable — return error; caller logs and exits.
- R1-E3: Token rejected by Telegram (unauthorized) — return error with clear message.

---

### R2: Start Long-Polling Loop
Enter a blocking loop that receives updates from Telegram via long-polling.

**Behaviors:**
- Accept a `context.Context`. The loop runs until the context is cancelled.
- Use a 60-second timeout for each long-polling request (design default).
- Automatically resume after transient network errors (connection reset, timeout) without crashing.
- When the context is cancelled, stop accepting new updates and return from the start method.
- Allow in-flight message handlers to complete before returning, but do not start new ones after cancellation.

**Edge cases:**
- R2-E1: Network outage mid-poll — resume polling when network recovers; propagate persistent failures after N retries? No: let the library handle backoff. Spec requirement: do not crash on transient errors.
- R2-E2: Context cancelled while waiting for an update — exit cleanly within a reasonable window (bound by the 60s poll timeout).
- R2-E3: Two consecutive `Start` calls — undefined behavior in MVP; singleton enforced by `main.go` lifecycle.

---

### R3: Text Message Routing
When a text message arrives, forward it to the application layer.

**Behaviors:**
- Extract the sender's user ID, chat ID, and message text.
- If `AllowedUserIDs` is non-empty and the sender's user ID is not in the list, drop the message silently (no reply, no logging beyond debug).
- If authorized (or unrestricted), invoke a caller-provided `MessageHandler` callback with:
  - `context.Context` (linked to the main cancellation context)
  - `chatID int64`
  - `text string`
- The callback is responsible for any reply. The Telegram package does not automatically ack text messages beyond the HTTP-level Telegram API ack.

**Interface contract (specification, not implementation):**
```go
// MessageHandler processes an incoming text message.
// The implementation (milestone 005+) parses commands vs. normal messages.
type MessageHandler func(ctx context.Context, chatID int64, text string) error
```

**Edge cases:**
- R3-E1: Empty text message (e.g., caption-only photo without text) — does not apply; non-text messages are handled by R4. A text message with empty string body is passed through to the handler as empty string. Handler decides.
- R3-E2: Message from a channel or group — if `AllowedUserIDs` is used, filter by the sender user's ID. If no sender user ID, drop silently.

---

### R4: Unsupported Message Type Handling
For every incoming update that is **not** a text message, send an auto-reply.

**Behaviors:**
- Detect the message type from the update payload.
- If the update is not a text message (including photos, videos, documents, stickers, location, contact, voice, audio, etc.), send the exact plain-text reply to the originating chat:
  > "Hilt only processes text and voice messages."
- Do not invoke the `MessageHandler` for unsupported types.

**Edge cases:**
- R4-E1: Bot is added to a group or channel (service message) — no reply; ignore non-text service updates.
- R4-E2: Voice message received — send the auto-reply (MVP limitation: voice transcription is deferred post-MVP).
- R4-E3: Edit of a previous message — ignore edits in MVP. No reply.
- R4-E4: Unsupported message from unauthorized user — still send the auto-reply (informative), or silently drop? **Decision:** silently drop unauthorized messages regardless of type. Filtering (R3) takes precedence.

---

### R5: Outgoing Message Delivery
Provide a primitive to send a text reply to a specific chat.

**Behaviors:**
- Accept a `context.Context`, a `chatID int64`, and a `text string`.
- Deliver the text to the specified Telegram chat.
- Return an error if delivery fails (network, Telegram API error, chat not found).
- Send messages as plain text; no special parse mode required for MVP.

**Edge cases:**
- R5-E1: Empty text string — attempt to send anyway (Telegram API will reject); return any error.
- R5-E2: Context cancelled before response — return the context error.
- R5-E3: Chat ID does not exist or bot was blocked — return the Telegram API error unwrapped.

---

### R6: Graceful Shutdown
The Telegram layer must shut down cleanly as part of application teardown.

**Behaviors:**
- When the root context is cancelled (SIGINT/SIGTERM), the polling loop stops initiating new long-polling requests.
- In-flight `MessageHandler` callbacks and `SendMessage` calls should observe the cancelled context and wrap up quickly.
- The `Start` method returns without error after the loop is fully stopped.
- The `main.go` `defer mgr.Close()` and shutdown logging continue to work as before.

**Edge cases:**
- R6-E1: Handler hangs indefinitely — caller relies on SIGKILL beyond the graceful window. No watchdog in MVP.

---

### R7: Integration into `main.go` Wire-Up
`cmd/hilt/main.go` must start the Telegram loop after config and session manager are ready.

**Behaviors:**
- After `startSessionManager` succeeds, initialize the Telegram bot with `cfg.TelegramBotToken`.
- Start the polling loop with the root `ctx` and a `MessageHandler` stub that logs received messages (real routing is milestone 005).
- If bot initialization fails, log error and exit with code 1.
- Log at `info` level when polling starts.
- Wait for `<-ctx.Done()` as already implemented; Telegram `Start` blocks until then.

**Edge cases:**
- R7-E1: Session manager succeeds but Telegram loop fails — exit code 1; session manager is closed via `defer`.

---

## Behaviors to Prioritize for Testing

1. **Bot initialization with valid / invalid token** — verify connectivity check and error paths.
2. **Polling loop start and stop** — verify clean shutdown on context cancellation.
3. **Text message delivery to handler** — verify handler is invoked with correct chatID and text.
4. **Unsupported message type auto-reply** — verify the exact reply text is sent for a non-text update.
5. **Allowed-user filtering** — verify unauthorized messages are silently dropped; authorized ones pass through.
6. **SendMessage wrapper** — verify it delegates to the underlying API and returns errors.
