#!/bin/sh
# Hilt Install Script
# Installs the Hilt development environment and initial configuration.
#
# Usage:
#   ./install.sh
#
# This script is interactive and must be run directly (not piped).
# It is a living document. As Hilt grows, new setup steps are added
# here incrementally — following the tracer-bullet ethos.

set -e

# Refuse to run if stdin is not a terminal — all reads would fail
if [ ! -t 0 ]; then
    echo "Error: This script is interactive and cannot be piped." >&2
    echo "  Run directly: ./install.sh" >&2
    exit 1
fi

# Colors for output (disabled if not a tty)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    NC='\033[0m' # No Color
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    NC=''
fi

info()  { printf "${BLUE}→${NC} %s\n" "$1"; }
ok()    { printf "${GREEN}✓${NC} %s\n" "$1"; }
warn()  { printf "${YELLOW}⚠${NC} %s\n" "$1"; }
error() { printf "${RED}✗${NC} %s\n" "$1"; }

# ──────────────────────────────────────────────
# Defaults
# ──────────────────────────────────────────────
HILT_CONFIG_DIR="${HOME}/.config/hilt"
HILT_WORKSPACE_DEFAULT="${HOME}/.hilt"
HILT_VERSION="0.1.0"

# ──────────────────────────────────────────────
# Dependency checks
# ──────────────────────────────────────────────
info "Checking dependencies..."

if ! command -v go >/dev/null 2>&1; then
    error "Go is not installed. Hilt requires Go 1.23+."
    error "  macOS: brew install go"
    error "  Linux: https://go.dev/doc/install"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
# Extract numeric major.minor, stripping any suffixes (e.g., "1.23rc1" → "1.23")
GO_NUMERIC=$(printf '%s' "$GO_VERSION" | sed -E 's/^([0-9]+\.[0-9]+).*/\1/')
GO_MAJOR=$(printf '%s' "$GO_NUMERIC" | cut -d. -f1)
GO_MINOR=$(printf '%s' "$GO_NUMERIC" | cut -d. -f2)

# Validate that we actually got numbers
if ! printf '%s' "$GO_MAJOR" | grep -qE '^[0-9]+$'; then
    error "Could not parse Go version: $GO_VERSION"
    exit 1
fi
if ! printf '%s' "$GO_MINOR" | grep -qE '^[0-9]+$'; then
    error "Could not parse Go version: $GO_VERSION"
    exit 1
fi

if [ "$GO_MAJOR" -lt 1 ] || { [ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 23 ]; }; then
    error "Go $GO_VERSION detected. Hilt requires Go 1.23+."
    exit 1
fi
ok "Go $GO_VERSION found"

# ffmpeg is only needed for voice transcription (post-MVP)
if command -v ffmpeg >/dev/null 2>&1; then
    ok "ffmpeg found (voice transcription ready)"
else
    warn "ffmpeg not found. Voice message transcription will be unavailable."
    warn "  Install with: brew install ffmpeg  (macOS)"
    warn "            or: sudo apt install ffmpeg  (Debian/Ubuntu)"
fi

# ──────────────────────────────────────────────
# Interactive configuration
# ──────────────────────────────────────────────
echo ""
info "Hilt v${HILT_VERSION} Setup"
echo ""

# Telegram bot token
printf "Telegram Bot Token (from @BotFather): "
read -r BOT_TOKEN
if [ -z "$BOT_TOKEN" ]; then
    warn "No bot token provided. You can set TELEGRAM_BOT_TOKEN later."
fi

# API keys — just confirm env var setup
printf "\n"
info "API Keys"
echo "Hilt reads API keys from environment variables (e.g., ANTHROPIC_API_KEY)."
echo "Ensure your secret manager (Doppler, 1Password, etc.) exports these before running Hilt."
printf "[Press Enter to continue]"
read -r DUMMY

# Workspace directory
printf "\nWorkspace directory [${HILT_WORKSPACE_DEFAULT}]: "
read -r WORKSPACE_DIR
if [ -z "$WORKSPACE_DIR" ]; then
    WORKSPACE_DIR="$HILT_WORKSPACE_DEFAULT"
