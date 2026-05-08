package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrswab/hilt/internal"
)

func TestLoad(t *testing.T) {
	t.Run("valid config file", func(t *testing.T) {
		workspaceDir := t.TempDir()
		configDir := t.TempDir()
		configContent := fmt.Sprintf(`telegram_bot_token = "toml-token"
workspace_dir = "%s"
session_ttl_days = 42
main_agent_model = "gpt-4"
context_window_default = 1000
maintenance_agent_model = "gpt-3.5"
whisper_model = "base"
allowed_user_ids = [123, 456]

[models]
gpt-4 = 8000
`, workspaceDir)
		configPath := filepath.Join(configDir, "config.toml")
		os.WriteFile(configPath, []byte(configContent), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load unexpected error: %v", err)
		}

		if cfg.TelegramBotToken != "toml-token" {
			t.Errorf("TelegramBotToken = %q, want toml-token", cfg.TelegramBotToken)
		}
		if cfg.WorkspaceDir != workspaceDir {
			t.Errorf("WorkspaceDir = %q, want %s", cfg.WorkspaceDir, workspaceDir)
		}
		if cfg.SessionTTLDays != 42 {
			t.Errorf("SessionTTLDays = %d, want 42", cfg.SessionTTLDays)
		}
		if cfg.MainAgentModel != "gpt-4" {
			t.Errorf("MainAgentModel = %q, want gpt-4", cfg.MainAgentModel)
		}
		if cfg.ContextWindowDefault != 1000 {
			t.Errorf("ContextWindowDefault = %d, want 1000", cfg.ContextWindowDefault)
		}
		if cfg.WhisperModel != "base" {
			t.Errorf("WhisperModel = %q, want base", cfg.WhisperModel)
		}
		if len(cfg.AllowedUserIDs) != 2 || cfg.AllowedUserIDs[0] != 123 || cfg.AllowedUserIDs[1] != 456 {
			t.Errorf("AllowedUserIDs = %v, want [123 456]", cfg.AllowedUserIDs)
		}
		if cfg.Models["gpt-4"] != 8000 {
			t.Errorf("Models[gpt-4] = %d, want 8000", cfg.Models["gpt-4"])
		}
	})

	t.Run("env var fallback for bot token", func(t *testing.T) {
		workspaceDir := t.TempDir()
		configDir := t.TempDir()
		configContent := fmt.Sprintf(`telegram_bot_token = ""
workspace_dir = "%s"
session_ttl_days = 1
context_window_default = 1
whisper_model = "base"
`, workspaceDir)
		configPath := filepath.Join(configDir, "config.toml")
		os.WriteFile(configPath, []byte(configContent), 0644)

		t.Setenv("TELEGRAM_BOT_TOKEN", "env-token")

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load unexpected error: %v", err)
		}
		if cfg.TelegramBotToken != "env-token" {
			t.Errorf("TelegramBotToken = %q, want env-token", cfg.TelegramBotToken)
		}
	})

	t.Run("missing file triggers auto-creation", func(t *testing.T) {
		t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
		configDir := t.TempDir()
		configPath := filepath.Join(configDir, "config.toml")
		// configPath does not exist

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load unexpected error: %v", err)
		}

		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Fatalf("config.toml was not created")
		}
		if cfg.SessionTTLDays != 30 {
			t.Errorf("SessionTTLDays = %d, want 30", cfg.SessionTTLDays)
		}
		if cfg.WhisperModel != "small" {
			t.Errorf("WhisperModel = %q, want small", cfg.WhisperModel)
		}
	})

	t.Run("empty file triggers auto-creation", func(t *testing.T) {
		t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
		configDir := t.TempDir()
		configPath := filepath.Join(configDir, "config.toml")
		os.WriteFile(configPath, []byte{}, 0644) // zero bytes

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load unexpected error: %v", err)
		}

		data, _ := os.ReadFile(configPath)
		if !strings.Contains(string(data), "session_ttl_days = 30") {
			t.Errorf("empty file was not overwritten with defaults")
		}
		if cfg.SessionTTLDays != 30 {
			t.Errorf("SessionTTLDays = %d, want 30", cfg.SessionTTLDays)
		}
	})

	t.Run("invalid TOML returns error", func(t *testing.T) {
		configDir := t.TempDir()
		configPath := filepath.Join(configDir, "config.toml")
		os.WriteFile(configPath, []byte("not valid toml {{"), 0644)

		_, err := Load(configPath)
		if err == nil {
			t.Fatal("expected error for invalid TOML, got nil")
		}
	})
}

