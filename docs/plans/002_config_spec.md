# 002: Config — Load and validate `config.toml` on startup

## Section 1: Context & Constraints

### Codebase State
Milestone 001 (Bootstrap) is complete. The repository now contains:
- `go.mod` declaring module `github.com/jrswab/hilt` with Go 1.23; **no external dependencies are declared yet**
- `cmd/hilt/main.go` with CLI flag/env wiring, structured logging, and graceful shutdown scaffolding
- `internal/config/config.go` containing **only a package documentation comment** — no types, no functions
- `internal/errors.go` defining sentinel errors including `ErrInvalidConfig`
- Stub packages for `session`, `memory`, `telegram`, and `server`

The entry point currently resolves a config path via `-config` flag and `HILT_CONFIG` env var, logs it, and exits. No config file is read or validated.

### Decisions Already Made
| Decision | Resolution | Rationale |
|----------|------------|-----------|
| Config file format | **TOML** | Already chosen and locked in. Human-readable, table structures map cleanly to nested config. |
| TOML parser | **`github.com/BurntSushi/toml`** | Already chosen and locked in. Standard, stable, no re-evaluation. |
| Config location | **`~/.config/hilt/config.toml`** | XDG-compliant default. User can override via `-config` or `HILT_CONFIG`. |
| First-run behavior | **Go binary auto-creates stubs** | Install script was deferred. The binary must create missing files/directories at startup. |
| Secret storage | **Environment variables only** | Hilt must NOT read API keys from `config.toml`. Bot token may be in env var `TELEGRAM_BOT_TOKEN` or in TOML. |
| Allowed user IDs | **Array of ints in TOML** | Empty array means unrestricted (dev mode). |
| Model context windows | **Hardcoded defaults + TOML overrides** | `[models]` table in TOML overrides/adds to a built-in map. |

### Approaches Ruled Out (Do Not Re-evaluate)
| Approach | Why Rejected |
|----------|--------------|
| YAML or JSON for config | Rejected in favor of TOML before milestone 001. |
| `spf13/viper` | Rejected. Standard library + `BurntSushi/toml` is sufficient. |
| Environment variables as primary config store | Rejected. Env vars are fallback for secrets and optional overrides; TOML is the primary configuration surface. |

### Constraints & Assumptions
- **Home directory expansion** — `~` in `workspace_dir` and `config.toml` paths must be expanded to the user's actual home directory (`os.UserHomeDir()`). Failure to expand is a fatal error.
- **Single-user design** — There is no per-user config isolation; a single `config.toml` serves the entire Hilt instance.
- **Fail-fast on invalid config** — The server must refuse to start if required fields are missing/invalid. This aligns with `ErrInvalidConfig` already defined in `internal/errors.go`.
- **`agents/main.toml` and `AGENTS.md` stubs** — If these files do not exist, Hilt must create them with sensible default content so Axe can run. These stubs are structural, not behavioral.
- **No secrets in TOML validation** — The config loader validates structure and required fields but does not validate that API keys are set. That is Axe's concern at runtime.
- **Go standard `fs.PathError` compatibility** — File operations should return errors compatible with `errors.Is(err, fs.ErrNotExist)` where applicable.

### Open Questions Resolved
1. **What is the exact `Config` struct layout?** → Matches the design doc TOML schema (see Section 2, R1).
2. **Should the bot token be allowed in TOML?** → Yes, but `TELEGRAM_BOT_TOKEN` env var overrides it.
3. **Should Hilt create `~/.config/hilt/` or fail?** → Auto-create with `0755` permissions.
4. **Validation strictness?** → Fail fast on startup for missing required fields. Unknown TOML fields are silently ignored (standard `BurntSushi/toml` behavior).

---

## Section 2: Requirements

### R1: Config Struct Definition
A `Config` struct must be defined in `internal/config` that matches the design doc schema.

**Required fields:**
- `TelegramBotToken` — `string`
- `WorkspaceDir` — `string`
- `SessionTTLDays` — `int`
- `MainAgentModel` — `string`
- `ContextWindowDefault` — `int`
- `MaintenanceAgentModel` — `string`
- `WhisperModel` — `string`
- `AllowedUserIDs` — `[]int64`
- `Models` — `map[string]int` (context window overrides/additions)

**Field tag convention:** All fields must be tagged for `BurntSushi/toml` unmarshaling with names matching the design doc (snake_case). Example: `TelegramBotToken string `toml:"telegram_bot_token"``.

**Edge cases:**
- R1-E1: Extra/unknown keys in `config.toml` must not cause unmarshal errors; they are ignored.
- R1-E2: Missing optional fields (all fields are currently optional at the TOML level) must result in their zero values in the struct. Validation happens separately (see R3).

---

### R2: Config File Discovery
Expose a function (e.g., `Load(path string) (*Config, error)`) that resolves and reads the config file.

**Behaviors:**
- If `path` is non-empty, read that exact file.
- If `path` is empty, default to `~/.config/hilt/config.toml`.
- Expand `~` in both the explicit path and the default path.
- Return `fs.ErrNotExist` wrapped in an informative error when the file is missing (so that the caller can trigger auto-creation).