fi
# Expand ~ to $HOME safely — avoids sed replacement-string pitfalls and
# case-pattern tilde-expansion ambiguity in POSIX sh.
_first_char=$(printf '%s' "$WORKSPACE_DIR" | cut -c1)
if [ "$_first_char" = "~" ]; then
    _rest=$(printf '%s' "$WORKSPACE_DIR" | cut -c2-)
    if [ "$_rest" = "" ]; then
        WORKSPACE_DIR="${HOME}"
    else
        WORKSPACE_DIR="${HOME}${_rest}"
    fi
fi
unset _first_char _rest

# Main agent model
printf "Main agent model [anthropic/claude-3-haiku-4-5]: "
read -r MAIN_MODEL
if [ -z "$MAIN_MODEL" ]; then
    MAIN_MODEL="anthropic/claude-3-haiku-4-5"
fi

printf "Session TTL (days) [30]: "
read -r SESSION_TTL
if [ -z "$SESSION_TTL" ]; then
    SESSION_TTL=30
fi

# ──────────────────────────────────────────────
# Create directory structure
# ──────────────────────────────────────────────
echo ""
info "Creating directory structure..."

mkdir -p "${HILT_CONFIG_DIR}/agents"
mkdir -p "${HILT_CONFIG_DIR}/skills"
mkdir -p "${HILT_CONFIG_DIR}/whisper/models"
mkdir -p "${WORKSPACE_DIR}/memory"
mkdir -p "${WORKSPACE_DIR}/projects"

ok "~/.config/hilt/"
ok "~/.config/hilt/agents/"
ok "~/.config/hilt/skills/"
ok "~/.config/hilt/whisper/"
ok "${WORKSPACE_DIR}/"
ok "${WORKSPACE_DIR}/memory/"
ok "${WORKSPACE_DIR}/projects/"

# ──────────────────────────────────────────────
# Write config.toml
# ──────────────────────────────────────────────
info "Writing config.toml..."

if [ -f "${HILT_CONFIG_DIR}/config.toml" ]; then
    BACKUP="${HILT_CONFIG_DIR}/config.toml.bak.$(date +%Y%m%d%H%M%S)"
    warn "config.toml already exists. Creating backup at ${BACKUP}"
    cp "${HILT_CONFIG_DIR}/config.toml" "$BACKUP"
fi

cat > "${HILT_CONFIG_DIR}/config.toml" <<EOF
# Hilt behavior settings (non-secrets)
# See docs/plans/000_hilt_design.md for full documentation.

telegram_bot_token = "${BOT_TOKEN}"
workspace_dir = "${WORKSPACE_DIR}"
session_ttl_days = ${SESSION_TTL}
main_agent_model = "${MAIN_MODEL}"
context_window_default = 200000
maintenance_agent_model = "openrouter/deepseek/deepseek-chat"
whisper_model = "small"

# Allowed Telegram user IDs. Empty = no restriction (development mode).
# Hilt drops messages from unauthorized users silently.
allowed_user_ids = []

# Model context window overrides and additions.
# Models known to Hilt have built-in defaults. Add or override here:
[models]
"grok-4" = 131072
EOF

ok "${HILT_CONFIG_DIR}/config.toml"

# ──────────────────────────────────────────────
# Write stub default agent files
# ──────────────────────────────────────────────
info "Creating default agent files..."

if [ ! -f "${HILT_CONFIG_DIR}/agents/main.toml" ]; then
    cat > "${HILT_CONFIG_DIR}/agents/main.toml" <<EOF
name = "main"
model = "${MAIN_MODEL}"
system_prompt = """
You are Hilt, a lightweight and efficient coding assistant.
You have access to file tools (read, write, edit, list) and a sandboxed run_command.
You work from the user's workspace directory.
Memory is managed via daily notes and critical state files — use these when relevant.
"""
tools = ["read_file", "write_file", "edit_file", "list_directory", "run_command"]
EOF
    ok "agents/main.toml"
else
    ok "agents/main.toml (already exists)"
fi

# ──────────────────────────────────────────────
# Write workspace AGENTS.md
# ──────────────────────────────────────────────
if [ ! -f "${WORKSPACE_DIR}/AGENTS.md" ]; then
    cat > "${WORKSPACE_DIR}/AGENTS.md" <<EOF
