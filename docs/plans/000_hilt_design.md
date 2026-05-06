# Hilt Design Document

This document captures the architecture and intent behind Hilt — a lightweight, fast, and efficient LLM assistant triggered from Telegram.

**Goal:** A main agent for general chat with persistent memory, plus specific, self-contained command agents for skills and user-defined multi-agent workflows.

**Design principles:**
- Composable
- Command focused
- Minimal "chat"
- An assistant, not a therapist or friend

**Flow:** Telegram message → Go webserver (stateful orchestrator) → Assemble context → Axe `pkg/runner` (execution engine) → Return response to Telegram.

> **Milestone 1 Blocker:** This design depends on [Axe Issue #80](https://github.com/jrswab/axe/issues/80) — extracting Axe's core execution logic into a public `pkg/runner` Go package so Hilt can import it as a library rather than shelling out to the CLI.

---

## Go Server

The Go server is a **stateful orchestrator**. It manages session state, memory tiering, token bookkeeping, Telegram integration, and workflow execution. It does NOT talk to LLMs directly — that is Axe's job, invoked via imported `pkg/runner`.

### Responsibilities
- Receive Telegram messages via **long-polling** (`getUpdates` with 60s timeout)
- Route messages:
  - Messages starting with `/` → command resolution
  - Everything else → main agent
- Manage session persistence (SQLite database)
- Assemble context for the main agent (memory layers, conversation history)
- Track token budgets and warn at 80%
- Orchestrate multi-agent workflows
- Transcribe voice messages via whisper.cpp

### Telegram Integration
- **Mode:** Long-polling (default), configurable via `TELEGRAM_MODE`
- **Why long-polling:** No public IP, TLS cert, or domain required. Works from any VPS or home server. Latency is ~network RTT because `getUpdates` with `timeout=60` holds the connection open until a message arrives.
- **Supported message types:** Text and voice messages. Voice messages are transcribed to text before routing. All other message types receive an auto-reply: "Hilt only processes text and voice messages."

---

## Session Model

- **One active session globally** (single-user design).
- Sessions are persisted to an SQLite database.
- **`/new`** starts a fresh session (archives the current one) **without blocking** — memory maintenance runs in a background goroutine.
- **`/sessions`** lists archived sessions within the TTL window.
- **TTL-pruning** removes old archived sessions automatically.

### Session Structure (SQLite)

```sql
CREATE TABLE sessions (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    title               TEXT,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_activity       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    archived_at         DATETIME,           -- NULL = active session
    total_input_tokens  INTEGER DEFAULT 0,
    total_output_tokens INTEGER DEFAULT 0,
    turns_json          TEXT NOT NULL       -- JSON array of turn objects
);
```

- `title`: Auto-generated from the first user message (truncated to ~40 chars).
- `turns_json`: The full conversation history used to assemble context for subsequent turns.
- Only one row may have `archived_at IS NULL` (enforced by application logic).

### Session Lifecycle
1. **New session:** Hilt loads AGENTS.md (workspace-specific rules), `memory/critical.md` (L1), today + yesterday's daily notes (L2). These are assembled into the first turn's context and sent to Axe via `runner.Run()`.
2. **Ongoing turns:** Hilt assembles the full conversation history from `turns_json` plus the current user message, passing them as a proper message slice to `pkg/runner`.
3. **Session end:** `/new`, TTL expiry, or server restart (session is already persisted).
4. **Memory maintenance on `/new`:** When a session is archived, Hilt spawns a background goroutine that sends the session transcript to a lightweight memory maintenance agent. This agent appends to today's daily note, adds graph triples, and updates `critical.md` if needed. Maintenance is **non-blocking** — the user can start the new session immediately.

---

## Directory Layout

### Hilt Configuration & Data: `~/.config/hilt/`
```
~/.config/hilt/
├── config.toml          # Hilt behavior settings (non-secrets)
├── hilt.sqlite          # Session and workflow persistence
├── agents/              # Flat TOML files for Axe discovery
│   ├── main.toml        # Main agent (system prompt + tools)
│   ├── edit-file.toml   # Skill: edit file
│   └── summarize.toml   # Skill: summarize
├── skills/              # Parallel resource tree (scripts, docs)
│   ├── edit-file/
│   │   └── scripts/
│   └── summarize/
│       └── SKILL.md
└── whisper/             # whisper.cpp binary and models (auto-downloaded)
    ├── whisper-cli
    └── models/
```

### Workspace: `~/.hilt/` (configurable, default)
```
~/.hilt/
├── AGENTS.md            # Workspace-specific rules, tunnels, priorities
├── memory/
│   ├── critical.md      # L1 — compressed current state (~200 tokens)
│   ├── graph.md         # L3 — temporal knowledge graph
│   ├── 2026-05-04.md    # L2 — daily note (Wing/Hall/Room format)
│   └── 2026-05-04-topic.md
└── projects/
    └── project-a/
        └── AGENTS.md
```

### Axe Integration
- Hilt imports Axe via `github.com/jrswab/axe/pkg/runner`.
- `runner.Options` configures agents directory, workspace, model, messages, tools, etc.
- `runner.Result` returns content, input/output tokens, stop reason, and tool call details.
- Axe's config (`~/.config/axe/config.toml`) and environment variables (e.g., `ANTHROPIC_API_KEY`) are used for provider/API configuration.

---

## Agent Types

### Main Agent
- **Static TOML:** `~/.config/hilt/agents/main.toml`
- **System prompt (in TOML):** Permanent identity, memory system overview, search procedures, daily note format, graph triple rules, and tool guidance. Loaded by Axe on **every turn**.
- **Dynamic context (assembled by Hilt):** AGENTS.md content (turn 1 only), L1/L2 memory (turn 1 only), conversation history (turn 2+), current message.
- **Tools:** `read_file`, `write_file`, `edit_file`, `list_directory`, `run_command`
  - NO `url_fetch`, NO `web_search`
  - `run_command` is sandboxed to `~/.hilt/` by Axe (path traversal blocked)
- **Memory ownership:** Hilt owns the memory structure and schemas. The agent writes content to well-defined files via tools.

### Command Agents (Skills)
- Triggered by `/agent-name` from Telegram.
- Self-contained TOML files in `~/.config/hilt/agents/`.
- Can have parallel resources in `~/.config/hilt/skills/{agent-name}/`.
- Executed via `runner.Run()` with the agent name and agents directory.
- Session context injection is **opt-in**: If the agent TOML contains `[hilt] load_session = true`, Hilt prepends a compressed session summary to the skill's prompt. Otherwise, the skill runs in isolation.

### Workflows
- **No shell scripts.** Workflows are multi-agent orchestrations defined in SQLite and executed by Hilt in Go using `runner.Run()`.
- Created interactively via the `/flow` command: user picks agents, their order, pause flags, and optional overrides.
- **Execution:** Auto-advance with streaming status updates. Each step's output is posted to Telegram prefixed with `[step N/N: agent-name]`.
- **Human-in-the-loop:** Steps with `requires_user_input: true` pause after execution. Hilt posts the step output plus a prompt, waits for the user's reply, then resumes.

---

## Workflow Step Schema

Workflows are stored in SQLite with `steps_json` as a JSON array:

```sql
CREATE TABLE workflows (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    steps_json TEXT NOT NULL
);
```

Each step:

```json
{
  "agent": "code-review",
  "model": "anthropic/claude-3-haiku-4-5",
  "load_session": false,
  "requires_user_input": false,
  "prompt_override": "Extra instructions for this step"
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `agent` | yes | Name matching a TOML in `~/.config/hilt/agents/` |
| `model` | no | Override the agent's default model for this step |
| `load_session` | no | Force session context injection regardless of agent TOML setting |
| `requires_user_input` | no | Pause after this step and wait for user reply (default: false) |
| `prompt_override` | no | Prepend this text to the step's input |

---

## Skills & Commands

### Built-in Commands (Hilt executes directly, no LLM call)
- `/skills` / `/commands` — List available command agents and workflows
- `/flow` / `/workflow` — Create, list, or invoke workflows
- `/delete <name>` — Remove agent TOML + parallel `skills/` resources, or delete a workflow
- `/sessions` — List archived sessions within TTL
- `/new` — Start a fresh session (archive current, non-blocking)

### Command Resolution Order
1. Built-in command? → Execute in Go
2. Workflow name in SQLite? → Execute workflow via Go orchestration
3. Agent TOML in `agents/`? → `runner.Run()` with agent name
4. Unknown → "Command not found. Use /skills to see available commands."

### Skill TOML Extensions
Hilt-specific metadata lives in a `[hilt]` table in the agent TOML. Axe's `toml.Unmarshal` silently ignores unknown sections, so no Axe changes are needed.

```toml
# ~/.config/hilt/agents/summarize.toml
name = "summarize"
model = "anthropic/claude-3-haiku-4-5"
system_prompt = "..."
tools = ["read_file"]

[hilt]
load_session = true
description = "Summarize the current session context"
```

### Skill Creation
The main agent can create new skills by writing TOML files to `~/.config/hilt/agents/` and resources to `~/.config/hilt/skills/`. The main agent does NOT know the list of existing skills. If asked to run a skill, it tells the user to use a slash command.

---

## Memory System

Hilt implements the Memory Palace design (see `memory-system-walkthrough.md`). Hilt **owns the structure**; the agent writes content via tools.

### Session Startup Loading (Turn 1 Only)
| Layer | Source | Loaded |
|-------|--------|--------|
| L0 | `main.toml` system_prompt | Every turn by Axe |
| L0 | `~/.hilt/AGENTS.md` | Turn 1 stdin |
| L1 | `~/.hilt/memory/critical.md` | Turn 1 stdin |
| L2 | `~/.hilt/memory/YYYY-MM-DD.md` (today + yesterday) | Turn 1 stdin |

### Turn 1 Context Assembly Format
Hilt assembles the first turn using hierarchical Markdown headers:

```markdown
## Workspace Context

### Rules (AGENTS.md)
[content]

### Critical State
[content of critical.md]

## Recent Memory

### Note: YYYY-MM-DD
[content of today's note]

### Note: YYYY-MM-DD
[content of yesterday's note]

## Current Task

[user's actual message here]
```

`## Current Task` is the bottom section so the model treats it as the primary instruction.

### Ongoing Turns
- Hilt passes the full conversation history as a `[]runner.Message` slice to `pkg/runner`, plus the current user message.
- Agent uses `read_file` and `run_command` (grep/rg) to load L3 memory on demand.

### Maintenance
- **`critical.md`:** Weekly full rewrite (background maintenance agent)
- **Daily notes:** Appended during session by the main agent; finalized by background maintenance on `/new`
- **Graph triples:** Added during session when qualifying events occur
- **Closet summaries:** On demand when scanning historical notes >7 days old
- **Daily note creation:** Hilt creates an empty `memory/YYYY-MM-DD.md` skeleton at session start if missing.

---

## Token Budget Management

- **No proactive token estimation.** No `tiktoken-go` dependency.
- **Ground truth:** `runner.Result.InputTokens` reports actual input tokens per turn.
- **Tracking:** Hilt tracks `max_input_tokens_seen` per session (per-turn peak, **not cumulative**).
- **Warning at 80%:** When `result.InputTokens > context_window * 0.8`, Hilt appends a Telegram warning: `⚠️ Session using X% of context window. Use /new to start fresh.`
- **Hard limit:** Pass `MaxTokens` to `runner.Options` as a safety net.
- **Model context windows:** Hardcoded map of known models with configurable fallback.

---

## Error Handling

Axe `runner.Run()` returns Go-idiomatic typed errors. Hilt switches on error type to produce user-friendly Telegram messages:

| Error | User Message |
|-------|-------------|
| `runner.ErrInvalidRequest` | "I couldn't process that request." + details |
| `runner.ErrConfig` | "Configuration issue." + details |
| `runner.ErrUnavailable` | "Service temporarily unavailable." + details |
| `runner.ErrBudgetExceeded` | "Token limit reached. Start a new session with /new." + details |
| `runner.ErrToolFailure` | "A tool failed during execution." + details |

---

## Voice Message Transcription

- Voice messages are transcribed locally using **whisper.cpp**.
- **Deployment:** whisper.cpp compiled as a static binary, downloaded at install time for the user's platform (or built from a git submodule).
- **Process:** Telegram sends voice as OGG Opus. Hilt converts if needed, shells out to `whisper-cli`, receives text, routes it as a text message.
- **Privacy:** All transcription is local. No voice data leaves the machine.

---

## Configuration

### `~/.config/hilt/config.toml`
```toml
# Hilt behavior settings (no secrets)
telegram_bot_token = ""   # Can also use TELEGRAM_BOT_TOKEN env var
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

An install script collects required data and sets up the environment:
- Telegram bot token
- Confirmation of environment variable setup (API keys via Doppler or other secret manager)
- Workspace directory path (defaults to `~/.hilt`)
- Creates directory structure (`~/.config/hilt/`, `~/.hilt/memory/`, etc.)
- Downloads whisper.cpp binary for the host platform
- Provides systemd unit file template

---

## Resolved Decisions

| Decision | Resolution |
|----------|------------|
| Stateless vs stateful | **Stateful orchestrator** |
| Session persistence | **SQLite** (`~/.config/hilt/hilt.sqlite`) with pure-Go `modernc.org/sqlite` |
| Sessions per user | **One active globally** (single-user design) |
| Main agent context delivery | **Message slice to `pkg/runner`** (proper history, not stdin embedding) |
| Agent directory layout | **Hybrid**: flat `agents/` for TOML, parallel `skills/` for resources |
| Main agent tools | **`read_file`, `write_file`, `edit_file`, `list_directory`, `run_command`** |
| Telegram mode | **Long-polling** (60s timeout) |
| Configuration | **Separate `~/.config/hilt/config.toml`** + env vars for API keys |
| Execution model | **Import Axe `pkg/runner`** (blocked on Axe Issue #80) |
| Command resolution | **Built-ins → workflows → agents** |
| `/delete` behavior | **Removes TOML + skills/ resources + workflows** |
| Token tracking | **Per-turn reactive** — `result.InputTokens` from Axe, no tiktoken-go |
| Token warning | **80% of model context window** based on per-turn input |
| Workspace directory | **Configurable, default `~/.hilt`** |
| Memory ownership | **Hilt owns structure**, agent writes content via tools |
| Context format | **Hierarchical Markdown headers with role labels** |
| System prompt split | **Permanent memory instructions in `main.toml`; workspace rules in AGENTS.md (turn 1 only)** |
| History on turn 2+ | **Proper message slice via `pkg/runner`** |
| Session title | **Auto-generated from first message** |
| Error communication | **Go-idiomatic typed errors from `pkg/runner`** |
| Concurrency model | **Single goroutine** (single-user, simple) |
| Session context for skills | **Opt-in via `[hilt] load_session = true` in agent TOML** |
| Workflow definition | **Named and persistent in SQLite**, created via interactive `/flow` |
| Workflow execution | **Go-orchestrated `runner.Run()` calls**, streaming status, auto-advance |
| Workflow human-in-the-loop | **Declarative `requires_user_input` flag per step** |
| Skill TOML extensions | **`[hilt]` table** ignored by Axe, parsed by Hilt |
| Workflow security | **No shell scripts** — pure Go orchestration via Axe library |
| Voice messages | **Transcribed locally via whisper.cpp**, downloaded at install |
| Memory maintenance on `/new` | **Background goroutine**, non-blocking to user |
