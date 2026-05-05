# Hilt Design Document

This document captures the architecture and intent behind Hilt — a lightweight, fast, and efficient LLM assistant triggered from Telegram.

**Goal:** A main agent for general chat with persistent memory, plus specific, self-contained command agents for workflows and skills.

**Design principles:**
- Composable
- Command focused
- Minimal "chat"
- An assistant, not a therapist or friend

**Flow:** Telegram message → Go webserver (stateful orchestrator) → Assemble context → Shell out to Axe CLI (execution engine) → Return response to Telegram.

---

## Go Server

The Go server is a **stateful orchestrator**. It manages session state, memory tiering, token bookkeeping, and Telegram integration. It does NOT talk to LLMs directly — that is Axe's job.

### Responsibilities
- Receive Telegram messages via **long-polling** (`getUpdates` with 60s timeout)
- Route messages:
  - Messages starting with `/` → command resolution
  - Everything else → main agent
- Manage session persistence (JSON files on disk)
- Assemble context for the main agent (memory layers, conversation history)
- Track token budgets and warn at 80%
- Shell out to `axe` for all LLM execution

### Telegram Integration
- **Mode:** Long-polling (default), configurable via `TELEGRAM_MODE`
- **Why long-polling:** No public IP, TLS cert, or domain required. Works from any VPS or home server. Latency is ~network RTT because `getUpdates` with `timeout=60` holds the connection open until a message arrives.

---

## Session Model

- **One active session per chat ID.**
- Sessions are persisted to disk as JSON files.
- **`/new`** starts a fresh session (archives the current one).
- **`/sessions`** lists archived sessions within the TTL window.
- **TTL-pruning** removes old archived sessions automatically.

### Session JSON Structure
```json
{
  "chat_id": 123456789,
  "title": "Project Atlas discussion",
  "created_at": "2026-05-04T14:30:00Z",
  "last_activity": "2026-05-04T15:45:00Z",
  "total_input_tokens": 4500,
  "total_output_tokens": 1200,
  "turns": [
    {"role": "user", "content": "display message", "raw_input": "...", "timestamp": "..."},
    {"role": "assistant", "content": "...", "timestamp": "..."}
  ]
}
```

- `title`: Auto-generated from the first user message (truncated to ~40 chars).
- `raw_input`: The full assembled context actually sent to Axe (for debugging).
- `display_message`: What the user actually typed (used in history assembly on subsequent turns).