**Edge cases:**
- R2-E1: Path expansion fails because `HOME` is unset — return an error describing the issue.
- R2-E2: The config file exists but is a directory — return a clear error.
- R2-E3: The config file exists but is empty (zero bytes) — treat as missing and trigger auto-creation.

---

### R3: Config Validation
After unmarshaling, the returned `*Config` must be validated. If validation fails, return an error wrapping `ErrInvalidConfig`.

**Required validations:**
- `TelegramBotToken` is non-empty (but see R4 — env var override is allowed).
- `WorkspaceDir` is non-empty.
- `SessionTTLDays` is greater than 0.
- `ContextWindowDefault` is greater than 0.
- `WhisperModel` is one of: `tiny`, `base`, `small`, `medium`, `large`.

**Edge cases:**
- R3-E1: `TelegramBotToken` is empty in TOML but set via env var — validation must pass (env var checked before or during validation).
- R3-E2: `MainAgentModel` or `MaintenanceAgentModel` is empty — allow empty for now; Axe may use its own default. No validation error.
- R3-E3: `AllowedUserIDs` is nil or empty slice — valid; means unrestricted.
- R3-E4: `Models` map contains a model with a context window ≤ 0 — reject with clear error.

---

### R4: Environment Variable Fallback for Bot Token
Before failing validation on an empty `TelegramBotToken`, the loader must check the `TELEGRAM_BOT_TOKEN` environment variable.

**Precedence:**
1. Value in `config.toml` (if non-empty)
2. `TELEGRAM_BOT_KEY` environment variable (if non-empty)
3. Fail validation

**Edge cases:**
- R4-E1: Both TOML and env var are set — TOML wins.
- R4-E2: Env var contains only whitespace — treat as empty and fail.

---

### R5: Auto-Creation of Missing Files and Directories
If `config.toml` does not exist, Hilt must create it along with required parent directories and stub content.

**Behaviors:**
- Create `~/.config/hilt/` with permissions `0755`.
- Write a `config.toml` stub with default values and extensive comments explaining each field.
- Create `agents/` subdirectory with `0755`.
- Create `agents/main.toml` with a minimal valid Axe agent stub (system prompt + tools array).
- Create `AGENTS.md` in the workspace directory (resolved from `workspace_dir`) with a header and placeholder content.
- **After creation**, return the default `*Config` populated with those defaults so the server can start.

**Default values in generated `config.toml`:**
```toml
# Hilt configuration
# Set telegram_bot_token here or via TELEGRAM_BOT_TOKEN env var
telegram_bot_token = ""
workspace_dir = "~/.hilt"
session_ttl_days = 30
main_agent_model = "anthropic/claude-3-haiku-4-5"
context_window_default = 200000
maintenance_agent_model = "openrouter/deepseek/deepseek-chat"
whisper_model = "small"
allowed_user_ids = []

[models]
```

**Edge cases:**
- R5-E1: The parent directory exists but is not writable — return a clear permission error.
- R5-E2: `config.toml` exists but is corrupted/invalid TOML — return a parse error, do not overwrite.
- R5-E3: `agents/main.toml` already exists — do not overwrite; leave as-is.
- R5-E4: `AGENTS.md` already exists — do not overwrite; leave as-is.
- R5-E5: `workspace_dir` does not exist and points to `~/.hilt` — create the directory with `0755`.

---

### R6: Workspace Directory Expansion and Validation
`WorkspaceDir` must be expanded if it contains `~`, and the directory must exist or be auto-created.

**Behaviors:**
- Expand `~` to the user's home directory using `os.UserHomeDir()`.
- If the expanded path does not exist, create it with `0755`.
- If the expanded path exists but is not a directory, return an error.

**Edge cases:**
- R6-E1: `WorkspaceDir` is a relative path (e.g., `./hilt-workspace`) — use as-is; do not error.
- R6-E2: `WorkspaceDir` expansion fails (no home dir) — return error.

---

### R7: Integration with Entry Point
`cmd/hilt/main.go` must call the config loader with the resolved path and handle the results.

**Behaviors:**
- On successful load+validation, log `info`: "config loaded", with `path` and `workspace_dir`.
- On failure, log `error` with the failure reason and exit with code 1.
- The `Config` value must be available in `main` for passthrough to later milestones (e.g., as a struct field, constructor argument, or closure).

**Edge cases:**
- R7-E1: Invalid log level flag from milestone 001 still works as before; config loading happens after log setup so logger is available for config errors.

---

### Parallel Work
The following are independent and can proceed in parallel once this spec is approved:
- **Track A**: R1 (struct definition) + R2 (file discovery) + R3 (validation) — all live in `internal/config`.
- **Track B**: R5 (auto-creation) — depends on R2 returning "not found" and R1 having default values, but the logic can be written in parallel if interfaces are agreed.
- **Track C**: R7 (entry point integration) — depends on Tracks A and B having exported functions.

R4 (env var fallback) must be coordinated with R3 (validation) because they share the same validation function, but they can be authored side-by-side.