func TestResolveBotToken(t *testing.T) {
	original := os.Getenv("TELEGRAM_BOT_TOKEN")
	defer os.Setenv("TELEGRAM_BOT_TOKEN", original)

	tests := []struct {
		name      string
		tomlValue string
		envValue  string
		want      string
	}{
		{
			name:      "toml takes precedence when non-empty",
			tomlValue: "toml-token",
			envValue:  "env-token",
			want:      "toml-token",
		},
		{
			name:      "env var used when toml empty",
			tomlValue: "",
			envValue:  "env-token",
			want:      "env-token",
		},
		{
			name:      "whitespace env var treated as empty",
			tomlValue: "",
			envValue:  "   ",
			want:      "",
		},
		{
			name:      "both empty returns empty",
			tomlValue: "",
			envValue:  "",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue == "" {
				os.Unsetenv("TELEGRAM_BOT_TOKEN")
			} else {
				os.Setenv("TELEGRAM_BOT_TOKEN", tt.envValue)
			}

			got := resolveBotToken(tt.tomlValue)
			if got != tt.want {
				t.Fatalf("resolveBotToken(%q) = %q, want %q", tt.tomlValue, got, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	base := Config{
		TelegramBotToken:      "token",
		WorkspaceDir:          "/tmp",
		SessionTTLDays:        1,
		MainAgentModel:        "gpt-4",
		ContextWindowDefault:  1000,
		MaintenanceAgentModel: "gpt-3.5",
		WhisperModel:          "base",
		AllowedUserIDs:        []int64{1},
		Models:                map[string]int{"gpt-4": 1000},
	}

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "all valid values",
			config:  base,
			wantErr: false,
		},
		{
			name: "empty TelegramBotToken",
			config: func() Config {
				c := base
				c.TelegramBotToken = ""
				return c
			}(),
			wantErr: true,
		},
		{
			name: "empty WorkspaceDir",
			config: func() Config {
				c := base
				c.WorkspaceDir = ""
				return c
			}(),
			wantErr: true,
		},
		{
			name: "SessionTTLDays zero",
			config: func() Config {
				c := base
				c.SessionTTLDays = 0
				return c
			}(),
			wantErr: true,
		},
		{
			name: "SessionTTLDays negative",
			config: func() Config {
				c := base
				c.SessionTTLDays = -1
				return c
			}(),
			wantErr: true,
		},
		{
			name: "ContextWindowDefault zero",
			config: func() Config {
				c := base
				c.ContextWindowDefault = 0
				return c
			}(),
			wantErr: true,
		},
		{
			name: "ContextWindowDefault negative",
			config: func() Config {
				c := base
				c.ContextWindowDefault = -1
				return c
			}(),
			wantErr: true,
		},
		{
			name: "WhisperModel invalid",
			config: func() Config {
				c := base
				c.WhisperModel = "invalid"
				return c
			}(),
			wantErr: true,
		},
		{
			name: "WhisperModel empty",
			config: func() Config {
				c := base
				c.WhisperModel = ""
				return c
			}(),
			wantErr: true,
		},
		{
			name: "model context window zero",
			config: func() Config {
				c := base
				c.Models = map[string]int{"gpt-4": 0}
				return c
			}(),
			wantErr: true,
		},
		{
			name: "model context window negative",
			config: func() Config {
				c := base
				c.Models = map[string]int{"gpt-4": -1}
				return c
			}(),
			wantErr: true,
		},
		{
			name: "MainAgentModel empty",
			config: func() Config {
				c := base
				c.MainAgentModel = ""
				return c
			}(),
			wantErr: false,
		},
		{
			name: "MaintenanceAgentModel empty",
			config: func() Config {
				c := base
				c.MaintenanceAgentModel = ""
				return c
			}(),
			wantErr: false,
		},
		{
			name: "AllowedUserIDs nil",
			config: func() Config {
				c := base
				c.AllowedUserIDs = nil
				return c
			}(),
			wantErr: false,
		},
		{
			name: "AllowedUserIDs empty",
			config: func() Config {
				c := base
				c.AllowedUserIDs = []int64{}
				return c
			}(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(&tt.config)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("validate() expected error, got nil")
				}
				if !errors.Is(err, internal.ErrInvalidConfig) {
					t.Fatalf("expected error wrapping %v, got: %v", internal.ErrInvalidConfig, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validate() unexpected error: %v", err)
			}
		})
	}
}

func TestCreateDefaults(t *testing.T) {
	t.Run("creates all files with defaults", func(t *testing.T) {
		configDir := t.TempDir()
		workspaceDir := t.TempDir()

		cfg, err := createDefaults(configDir, workspaceDir)
		if err != nil {
			t.Fatalf("createDefaults unexpected error: %v", err)
		}

		// Check config.toml
		configPath := filepath.Join(configDir, "config.toml")
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("config.toml not created: %v", err)
		}
		content := string(data)
		for _, s := range []string{
			"# Hilt configuration",
			"telegram_bot_token = \"\"",
			"workspace_dir = \"~/.hilt\"",
			"session_ttl_days = 30",
			"main_agent_model = \"anthropic/claude-3-haiku-4-5\"",
			"context_window_default = 200000",
			"maintenance_agent_model = \"openrouter/deepseek/deepseek-chat\"",
			"whisper_model = \"small\"",
			"[models]",
		} {
			if !strings.Contains(content, s) {
				t.Errorf("config.toml missing %q", s)
			}
		}

		// Check agents/main.toml
		mainAgentPath := filepath.Join(configDir, "agents", "main.toml")
		data, err = os.ReadFile(mainAgentPath)
		if err != nil {
			t.Fatalf("agents/main.toml not created: %v", err)
		}
		if !strings.Contains(string(data), "You are Hilt's main agent.") {
			t.Errorf("agents/main.toml missing expected content")
		}

		// Check AGENTS.md
		agentsMdPath := filepath.Join(workspaceDir, "AGENTS.md")
		data, err = os.ReadFile(agentsMdPath)
		if err != nil {
			t.Fatalf("AGENTS.md not created: %v", err)
		}
		if !strings.Contains(string(data), "# Hilt Agents") {
			t.Errorf("AGENTS.md missing expected content")
		}

		// Check returned config
		if cfg.SessionTTLDays != 30 {
			t.Errorf("SessionTTLDays = %d, want 30", cfg.SessionTTLDays)
		}
		if cfg.ContextWindowDefault != 200000 {
			t.Errorf("ContextWindowDefault = %d, want 200000", cfg.ContextWindowDefault)
		}
		if cfg.WhisperModel != "small" {
			t.Errorf("WhisperModel = %q, want small", cfg.WhisperModel)
		}
		if cfg.WorkspaceDir != "~/.hilt" {
			t.Errorf("WorkspaceDir = %q, want ~/.hilt", cfg.WorkspaceDir)
		}
	})

	t.Run("does not overwrite existing files", func(t *testing.T) {
		configDir := t.TempDir()
		workspaceDir := t.TempDir()

		configContent := `telegram_bot_token = "token"
workspace_dir = "/tmp"
session_ttl_days = 1
context_window_default = 1
whisper_model = "base"
`
		configPath := filepath.Join(configDir, "config.toml")
		os.WriteFile(configPath, []byte(configContent), 0644)

		agentsDir := filepath.Join(configDir, "agents")
		os.MkdirAll(agentsDir, 0755)
		mainAgentPath := filepath.Join(agentsDir, "main.toml")
		os.WriteFile(mainAgentPath, []byte("existing agent"), 0644)

		agentsMdPath := filepath.Join(workspaceDir, "AGENTS.md")
		os.WriteFile(agentsMdPath, []byte("existing agents md"), 0644)

		_, err := createDefaults(configDir, workspaceDir)
		if err != nil {
			t.Fatalf("createDefaults unexpected error: %v", err)
		}

		data, _ := os.ReadFile(configPath)
		content := string(data)
		if !strings.Contains(content, "telegram_bot_token = \"token\"") {
			t.Errorf("config.toml was overwritten")
		}

		data, _ = os.ReadFile(mainAgentPath)
		if string(data) != "existing agent" {
			t.Errorf("agents/main.toml was overwritten")
		}

		data, _ = os.ReadFile(agentsMdPath)
		if string(data) != "existing agents md" {
			t.Errorf("AGENTS.md was overwritten")
		}
	})

	t.Run("creates workspace dir if missing", func(t *testing.T) {
		configDir := t.TempDir()
		workspaceDir := filepath.Join(t.TempDir(), "workspace")

		_, err := createDefaults(configDir, workspaceDir)
		if err != nil {
			t.Fatalf("createDefaults unexpected error: %v", err)
		}

		fi, err := os.Stat(workspaceDir)
		if err != nil {
			t.Fatalf("workspace dir not created: %v", err)
		}
		if !fi.IsDir() {
			t.Errorf("workspace path is not a directory")
		}
	})

	t.Run("errors if workspace path exists but is a file", func(t *testing.T) {
		configDir := t.TempDir()
		workspaceDir := filepath.Join(t.TempDir(), "workspace")
		os.WriteFile(workspaceDir, []byte("not a dir"), 0644)

		_, err := createDefaults(configDir, workspaceDir)
		if err == nil {
			t.Fatal("expected error when workspace is a file")
		}
	})
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("could not get user home dir: %v", err)
	}

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
		unsetHome bool
	}{
		{
			name:  "tilde expands to home",
			input: "~/foo",
			want:  filepath.Join(home, "foo"),
		},
		{
			name:  "absolute path passes through",
			input: "/absolute/path",
			want:  "/absolute/path",
		},
		{
			name:  "relative path passes through",
			input: "relative/path",
			want:  "relative/path",
		},
		{
			name:  "tilde with nothing after",
			input: "~",
			want:  home,
		},
		{
			name:      "error when HOME unset and tilde used",
			input:     "~/foo",
			wantErr:   true,
			unsetHome: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.unsetHome {
				os.Unsetenv("HOME")
				defer os.Setenv("HOME", home)
			}

			got, err := expandPath(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expandPath(%q) expected error, got nil", tt.input)
				}
				if !strings.Contains(err.Error(), "HOME") {
					t.Fatalf("expected error to mention HOME, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expandPath(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("expandPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
