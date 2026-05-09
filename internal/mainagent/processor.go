// Package mainagent orchestrates the main agent execution loop for Hilt.
// It assembles context from memory files, invokes the LLM via Axe,
// persists turn deltas, and dispatches replies to Telegram.
package mainagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jrswab/axe/pkg/runner"
	"github.com/jrswab/hilt/internal/session"
)

// ActiveSessionProvider resolves the single active session.
type ActiveSessionProvider interface {
	GetActiveSession(ctx context.Context) (*session.Session, error)
	UpdateSessionActivity(ctx context.Context) error
}

// TurnStore manages the turns table.
type TurnStore interface {
	GetTurnCount(ctx context.Context, sessionID int64) (int, error)
	RecordTurn(ctx context.Context, sessionID int64, turnNum int, userMessage string, newMessagesJSON string) error
}

// SessionStore manages session metadata updates.
type SessionStore interface {
	UpdateSessionTokens(ctx context.Context, sessionID, inputTokens, outputTokens int64) error
	SetSessionTitle(ctx context.Context, sessionID int64, title string) error
}

// FileReader reads workspace memory files.
type FileReader interface {
	ReadAGENTSMD() (string, error)
	ReadCriticalMD() (string, error)
	ReadDailyNote(date string) (string, error)
	EnsureDailyNoteSkeleton(date string) error
}

// Runner invokes the LLM.
type Runner interface {
	Run(ctx context.Context, opts runner.Options) (*runner.Result, error)
}

// Messenger sends replies to Telegram.
type Messenger interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

// message is a lightweight representation for JSON persistence.
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Processor is the main agent execution orchestrator.
type Processor struct {
	sessions  ActiveSessionProvider
	turns     TurnStore
	store     SessionStore
	reader    FileReader
	agentsDir string
	model     string
	runner    Runner
	messenger Messenger
	logger    *slog.Logger
}

