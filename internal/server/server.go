// Package server routes incoming Telegram messages to built-in commands or the main agent stub.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jrswab/hilt/internal"
	"github.com/jrswab/hilt/internal/session"
)

// Messenger sends text replies to Telegram.
type Messenger interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

// SessionManager performs session lifecycle operations needed by routing.
type SessionManager interface {
	GetActiveSession(ctx context.Context) (*session.Session, error)
	ArchiveSession(ctx context.Context, id int64) error
	CreateSession(ctx context.Context) (*session.Session, error)
	ListArchivedSessions(ctx context.Context, cutoff time.Time) ([]session.Session, error)
}

// TurnProcessor handles normal (non-command) messages.
type TurnProcessor interface {
	ProcessTurn(ctx context.Context, chatID int64, text string) error
}

// Router holds dependencies for dispatching incoming messages.
type Router struct {
	messenger Messenger
	sessions  SessionManager
	processor TurnProcessor
	ttlDays   int
	logger    *slog.Logger
}

// NewRouter creates a Router. Both messenger and sessions must be non-nil;
// otherwise it panics with a clear message. processor may be nil for
// backward compatibility.
func NewRouter(messenger Messenger, sessions SessionManager, processor TurnProcessor, ttlDays int, logger *slog.Logger) *Router {
	if messenger == nil {
		panic("messenger must not be nil")
	}
	if sessions == nil {
		panic("sessions must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Router{
		messenger: messenger,
		sessions:  sessions,
		processor: processor,
		ttlDays:   ttlDays,
		logger:    logger,
	}
}

// HandleMessage parses the incoming text and dispatches to the appropriate handler.
// It always returns nil; any errors are logged and surfaced to the user via Telegram.
func (r *Router) HandleMessage(ctx context.Context, chatID int64, text string) error {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "/") {
		after := trimmed[1:]
		var cmd string
		if i := strings.IndexByte(after, ' '); i >= 0 {
			cmd = after[:i]
		} else {
			cmd = after
		}
		cmd = strings.ToLower(cmd)
		switch cmd {
		case "new":
			r.handleNewCmd(ctx, chatID)
		case "sessions":
			r.handleSessionsCmd(ctx, chatID)
		default:
			r.sendUnknownCommand(ctx, chatID)
		}
		return nil
	}
	r.handleNormalMessage(ctx, chatID, trimmed)
	return nil
}

func (r *Router) handleNewCmd(ctx context.Context, chatID int64) {
	active, err := r.sessions.GetActiveSession(ctx)
	if err != nil && !internal.IsNotFound(err) {
		r.logger.Error("get active session failed", slog.Any("error", err))
		_ = r.messenger.SendMessage(ctx, chatID, "Something went wrong starting a new session.")
		return
	}

	if active != nil {
		if err := r.sessions.ArchiveSession(ctx, active.ID); err != nil {
			if !internal.IsNotFound(err) {
				r.logger.Error("archive session failed", slog.Any("error", err))
				// Non-NotFound archive errors should not block creating a new session,
				// but we also should not abort on ErrNotFound. Continue to create.
			}
		}
	}

	newSession, err := r.sessions.CreateSession(ctx)
	if err != nil {
		r.logger.Error("create session failed", slog.Any("error", err))
		_ = r.messenger.SendMessage(ctx, chatID, "Something went wrong starting a new session.")
		return
	}

	_ = r.messenger.SendMessage(ctx, chatID, fmt.Sprintf("New session started (ID: %d).", newSession.ID))
}

func (r *Router) handleSessionsCmd(ctx context.Context, chatID int64) {
	cutoff := time.Now().AddDate(0, 0, -r.ttlDays)
	sessions, err := r.sessions.ListArchivedSessions(ctx, cutoff)
	if err != nil {
		r.logger.Error("list archived sessions failed", slog.Any("error", err))
		_ = r.messenger.SendMessage(ctx, chatID, "Could not list archived sessions.")
		return
	}

	if len(sessions) == 0 {
		_ = r.messenger.SendMessage(ctx, chatID, "No archived sessions found.")
		return
	}

	var lines []string
	for i, s := range sessions {
		title := "Untitled"
		if s.Title != nil && *s.Title != "" {
			title = *s.Title
		}
		lines = append(lines, fmt.Sprintf("%d. ID: %d, Title: %s, Created: %s, Archived: %s",
			i+1, s.ID, title,
			s.CreatedAt.Format(time.RFC3339),
			s.ArchivedAt.Format(time.RFC3339)))
	}

	_ = r.messenger.SendMessage(ctx, chatID, strings.Join(lines, "\n"))
}

func (r *Router) sendUnknownCommand(ctx context.Context, chatID int64) {
	_ = r.messenger.SendMessage(ctx, chatID, "Command not found. Use /skills to see available commands.")
}

func (r *Router) handleNormalMessage(ctx context.Context, chatID int64, text string) {
	if r.processor != nil {
		if err := r.processor.ProcessTurn(ctx, chatID, text); err != nil {
			r.logger.Error("process turn failed", slog.Any("error", err))
			_ = r.messenger.SendMessage(ctx, chatID, "Something went wrong processing your message.")
		}
		return
	}
	_ = r.messenger.SendMessage(ctx, chatID, "Message received. The main agent is not yet online.")
}
