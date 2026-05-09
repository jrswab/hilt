# 004: Telegram — Implementation Guide

## Section 1: Context Summary

**Milestone:** `000_hilt_milestones.md` — 004: Telegram — Long-polling loop with `gotgbot/v2`

Milestones 001–003 established the Go module, config loading, and SQLite session lifecycle. Now Hilt needs a live Telegram connection: initialize a bot with the token from config, start a 60-second long-polling loop, route incoming text messages to an application-level `MessageHandler` callback, silently drop messages from unauthorized users, auto-reply with "Hilt only processes text and voice messages." for unsupported message types, provide a `SendMessage` primitive for outgoing replies, and shut down cleanly when the root context is cancelled by SIGINT/SIGTERM. Command parsing is intentionally deferred to milestone 005; 004 routes all text to a single handler stub.

---

## Section 2: Implementation Checklist

### Dependency

- [x] `go.mod` / `go.sum`: Add `github.com/PaulSonOfLars/gotgbot/v2` and run `go mod tidy` to update module files.

### Core Types

- [x] `internal/telegram/telegram.go`: Define `MessageHandler` type alias — `func(ctx context.Context, chatID int64, text string) error`.
- [x] `internal/telegram/telegram.go`: Define `Bot` struct with fields `api *gotgbot.Bot` and `allowed []int64`.

### Bot Initialization

- [x] `internal/telegram/telegram.go`: Implement `NewBot(token string, allowed []int64) (*Bot, error)` — validate token is non-empty, create `gotgbot.NewBot`, call `GetMe` to verify connectivity, return error on any failure, otherwise return `*Bot`.
- [x] `internal/telegram/telegram_test.go`: Test `NewBot` with empty token returns error without network call.
- [x] `internal/telegram/telegram_test.go`: Test `NewBot` with invalid/rejected token returns error (integration-style test against real Telegram API; skip or guard with build tag if network unavailable).

### Outgoing Messages

- [x] `internal/telegram/telegram.go`: Implement `(*Bot) SendMessage(ctx context.Context, chatID int64, text string) error` — delegates to `b.api.SendMessage` with plain text, no parse mode, returns wrapped errors.

### Message Classification & Routing

- [x] `internal/telegram/telegram.go`: Implement `isAuthorized(userID int64) bool` on `*Bot` — returns `true` if `allowed` slice is empty or contains `userID`.
- [x] `internal/telegram/telegram.go`: Implement `isTextMessage(msg *gotgbot.Message) bool` — unexported helper that returns `true` when the message has no media fields (photo, voice, audio, video, document, sticker, location, contact) and is therefore a text-only update (including empty body).
- [x] `internal/telegram/telegram.go`: Implement `handleUpdate(rootCtx context.Context, ctx *ext.Context, handler MessageHandler) error` on `*Bot` — checks `EffectiveMessage` is non-nil and `From` is non-nil; if unauthorized silently returns `nil`; if `isTextMessage` delegates `msg.Text` to `handler`; otherwise sends the unsupported auto-reply via `SendMessage`.
- [x] `internal/telegram/telegram_test.go`: Test `isAuthorized` with empty allowed list (passes any ID), populated list (passes matching ID, rejects unknown ID).
- [x] `internal/telegram/telegram_test.go`: Test `isTextMessage` with photo/voice/document/sticker/location/contact messages returns `false`; with plain text (including empty string) returns `true`.
- [x] `internal/telegram/telegram_test.go`: Test `handleUpdate` with unauthorized user ID — verify `handler` is not invoked and `SendMessage` is not called.
- [x] `internal/telegram/telegram_test.go`: Test `handleUpdate` with authorized text message — verify `handler` is called with correct `chatID` and `text`.
- [x] `internal/telegram/telegram_test.go`: Test `handleUpdate` with authorized unsupported message — verify auto-reply `SendMessage` is called with exact text "Hilt only processes text and voice messages.".
- [x] `internal/telegram/telegram_test.go`: Test `handleUpdate` with `EffectiveMessage == nil` or `From == nil` — returns `nil` without side effects.

### Polling Loop & Lifecycle

- [x] `internal/telegram/telegram.go`: Implement `(*Bot) Start(ctx context.Context, handler MessageHandler) error` — create `ext.NewDispatcher` with a minimal error callback (no-op for MVP), add a `handlers.NewMessage(filters.Message.All, …)` handler that routes through `handleUpdate` with the root `ctx`, create `ext.NewUpdater`, call `updater.StartPolling` with 60-second timeout and `DropPendingUpdates: true`, block on `<-ctx.Done()`, then `updater.Stop` and return `nil`.
- [x] `internal/telegram/telegram_test.go`: Test `Start` stops cleanly when context is cancelled — create a context with timeout/cancel, call `Start` in a goroutine, cancel context, verify `Start` returns without error within a bounded window.

### Application Wire-Up

- [x] `cmd/hilt/main.go`: After `startSessionManager` succeeds, add call to `telegram.NewBot(cfg.TelegramBotToken, cfg.AllowedUserIDs)`. On error, log and `os.Exit(1)`.
- [x] `cmd/hilt/main.go`: Define a `MessageHandler` stub that logs incoming messages via `logger.Info` (real routing is milestone 005).
- [x] `cmd/hilt/main.go`: Call `bot.Start(ctx, stubHandler)` after bot initialization. It blocks until signal cancellation; the existing `<-ctx.Done()` and shutdown logging continue to work.
- [x] `cmd/hilt/main.go`: Ensure `mgr.Close()` `defer` runs after `bot.Start` returns (existing `defer mgr.Close()` in `main` is sufficient; confirm ordering is unchanged).

### Edge-Case Guards

- [x] `internal/telegram/telegram.go`: `Start` creates a fresh dispatcher and updater per call, so no double-registration occurs (singleton enforcement by `main.go` lifecycle).
- [x] `internal/telegram/telegram.go`: Dispatcher error callback returns `ext.DispatcherActionNoop` for all errors (no-re-register behavior).
