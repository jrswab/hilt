# 001: Bootstrap — Initialize Go Module and Core Packages

## Section 1: Context & Constraints

### Codebase State
The project is a **greenfield Go repository** at `/Users/jaronswab/go/src/github.com/jrswab/hilt`. It currently contains **no Go source files**, no `go.mod`, and no package structure. Existing content is limited to documentation: a design document, a memory system walkthrough, and standard repository files (README, LICENSE, .gitignore).

This milestone is the foundation for all subsequent work. Everything built here is scaffolding that later milestones depend on directly.

### Decisions Already Made

| Decision | Rationale |
|----------|-----------|
| **Package layout: minimal MVP** | Only create packages the MVP needs. Defer premature abstraction. Packages required for MVP: `config`, `telegram`, `session`, `memory`, `server`. The design doc references `db` as an implementation concern; the `session` and `memory` packages will own persistence logic directly. |
| **Telegram library: `gotgbot/v2`** | Modern, actively maintained, superior `context.Context` support compared to alternatives. |
| **SQLite: `modernc.org/sqlite`** | Pure-Go implementation. No CGO dependency. |
| **Config format: TOML via `BurntSushi/toml`** | Human-readable, supports clear table structures for agent definitions. |
| **Logging: `log/slog`** | Standard library, structured logging, no external dependency. |
| **Development style: vertical traces** | Build end-to-end slices. This milestone intentionally does not implement behavior—only the structural skeleton that enables vertical slices in later milestones. |
| **Single-user, permanently** | Simplifies concurrency model. No need for multi-tenant package boundaries in MVP scaffolding. |

### Approaches Ruled Out (Do Not Re-evaluate)

| Approach | Why Rejected |
|----------|--------------|
| Full package layout on day one | Premature abstraction. Only packages needed for MVP will be created. No `workflow`, `voice`, or standalone `agents` packages yet. |
| `telegram-bot-api/v5` | Rejected in favor of `gotgbot/v2` for better context support and active maintenance. |
| CGO SQLite drivers (e.g., `mattn/go-sqlite3`) | Rejected in favor of pure-Go `modernc.org/sqlite` to eliminate build complexity and cross-compilation issues. |
| Third-party logging frameworks (e.g., zap, zerolog) | Rejected in favor of standard library `log/slog` to minimize dependencies. |
| `spf13/cobra` or other CLI frameworks | Rejected. MVP requires at most one flag (config path). Standard `flag` package is sufficient. |

### Constraints and Assumptions
- **Go 1.23+** is available on the development machine. Later milestones depend on features available in recent Go versions.
- **Axe Issue #82 is merged** — the `runner.Options.Messages` API for multi-turn history is assumed stable and available. The `go.mod` must reference a version of Axe that includes this functionality.
- **No install script exists yet** — the install script was explicitly deferred until after the Go binary is stable. First-run file creation will be handled by the Go binary in milestone 003, not by this milestone.
- **No Go source exists yet** — this milestone must create the module from scratch without conflicting with existing Go code.
- All packages live under `internal/` to prevent external import. The design doc references `internal/` packages; the public surface is only the `cmd/hilt/main.go` entry point.
- The `memory` package name in code replaces the conceptual "Memory Palace" terminology from the walkthrough document. It handles L2 archival memory (daily notes, critical state).

### Open Questions Resolved
- **What packages to create?** → `internal/config`, `internal/telegram`, `internal/session`, `internal/memory`, `internal/server`.
- **Where does shared error types live?** → `internal/errors.go` at the `internal` package level.
- **CLI framework?** → Standard library `flag` only.
- **Logging framework?** → `log/slog`.

---

## Section 2: Requirements

### R1: Initialize Go Module
A `go.mod` file must exist at the repository root declaring the module path `github.com/jrswab/hilt`.

**Dependencies that must be declared** (versions must be compatible with the APIs referenced in the design doc and milestones):
- `github.com/PullRequestInc/go-gpt3` or equivalent Axe dependency — the exact module path for the Axe runner package.
- `github.com/PaulSonOfLars/gotgbot/v2`
- `github.com/BurntSushi/toml`
- `gitlab.com/cznic/sqlite` (or `modernc.org/sqlite` if the import path changed)

