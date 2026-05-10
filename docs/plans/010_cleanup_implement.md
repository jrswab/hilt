# Milestone 010: Cleanup — Implementation Guide

## Section 1: Context Summary

**Associated milestone document:** `docs/plans/000_hilt_milestones.md`

Milestone 010 is a pure cleanup and hygiene pass over the fully-functional MVP codebase built in milestones 001–009. No new features are added. The goal is to harden the existing code through logging consistency review, error path verification, a one-time secret audit, expanded configuration edge-case tests, repository hygiene (`go mod tidy`, `go vet`, artifact removal), and documentation (`README.md`, `CHANGELOG.md`). Post-MVP groundwork is documentation-only; no new packages or interfaces are created. The interactive `install.sh` must not be modified.

---

## Section 2: Implementation Checklist

### Group 1: Configuration Tests (R8)

These are the highest-priority testing tasks. All tests belong in `internal/config/config_test.go`.

- [x] **Add test `TestLoad/config_path_is_a_directory`**
  - File: `internal/config/config_test.go`
  - Create a directory at the config path, call `Load()`, and assert the error wraps `internal.ErrInvalidConfig`.

- [x] **Add test `TestLoad/missing_bot_token_in_file_and_env`**
  - File: `internal/config/config_test.go`
  - Write a valid config file with `telegram_bot_token = ""`, ensure `TELEGRAM_BOT_TOKEN` env var is unset, call `Load()`, and assert the error wraps `internal.ErrInvalidConfig`.

- [x] **Add test `TestLoad/workspace_dir_with_leading_tilde`**
  - File: `internal/config/config_test.go`
  - Write a config file with `workspace_dir = "~/.hilt-test"`, set `TELEGRAM_BOT_TOKEN`, call `Load()`, and assert `cfg.WorkspaceDir` equals the expanded path (e.g., `/home/user/.hilt-test`).

- [x] **Strengthen test `TestLoad/missing_file_triggers_auto_creation`**
  - File: `internal/config/config_test.go`
  - Existing test sets `TELEGRAM_BOT_TOKEN=test-token` but never asserts `cfg.TelegramBotToken`. Add an assertion that `cfg.TelegramBotToken == "test-token"`.

- [x] **Strengthen test `TestLoad/empty_file_triggers_auto_creation`**
  - File: `internal/config/config_test.go`
  - Same gap as above: add assertion that `cfg.TelegramBotToken == "test-token"`.

- [x] **Run config package tests**
  - Command: `go test ./internal/config/...`
  - All tests must pass before proceeding.

---

### Group 2: Logging Level Consistency (R1)

Review and adjust log levels in the following locations. The principle: `Error` is for unrecoverable request failures; `Warn` is for recoverable anomalies where processing continues (e.g., token update fails but reply still sent).

- [x] **Adjust `cmd/hilt/main.go: startSessionManager()` stale-session log level**
  - Change `logger.Info("archiving stale session", ...)` to `logger.Warn("archiving stale session", ...)`. Archiving a stale session is a recoverable anomaly.

- [x] **Adjust `internal/mainagent/processor.go: ProcessTurn()` token update log level**
  - Change `p.logger.Error("update session tokens failed", ...)` to `p.logger.Warn(...)`. The turn reply is still sent to the user.

- [x] **Adjust `internal/mainagent/processor.go: ProcessTurn()` activity update log level**
  - Change `p.logger.Error("update session activity failed", ...)` to `p.logger.Warn(...)`. The turn reply is still sent.

- [x] **Adjust `internal/mainagent/processor.go: ProcessTurn()` title update log level**
  - Change `p.logger.Error("set session title failed", ...)` to `p.logger.Warn(...)`. Title is non-critical; processing continues.

- [x] **Adjust `internal/mainagent/processor.go: processTurn2Plus()` token update log level**
  - Change `p.logger.Error("update session tokens failed", ...)` to `p.logger.Warn(...)`. The turn reply is still sent.

