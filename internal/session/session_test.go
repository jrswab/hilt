package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jrswab/hilt/internal"
)

func testManager(t *testing.T) (*Manager, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	m, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m, func() { _ = m.Close() }
}

func TestNewManager(t *testing.T) {
	t.Run("creates database and schema on first run", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "hilt.sqlite")
		m, err := NewManager(dbPath)
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}
		_ = m.Close()
		// Force re-open to verify persistence
		m2, err := NewManager(dbPath)
		if err != nil {
			t.Fatalf("re-opening manager: %v", err)
		}
		defer m2.Close()

		// Verify schema by trying to insert a session.
		s, err := m2.CreateSession(context.Background())
		if err != nil {
			t.Fatalf("CreateSession after re-open: %v", err)
		}
		if s.ID == 0 {
			t.Error("expected non-zero session ID")
		}
	})

	t.Run("empty db path returns error", func(t *testing.T) {
		_, err := NewManager("")
		if err == nil {
			t.Fatal("expected error for empty path")
		}
	})

	t.Run("unwritable parent directory returns error", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "readonly")
		if err := os.MkdirAll(parent, 0555); err != nil {
			t.Skipf("could not create readonly dir: %v", err)
		}
		dbPath := filepath.Join(parent, "hilt.sqlite")
		_, err := NewManager(dbPath)
		if err == nil {
			t.Fatal("expected error for unwritable directory")
		}
	})

	t.Run("home expansion with missing HOME returns error", func(t *testing.T) {
		home := os.Getenv("HOME")
		os.Unsetenv("HOME")
		defer os.Setenv("HOME", home)

		_, err := NewManager("~/hilt.sqlite")
		if err == nil {
			t.Fatal("expected error when HOME is unset")
		}
	})
}

func TestCreateSession(t *testing.T) {
	m, cleanup := testManager(t)
	defer cleanup()
	ctx := context.Background()

	s, err := m.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.ID == 0 {
		t.Error("expected non-zero session ID")
	}
	if s.Title != nil {
		t.Errorf("expected nil title, got %q", *s.Title)
	}
	if s.ArchivedAt != nil {
		t.Error("expected nil archived_at for new session")
	}
	if time.Since(s.CreatedAt) > time.Second {
		t.Error("created_at should be recent")
	}
	if time.Since(s.LastActivity) > time.Second {
		t.Error("last_activity should be recent")
	}

	s2, err := m.CreateSession(ctx)
	if err != nil {
		t.Fatalf("second CreateSession: %v", err)
	}
	if s2.ID == s.ID {
		t.Error("expected different ID for second session")
	}
}

func TestGetActiveSession(t *testing.T) {
	t.Run("empty db auto-creates session", func(t *testing.T) {
		m, cleanup := testManager(t)
		defer cleanup()
		ctx := context.Background()

		s, err := m.GetActiveSession(ctx)
		if err != nil {
			t.Fatalf("GetActiveSession: %v", err)
		}
		if s.ArchivedAt != nil {
			t.Error("expected active session")
		}
	})

	t.Run("returns existing active session", func(t *testing.T) {
		m, cleanup := testManager(t)
		defer cleanup()
		ctx := context.Background()

		s1, _ := m.CreateSession(ctx)
		s2, err := m.GetActiveSession(ctx)
		if err != nil {
			t.Fatalf("GetActiveSession: %v", err)
		}
		if s2.ID != s1.ID {
			t.Errorf("expected same session ID, got %d want %d", s2.ID, s1.ID)
		}
	})

	t.Run("archives extra active sessions keeping most recent", func(t *testing.T) {
		m, cleanup := testManager(t)
		defer cleanup()
		ctx := context.Background()

		s1, _ := m.CreateSession(ctx)
		// Simulate older session by moving time back. Since we can't change
		// the clock easily, we create a second session and then manually
		// update the first to have an older last_activity.
		_, err := m.db.ExecContext(ctx,
			`UPDATE sessions SET last_activity = datetime('now', '-1 day') WHERE id = ?`, s1.ID)
		if err != nil {
			t.Fatalf("updating last_activity: %v", err)
		}

		s2, _ := m.CreateSession(ctx)

		s, err := m.GetActiveSession(ctx)
		if err != nil {
			t.Fatalf("GetActiveSession: %v", err)
		}
		if s.ID != s2.ID {
			t.Errorf("expected most recent session %d, got %d", s2.ID, s.ID)
		}

		// s1 should now be archived
		s1Reload, err := m.sessionByID(ctx, s1.ID)
		if err != nil {
			t.Fatalf("reloading s1: %v", err)
		}
		if s1Reload.ArchivedAt == nil {
			t.Error("expected s1 to be archived")
		}
	})

	t.Run("after archive, creates new session", func(t *testing.T) {
		m, cleanup := testManager(t)
		defer cleanup()
		ctx := context.Background()

		s1, _ := m.GetActiveSession(ctx)
		_ = m.ArchiveSession(ctx, s1.ID)

		s2, err := m.GetActiveSession(ctx)
		if err != nil {
			t.Fatalf("GetActiveSession after archive: %v", err)
		}
		if s2.ID == s1.ID {
			t.Error("expected new session after archive")
		}
	})
}