// NewProcessor creates a Processor with all dependencies validated.
func NewProcessor(
	sessions ActiveSessionProvider,
	turns TurnStore,
	store SessionStore,
	reader FileReader,
	runner Runner,
	messenger Messenger,
	agentsDir string,
	model string,
	logger *slog.Logger,
) *Processor {
	if sessions == nil {
		panic("sessions must not be nil")
	}
	if turns == nil {
		panic("turns must not be nil")
	}
	if store == nil {
		panic("store must not be nil")
	}
	if reader == nil {
		panic("reader must not be nil")
	}
	if runner == nil {
		panic("runner must not be nil")
	}
	if messenger == nil {
		panic("messenger must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Processor{
		sessions:  sessions,
		turns:     turns,
		store:     store,
		reader:    reader,
		agentsDir: agentsDir,
		model:     model,
		runner:    runner,
		messenger: messenger,
		logger:    logger,
	}
}

// assembleContext builds the hierarchical Markdown context from memory files.
func assembleContext(reader FileReader, userMessage string) (string, error) {
	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")

	if err := reader.EnsureDailyNoteSkeleton(today); err != nil {
		return "", fmt.Errorf("creating daily note skeleton: %w", err)
	}

	agentsContent, err := reader.ReadAGENTSMD()
	if err != nil {
		return "", fmt.Errorf("reading AGENTS.md: %w", err)
	}

	criticalContent, err := reader.ReadCriticalMD()
	if err != nil {
		return "", fmt.Errorf("reading critical.md: %w", err)
	}

	todayNoteContent, err := reader.ReadDailyNote(today)
	if err != nil {
		return "", fmt.Errorf("reading today note: %w", err)
	}

	yesterdayNoteContent, err := reader.ReadDailyNote(yesterday)
	if err != nil {
		return "", fmt.Errorf("reading yesterday note: %w", err)
	}

	var b strings.Builder
	b.WriteString("## Workspace Context\n\n")
	b.WriteString("### Rules (AGENTS.md)\n")
	b.WriteString(agentsContent)
	b.WriteString("\n\n### Critical State\n")
	b.WriteString(criticalContent)
	b.WriteString("\n\n## Recent Memory\n\n")
	b.WriteString("### Note: ")
	b.WriteString(today)
	b.WriteString("\n")
	b.WriteString(todayNoteContent)

	if yesterdayNoteContent != "" {
		b.WriteString("\n\n### Note: ")
		b.WriteString(yesterday)
		b.WriteString("\n")
		b.WriteString(yesterdayNoteContent)
	}

	b.WriteString("\n\n## Current Task\n\n")
	b.WriteString(strings.TrimSpace(userMessage))

	return b.String(), nil
}

// truncateTitle returns the input truncated to maxRunes runes.
func truncateTitle(input string, maxRunes int) string {
	if utf8.RuneCountInString(input) <= maxRunes {
		return input
	}
	runes := []rune(input)
	return string(runes[:maxRunes])
}

// ProcessTurn handles a single user message: detects turn number,
// assembles context, invokes the LLM, persists results, and sends reply.
func (p *Processor) ProcessTurn(ctx context.Context, chatID int64, text string) error {
	trimmed := strings.TrimSpace(text)

	session, err := p.sessions.GetActiveSession(ctx)
	if err != nil {
		p.logger.Error("get active session failed", slog.Any("error", err))
		_ = p.messenger.SendMessage(ctx, chatID, "Something went wrong preparing your session.")
		return nil
	}

	turnCount, err := p.turns.GetTurnCount(ctx, session.ID)
	if err != nil {
		p.logger.Error("get turn count failed", slog.Any("error", err))
		_ = p.messenger.SendMessage(ctx, chatID, "Something went wrong preparing your session.")
		return nil
	}

	if turnCount > 0 {
		_ = p.messenger.SendMessage(ctx, chatID, "Message received. The main agent is not yet online for turn 2+.")
		return nil
	}

	// Turn 1
	assembled, err := assembleContext(p.reader, trimmed)
	if err != nil {
		p.logger.Error("assemble context failed", slog.Any("error", err))
		_ = p.messenger.SendMessage(ctx, chatID, "Something went wrong preparing your session.")
		return nil
	}

	opts := runner.Options{
		AgentName:  "main",
		AgentsDirs: []string{p.agentsDir},
		Model:      p.model,
		Prompt:     assembled,
	}

	result, err := p.runner.Run(ctx, opts)
	if err != nil {
		p.logger.Error("runner failed", slog.Any("error", err))
		_ = p.messenger.SendMessage(ctx, chatID, "I couldn't process that request: "+err.Error())
		return nil
	}

	if result == nil {
		p.logger.Error("runner returned nil result")
		_ = p.messenger.SendMessage(ctx, chatID, "I couldn't process that request: unexpected empty result")
		return nil
	}

	// Build delta with just the assistant response (only thing we have from Result)
	delta := []message{
		{Role: "assistant", Content: result.Content},
	}
	deltaJSON, err := json.Marshal(delta)
	if err != nil {
		p.logger.Error("marshal delta failed", slog.Any("error", err))
		_ = p.messenger.SendMessage(ctx, chatID, result.Content)
		return nil
	}

	// Record turn
	if err := p.turns.RecordTurn(ctx, session.ID, 1, trimmed, string(deltaJSON)); err != nil {
		p.logger.Error("record turn failed", slog.Any("error", err))
		_ = p.messenger.SendMessage(ctx, chatID, result.Content)
		return nil
	}

	// Update session tokens
	if err := p.store.UpdateSessionTokens(ctx, session.ID, int64(result.InputTokens), int64(result.OutputTokens)); err != nil {
		p.logger.Error("update session tokens failed", slog.Any("error", err))
		_ = p.messenger.SendMessage(ctx, chatID, result.Content)
		return nil
	}

	// Update last activity
	if err := p.sessions.UpdateSessionActivity(ctx); err != nil {
		p.logger.Error("update session activity failed", slog.Any("error", err))
		_ = p.messenger.SendMessage(ctx, chatID, result.Content)
		return nil
	}

	// Set session title if empty
	if session.Title == nil || *session.Title == "" {
		title := truncateTitle(trimmed, 40)
		if err := p.store.SetSessionTitle(ctx, session.ID, title); err != nil {
			p.logger.Error("set session title failed", slog.Any("error", err))
			// Non-critical, continue
		}
	}

	// Send reply to user
	if err := p.messenger.SendMessage(ctx, chatID, result.Content); err != nil {
		return err
	}
	return nil
}