- [x] **Adjust `internal/mainagent/processor.go: processTurn2Plus()` activity update log level**
  - Change `p.logger.Error("update session activity failed", ...)` to `p.logger.Warn(...)`. The turn reply is still sent.

- [x] **Verify no `fmt.Print`/`fmt.Println`/`fmt.Printf` outside `main` flag parsing**
  - Scan all `.go` files. The only non-`slog` output should be the `fmt.Fprintf(os.Stderr, ...)` in `cmd/hilt/main.go` during flag validation, which is acceptable.

---

### Group 3: Error Path Coverage (R2)

Ensure every discarded error is logged. Focus on messenger send failures in the router.

- [x] **Log discarded messenger error in `internal/server/server.go: Router.handleNewCmd()`**
  - The line `_ = r.messenger.SendMessage(ctx, chatID, "Something went wrong starting a new session.")` discards the error. Capture and log it at `Error` level.

- [x] **Log discarded messenger error in `internal/server/server.go: Router.handleSessionsCmd()`**
  - The line `_ = r.messenger.SendMessage(ctx, chatID, "Could not list archived sessions.")` discards the error. Capture and log it at `Error` level.

- [x] **Log discarded messenger error in `internal/server/server.go: Router.sendUnknownCommand()`**
  - The line `_ = r.messenger.SendMessage(ctx, chatID, "Command not found...")` discards the error. Capture and log it at `Warn` level (command response is best-effort).

- [x] **Log discarded messenger error in `internal/server/server.go: Router.handleNormalMessage()`**
  - Both fallback messages (`"Something went wrong processing your message."` and `"Message received. The main agent is not yet online."`) discard `SendMessage` errors. Capture and log at `Error` level.

- [x] **Verify `internal/mainagent/processor.go: reportError()` does not recurse**
  - Confirm that `reportError` calls `p.messenger.SendMessage` directly and logs failures with `p.logger.Error`. There is no path back to `reportError`.

- [x] **Verify nil runner result produces both log and user message**
  - Confirm both `ProcessTurn` and `processTurn2Plus` call `p.reportError(ctx, chatID, nil, "")` when `result == nil`, which logs and sends the fallback.

---

### Group 4: Hardcoded Secret Audit (R3)

One-time manual review. Document findings.

- [x] **Audit all tracked files for hardcoded secrets**
  - Scope: all `.go`, `.sh`, `.md`, `.toml`, `.mod`, `.sum` files and any binary artifacts in the repo root.
  - Patterns to check: `sk-`, `AKIA`, BotFather token format, connection strings, personal paths.
  - Document outcome in a brief note (e.g., `docs/plans/010_secret_audit.md` or as a comment block in this guide).

---

### Group 5: Documentation (R4, R5, R9)

- [x] **Rewrite `README.md`**
  - File: `README.md`
  - Replace the existing one-line stub entirely. Required sections: Overview, Requirements, Installation, Building, Running, Configuration, Architecture, Future Work.

- [x] **Create `CHANGELOG.md`**
  - File: `CHANGELOG.md`
  - Initial `[0.1.0]` section with one bullet per milestone 001–009, following Keep a Changelog format.

---

### Group 6: Repository Hygiene (R7)

- [x] **Run `go mod tidy`**
  - Commit any changes to `go.mod` or `go.sum`.

- [x] **Run `go vet ./...` and fix findings**
  - Must exit with code 0 and no output.

- [x] **Remove stale compiled test artifact**
  - File: `mainagent.test` in repository root.
  - Remove from filesystem and from git index if tracked.

- [x] **Verify no unused imports or dead code**
  - `go build ./...` must succeed (catches unused imports).
  - Scan for commented-out code blocks older than this milestone; remove if found.

- [x] **Run full test suite**
  - Command: `go test ./...`
  - All packages must pass.

---

### Group 7: Version Tag (R6)

- [x] **Create annotated git tag `v0.1.0`**
  - Tag message must reference `CHANGELOG.md`.
  - Tag must point to the commit that completes this milestone.
