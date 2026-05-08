# 002: Config — Implementation Guide

## Section 1: Context Summary

Milestone 001 established a bare Go module with stub packages and CLI scaffolding. The entry point resolves a config path, logs it, and exits. This milestone must implement `internal/config` as a deep module: a single public entry point `Load(path string) (*Config, error)` that hides file discovery, TOML unmarshaling (~ expansion, validation, environment variable fallback for the bot token, and first-run auto-creation of `config.toml`, `agents/main.toml`, and `AGENTS.md`). The module must fail fast on invalid configuration and return a fully populated, validated `*Config` so that later milestones (session, memory, telegram, server) receive a ready-to-use configuration object.

---

## Section 2: Implementation Checklist

### Dependency: Add TOML parser to module
- [x] **Add `github.com/BurntSushi/toml`** — run `go get github.com/BurntSushi/toml` and verify `go mod tidy` succeeds.

### Track A: Config struct and internal helpers (internal/config/config.go)
- [x] **Define `Config` struct** — `internal/config/config.go`: add `Config` struct with all fields tagged for `toml:"..."` unmarshaling (R1).
- [x] **Add `expandPath` helper** — `internal/config/config.go: expandPath(path string) (string, error)` — replaces leading `~` with `os.UserHomeDir()` (R2, R6).
- [x] **Add `resolveBotToken` helper** — `internal/config/config.go: resolveBotToken(tomlValue string) string` — returns `tomlValue` if non-empty, else checks `TELEGRAM_BOT_TOKEN` env var (R4).
- [x] **Add `validate` helper** — `internal/config/config.go: validate(c *Config) error` — checks required fields and valid `WhisperModel` values (R3).
- [x] **Add `createDefaults` helper** — `internal/config/config.go: createDefaults(configDir, workspaceDir string) (*Config, error)` — creates `~/.config/hilt/`, writes `config.toml` stub, creates `agents/`, writes `agents/main.toml` stub, creates workspace dir, writes `AGENTS.md` stub (R5, R6).
- [x] **Add `Load` public function** — `internal/config/config.go: Load(path string) (*Config, error)` — orchestrates: expand path → read file (or create defaults) → unmarshal TOML → resolve bot token → validate (R2, R3, R4, R5, R6, R7).

### Track B: Entry point integration (cmd/hilt/main.go)
- [x] **Wire config loading into `main()`** — `cmd/hilt/main.go: main()` — call `config.Load(configPath)`, log result or fatal on error, store `*config.Config` in a local variable for downstream use (R7).

### Track C: Tests (internal/config/config_test.go)
- [x] **Test `expandPath`** — `internal/config/config_test.go` — verify `~` expansion, pass-through for absolute paths, and error when `HOME` is unset.
- [x] **Test `resolveBotToken`** — `internal/config/config_test.go` — verify TOML value precedence over env var, and whitespace-only env var treated as empty.
- [x] **Test `validate`** — `internal/config/config_test.go` — test each validation rule independently with valid and invalid inputs.
- [x] **Test `createDefaults`** — `internal/config/config_test.go` — call with a `t.TempDir()` config dir and workspace dir; assert all files exist with expected content and non-overwrite behavior for existing files.
- [x] **Test `Load` with valid file** — `internal/config/config_test.go` — write a valid `config.toml` to `t.TempDir()`, call `Load`, assert all fields match.
- [x] **Test `Load` with missing file** — `internal/config/config_test.go` — call `Load` with non-existent path in `t.TempDir()`; assert auto-creation of defaults and returned default config.
- [x] **Test `Load` with empty file** — `internal/config/config_test.go` — write zero-byte file to `t.TempDir()`, call `Load`; assert auto-creation triggers (treated as missing).
- [x] **Test `Load` with invalid TOML** — `internal/config/config_test.go` — write corrupt TOML, call `Load`; assert parse error (no auto-creation, no overwrite).

### Parallel Work
- **Track A** (struct + helpers + `Load`) must complete before **Track B** (entry point integration) and **Track C** (tests for `Load`).
- **Track C** sub-tasks for individual helpers (`expandPath`, `resolveBotToken`, `validate`) can proceed in parallel with each other and with the implementation of those helpers in Track A.
- **Track B** depends only on the exported `config.Load` signature; it can be written as soon as the function signature is stable.
