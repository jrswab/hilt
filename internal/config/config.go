// Package config loads and validates the TOML configuration file for Hilt.
// It expects a config.toml containing settings for the Telegram bot token,
// workspace directory, model selection, context window limits, agent definitions,
// and session TTL.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/jrswab/hilt/internal"
)

// Config holds the TOML configuration for Hilt.
type Config struct {
	TelegramBotToken      string         `toml:"telegram_bot_token"`
	WorkspaceDir          string         `toml:"workspace_dir"`
	SessionTTLDays        int            `toml:"session_ttl_days"`
	MainAgentModel        string         `toml:"main_agent_model"`
	ContextWindowDefault  int            `toml:"context_window_default"`
	MaintenanceAgentModel string         `toml:"maintenance_agent_model"`
	WhisperModel          string         `toml:"whisper_model"`
	AllowedUserIDs        []int64        `toml:"allowed_user_ids"`
	Models                map[string]int `toml:"models"`
}

// validate checks that all required fields in c are populated with sensible values.
func validate(c *Config) error {
	if c.TelegramBotToken == "" {
		return fmt.Errorf("telegram_bot_token is required: %w", internal.ErrInvalidConfig)
	}
	if c.WorkspaceDir == "" {
		return fmt.Errorf("workspace_dir is required: %w", internal.ErrInvalidConfig)
	}
	if c.SessionTTLDays <= 0 {
		return fmt.Errorf("session_ttl_days must be > 0: %w", internal.ErrInvalidConfig)
	}
	if c.ContextWindowDefault <= 0 {
		return fmt.Errorf("context_window_default must be > 0: %w", internal.ErrInvalidConfig)
	}
	validWhisper := map[string]bool{"tiny": true, "base": true, "small": true, "medium": true, "large": true}
	if !validWhisper[c.WhisperModel] {
		return fmt.Errorf("whisper_model must be one of tiny, base, small, medium, large: %w", internal.ErrInvalidConfig)
	}
	for name, cw := range c.Models {
		if cw <= 0 {
			return fmt.Errorf("model %s context window must be > 0: %w", name, internal.ErrInvalidConfig)
		}
	}
	return nil
}

// createDefaults creates default config files and directories if they do not exist.
// It does not overwrite existing files. It returns a default Config.
func createDefaults(configDir, workspaceDir string) (*Config, error) {
	// Expand workspace dir
	expandedWorkspace, err := expandPath(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("expanding workspace dir: %w", err)
	}

	// Check if workspace exists but is a file
	fi, err := os.Stat(expandedWorkspace)
	if err == nil && !fi.IsDir() {
		return nil, fmt.Errorf("workspace path exists but is not a directory: %s", expandedWorkspace)
	}

	// Create workspace dir if missing
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(expandedWorkspace, 0755); err != nil {
				return nil, fmt.Errorf("creating workspace dir: %w", err)
			}
		} else {
			return nil, fmt.Errorf("checking workspace dir: %w", err)
		}
	}

	// Create config dir if missing
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("creating config dir: %w", err)
	}

	configPath := filepath.Join(configDir, "config.toml")

	// Write default config.toml only if absent
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := `# Hilt configuration
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
`
		if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
			return nil, fmt.Errorf("writing config.toml: %w", err)
		}
	}

	// Create agents subdir
	agentsDir := filepath.Join(configDir, "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return nil, fmt.Errorf("creating agents dir: %w", err)
	}

	// Write agents/main.toml only if absent
	mainAgentPath := filepath.Join(agentsDir, "main.toml")
	if _, err := os.Stat(mainAgentPath); os.IsNotExist(err) {
		mainAgentStub := `system_prompt = "You are Hilt's main agent."
tools = []
`
		if err := os.WriteFile(mainAgentPath, []byte(mainAgentStub), 0644); err != nil {
			return nil, fmt.Errorf("writing agents/main.toml: %w", err)
		}
	}

	// Write AGENTS.md only if absent
	agentsMdPath := filepath.Join(expandedWorkspace, "AGENTS.md")
	if _, err := os.Stat(agentsMdPath); os.IsNotExist(err) {
		agentsMdStub := "# Hilt Agents\n\nDefine your agents in the `agents/` directory.\n"
		if err := os.WriteFile(agentsMdPath, []byte(agentsMdStub), 0644); err != nil {
			return nil, fmt.Errorf("writing AGENTS.md: %w", err)
		}
	}

	cfg := &Config{
		TelegramBotToken:      "",
		WorkspaceDir:          "~/.hilt",
		SessionTTLDays:        30,
		MainAgentModel:        "anthropic/claude-3-haiku-4-5",
		ContextWindowDefault:  200000,
		MaintenanceAgentModel: "openrouter/deepseek/deepseek-chat",
		WhisperModel:          "small",
		AllowedUserIDs:        []int64{},
		Models:                map[string]int{},
	}
	return cfg, nil
}

