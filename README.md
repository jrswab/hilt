# Hilt

Hilt is a Telegram-triggered LLM assistant built in Go. It uses the [Axe](https://github.com/jrswab/axe) CLI execution engine to run agentic workflows, manage persistent conversation sessions in SQLite, and maintain workspace memory across daily notes, critical state, and agent definitions. Hilt is designed for a single user who wants a minimal, command-focused assistant accessible from anywhere via Telegram.

## Requirements

- Go 1.23 or later
- A Telegram bot token from [@BotFather](https://t.me/botfather)
- API keys for your chosen LLM providers (managed via environment variables following Axe's conventions)
- Optional: `ffmpeg` for future voice message support

## Installation

Run the interactive installer from the repository root:

```bash
./install.sh
```

The installer creates:

- `~/.config/hilt/` — configuration, database, and agent definitions
- `~/.hilt/` — workspace memory (AGENTS.md, daily notes, critical state)

## Building

```bash
go build -o hilt ./cmd/hilt
```

## Running

```bash
# With explicit config path
hilt -config=/path/to/config.toml -log-level=debug

# With default config path (~/.config/hilt/config.toml)
hilt -log-level=info
```

The bot token can be set in `config.toml` (`telegram_bot_token`) or via the `TELEGRAM_BOT_TOKEN` environment variable. The TOML value takes precedence if both are set.

Log level options: `debug`, `info`, `warn`, `error`.

## Configuration

`config.toml` fields (see `docs/plans/000_hilt_design.md` for the full schema):

| Field | Description |
|-------|-------------|
| `telegram_bot_token` | Bot token from BotFather |
| `workspace_dir` | Path to workspace memory directory (default: `~/.hilt`) |
| `session_ttl_days` | Days before a stale session is archived (default: 30) |
| `main_agent_model` | Model identifier for the main agent |
| `context_window_default` | Default token context window |
| `maintenance_agent_model` | Model for background memory maintenance |
| `whisper_model` | Whisper.cpp model size (`tiny`, `base`, `small`, `medium`, `large`) |
| `allowed_user_ids` | Telegram user IDs permitted to interact with the bot |
| `[models]` | Map of model names to context window sizes |

## Architecture

- `cmd/hilt` — Entry point, signal handling, dependency wiring
- `internal/config` — TOML parsing, env var fallback, path expansion, first-run defaults
- `internal/session` — SQLite schema, session lifecycle, turn journaling
- `internal/memory` — Workspace file reader (AGENTS.md, critical.md, daily notes)
- `internal/telegram` — `gotgbot/v2` long-polling wrapper
- `internal/server` — Command router (`/new`, `/sessions`, unknown commands)
- `internal/mainagent` — Context assembly, turn 1 vs turn 2+ processing, delta persistence, error mapping
- `internal` — Shared sentinel errors

## Future Work

The following features are deferred for post-MVP development:

- **Voice message transcription** via whisper.cpp + ffmpeg
- **Workflow engine** (`/flow` command) for multi-step agent pipelines
- **Skill registry** (`/skills` command) for agent specialization
- **Memory maintenance background agent** for archive compaction and critical state updates

No implementation promises or timelines are attached to these items.