### Session Lifecycle
1. **New session:** Hilt loads AGENTS.md (workspace-specific rules), `memory/critical.md` (L1), today + yesterday's daily notes (L2). These are assembled into the first turn's stdin and sent to Axe.
2. **Ongoing turns:** The assembled stdin contains ONLY the conversation history ( user's actual short messages + assistant responses) + the current user message. The agent uses `read_file` and `run_command` (grep/rg) to refresh memory on demand.
3. **Session end:** `/new`, TTL expiry, or server restart (session is already persisted).

---

## Directory Layout

### Hilt Configuration & Data: `~/.config/hilt/`
```
~/.config/hilt/
├── config.toml          # Hilt behavior settings (non-secrets)
├── agents/              # Flat TOML files for Axe discovery
│   ├── main.toml        # Main agent (system prompt + tools)
│   ├── edit-file.toml   # Skill: edit file
│   └── summarize.toml   # Skill: summarize
├── skills/              # Parallel resource tree (scripts, docs)
│   ├── edit-file/
│   │   └── scripts/
│   └── summarize/
│       └── SKILL.md
├── workflows/           # Shell scripts for multi-agent workflows
│   └── deploy-pipeline.sh
└── sessions/            # Session persistence JSON files
    ├── 123456789-current.json
    └── 123456789-20260504T143000Z.json
```

### Workspace: `~/.hilt/` (configurable, default)
```
~/.hilt/
├── AGENTS.md            # Workspace-specific rules, tunnels, priorities
├── memory/
│   ├── critical.md      # L1 — compressed current state (~200 tokens)
│   ├── graph.md         # L3 — temporal knowledge graph
│   ├── 2026-05-04.md    # L2 — daily note
│   └── 2026-05-04-topic.md
└── projects/
    └── project-a/
        └── AGENTS.md
```

### Axe Integration
- Hilt shells out to `axe run` for all LLM execution.
- `--agents-dir ~/.config/hilt/agents` points Axe to Hilt-managed agents.
- `--workdir ~/.hilt` sets the workspace for file tools.
- `--json` wraps output for structured parsing.
- Axe's config (`~/.config/axe/config.toml`) and environment variables (e.g., `ANTHROPIC_API_KEY`) are used for provider/API configuration.

---

## Agent Types

### Main Agent
- **Static TOML:** `~/.config/hilt/agents/main.toml`
- **System prompt (in TOML):** Permanent instructions — identity, memory system overview, search procedures, tool guidance. Loaded by Axe on **every turn**.
- **Dynamic context (in stdin):** AGENTS.md content, L1/L2 memory, conversation history, current message. Assembled by Hilt and sent **once at session start** for L0/L1/L2; ongoing turns carry conversation history + current message only.
- **Tools:** `read_file`, `write_file`, `edit_file`, `list_directory`, `run_command`
  - NO `url_fetch`, NO `web_search`
  - `run_command` is sandboxed to `~/.hilt/` by Axe (path traversal blocked)
- **Memory:** Agent-owned. Hilt creates the `memory/` directory if missing. The agent creates `critical.md`, daily notes, `graph.md`, closets, etc. via its tools.
- **Context on demand:** On turn 2+, the agent uses `run_command` (grep/rg) or `read_file` to search and load specific memory files as needed.

### Command Agents (Skills)
- Triggered by `/agent-name` from Telegram.
- Self-contained TOML files in `~/.config/hilt/agents/`.
- Can have parallel resources in `~/.config/hilt/skills/{agent-name}/`.
- Executed via `axe run agent-name --agents-dir ~/.config/hilt/agents`.

### Workflows
- Multi-agent workflows using Axe CLI and Unix pipes.
- Shell scripts in `~/.config/hilt/workflows/`.
- Executed the same way as single agents: `/workflow-name` → Hilt runs the shell script.

---

## Skills & Commands

### Built-in Commands (Hilt executes directly, no LLM call)
- `/skills` / `/commands` — List available command agents and workflows
- `/delete <name>` — Remove agent TOML + parallel `skills/` resources, or delete a workflow
- `/sessions` — List archived sessions within TTL
- `/new` — Start a fresh session (archive current)

### Command Resolution Order
1. Built-in command? → Execute in Go
2. Workflow in `workflows/`? → Execute shell script
3. Agent TOML in `agents/`? → `axe run <name> --agents-dir ...`
4. Unknown → "Command not found. Use /skills to see available commands."

### Skill Creation
- The main agent can create new skills by writing TOML files to `~/.config/hilt/agents/` and resources to `~/.config/hilt/skills/`.
- The main agent does NOT know the list of existing skills. If asked to run a skill, it tells the user to use a slash command.

---

## Memory System

Hilt implements the Memory Palace design (see `memory-system-walkthrough.md`).

### Session Startup Loading (Turn 1 Only)
| Layer | Source | Loaded |
|-------|--------|--------|
| L0 | `main.toml` system_prompt | Every turn by Axe |
| L0 | `~/.hilt/AGENTS.md` | Turn 1 stdin |
| L1 | `~/.hilt/memory/critical.md` | Turn 1 stdin |
| L2 | `~/.hilt/memory/YYYY-MM-DD.md` (today + yesterday) | Turn 1 stdin |

### Ongoing Turns
- Only conversation history + current message in stdin.
- Agent uses `read_file` and `run_command` (grep/rg) to load L3 memory on demand.

### Maintenance
- `critical.md`: Weekly full rewrite (agent-managed)
- Daily notes: Every session (agent-managed)
- Graph triples: Every session when qualifying events occur (agent-managed)
- Closet summaries: On demand when scanning historical notes >7 days old (agent-managed)

---

## Token Budget Management

- **Proactive estimation:** tiktoken-go counts the assembled prompt before sending to Axe.
- **Ground truth:** Axe's JSON response reports actual input/output tokens per turn.
- **Tracking:** Hilt accumulates actual usage from JSON responses against a configurable model context window.
- **Warning at 80%:** When cumulative usage reaches 80% of the model's context window, warn the user.
- **Hard limit:** Pass `--max-tokens` to Axe as a safety net.
- **Model context windows:** Hardcoded map of known models (e.g., `anthropic/claude-3-opus-4-6 → 200000`) with configurable fallback.

---

## Error Handling

Axe exit codes are mapped to user-friendly Telegram messages with raw stderr appended (truncated to 4096 chars):

| Exit Code | Meaning | User Message |
|-----------|---------|--------------|
| 0 | Success | Send `content` field from JSON |
| 1 | Bad request / invalid input | "I couldn't process that request." + stderr |
| 2 | Config error / missing agent | "Configuration issue." + stderr |
| 3 | Auth / rate limit / timeout | "Service temporarily unavailable." + stderr |
| 4 | Budget exceeded | "Token limit reached. Start a new session with /new." + stderr |

---

## Configuration

### `~/.config/hilt/config.toml`
```toml
# Hilt behavior settings (no secrets)
telegram_bot_token = ""  # Can also use TELEGRAM_BOT_TOKEN env var
workspace_dir = "~/.hilt"
session_ttl_days = 30
main_agent_model = "anthropic/claude-3-haiku-4-5"
context_window_default = 200000
```

### API Keys
- API keys are read from environment variables following Axe's convention: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, etc.
- Compatible with secret managers like Doppler.

---

## Installation

An install script collects required data:
- Telegram bot token
- Confirmation of environment variable setup (API keys via Doppler or other secret manager)
- Workspace directory path (defaults to `~/.hilt`)
- Creates directory structure (`~/.config/hilt/`, `~/.hilt/memory/`, etc.)
- Provides systemd unit file template

---

## Resolved Decisions

| Decision | Resolution |
|----------|------------|
| Stateless vs stateful | **Stateful orchestrator** |
| Session persistence | **Persist to disk** (`~/.config/hilt/sessions/`) |
| Sessions per chat | **One active** (archive on `/new`) |
| Main agent context delivery | **Stdin assembly** (static TOML, dynamic stdin) |
| Agent directory layout | **Hybrid**: flat `agents/` for TOML, parallel `skills/` for resources |
| Main agent tools | **`read_file`, `write_file`, `edit_file`, `list_directory`, `run_command`** |
| Telegram mode | **Long-polling** (60s timeout) |
| Configuration | **Separate `~/.config/hilt/config.toml`** + env vars for API keys |
| Execution model | **Shell out to `axe` binary** with `--agents-dir`, `--json`, `--workdir` |
| Command resolution | **Built-ins → workflows → agents** |
| `/delete` behavior | **Removes TOML + skills/ resources + workflows** |
| Token tracking | **tiktoken-go estimation + Axe actual JSON reports** |
| Token warning | **80% of model context window** |
| Workspace directory | **Configurable, default `~/.hilt`** |
| Memory ownership | **Agent-owned** (Hilt creates dirs, agent creates content) |
| Context format | **Markdown headers (Option B)** |
| System prompt split | **Permanent memory instructions in `main.toml` system_prompt; workspace rules in AGENTS.md (session-start stdin only)** |
| History on turn 2+ | **User's actual short message only** (not full context block) |
| Session JSON title | **Auto-generated from first message** |
| Error communication | **Categorized prefix + raw stderr (truncated)** |
