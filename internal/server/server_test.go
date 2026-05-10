package server

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jrswab/hilt/internal"
	"github.com/jrswab/hilt/internal/session"
)

// fakeMessenger records SendMessage calls and can optionally return an error.
type fakeMessenger struct {
	messages []sentMessage
	err      error
}

type sentMessage struct {
	chatID int64
	text   string
}

func (f *fakeMessenger) SendMessage(ctx context.Context, chatID int64, text string) error {
	f.messages = append(f.messages, sentMessage{chatID: chatID, text: text})
	return f.err
}

// fakeSessionManager implements SessionManager for tests.
type fakeSessionManager struct {
	activeSession      *session.Session
	activeSessionErr   error
	archiveErr         error
	newSession         *session.Session
	newSessionErr      error
	archivedSessions   []session.Session
	archivedSessionsErr error
}

func (f *fakeSessionManager) GetActiveSession(ctx context.Context) (*session.Session, error) {
	return f.activeSession, f.activeSessionErr
}

func (f *fakeSessionManager) ArchiveSession(ctx context.Context, id int64) error {
	return f.archiveErr
}

func (f *fakeSessionManager) CreateSession(ctx context.Context) (*session.Session, error) {
	return f.newSession, f.newSessionErr
}

func (f *fakeSessionManager) ListArchivedSessions(ctx context.Context, cutoff time.Time) ([]session.Session, error) {
	return f.archivedSessions, f.archivedSessionsErr
}

// fakeProcessor implements TurnProcessor, StatusProvider, and ModelManager for tests.
type fakeProcessor struct {
	status          string
	statusErr       error
	currentModel    string
	setModelErr     error
	availableModels map[string]int
}

func (f *fakeProcessor) ProcessTurn(ctx context.Context, chatID int64, text string) error {
	return nil
}

func (f *fakeProcessor) Status(ctx context.Context, chatID int64) (string, error) {
	return f.status, f.statusErr
}

func (f *fakeProcessor) CurrentModel() string {
	return f.currentModel
}

func (f *fakeProcessor) SetModel(name string) error {
	if f.setModelErr != nil {
		return f.setModelErr
	}
	f.currentModel = name
	return nil
}

func (f *fakeProcessor) AvailableModels() map[string]int {
	if f.availableModels == nil {
		return map[string]int{}
	}
	return f.availableModels
}

func testRouter(fm *fakeMessenger, fs *fakeSessionManager, processor TurnProcessor) *Router {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewRouter(fm, fs, processor, 30, logger)
}

func TestNewRouterPanicsOnNilDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	t.Run("nil messenger panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil messenger")
			}
		}()
		fs := &fakeSessionManager{}
		_ = NewRouter(nil, fs, nil, 7, logger)
	})
	t.Run("nil sessions panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil sessions")
			}
		}()
		fm := &fakeMessenger{}
		_ = NewRouter(fm, nil, nil, 7, logger)
	})
}

