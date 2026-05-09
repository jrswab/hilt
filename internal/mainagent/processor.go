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

// persistedMessage is the hilt-agnostic JSON shape stored in new_messages_json.
type persistedMessage struct {
	Role         string                `json:"role"`
	Content      string                `json:"content"`
	ToolCalls    []persistedToolCall   `json:"tool_calls,omitempty"`
	ToolResults  []persistedToolResult `json:"tool_results,omitempty"`
}

// persistedToolCall is the JSON shape for a tool call in new_messages_json.
type persistedToolCall struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments"`
}

// persistedToolResult is the JSON shape for a tool result in new_messages_json.
type persistedToolResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// toRunnerMessages converts persisted messages to runner messages (lossless).
func toRunnerMessages(msgs []persistedMessage) []runner.Message {
	out := make([]runner.Message, len(msgs))
	for i, m := range msgs {
		tcs := make([]runner.ToolCall, len(m.ToolCalls))
		for j, tc := range m.ToolCalls {
			args := make(map[string]string, len(tc.Arguments))
			for k, v := range tc.Arguments {
				args[k] = v
			}
			tcs[j] = runner.ToolCall{ID: tc.ID, Name: tc.Name, Arguments: args}
		}
		trs := make([]runner.ToolResult, len(m.ToolResults))
		for j, tr := range m.ToolResults {
			trs[j] = runner.ToolResult{CallID: tr.CallID, Content: tr.Content, IsError: tr.IsError}
		}
		out[i] = runner.Message{
			Role:        m.Role,
			Content:     m.Content,
			ToolCalls:   tcs,
			ToolResults: trs,
		}
	}
	return out
}

// fromRunnerMessages converts runner messages to persisted messages (lossless).
func fromRunnerMessages(msgs []runner.Message) []persistedMessage {
	out := make([]persistedMessage, len(msgs))
	for i, m := range msgs {
		tcs := make([]persistedToolCall, len(m.ToolCalls))
		for j, tc := range m.ToolCalls {
			args := make(map[string]string, len(tc.Arguments))
			for k, v := range tc.Arguments {
				args[k] = v
			}
			tcs[j] = persistedToolCall{ID: tc.ID, Name: tc.Name, Arguments: args}
		}
		trs := make([]persistedToolResult, len(m.ToolResults))
		for j, tr := range m.ToolResults {
			trs[j] = persistedToolResult{CallID: tr.CallID, Content: tr.Content, IsError: tr.IsError}
		}
		out[i] = persistedMessage{
			Role:        m.Role,
			Content:     m.Content,
			ToolCalls:   tcs,
			ToolResults: trs,
		}
	}
	return out
}

const userRole = "user"
const assistantRole = "assistant"
const toolRole = "tool"

var validRoles = map[string]bool{
	userRole:      true,
	assistantRole: true,
	toolRole:      true,
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
	history   HistoryBuilder
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
	history HistoryBuilder,
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
	if history == nil {
		panic("history must not be nil")
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
		history:   history,
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
		return p.processTurn2Plus(ctx, chatID, trimmed, session, turnCount)
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
	delta := []persistedMessage{
		{Role: assistantRole, Content: result.Content},
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

// processTurn2Plus handles turns 2+ using conversation history via opts.Messages.
func (p *Processor) processTurn2Plus(ctx context.Context, chatID int64, trimmed string, session *session.Session, turnCount int) error {
	historyMsgs, err := p.history.BuildMessages(ctx, session.ID)
	if err != nil {
		p.logger.Error("build messages failed", slog.Any("error", err))
		_ = p.messenger.SendMessage(ctx, chatID, "Something went wrong loading conversation history.")
		return nil
	}

	// Append current user message to history
	historyMsgs = append(historyMsgs, runner.Message{Role: userRole, Content: trimmed})

	opts := runner.Options{
		AgentName:  "main",
		AgentsDirs: []string{p.agentsDir},
		Model:      p.model,
		Messages:   historyMsgs,
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

	delta := computeDelta(historyMsgs, result)
	persistedDelta := fromRunnerMessages(delta)
	deltaJSON, err := json.Marshal(persistedDelta)
	if err != nil {
		p.logger.Error("marshal delta failed", slog.Any("error", err))
		_ = p.messenger.SendMessage(ctx, chatID, extractReply(result))
		return nil
	}

	// Record turn
	if err := p.turns.RecordTurn(ctx, session.ID, turnCount+1, trimmed, string(deltaJSON)); err != nil {
		p.logger.Error("record turn failed", slog.Any("error", err))
		_ = p.messenger.SendMessage(ctx, chatID, extractReply(result))
		return nil
	}

	// Update session tokens
	if err := p.store.UpdateSessionTokens(ctx, session.ID, int64(result.InputTokens), int64(result.OutputTokens)); err != nil {
		p.logger.Error("update session tokens failed", slog.Any("error", err))
		_ = p.messenger.SendMessage(ctx, chatID, extractReply(result))
		return nil
	}

	// Update last activity
	if err := p.sessions.UpdateSessionActivity(ctx); err != nil {
		p.logger.Error("update session activity failed", slog.Any("error", err))
		_ = p.messenger.SendMessage(ctx, chatID, extractReply(result))
		return nil
	}

	reply := extractReply(result)
	if err := p.messenger.SendMessage(ctx, chatID, reply); err != nil {
		return err
	}
	return nil
}

