# 001: Bootstrap — Initialize Go Module and Core Packages

## Section 1: Context Summary

This milestone lays the structural foundation for Hilt, a single-user Telegram LLM assistant. The repository currently contains no Go source files, no `go.mod`, and no package structure. We are scaffolding the minimal set of packages (`internal/config`, `internal/telegram`, `internal/session`, `internal/memory`, `internal/server`) and a `cmd/hilt/main.go` entry point that will support all later milestones. Decisions are locked: `gotgbot/v2` for Telegram, `modernc.org/sqlite` for pure-Go SQLite, `BurntSushi/toml` for config, `log/slog` for logging, and standard library `flag` for CLI parsing. Axe is imported at `github.com/jrswab/axe/pkg/runner` and its `runner.Options.Messages` API (Issue #82) is assumed stable. This milestone produces compilable scaffolding only — no behavioral logic.

---

## Section 2: Implementation Checklist

### Track A: Go Module Initialization (Prerequisite for all other work)

- [x] **R1-A1**: Create `go.mod` at repository root with module path `github.com/jrswab/hilt` and Go directive `1.23`
- [x] **R1-A2**: Run `go mod tidy` to verify the module initializes cleanly
- [x] **R1-A3**: Test: Verify `go.mod` exists and contains the correct module path and Go version

### Track B: Entry Point and CLI Wiring (Depends on Track A)

- [x] **R2-B1**: Create `cmd/hilt/main.go: resolveLogLevel(input string) slog.Level` — maps `"debug"`→`slog.LevelDebug`, `"info"`→`slog.LevelInfo`, `"warn"`→`slog.LevelWarn`, `"error"`→`slog.LevelError`; returns `slog.LevelInfo` for invalid input
- [x] **R2-B2**: Create `cmd/hilt/main.go: resolveConfigPath(flagValue string) string` — returns flag value if non-empty, else `os.Getenv("HILT_CONFIG")`
- [x] **R2-B3**: Create `cmd/hilt/main.go: main()`:
  - Define `const version = "0.1.0"`
  - Register `-config` flag (default `""`) and `-log-level` flag (default `"info"`)
  - Resolve config path via `resolveConfigPath`
  - Resolve log level via `resolveLogLevel`
  - Initialize `slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))`
  - Log `"hilt starting"` at info level with `version` and `config_path` fields
  - Log resolved config path at debug level
  - Register `SIGINT`/`SIGTERM` handler via `signal.NotifyContext` that logs `"shutting down"` and exits 0
  - Block on `<-ctx.Done()` and print placeholder log: `"server not yet implemented"`
- [x] **R2-B4**: Test: `go build ./cmd/hilt` compiles without errors
- [x] **R2-B5**: Test: Run `./hilt` — verify startup JSON log with version, program exits 0
- [x] **R2-B6**: Test: Run `./hilt -log-level=debug` — verify debug-level JSON log includes config_path
- [x] **R2-B7**: Test: Run `./hilt -log-level=invalid` — verify falls back to info level, logs warning about invalid level
- [x] **R2-B8**: Test: `HILT_CONFIG=/tmp/hilt.toml ./hilt` — verify config_path resolves to `/tmp/hilt.toml`
- [x] **R2-B9**: Test: `./hilt -config=/flag/path.toml` — verify flag value takes precedence over env var

### Track C: Shared Error Types (Depends on Track A; parallel with Track B and Track D)

- [x] **R4-C1**: Create `internal/errors.go: ErrNotFound`, `ErrInvalidConfig`, `ErrSessionExpired` as exported `var` initialized with `errors.New`
- [x] **R4-C2**: Ensure `internal/errors.go` includes `// Package internal ...` documentation comment
- [x] **R4-C3**: Test: `go build ./internal` compiles without errors
- [x] **R4-C4**: Test: `go test ./internal -run TestSentinelsExist` — verify all three sentinels are non-nil

### Track D: Package Scaffolding (Depends on Track A; parallel with Track B and Track C)

- [x] **R3-D1**: Create `internal/config/config.go: Package config` documentation comment describing future TOML config loading and validation responsibilities
- [x] **R3-D2**: Create `internal/telegram/telegram.go: Package telegram` documentation comment describing future `gotgbot/v2` polling loop and message dispatch responsibilities
- [x] **R3-D3**: Create `internal/session/session.go: Package session` documentation comment describing future SQLite session lifecycle (active/archived, TTL, pruning) responsibilities
- [x] **R3-D4**: Create `internal/memory/memory.go: Package memory` documentation comment describing future L2 archival memory (daily notes, critical.md, graph) responsibilities
- [x] **R3-D5**: Create `internal/server/server.go: Package server` documentation comment describing future HTTP server initialization and graceful shutdown responsibilities
- [x] **R3-D6**: Test: `go build ./internal/...` compiles all packages without errors
- [x] **R3-D7**: Test: Verify no import cycles exist between scaffolded packages (run `go list -deps ./internal/...` and inspect)

---

## Parallel Work Summary

```
Track A (go.mod init)
    ├── Track B (entry point + CLI) ──┐
    ├── Track C (shared errors)       ├── All complete → go build ./...
    └── Track D (package scaffolding)─┘
```