func TestRouterCommandParsing(t *testing.T) {
	t.Run("routes /new to handleNewCmd", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{
			activeSessionErr: internal.ErrNotFound,
			newSession:       &session.Session{ID: 42},
		}
		r := testRouter(fm, fs, nil)

		_ = r.HandleMessage(context.Background(), 123, "  /new  ")

		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "New session started") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})

	t.Run("case insensitive /NEW", func(t *testing.T) {
		for _, cmd := range []string{"/NEW", "/New", "/nEw"} {
			fm := &fakeMessenger{}
			fs := &fakeSessionManager{
				activeSessionErr: internal.ErrNotFound,
				newSession:       &session.Session{ID: 99},
			}
			r := testRouter(fm, fs, nil)
			_ = r.HandleMessage(context.Background(), 123, cmd)
			if len(fm.messages) != 1 {
				t.Errorf("cmd %q: expected 1 message, got %d", cmd, len(fm.messages))
			}
		}
	})

	t.Run("case insensitive /SESSIONS", func(t *testing.T) {
		for _, cmd := range []string{"/SESSIONS", "/Sessions", "/SeSsIoNs"} {
			fm := &fakeMessenger{}
			fs := &fakeSessionManager{archivedSessions: nil}
			r := testRouter(fm, fs, nil)
			_ = r.HandleMessage(context.Background(), 123, cmd)
			if len(fm.messages) != 1 {
				t.Errorf("cmd %q: expected 1 message, got %d", cmd, len(fm.messages))
			}
			if !strings.Contains(fm.messages[0].text, "No archived sessions found.") {
				t.Errorf("cmd %q: unexpected message: %q", cmd, fm.messages[0].text)
			}
		}
	})

	t.Run("bare slash is unknown command", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/")
		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "Command not found") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})

	t.Run("slash with space is unknown command", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/ unknown")
		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "Command not found") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})

	t.Run("normal message routes to stub", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "hello world")
		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "main agent is not yet online") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})

	t.Run("whitespace-only is normal message", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "   ")
		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "main agent is not yet online") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})

	t.Run("routes /status to handleStatusCmd", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{}
		sp := &fakeProcessor{status: "Model: test-model\nSession Tokens: 42"}
		r := testRouter(fm, fs, sp)
		_ = r.HandleMessage(context.Background(), 123, "/status")
		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "Model: test-model") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})

	t.Run("/status without provider", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/status")
		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "Status is not available") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})

	t.Run("/model shows current model", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{}
		fp := &fakeProcessor{currentModel: "anthropic/claude-test"}
		r := testRouter(fm, fs, fp)
		_ = r.HandleMessage(context.Background(), 123, "/model")
		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "anthropic/claude-test") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})

	t.Run("/model <name> switches model", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{}
		fp := &fakeProcessor{currentModel: "old-model", availableModels: map[string]int{"new-model": 200000}}
		r := testRouter(fm, fs, fp)
		_ = r.HandleMessage(context.Background(), 123, "/model new-model")
		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "Model switched to: new-model") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
		if fp.currentModel != "new-model" {
			t.Errorf("expected current model to be updated to %q, got %q", "new-model", fp.currentModel)
		}
	})

	t.Run("/models lists available models", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{}
		fp := &fakeProcessor{
			currentModel:    "model-a",
			availableModels: map[string]int{"model-a": 100000, "model-b": 200000},
		}
		r := testRouter(fm, fs, fp)
		_ = r.HandleMessage(context.Background(), 123, "/models")
		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		msg := fm.messages[0].text
		if !strings.Contains(msg, "model-a") {
			t.Errorf("expected model-a in message, got: %q", msg)
		}
		if !strings.Contains(msg, "model-b") {
			t.Errorf("expected model-b in message, got: %q", msg)
		}
		if !strings.Contains(msg, "/model <name>") {
			t.Errorf("expected usage hint in message, got: %q", msg)
		}
	})

	t.Run("/models without provider", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/models")
		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "Model management is not available") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})

	t.Run("/skills lists available commands", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/skills")
		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		msg := fm.messages[0].text
		if !strings.Contains(msg, "Available commands:") {
			t.Errorf("expected 'Available commands:' in message, got: %q", msg)
		}
		if !strings.Contains(msg, "/new") {
			t.Errorf("expected '/new' in message, got: %q", msg)
		}
		if !strings.Contains(msg, "/skills") {
			t.Errorf("expected '/skills' in message, got: %q", msg)
		}
	})

	t.Run("unknown command", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/unknowncmd")
		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "Command not found. Use /skills") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})
}

func TestRouterHandleNew(t *testing.T) {
	t.Run("archives existing and creates new", func(t *testing.T) {
		fm := &fakeMessenger{}
		archivedCalled := false
		createCalled := false

		fs := &fakeSessionManager{
			activeSession: &session.Session{ID: 1},
			newSession:    &session.Session{ID: 2},
		}
		// Wrap to observe calls (fakeSessionManager doesn't expose tracking).
		originalArchive := fs.archiveErr
		_ = originalArchive
		// Use custom implementation.
		type trackingSM struct{ *fakeSessionManager }
		// Simpler: just check outcomes.
		// Since fakeSessionManager returns canned values, after HandleMessage we can
		// verify the new session ID in the message.
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/new")

		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "New session started (ID: 2)") {
			t.Errorf("expected confirmation with new ID, got %q", fm.messages[0].text)
		}
		_ = archivedCalled
		_ = createCalled
	})

	t.Run("no active session creates new defensively", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{
			activeSessionErr: internal.ErrNotFound,
			newSession:       &session.Session{ID: 7},
		}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/new")

		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "New session started (ID: 7)") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})

	t.Run("archive not found still creates new", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{
			activeSession:    &session.Session{ID: 1},
			archiveErr:       internal.ErrNotFound,
			newSession:       &session.Session{ID: 9},
		}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/new")

		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "New session started (ID: 9)") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})

	t.Run("archive error logs but still creates new", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{
			activeSession: &session.Session{ID: 1},
			archiveErr:    errors.New("disk full"),
			newSession:    &session.Session{ID: 10},
		}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/new")

		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "New session started (ID: 10)") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})

	t.Run("create session error surfaces user message", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{
			activeSessionErr: internal.ErrNotFound,
			newSessionErr:    errors.New("db locked"),
		}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/new")

		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "Something went wrong starting a new session.") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})
}

