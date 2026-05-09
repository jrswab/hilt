// Package session manages the SQLite-backed session lifecycle.
// It creates, loads, archives, and prunes user sessions, enforcing
// TTL-based expiration and tracking per-session metadata such as
// total token consumption and last activity.
package session

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jrswab/hilt/internal"
	"github.com/jrswab/hilt/internal/config"
)

// Session represents a row from the sessions table.
type Session struct {
	ID                 int64
	Title              *string
	CreatedAt          time.Time
	LastActivity       time.Time
	ArchivedAt         *time.Time
	TotalInputTokens   int64
	TotalOutputTokens  int64
}

// Manager encapsulates all database access for session lifecycle operations.
type Manager struct {
	db *sql.DB
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS sessions (
	id                  INTEGER PRIMARY KEY AUTOINCREMENT,
	title               TEXT,
	created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	last_activity       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	archived_at         DATETIME,
	total_input_tokens  INTEGER NOT NULL DEFAULT 0,
	total_output_tokens INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_sessions_archived_at ON sessions(archived_at);

CREATE TABLE IF NOT EXISTS turns (
	id                  INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id          INTEGER NOT NULL,
	turn_number         INTEGER NOT NULL,
	timestamp           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	user_message        TEXT NOT NULL DEFAULT '',
	new_messages_json   TEXT NOT NULL DEFAULT '[]',
	FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_turns_session_id ON turns(session_id);
`

// NewManager opens (or creates) the SQLite database at dbPath, initializes
// the schema, and returns a ready-to-use Manager. The dbPath may contain a
// leading tilde which is expanded to the user's home directory.
func NewManager(dbPath string) (*Manager, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("db path must not be empty")
	}

	expanded, err := config.ExpandPath(dbPath)
	if err != nil {
		return nil, fmt.Errorf("expanding db path: %w", err)
	}

	dir := filepath.Dir(expanded)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	db, err := sql.Open("sqlite", "file:"+expanded+"?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("opening sqlite db: %w", err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging sqlite db: %w", err)
	}

	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}

	return &Manager{db: db}, nil
}

// GetTurnCount returns the number of turns for the given session.
func (m *Manager) GetTurnCount(ctx context.Context, sessionID int64) (int, error) {
	var count int
	err := m.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM turns WHERE session_id = ?`, sessionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting turns: %w", err)
	}
	return count, nil
}

// RecordTurn inserts a turn row into the turns table.
func (m *Manager) RecordTurn(ctx context.Context, sessionID int64, turnNum int, userMessage string, newMessagesJSON string) error {
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO turns (session_id, turn_number, user_message, new_messages_json) VALUES (?, ?, ?, ?)`,
		sessionID, turnNum, userMessage, newMessagesJSON)
	if err != nil {
		return fmt.Errorf("inserting turn: %w", err)
	}
	return nil
}

// SetSessionTitle updates the title for the given session.
// Returns ErrNotFound if the session does not exist.
func (m *Manager) SetSessionTitle(ctx context.Context, sessionID int64, title string) error {
	res, err := m.db.ExecContext(ctx,
		`UPDATE sessions SET title = ? WHERE id = ?`, title, sessionID)
	if err != nil {
		return fmt.Errorf("updating session title: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("getting rows affected: %w", err)
	}
	if n == 0 {
		return internal.ErrNotFound
	}
	return nil
}

// Close closes the underlying database connection.
func (m *Manager) Close() error {
	return m.db.Close()
}

// CreateSession inserts a new empty active session and returns it.
func (m *Manager) CreateSession(ctx context.Context) (*Session, error) {
	res, err := m.db.ExecContext(ctx,
		`INSERT INTO sessions (archived_at) VALUES (NULL)`)
	if err != nil {
		return nil, fmt.Errorf("inserting session: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting last insert id: %w", err)
	}

	return m.sessionByID(ctx, id)
}

// GetActiveSession returns the single active session. If none exists, a new
// session is created. If more than one session is active, all but the most
// recently active are archived.
func (m *Manager) GetActiveSession(ctx context.Context) (*Session, error) {
	rows, err := m.db.QueryContext(ctx,
		`SELECT id FROM sessions WHERE archived_at IS NULL ORDER BY last_activity DESC`)
	if err != nil {
		return nil, fmt.Errorf("querying active sessions: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning active session id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating active sessions: %w", err)
	}

	switch len(ids) {
	case 0:
		return m.CreateSession(ctx)
	case 1:
		return m.sessionByID(ctx, ids[0])
	default:
		// Archive all but the most recently active.
		for _, id := range ids[1:] {
			if err := m.ArchiveSession(ctx, id); err != nil {
				return nil, fmt.Errorf("archiving stale active session %d: %w", id, err)
			}
		}
		return m.sessionByID(ctx, ids[0])
	}
}

// ArchiveSession marks the session with the given id as archived.
// Returns ErrNotFound if the id does not exist.
// Returns ErrSessionExpired if the session is already archived.
func (m *Manager) ArchiveSession(ctx context.Context, id int64) error {
	var archivedAt sql.NullTime
	err := m.db.QueryRowContext(ctx,
		`SELECT archived_at FROM sessions WHERE id = ?`, id).Scan(&archivedAt)
	if err != nil {
		return mapSQLError(err)
	}
	if archivedAt.Valid {
		return internal.ErrSessionExpired
	}

	_, err = m.db.ExecContext(ctx,
		`UPDATE sessions SET archived_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("updating archived_at: %w", err)
	}
	return nil
}

