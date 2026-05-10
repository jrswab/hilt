# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-05-10

### Added

- **Bootstrap** — Project skeleton, `cmd/hilt` entry point, and interactive `install.sh`.
- **Config** — TOML configuration loader with env var fallback, path expansion, first-run defaults, and validation.
- **Database** — SQLite session store with active session lifecycle, turn journaling, and TTL-based pruning.
- **Telegram** — Long-polling bot integration using `gotgbot/v2` with allowed-user filtering.
- **Routing** — Message router supporting `/new`, `/sessions`, and unknown command handling.
- **Turn 1** — Full context assembly from workspace memory (AGENTS.md, critical.md, daily notes) and main agent execution via Axe.
- **Turn 2+** — Conversation history reconstruction, delta computation, and persistent turn journaling.
- **Error Handling** — Axe error mapping to user-facing Telegram messages, structured logging with `log/slog`, and graceful shutdown.
- **Integration** — End-to-end wiring of all packages into a functional single-user Telegram assistant.

[0.1.0]: https://github.com/jrswab/hilt/releases/tag/v0.1.0