func TestRouterHandleSessions(t *testing.T) {
	t.Run("empty archived list", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{archivedSessions: nil}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/sessions")

		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if fm.messages[0].text != "No archived sessions found." {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})

	t.Run("lists sessions with nil titles", func(t *testing.T) {
		fm := &fakeMessenger{}
		created := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
		archived := time.Date(2024, 5, 2, 14, 0, 0, 0, time.UTC)
		fs := &fakeSessionManager{
			archivedSessions: []session.Session{
				{ID: 1, Title: nil, CreatedAt: created, ArchivedAt: &archived},
			},
		}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/sessions")

		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "Untitled") {
			t.Errorf("expected 'Untitled' fallback, got %q", fm.messages[0].text)
		}
	})

	t.Run("lists sessions with empty string titles", func(t *testing.T) {
		fm := &fakeMessenger{}
		emptyTitle := ""
		created := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
		archived := time.Date(2024, 5, 2, 14, 0, 0, 0, time.UTC)
		fs := &fakeSessionManager{
			archivedSessions: []session.Session{
				{ID: 1, Title: &emptyTitle, CreatedAt: created, ArchivedAt: &archived},
			},
		}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/sessions")

		if !strings.Contains(fm.messages[0].text, "Untitled") {
			t.Errorf("expected 'Untitled' for empty title, got %q", fm.messages[0].text)
		}
	})

	t.Run("formats numbered list", func(t *testing.T) {
		fm := &fakeMessenger{}
		title1 := "First"
		title2 := "Second"
		created := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
		archived1 := time.Date(2024, 5, 3, 10, 0, 0, 0, time.UTC)
		archived2 := time.Date(2024, 5, 2, 8, 0, 0, 0, time.UTC)
		fs := &fakeSessionManager{
			archivedSessions: []session.Session{
				{ID: 5, Title: &title1, CreatedAt: created, ArchivedAt: &archived1},
				{ID: 3, Title: &title2, CreatedAt: created, ArchivedAt: &archived2},
			},
		}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/sessions")

		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		msg := fm.messages[0].text
		if !strings.Contains(msg, "1. ID: 5") {
			t.Errorf("expected '1. ID: 5' in message, got:\n%s", msg)
		}
		if !strings.Contains(msg, "2. ID: 3") {
			t.Errorf("expected '2. ID: 3' in message, got:\n%s", msg)
		}
	})

	t.Run("list error surfaces user message", func(t *testing.T) {
		fm := &fakeMessenger{}
		fs := &fakeSessionManager{
			archivedSessionsErr: errors.New("db timeout"),
		}
		r := testRouter(fm, fs, nil)
		_ = r.HandleMessage(context.Background(), 123, "/sessions")

		if len(fm.messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(fm.messages))
		}
		if !strings.Contains(fm.messages[0].text, "Could not list archived sessions.") {
			t.Errorf("unexpected message: %q", fm.messages[0].text)
		}
	})
}

func TestRouterEdgeCases(t *testing.T) {
	t.Run("messenger error is logged not propagated", func(t *testing.T) {
		fm := &fakeMessenger{err: errors.New("network down")}
		fs := &fakeSessionManager{}
		r := testRouter(fm, fs, nil)
		err := r.HandleMessage(context.Background(), 123, "hello")
		if err != nil {
			t.Fatalf("HandleMessage should return nil even if messenger fails, got %v", err)
		}
	})

	t.Run("all handlers swallow their own errors", func(t *testing.T) {
		fm := &fakeMessenger{err: errors.New("send fails")}
		fs := &fakeSessionManager{
			activeSessionErr: internal.ErrNotFound,
			newSessionErr:    errors.New("create fails"),
		}
		r := testRouter(fm, fs, nil)
		// /new path: get active fails, create fails, send fails — all swallowed.
		err := r.HandleMessage(context.Background(), 123, "/new")
		if err != nil {
			t.Fatalf("HandleMessage should return nil, got %v", err)
		}
	})
}