// ListArchivedSessions returns archived sessions whose archived_at is >= cutoff,
// ordered most-recent-first. Returns nil (not empty slice) when there are no results.
func (m *Manager) ListArchivedSessions(ctx context.Context, cutoff time.Time) ([]Session, error) {
	cutoffStr := cutoff.UTC().Format("2006-01-02 15:04:05")
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, title, created_at, last_activity, archived_at, total_input_tokens, total_output_tokens
		 FROM sessions
		 WHERE archived_at IS NOT NULL AND archived_at >= ?
		 ORDER BY archived_at DESC`, cutoffStr)
	if err != nil {
		return nil, fmt.Errorf("querying archived sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		var title sql.NullString
		var archivedAt sql.NullTime
		if err := rows.Scan(
			&s.ID, &title, &s.CreatedAt, &s.LastActivity, &archivedAt,
			&s.TotalInputTokens, &s.TotalOutputTokens,
		); err != nil {
			return nil, fmt.Errorf("scanning archived session: %w", err)
		}
		if title.Valid {
			s.Title = &title.String
		}
		if archivedAt.Valid {
			s.ArchivedAt = &archivedAt.Time
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating archived sessions: %w", err)
	}

	if len(sessions) == 0 {
		return nil, nil
	}
	return sessions, nil
}

// PruneOldSessions deletes archived sessions older than the given cutoff.
// It returns the number of sessions deleted.
func (m *Manager) PruneOldSessions(ctx context.Context, cutoff time.Time) (int64, error) {
	// Format cutoff to match SQLite CURRENT_TIMESTAMP (UTC, no fractional seconds)
	// so that text comparisons with stored DATETIME values behave correctly.
	cutoffStr := cutoff.UTC().Format("2006-01-02 15:04:05")
	res, err := m.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE archived_at IS NOT NULL AND archived_at < ?`, cutoffStr)
	if err != nil {
		return 0, fmt.Errorf("deleting old sessions: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("getting rows affected: %w", err)
	}
	return n, nil
}

// UpdateSessionActivity refreshes last_activity for the active session.
func (m *Manager) UpdateSessionActivity(ctx context.Context) error {
	res, err := m.db.ExecContext(ctx,
		`UPDATE sessions SET last_activity = CURRENT_TIMESTAMP WHERE archived_at IS NULL`)
	if err != nil {
		return fmt.Errorf("updating last_activity: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("getting rows affected: %w", err)
	}
	if n == 0 {
		return internal.ErrNotFound
	}
	return nil
}

// UpdateSessionTokens atomically increments the token counters for the given session.
func (m *Manager) UpdateSessionTokens(ctx context.Context, sessionID, inputTokens, outputTokens int64) error {
	res, err := m.db.ExecContext(ctx,
		`UPDATE sessions SET total_input_tokens = total_input_tokens + ?, total_output_tokens = total_output_tokens + ? WHERE id = ?`,
		inputTokens, outputTokens, sessionID)
	if err != nil {
		return fmt.Errorf("updating session tokens: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("getting rows affected: %w", err)
	}
	if n == 0 {
		return internal.ErrNotFound
	}
	return nil
}

// sessionByID loads a Session by its primary key.
func (m *Manager) sessionByID(ctx context.Context, id int64) (*Session, error) {
	var s Session
	var title sql.NullString
	var archivedAt sql.NullTime

	err := m.db.QueryRowContext(ctx,
		`SELECT id, title, created_at, last_activity, archived_at, total_input_tokens, total_output_tokens
		 FROM sessions WHERE id = ?`, id).Scan(
		&s.ID, &title, &s.CreatedAt, &s.LastActivity, &archivedAt, &s.TotalInputTokens, &s.TotalOutputTokens,
	)
	if err != nil {
		return nil, mapSQLError(err)
	}

	if title.Valid {
		s.Title = &title.String
	}
	if archivedAt.Valid {
		s.ArchivedAt = &archivedAt.Time
	}

	return &s, nil
}

// mapSQLError converts sql.ErrNoRows to internal.ErrNotFound.
func mapSQLError(err error) error {
	if err == sql.ErrNoRows {
		return internal.ErrNotFound
	}
	return err
}
