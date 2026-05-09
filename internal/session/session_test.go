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