func TestArchiveSession(t *testing.T) {
	m, cleanup := testManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("successfully archives", func(t *testing.T) {
		s, _ := m.CreateSession(ctx)
		if err := m.ArchiveSession(ctx, s.ID); err != nil {
			t.Fatalf("ArchiveSession: %v", err)
		}

		sReload, err := m.sessionByID(ctx, s.ID)
		if err != nil {
			t.Fatalf("reloading session: %v", err)
		}
		if sReload.ArchivedAt == nil {
			t.Error("expected archived session")
		}
	})

	t.Run("already archived returns ErrSessionExpired", func(t *testing.T) {
		s, _ := m.CreateSession(ctx)
		if err := m.ArchiveSession(ctx, s.ID); err != nil {
			t.Fatalf("first archive: %v", err)
		}
		if err := m.ArchiveSession(ctx, s.ID); err != internal.ErrSessionExpired {
			t.Fatalf("expected ErrSessionExpired, got %v", err)
		}
	})

	t.Run("non-existent id returns ErrNotFound", func(t *testing.T) {
		if err := m.ArchiveSession(ctx, 99999); err != internal.ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestPruneOldSessions(t *testing.T) {
	m, cleanup := testManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("deletes only old archived sessions", func(t *testing.T) {
		// Create three sessions and archive two of them.
		s1, _ := m.CreateSession(ctx)
		s2, _ := m.CreateSession(ctx)
		s3, _ := m.CreateSession(ctx)

		_ = m.ArchiveSession(ctx, s1.ID)
		_ = m.ArchiveSession(ctx, s2.ID)

		// Set s1 archived_at to the distant past.
		_, err := m.db.ExecContext(ctx,
			`UPDATE sessions SET archived_at = datetime('now', '-60 days') WHERE id = ?`, s1.ID)
		if err != nil {
			t.Fatalf("setting old archived_at: %v", err)
		}

		cutoff := time.Now().AddDate(0, 0, -30)
		deleted, err := m.PruneOldSessions(ctx, cutoff)
		if err != nil {
			t.Fatalf("PruneOldSessions: %v", err)
		}
		if deleted != 1 {
			t.Errorf("expected 1 deleted, got %d", deleted)
		}

		// s2 should still exist (archived but recent).
		_, err = m.sessionByID(ctx, s2.ID)
		if err != nil {
			t.Errorf("recent archived session should still exist: %v", err)
		}

		// s3 should still exist (active).
		_, err = m.sessionByID(ctx, s3.ID)
		if err != nil {
			t.Errorf("active session should still exist: %v", err)
		}

		// s1 should be gone.
		_, err = m.sessionByID(ctx, s1.ID)
		if err != internal.ErrNotFound {
			t.Errorf("expected s1 to be pruned, got: %v", err)
		}
	})

	t.Run("no archived sessions returns zero", func(t *testing.T) {
		_, _ = m.GetActiveSession(ctx) // ensures one active session
		cutoff := time.Now().AddDate(0, 0, -1)
		deleted, err := m.PruneOldSessions(ctx, cutoff)
		if err != nil {
			t.Fatalf("PruneOldSessions: %v", err)
		}
		if deleted != 0 {
			t.Errorf("expected 0 deleted, got %d", deleted)
		}
	})
}

func TestUpdateSessionActivity(t *testing.T) {
	m, cleanup := testManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("updates active session", func(t *testing.T) {
		s, _ := m.CreateSession(ctx)
		// SQLite CURRENT_TIMESTAMP has 1-second granularity.
		time.Sleep(1100 * time.Millisecond)
		if err := m.UpdateSessionActivity(ctx); err != nil {
			t.Fatalf("UpdateSessionActivity: %v", err)
		}

		sReload, _ := m.sessionByID(ctx, s.ID)
		if !sReload.LastActivity.After(s.LastActivity) {
			t.Error("expected last_activity to be updated")
		}
		// Clean up so the next subtest starts with zero active sessions.
		_ = m.ArchiveSession(ctx, s.ID)
	})

	t.Run("no active session returns ErrNotFound", func(t *testing.T) {
		s, _ := m.CreateSession(ctx)
		_ = m.ArchiveSession(ctx, s.ID)

		if err := m.UpdateSessionActivity(ctx); err != internal.ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestUpdateSessionTokens(t *testing.T) {
	m, cleanup := testManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("increments tokens", func(t *testing.T) {
		s, _ := m.CreateSession(ctx)
		if err := m.UpdateSessionTokens(ctx, s.ID, 100, 50); err != nil {
			t.Fatalf("UpdateSessionTokens: %v", err)
		}

		sReload, _ := m.sessionByID(ctx, s.ID)
		if sReload.TotalInputTokens != 100 {
			t.Errorf("input tokens = %d, want 100", sReload.TotalInputTokens)
		}
		if sReload.TotalOutputTokens != 50 {
			t.Errorf("output tokens = %d, want 50", sReload.TotalOutputTokens)
		}
	})

	t.Run("negative values work", func(t *testing.T) {
		s, _ := m.CreateSession(ctx)
		_ = m.UpdateSessionTokens(ctx, s.ID, 100, 50)
		if err := m.UpdateSessionTokens(ctx, s.ID, -10, -5); err != nil {
			t.Fatalf("UpdateSessionTokens negative: %v", err)
		}

		sReload, _ := m.sessionByID(ctx, s.ID)
		if sReload.TotalInputTokens != 90 {
			t.Errorf("input tokens = %d, want 90", sReload.TotalInputTokens)
		}
		if sReload.TotalOutputTokens != 45 {
			t.Errorf("output tokens = %d, want 45", sReload.TotalOutputTokens)
		}
	})

	t.Run("non-existent session returns ErrNotFound", func(t *testing.T) {
		if err := m.UpdateSessionTokens(ctx, 99999, 1, 1); err != internal.ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("works on archived session", func(t *testing.T) {
		s, _ := m.CreateSession(ctx)
		_ = m.ArchiveSession(ctx, s.ID)
		if err := m.UpdateSessionTokens(ctx, s.ID, 10, 5); err != nil {
			t.Fatalf("UpdateSessionTokens on archived: %v", err)
		}
	})
}

func TestListArchivedSessions(t *testing.T) {
	t.Run("no archived sessions returns nil", func(t *testing.T) {
		m, cleanup := testManager(t)
		defer cleanup()
		ctx := context.Background()

		sessions, err := m.ListArchivedSessions(ctx, time.Now().AddDate(0, 0, -7))
		if err != nil {
			t.Fatalf("ListArchivedSessions: %v", err)
		}
		if sessions != nil {
			t.Errorf("expected nil, got %v", sessions)
		}
	})

	t.Run("filters outside TTL", func(t *testing.T) {
		m, cleanup := testManager(t)
		defer cleanup()
		ctx := context.Background()

		s1, _ := m.CreateSession(ctx)
		s2, _ := m.CreateSession(ctx)
		_ = m.ArchiveSession(ctx, s1.ID)
		_ = m.ArchiveSession(ctx, s2.ID)

		_, err := m.db.ExecContext(ctx,
			`UPDATE sessions SET archived_at = datetime('now', '-60 days') WHERE id = ?`, s1.ID)
		if err != nil {
			t.Fatalf("setting old archived_at: %v", err)
		}

		cutoff := time.Now().AddDate(0, 0, -30)
		sessions, err := m.ListArchivedSessions(ctx, cutoff)
		if err != nil {
			t.Fatalf("ListArchivedSessions: %v", err)
		}
		if len(sessions) != 1 {
			t.Fatalf("expected 1 session, got %d", len(sessions))
		}
		if sessions[0].ID != s2.ID {
			t.Errorf("expected s2 (id=%d), got id=%d", s2.ID, sessions[0].ID)
		}
	})

	t.Run("orders most-recent-first", func(t *testing.T) {
		m, cleanup := testManager(t)
		defer cleanup()
		ctx := context.Background()

		s1, _ := m.CreateSession(ctx)
		s2, _ := m.CreateSession(ctx)
		_ = m.ArchiveSession(ctx, s1.ID)
		_ = m.ArchiveSession(ctx, s2.ID)

		_, err := m.db.ExecContext(ctx,
			`UPDATE sessions SET archived_at = datetime('now', '-5 days') WHERE id = ?`, s1.ID)
		if err != nil {
			t.Fatalf("setting s1 archived_at: %v", err)
		}
		_, err = m.db.ExecContext(ctx,
			`UPDATE sessions SET archived_at = datetime('now', '-1 days') WHERE id = ?`, s2.ID)
		if err != nil {
			t.Fatalf("setting s2 archived_at: %v", err)
		}

		cutoff := time.Now().AddDate(0, 0, -7)
		sessions, err := m.ListArchivedSessions(ctx, cutoff)
		if err != nil {
			t.Fatalf("ListArchivedSessions: %v", err)
		}
		if len(sessions) != 2 {
			t.Fatalf("expected 2 sessions, got %d", len(sessions))
		}
		if sessions[0].ID != s2.ID {
			t.Errorf("expected most recent s2 first, got id=%d", sessions[0].ID)
		}
		if sessions[1].ID != s1.ID {
			t.Errorf("expected older s1 second, got id=%d", sessions[1].ID)
		}
	})
}

func TestGetTurnCount(t *testing.T) {
	m, cleanup := testManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("returns 0 for new session", func(t *testing.T) {
		s, _ := m.CreateSession(ctx)
		count, err := m.GetTurnCount(ctx, s.ID)
		if err != nil {
			t.Fatalf("GetTurnCount: %v", err)
		}
		if count != 0 {
			t.Errorf("expected 0, got %d", count)
		}
	})

	t.Run("increments after recording turns", func(t *testing.T) {
		s, _ := m.CreateSession(ctx)
		if err := m.RecordTurn(ctx, s.ID, 1, "hello", "[]"); err != nil {
			t.Fatalf("RecordTurn: %v", err)
		}
		count, err := m.GetTurnCount(ctx, s.ID)
		if err != nil {
			t.Fatalf("GetTurnCount: %v", err)
		}
		if count != 1 {
			t.Errorf("expected 1, got %d", count)
		}
	})
}

func TestRecordTurn(t *testing.T) {
	m, cleanup := testManager(t)
	defer cleanup()
	ctx := context.Background()

	s, _ := m.CreateSession(ctx)
	if err := m.RecordTurn(ctx, s.ID, 1, "hello", `[{"role":"assistant"}]`); err != nil {
		t.Fatalf("RecordTurn: %v", err)
	}

	var sessionID int64
	var turnNum int
	var userMsg, jsonStr string
	err := m.db.QueryRowContext(ctx,
		`SELECT session_id, turn_number, user_message, new_messages_json FROM turns WHERE session_id = ?`,
		s.ID).Scan(&sessionID, &turnNum, &userMsg, &jsonStr)
	if err != nil {
		t.Fatalf("querying turn: %v", err)
	}
	if sessionID != s.ID {
		t.Errorf("session_id = %d, want %d", sessionID, s.ID)
	}
	if turnNum != 1 {
		t.Errorf("turn_number = %d, want 1", turnNum)
	}
	if userMsg != "hello" {
		t.Errorf("user_message = %q, want \"hello\"", userMsg)
	}
	if jsonStr != `[{"role":"assistant"}]` {
		t.Errorf("new_messages_json = %q", jsonStr)
	}
}

func TestSetSessionTitle(t *testing.T) {
	m, cleanup := testManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("updates existing session title", func(t *testing.T) {
		s, _ := m.CreateSession(ctx)
		if err := m.SetSessionTitle(ctx, s.ID, "Test Title"); err != nil {
			t.Fatalf("SetSessionTitle: %v", err)
		}
		reloaded, err := m.sessionByID(ctx, s.ID)
		if err != nil {
			t.Fatalf("sessionByID: %v", err)
		}
		if reloaded.Title == nil || *reloaded.Title != "Test Title" {
			t.Errorf("title = %v, want \"Test Title\"", reloaded.Title)
		}
	})

	t.Run("returns ErrNotFound for non-existent session", func(t *testing.T) {
		err := m.SetSessionTitle(ctx, 99999, "No Session")
		if !internal.IsNotFound(err) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestGetTurns(t *testing.T) {
	m, cleanup := testManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("returns empty slice for new session", func(t *testing.T) {
		s, _ := m.CreateSession(ctx)
		turns, err := m.GetTurns(ctx, s.ID)
		if err != nil {
			t.Fatalf("GetTurns: %v", err)
		}
		if turns == nil {
			t.Error("expected non-nil empty slice, got nil")
		}
		if len(turns) != 0 {
			t.Errorf("expected 0 turns, got %d", len(turns))
		}
	})

	t.Run("returns turns in chronological order", func(t *testing.T) {
		s, _ := m.CreateSession(ctx)
		if err := m.RecordTurn(ctx, s.ID, 1, "first", `[{"role":"assistant","content":"hi"}]`); err != nil {
			t.Fatalf("RecordTurn: %v", err)
		}
		if err := m.RecordTurn(ctx, s.ID, 2, "second", `[{"role":"assistant","content":"ok"}]`); err != nil {
			t.Fatalf("RecordTurn: %v", err)
		}

		turns, err := m.GetTurns(ctx, s.ID)
		if err != nil {
			t.Fatalf("GetTurns: %v", err)
		}
		if len(turns) != 2 {
			t.Fatalf("expected 2 turns, got %d", len(turns))
		}
		if turns[0].TurnNumber != 1 || turns[0].UserMessage != "first" {
			t.Errorf("turn[0] = %+v, want turn 1", turns[0])
		}
		if turns[1].TurnNumber != 2 || turns[1].UserMessage != "second" {
			t.Errorf("turn[1] = %+v, want turn 2", turns[1])
		}
	})
}

func TestCascadeDelete(t *testing.T) {
	m, cleanup := testManager(t)
	defer cleanup()
	ctx := context.Background()

	// Insert a session and a turn.
	s, _ := m.CreateSession(ctx)
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO turns (session_id, turn_number, user_message, new_messages_json) VALUES (?, 1, 'hello', '[]')`,
		s.ID)
	if err != nil {
		t.Fatalf("inserting turn: %v", err)
	}

	// Count turns.
	var countBefore int
	_ = m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM turns WHERE session_id = ?`, s.ID).Scan(&countBefore)
	if countBefore != 1 {
		t.Fatalf("expected 1 turn before prune, got %d", countBefore)
	}

	// Archive and prune.
	_ = m.ArchiveSession(ctx, s.ID)
	_, err = m.PruneOldSessions(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("PruneOldSessions: %v", err)
	}

	// Verify turn is gone.
	var countAfter int
	_ = m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM turns WHERE session_id = ?`, s.ID).Scan(&countAfter)
	if countAfter != 0 {
		t.Errorf("expected 0 turns after cascade delete, got %d", countAfter)
	}
}