# Workspace Rules

## Context
- Add project-specific rules, conventions, and priorities here.
- Hilt loads this file on turn 1 of every new session.

## Coding Standards
- Write Go code with explicit error handling.
- Prefer composition over inheritance.
- Document exported APIs.

## Tools
- Use \`read_file\` to inspect existing code.
- Use \`write_file\` for new files, \`edit_file\` for targeted changes.
- \`run_command\` is sandboxed to the workspace directory.
EOF
    ok "${WORKSPACE_DIR}/AGENTS.md"
else
    ok "${WORKSPACE_DIR}/AGENTS.md (already exists)"
fi

# ──────────────────────────────────────────────
# Write today's daily note skeleton
# ──────────────────────────────────────────────
TODAY=$(date +%Y-%m-%d)
DAILY_NOTE="${WORKSPACE_DIR}/memory/${TODAY}.md"
if [ ! -f "$DAILY_NOTE" ]; then
    cat > "$DAILY_NOTE" <<EOF
# ${TODAY}

## Wing: Work

### Hall: decisions

### Hall: events

### Hall: discoveries

### Hall: tasks

### Hall: blockers

## Wing: Health

### Hall: metrics
EOF
    ok "memory/${TODAY}.md"
else
    ok "memory/${TODAY}.md (already exists)"
fi

# ──────────────────────────────────────────────
# Systemd unit file
# ──────────────────────────────────────────────
info "Writing systemd service template..."

if [ -f "${HILT_CONFIG_DIR}/hilt.service" ]; then
    BACKUP="${HILT_CONFIG_DIR}/hilt.service.bak.$(date +%Y%m%d%H%M%S)"
    warn "hilt.service already exists. Creating backup at ${BACKUP}"
    cp "${HILT_CONFIG_DIR}/hilt.service" "$BACKUP"
fi

cat > "${HILT_CONFIG_DIR}/hilt.service" <<EOF
[Unit]
Description=Hilt — Telegram LLM Assistant
After=network.target

[Service]
Type=simple
ExecStart=%h/go/bin/hilt
Restart=on-failure
RestartSec=5
Environment="HOME=%h"

# Ensure your API keys are available — e.g., via a secret manager wrapper
# or by adding Environment= lines here. Example:
# Environment="ANTHROPIC_API_KEY=sk-..."

[Install]
WantedBy=default.target
EOF

ok "${HILT_CONFIG_DIR}/hilt.service"

# ──────────────────────────────────────────────
# Build Hilt binary
# ──────────────────────────────────────────────
echo ""
info "Building Hilt..."

if [ -f "go.mod" ] && [ -d "cmd/hilt" ]; then
    go build -o "${HOME}/go/bin/hilt" ./cmd/hilt
    ok "Binary built: ${HOME}/go/bin/hilt"
else
    warn "Go module not found in current directory. Skipping build."
    warn "  Run this script from the repository root, then: go build -o ~/go/bin/hilt ./cmd/hilt"
fi

# ──────────────────────────────────────────────
# Summary
# ──────────────────────────────────────────────
echo ""
echo "═════════════════════════════════════════════════"
info "Hilt v${HILT_VERSION} installed successfully"
echo "═════════════════════════════════════════════════"
echo ""
echo "  Config directory:  ${HILT_CONFIG_DIR}"
echo "  Workspace:         ${WORKSPACE_DIR}"
echo "  Binary:            ${HOME}/go/bin/hilt"
echo ""
echo "Next steps:"
echo "  1. Ensure API keys are exported in your environment."
echo "  2. Run: hilt -log-level=debug"
echo "  3. Or start with systemd:"
echo "       cp ${HILT_CONFIG_DIR}/hilt.service ~/.config/systemd/user/"
echo "       systemctl --user daemon-reload"
echo "       systemctl --user enable --now hilt"
echo ""
echo "  Use /new in Telegram to start a session."
echo ""

# ──────────────────────────────────────────────
# whisper.cpp (post-MVP — placeholder)
# ──────────────────────────────────────────────
# TODO: Download whisper.cpp prebuilt binary for host platform.
# TODO: Download default "small" model (~466MB).
# TODO: Verify ffmpeg is installed for voice message conversion.