**Edge cases:**
- R1-E1: If `go.mod` already exists (e.g., from a partial prior attempt), the existing file must be preserved if it is valid, or overwritten with a clean declaration if it is malformed.
- R1-E2: The module must initialize successfully with `go mod tidy` without errors.
- R1-E3: The Go version declared in `go.mod` must be 1.23 or later.

### R2: Create Application Entry Point
A `cmd/hilt/main.go` file must exist.

**Behaviors:**
- Defines a `main()` function that compiles and runs without panic.
- Accepts an optional `-config` CLI flag specifying a path to a config file. The default value must be empty (placeholder for milestone 002 to resolve).
- Initializes a structured logger (`log/slog`) with JSON output format.
- Accepts an optional `-log-level` CLI flag with valid values: `debug`, `info`, `warn`, `error`. Default is `info`.
- Logs a startup message at `info` level including the version string (hardcoded as a constant, e.g., `"0.1.0"`, for now).
- Exits cleanly (status 0) after printing a placeholder log message indicating the server is not yet implemented.

**Edge cases:**
- R2-E1: Invalid `-log-level` value must fall back to `info` and log a warning.
- R2-E2: The program must handle `SIGINT` and `SIGTERM` gracefully even in this scaffolding phase (register handlers that log shutdown and exit 0).

### R3: Scaffold Internal Packages
The following package directories must exist under `internal/`, each containing at minimum a Go source file with the package declaration and a blank `// Package <name> ...` documentation comment:

- `internal/config/`
- `internal/telegram/`
- `internal/session/`
- `internal/memory/`
- `internal/server/`

**Requirements:**
- Each package file must compile (no syntax errors, no undefined references).
- No behavioral logic is required yet; the files are structural placeholders.
- Package documentation comments must briefly describe the package's future responsibility per the design doc.

**Edge cases:**
- R3-E1: If files already exist in any of these directories, they must not be overwritten unless they are empty or contain only a package declaration.
- R3-E2: Each package must have a unique import path and must not create import cycles with other scaffolded packages.

### R4: Create Shared Error Types
A file `internal/errors.go` must exist defining shared error types and sentinel errors.

**Requirements:**
- Defines at minimum: `ErrNotFound`, `ErrInvalidConfig`, `ErrSessionExpired` as exported `error` values.
- Uses `errors.New` for simple sentinels.
- Includes a `// Package internal ...` documentation comment.
- Compiles without errors.

**Edge cases:**
- R4-E1: If `internal/errors.go` already exists, merge new sentinels with existing ones rather than overwriting.

### R5: Basic Environment and CLI Wiring
The entry point must read the `-config` flag value and check for the presence of the `HILT_CONFIG` environment variable.

**Requirements:**
- CLI flag `-config` takes precedence over the `HILT_CONFIG` environment variable.
- If neither is set, the config path remains empty (milestone 002 will define default path resolution).
- The resolved config path (even if empty) must be logged at `debug` level at startup.
- The entry point must not attempt to read or validate a config file in this milestone—it only captures the path.

**Edge cases:**
- R5-E1: Empty string from both flag and env var is a valid state; no error is raised.
- R5-E2: A config path pointing to a non-existent file is acceptable at this stage; no existence check is performed.

### Parallel Work
The following workstreams are independent once R1 (`go.mod` initialization) is complete and can proceed in parallel:
- **Track A**: R2 (entry point) + R5 (CLI/env wiring) — both live in `cmd/hilt/main.go`.
- **Track B**: R3 (package scaffolding) — each package directory is independent.
- **Track C**: R4 (shared errors) — has no dependencies on other packages.

R1 must precede all other work because `go.mod` provides the module context that validates import paths.

---

## Open Questions for User
1. **Axe module path:** What is the exact Go module import path for the Axe runner package? The milestones reference `runner.Options` and `runner.Run()` but the design doc does not specify the module name.
2. **Go version constraint:** Should `go.mod` pin the minimum Go version to 1.23, or is 1.22 acceptable if that is the current toolchain version?
3. **Modernc SQLite import path:** Should the dependency use `modernc.org/sqlite` or `gitlab.com/cznic/sqlite`? The import path has changed across versions.
