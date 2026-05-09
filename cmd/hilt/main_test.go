package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"log/slog"

	_ "modernc.org/sqlite"

	"github.com/jrswab/hilt/internal/config"
	"github.com/jrswab/hilt/internal/session"
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

func makeTestConfig(tempDir string) *config.Config {
	return &config.Config{
		TelegramBotToken:       "test-token",
		WorkspaceDir:           tempDir,
		SessionTTLDays:         30,
		MainAgentModel:         "anthropic/claude-3-haiku-4-5",
		ContextWindowDefault:   200000,
		MaintenanceAgentModel:  "openrouter/deepseek/deepseek-chat",
		WhisperModel:           "small",
		AllowedUserIDs:         []int64{},
		Models:                 map[string]int{},
	}
}

func TestStartSessionManager(t *testing.T) {
	ctx := context.Background()

	t.Run("first run creates db and session", func(t *testing.T) {
		tempDir := t.TempDir()
		dbPath := filepath.Join(tempDir, "hilt.sqlite")
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))

		cfg := makeTestConfig(tempDir)
		mgr, sess, err := startSessionManager(ctx, dbPath, cfg, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer mgr.Close()

		if sess.ID == 0 {
			t.Error("expected non-zero session ID")
		}
		if _, statErr := os.Stat(dbPath); statErr != nil {
			t.Errorf("expected db file to exist: %v", statErr)
		}
		logOut := buf.String()
		if !strings.Contains(logOut, fmt.Sprintf("%d", sess.ID)) {
			t.Errorf("expected log to contain session ID %d, got: %s", sess.ID, logOut)
		}
	})

	t.Run("restart loads existing active session", func(t *testing.T) {
		tempDir := t.TempDir()
		dbPath := filepath.Join(tempDir, "hilt.sqlite")
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		cfg := makeTestConfig(tempDir)

		mgr1, s1, err := startSessionManager(ctx, dbPath, cfg, logger)
		if err != nil {
			t.Fatalf("first start: %v", err)
		}
		mgr1.Close()

		mgr2, s2, err := startSessionManager(ctx, dbPath, cfg, logger)
		if err != nil {
			t.Fatalf("second start: %v", err)
		}
		defer mgr2.Close()

		if s2.ID != s1.ID {
			t.Errorf("expected same session ID, got %d want %d", s2.ID, s1.ID)
		}
	})

	t.Run("stale session archived and new created", func(t *testing.T) {
		tempDir := t.TempDir()
		dbPath := filepath.Join(tempDir, "hilt.sqlite")
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		cfg := makeTestConfig(tempDir)

		mgr1, s1, err := startSessionManager(ctx, dbPath, cfg, logger)
		if err != nil {
			t.Fatalf("first start: %v", err)
		}
		mgr1.Close()

		// Manually regress last_activity to simulate a stale session.
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("opening db: %v", err)
		}
		if _, err := db.Exec("UPDATE sessions SET last_activity = datetime('now', '-60 days') WHERE id = ?", s1.ID); err != nil {
			db.Close()
			t.Fatalf("regressing last_activity: %v", err)
		}
		db.Close()

		mgr2, s2, err := startSessionManager(ctx, dbPath, cfg, logger)
		if err != nil {
			t.Fatalf("second start after stale: %v", err)
		}
		defer mgr2.Close()

		if s2.ID == s1.ID {
			t.Error("expected new session after stale archive")
		}

		// Verify s1 is archived via raw SQL.
		db, err = sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("re-opening db: %v", err)
		}
		defer db.Close()
		var archivedAt interface{}
		if err := db.QueryRow("SELECT archived_at FROM sessions WHERE id = ?", s1.ID).Scan(&archivedAt); err != nil {
			t.Fatalf("querying archived_at: %v", err)
		}
		if archivedAt == nil {
			t.Error("expected s1 to be archived")
		}
	})

	t.Run("manager creation failure returns error", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		cfg := makeTestConfig(t.TempDir())
		_, _, err := startSessionManager(ctx, "", cfg, logger)
		if err == nil {
			t.Fatal("expected error for empty db path")
		}
	})

	t.Run("prune removes old archived sessions", func(t *testing.T) {
		tempDir := t.TempDir()
		dbPath := filepath.Join(tempDir, "hilt.sqlite")
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		cfg := makeTestConfig(tempDir)

		mgr, s1, err := startSessionManager(ctx, dbPath, cfg, logger)
		if err != nil {
			t.Fatalf("first start: %v", err)
		}
		mgr.Close()

		// Manually set s1 as very old archived session.
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("opening db: %v", err)
		}
		_, err = db.Exec("UPDATE sessions SET archived_at = datetime('now', '-60 days'), last_activity = datetime('now', '-60 days') WHERE id = ?", s1.ID)
		if err != nil {
			db.Close()
			t.Fatalf("updating s1: %v", err)
		}
		db.Close()

		mgr2, s2, err := startSessionManager(ctx, dbPath, cfg, logger)
		if err != nil {
			t.Fatalf("second start: %v", err)
		}
		defer mgr2.Close()

		// s1 should have been pruned.
		_, err = session.NewManager(dbPath)
		if err != nil {
			t.Fatalf("opening manager to check prune: %v", err)
		}
		// We don't have an exported sessionByID, so use raw SQL.
		db, err = sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("opening db: %v", err)
		}
		defer db.Close()
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ?", s1.ID).Scan(&count); err != nil {
			t.Fatalf("counting s1: %v", err)
		}
		if count != 0 {
			t.Errorf("expected s1 to be pruned, found %d rows", count)
		}
		if s2.ID == 0 {
			t.Error("expected new active session")
		}
	})
}
