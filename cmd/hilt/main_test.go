package main

import (
	"testing"

	"log/slog"
)

func TestResolveLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"invalid", slog.LevelInfo},
		{"", slog.LevelInfo},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := resolveLogLevel(tt.input)
			if got != tt.expected {
				t.Fatalf("resolveLogLevel(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestResolveConfigPath(t *testing.T) {
	t.Run("flag takes precedence", func(t *testing.T) {
		t.Setenv("HILT_CONFIG", "/env/path.toml")
		got := resolveConfigPath("/flag/path.toml")
		if got != "/flag/path.toml" {
			t.Fatalf("flag should take precedence, got %q", got)
		}
	})

	t.Run("env used when flag empty", func(t *testing.T) {
		t.Setenv("HILT_CONFIG", "/env/path.toml")
		got := resolveConfigPath("")
		if got != "/env/path.toml" {
			t.Fatalf("env should be used, got %q", got)
		}
	})

	t.Run("empty when both empty", func(t *testing.T) {
		t.Setenv("HILT_CONFIG", "")
		got := resolveConfigPath("")
		if got != "" {
			t.Fatalf("should be empty, got %q", got)
		}
	})
}