// loadFromFile parses a TOML config file without resolving tokens or validating.
func loadFromFile(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return &cfg, nil
}

// postProcess expands workspace_dir, ensures it exists, resolves the bot token,
// and validates the config. It mutates cfg in place.
func postProcess(cfg *Config) error {
	expandedWorkspace, err := expandPath(cfg.WorkspaceDir)
	if err != nil {
		return fmt.Errorf("expanding workspace_dir: %w", err)
	}
	wsFi, err := os.Stat(expandedWorkspace)
	if err == nil && !wsFi.IsDir() {
		return fmt.Errorf("workspace_dir %q exists but is not a directory: %w", expandedWorkspace, internal.ErrInvalidConfig)
	}
	if os.IsNotExist(err) {
		if err := os.MkdirAll(expandedWorkspace, 0755); err != nil {
			return fmt.Errorf("creating workspace_dir: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("checking workspace_dir: %w", err)
	}
	cfg.WorkspaceDir = expandedWorkspace

	cfg.TelegramBotToken = resolveBotToken(cfg.TelegramBotToken)
	if err := validate(cfg); err != nil {
		return err
	}
	return nil
}

// Load reads and validates the config file at the given path.
// If path is empty, it defaults to ~/.config/hilt/config.toml.
// If the config file is missing or empty, default files are created.
func Load(path string) (*Config, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolving home dir: %w", err)
		}
		path = filepath.Join(home, ".config", "hilt", "config.toml")
	}

	expandedPath, err := expandPath(path)
	if err != nil {
		return nil, fmt.Errorf("expanding config path: %w", err)
	}

	configDir := filepath.Dir(expandedPath)
	workspaceDir := "~/.hilt" // default workspace path for first-run creation

	var cfg *Config

	fi, err := os.Stat(expandedPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg, err = createDefaults(configDir, workspaceDir)
			if err != nil {
				return nil, fmt.Errorf("creating defaults: %w", err)
			}
			if err := postProcess(cfg); err != nil {
				return nil, err
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("checking config file: %w", err)
	}

	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("config file %s is not a regular file: %w", expandedPath, internal.ErrInvalidConfig)
	}

	if fi.Size() == 0 {
		if err := os.Remove(expandedPath); err != nil {
			return nil, fmt.Errorf("removing empty config file: %w", err)
		}
		cfg, err = createDefaults(configDir, workspaceDir)
		if err != nil {
			return nil, fmt.Errorf("creating defaults: %w", err)
		}
		if err := postProcess(cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}

	cfg, err = loadFromFile(expandedPath)
	if err != nil {
		return nil, err
	}

	if err := postProcess(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// resolveBotToken returns the bot token to use.
// TOML value takes precedence; if empty, it falls back to TELEGRAM_BOT_TOKEN env var
// (ignoring env vars that are whitespace-only).
func resolveBotToken(tomlValue string) string {
	if tomlValue != "" {
		return tomlValue
	}
	return strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
}

// expandPath expands a leading tilde to the user's home directory.
// Absolute and relative paths are returned unchanged.
func expandPath(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	if path == "~" {
		return home, nil
	}

	return strings.Replace(path, "~", home, 1), nil
}
