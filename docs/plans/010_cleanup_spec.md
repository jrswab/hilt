# Milestone 010: Cleanup — Specification

## Section 1: Context & Constraints

### Milestone Entry

> **010: Cleanup — Code review, logging polish, and repository hygiene**
> - Review all error paths and logging levels
> - Ensure no hardcoded secrets or tokens in code
> - Add README.md with build and run instructions
> - Tag initial version or create release notes
> - Prepare groundwork for post-MVP features (voice, workflows, skills, memory maintenance)

### Research Findings Relevant to This Milestone

**Codebase State**
The project is a greenfield Go repo containing the following packages, all implemented during milestones 001–009:
- `cmd/hilt` — entry point with version constant `0.1.0`
- `internal/config` — TOML parsing, env var fallback, path expansion, first-run defaults
- `internal/telegram` — `gotgbot/v2` long-polling wrapper
- `internal/session` — SQLite schema, session lifecycle, turn journaling
- `internal/memory` — workspace file reader (AGENTS.md, critical.md, daily notes)
- `internal/server` — command router (`/new`, `/sessions`, unknown commands)
- `internal/mainagent` — context assembly, turn 1 vs turn 2+ processing, delta persistence, error mapping
- `internal` — shared sentinel errors

An interactive `install.sh` exists and must remain interactive. A compiled test artifact (`mainagent.test`) is present in the repo root and must be removed.

**Decisions Already Made**
- Logging uses `log/slog` with a JSON handler. Levels are resolved from a CLI flag.
- All Axe errors are mapped to user-facing Telegram messages via `ErrorMapper`.
- Configuration is loaded from `~/.config/hilt/config.toml` with env var fallback for the bot token.
- The project is single-user, single-goroutine; no concurrency primitives are needed for MVP.
- Post-MVP features (voice transcription, workflows, skills, memory maintenance) are explicitly deferred.

**Constraints**
- No new packages or interfaces may be created for post-MVP features.
- The secret audit is a one-time manual review, not a persistent automated test.
- `install.sh` must not be modified to support non-interactive modes.

**Open Questions Resolved**
1. Should post-MVP groundwork include stub code? → No. Documentation only.
2. Should logging get a new wrapper/interface? → No. Existing `slog` usage is sufficient.
3. Should the install script get batch flags? → No. Leave it interactive.
4. Should secret detection be a reusable test? → No. One-time audit.
5. What tests matter most? → Configuration edge cases.

---

## Section 2: Requirements

### R1: Logging Consistency Review

Every log emission in the codebase must be reviewed for correct level usage:
- `Error` — unrecoverable failures that prevent a request or operation from completing (e.g., database write failure, runner invocation failure, messenger dispatch failure).
- `Warn` — recoverable anomalies that do not stop processing but deserve attention (e.g., empty config file recreated, stale session archived, token update failed but reply still sent).
- `Info` — state changes and lifecycle events (e.g., server start, session creation, config loaded, turn recorded).
- `Debug` — diagnostic detail useful during development (e.g., resolved config path, turn delta content, message counts).

No `fmt.Print`, `fmt.Println`, or `fmt.Printf` may remain outside of `main` flag-parsing help text.

### R2: Error Path Coverage

Every function that returns an error must either:
- Log it at `Error` or `Warn` before returning or swallowing, **or**
- Pass it to a caller that will log it.

Edge cases:
- Messenger send failures inside `reportError` must be logged; they must not trigger infinite recursion or unlogged drops.
- `nil` results from the runner must produce a log entry and a user-facing fallback message.
- Session activity update failures must not crash the bot; they must be logged and processing must continue.

### R3: Hardcoded Secret Audit

Perform a one-time audit of every file tracked by git to verify no hardcoded secrets exist:
- API keys (patterns: `sk-`, `AKIA`, etc.)
- Telegram bot tokens (patterns matching BotFather token format)
- Database passwords or connection strings
- Personal file paths or user-specific identifiers that are not configuration-driven

Scope includes `.go`, `.sh`, `.md`, `.toml`, `.mod`, `.sum`, and any binary artifacts. The audit outcome must be documented: either "no secrets found" or a list of findings with remediation.

### R4: README.md

Create or rewrite `README.md` with the following sections:
- **Overview**: one-paragraph description of Hilt (Telegram-triggered LLM assistant using Axe).
- **Requirements**: Go 1.23+, optional ffmpeg for future voice support.
- **Installation**: run `./install.sh` interactively; what it creates (`~/.config/hilt/`, `~/.hilt/`).
- **Building**: `go build -o hilt ./cmd/hilt`.
- **Running**: `hilt -config=/path/to/config.toml -log-level=debug`, plus env var notes (`TELEGRAM_BOT_TOKEN`).
- **Configuration**: summary of `config.toml` fields (not full schema; point to design doc for details).
- **Architecture**: brief bullet list of packages and responsibilities.
- **Future Work**: deferred post-MVP features (voice, workflows, skills, memory maintenance) with no implementation promises.

The README must be usable by someone cloning the repo with no prior context.

### R5: CHANGELOG.md

Create `CHANGELOG.md` with an initial `[0.1.0]` section dated to the milestone completion date. The entry must summarize the vertical slices delivered in milestones 001–009 using one bullet per milestone:
- Bootstrap, Config, Database, Telegram, Routing, Turn 1, Turn 2+, Error Handling, Integration.

No commit-level granularity. The format should follow [Keep a Changelog](https://keepachangelog.com/) conventions.

### R6: Version Tag

Create a git annotated tag `v0.1.0` pointing to the commit that completes this milestone. The tag message must reference the CHANGELOG.

### R7: Repository Hygiene

- Run `go mod tidy` and ensure `go.sum` is consistent. Any discrepancy between `go.mod` and imports must be resolved.
- Run `go vet ./...` with zero findings.
- Remove the stale compiled test artifact `mainagent.test` from the repo root.
- Verify no unused imports, no dead code, and no commented-out blocks older than this milestone.
- Ensure all test files pass: `go test ./...`.

### R8: Configuration Test Priority

Expand test coverage for `internal/config` to exercise all edge cases:
- Missing config file → defaults created, loads successfully.
- Empty config file → removed, defaults created, loads successfully.
- Config file is a directory → returns error with `ErrInvalidConfig`.
- Missing required field (`telegram_bot_token` absent in file and env) → returns error with `ErrInvalidConfig`.
- `TELEGRAM_BOT_TOKEN` env var set but TOML field empty → uses env var.
- TOML field set and env var set → TOML wins.
- `workspace_dir` with leading tilde → expanded correctly.
- `session_ttl_days` ≤ 0 → validation error.
- `context_window_default` ≤ 0 → validation error.
- Invalid `whisper_model` → validation error.
- Model context window ≤ 0 → validation error.

These tests are the primary testing focus of this milestone. Other new tests (logging, shutdown, secrets) are out of scope.

### R9: Post-MVP Groundwork (Documentation Only)

Add a "Future Work" section to `README.md` listing the deferred features without creating packages, stubs, or interfaces:
- Voice message transcription via whisper.cpp + ffmpeg.
- Workflow engine (`/flow` command) for multi-step agent pipelines.
- Skill registry (`/skills` command) for agent specialization.
- Memory maintenance background agent for archive compaction and critical state updates.

No design promises or timelines. The section exists solely to orient future contributors.

---

## Edge Cases Documented

| Scenario | Expected Behavior |
|----------|-------------------|
| `go mod tidy` changes `go.sum` | Commit the updated `go.sum`; do not ignore the delta. |
| `go vet` reports issues | Fix all issues before tagging; no suppressions. |
| Secret audit finds a false positive (e.g., `"sk-"` in a markdown code block example) | Document the finding as a false positive and leave the text unchanged. |
| Secret audit finds a real secret | Rotate the secret immediately, rewrite git history if it was committed, and document remediation. |
| `install.sh` is mentioned in README but user runs it from wrong directory | README must note: "run from the repository root." |
| CHANGELOG date vs tag date mismatch | Both must use the same date (milestone completion date). |
| Existing README has content (currently a one-line stub) | Replace entirely with the new README; do not append. |
| `mainagent.test` is untracked or tracked | Remove from filesystem and from git index if tracked. |
| Config test needs temp directories | Use `t.TempDir()`; do not write to `~/.config/hilt` during tests. |
