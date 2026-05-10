package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"log/slog"

	"github.com/jrswab/axe/pkg/runner"
	"github.com/jrswab/hilt/internal/mainagent"
	"github.com/jrswab/hilt/internal/memory"
	"github.com/jrswab/hilt/internal/server"
)

// fakeSmokeRunner implements mainagent.Runner.
type fakeSmokeRunner struct {
	calls   []runner.Options
	results []*runner.Result
}

func (f *fakeSmokeRunner) Run(ctx context.Context, opts runner.Options) (*runner.Result, error) {
	f.calls = append(f.calls, opts)
	idx := len(f.calls) - 1
	if idx < len(f.results) {
		return f.results[idx], nil
	}
	return &runner.Result{Content: "default response"}, nil
}

// fakeSmokeMessenger implements server.Messenger and mainagent.Messenger.
type fakeSmokeMessenger struct {
	sent []struct {
		ChatID int64
		Text   string
	}
}

func (f *fakeSmokeMessenger) SendMessage(ctx context.Context, chatID int64, text string) error {
	f.sent = append(f.sent, struct {
		ChatID int64
		Text   string
	}{ChatID: chatID, Text: text})
	return nil
}

func TestSmoke(t *testing.T) {
	ctx := context.Background()
	chatID := int64(12345)

	// R1: Test Harness Setup
	tempDir := t.TempDir()
	workspaceDir := filepath.Join(tempDir, "workspace")
	memoryDir := filepath.Join(workspaceDir, "memory")
	agentsDir := filepath.Join(tempDir, "agents")

	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		t.Fatalf("creating memory dir: %v", err)
	}
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("creating agents dir: %v", err)
	}

	// Write workspace files
	agentsContent := "# Agent Rules\n\nBe helpful."
	criticalContent := "# Critical State\n\nUser prefers concise answers."
	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")

	if err := os.WriteFile(filepath.Join(workspaceDir, "AGENTS.md"), []byte(agentsContent), 0644); err != nil {
		t.Fatalf("writing AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "critical.md"), []byte(criticalContent), 0644); err != nil {
		t.Fatalf("writing critical.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, today+".md"), []byte("- Task A"), 0644); err != nil {
		t.Fatalf("writing today note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, yesterday+".md"), []byte("- Task B"), 0644); err != nil {
		t.Fatalf("writing yesterday note: %v", err)
	}

	cfg := makeTestConfig(workspaceDir)
	dbPath := filepath.Join(tempDir, "hilt.sqlite")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	mgr, activeSession, err := startSessionManager(ctx, dbPath, cfg, logger)
	if err != nil {
		t.Fatalf("starting session manager: %v", err)
	}
	originalSessionID := activeSession.ID

	memoryReader := memory.NewReader(cfg.WorkspaceDir)
	historyBuilder := mainagent.NewHistoryBuilder(mgr)

	fakeRunner := &fakeSmokeRunner{
		results: []*runner.Result{
			{
				Content:      "Turn 1 assistant reply",
				InputTokens:  100,
				OutputTokens: 50,
			},
		},
	}
	fakeMessenger := &fakeSmokeMessenger{}

	processor := mainagent.NewProcessor(
		mgr,
		mgr,
		mgr,
		memoryReader,
		fakeRunner,
		fakeMessenger,
		agentsDir,
		cfg.MainAgentModel,
		logger,
		historyBuilder,
		&mainagent.AxeErrorMapper{},
	)

	router := server.NewRouter(fakeMessenger, mgr, processor, cfg.SessionTTLDays, logger)

	// R2: Turn 1
	router.HandleMessage(ctx, chatID, "first user message")

	if len(fakeMessenger.sent) == 0 {
		t.Fatal("expected messenger to have sent a reply for turn 1")
	}
	if fakeMessenger.sent[0].Text != "Turn 1 assistant reply" {
		t.Errorf("turn 1 reply: got %q, want %q", fakeMessenger.sent[0].Text, "Turn 1 assistant reply")
	}

	if len(fakeRunner.calls) != 1 {
		t.Fatalf("expected 1 runner call, got %d", len(fakeRunner.calls))
	}
	call0 := fakeRunner.calls[0]
	if call0.Prompt == "" {
		t.Error("expected Turn 1 Prompt to be non-empty")
	}
	for _, hdr := range []string{"## Workspace Context", "### Critical State", "## Recent Memory", "## Current Task"} {
		if !strings.Contains(call0.Prompt, hdr) {
			t.Errorf("expected Turn 1 Prompt to contain %q", hdr)
		}
	}
	if !strings.Contains(call0.Prompt, agentsContent) {
		t.Error("expected Turn 1 Prompt to contain AGENTS.md content")
	}
	if !strings.Contains(call0.Prompt, criticalContent) {
		t.Error("expected Turn 1 Prompt to contain critical.md content")
	}
	if len(call0.Messages) != 0 {
		t.Errorf("expected Turn 1 Messages to be empty, got %d", len(call0.Messages))
	}

	turns, err := mgr.GetTurns(ctx, originalSessionID)
	if err != nil {
		t.Fatalf("getting turns: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].TurnNumber != 1 {
		t.Errorf("expected turn_number 1, got %d", turns[0].TurnNumber)
	}
	if turns[0].UserMessage != "first user message" {
		t.Errorf("expected user_message %q, got %q", "first user message", turns[0].UserMessage)
	}
	var persistedMsgs []map[string]interface{}
	if err := json.Unmarshal([]byte(turns[0].NewMessagesJSON), &persistedMsgs); err != nil {
		t.Fatalf("unmarshaling new_messages_json: %v", err)
	}
	if len(persistedMsgs) != 1 {
		t.Fatalf("expected 1 persisted message, got %d", len(persistedMsgs))
	}
	if persistedMsgs[0]["role"] != "assistant" || persistedMsgs[0]["content"] != "Turn 1 assistant reply" {
		t.Errorf("unexpected persisted message: %v", persistedMsgs[0])
	}

	// Refresh session to check tokens and title
	sessionAfterTurn1, err := mgr.GetActiveSession(ctx)
	if err != nil {
		t.Fatalf("getting active session after turn 1: %v", err)
	}
	if sessionAfterTurn1.TotalInputTokens != 100 {
		t.Errorf("expected input tokens 100, got %d", sessionAfterTurn1.TotalInputTokens)
	}
	if sessionAfterTurn1.TotalOutputTokens != 50 {
		t.Errorf("expected output tokens 50, got %d", sessionAfterTurn1.TotalOutputTokens)
	}
	if sessionAfterTurn1.Title == nil || *sessionAfterTurn1.Title != "first user message" {
		var title string
		if sessionAfterTurn1.Title != nil {
			title = *sessionAfterTurn1.Title
		}
		t.Errorf("expected title %q, got %q", "first user message", title)
	}

	// R3: Turn 2
	fakeRunner.results = append(fakeRunner.results, &runner.Result{
		Content:      "Turn 2 assistant reply",
		InputTokens:  80,
		OutputTokens: 40,
		Messages: []runner.Message{
			{Role: "user", Content: "first user message"},
			{Role: "assistant", Content: "Turn 1 assistant reply"},
			{Role: "user", Content: "second user message"},
			{Role: "assistant", Content: "Turn 2 assistant reply"},
		},
	})

	router.HandleMessage(ctx, chatID, "second user message")

	if len(fakeMessenger.sent) != 2 {
		t.Fatalf("expected 2 messenger messages, got %d", len(fakeMessenger.sent))
	}
	if fakeMessenger.sent[1].Text != "Turn 2 assistant reply" {
		t.Errorf("turn 2 reply: got %q, want %q", fakeMessenger.sent[1].Text, "Turn 2 assistant reply")
	}

	if len(fakeRunner.calls) != 2 {
		t.Fatalf("expected 2 runner calls, got %d", len(fakeRunner.calls))
	}
	call1 := fakeRunner.calls[1]
	if call1.Prompt != "" {
		t.Errorf("expected Turn 2 Prompt to be empty, got %q", call1.Prompt)
	}
	if len(call1.Messages) != 3 {
		t.Fatalf("expected 3 messages in Turn 2, got %d", len(call1.Messages))
	}
	if call1.Messages[0].Role != "user" || call1.Messages[0].Content != "first user message" {
		t.Errorf("unexpected message 0: %+v", call1.Messages[0])
	}
	if call1.Messages[1].Role != "assistant" || call1.Messages[1].Content != "Turn 1 assistant reply" {
		t.Errorf("unexpected message 1: %+v", call1.Messages[1])
	}
	if call1.Messages[2].Role != "user" || call1.Messages[2].Content != "second user message" {
		t.Errorf("unexpected message 2: %+v", call1.Messages[2])
	}

	turns, err = mgr.GetTurns(ctx, originalSessionID)
	if err != nil {
		t.Fatalf("getting turns after turn 2: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	if turns[1].TurnNumber != 2 {
		t.Errorf("expected turn_number 2, got %d", turns[1].TurnNumber)
	}

	sessionAfterTurn2, err := mgr.GetActiveSession(ctx)
	if err != nil {
		t.Fatalf("getting active session after turn 2: %v", err)
	}
	if sessionAfterTurn2.TotalInputTokens != 180 {
		t.Errorf("expected input tokens 180, got %d", sessionAfterTurn2.TotalInputTokens)
	}
	if sessionAfterTurn2.TotalOutputTokens != 90 {
		t.Errorf("expected output tokens 90, got %d", sessionAfterTurn2.TotalOutputTokens)
	}

	// R4: /new
	router.HandleMessage(ctx, chatID, "/new")

	if len(fakeMessenger.sent) != 3 {
		t.Fatalf("expected 3 messenger messages after /new, got %d", len(fakeMessenger.sent))
	}

	newSession, err := mgr.GetActiveSession(ctx)
	if err != nil {
		t.Fatalf("getting active session after /new: %v", err)
	}
	if newSession.ID == originalSessionID {
		t.Error("expected new session to have different ID")
	}
	if newSession.TotalInputTokens != 0 || newSession.TotalOutputTokens != 0 {
		t.Errorf("expected new session to have zero tokens, got in=%d out=%d", newSession.TotalInputTokens, newSession.TotalOutputTokens)
	}
	if newSession.Title != nil {
		t.Errorf("expected new session title to be nil, got %q", *newSession.Title)
	}

	if !strings.Contains(fakeMessenger.sent[2].Text, fmt.Sprintf("%d", newSession.ID)) {
		t.Errorf("expected /new confirmation to contain new session ID %d, got %q", newSession.ID, fakeMessenger.sent[2].Text)
	}

	// Verify archived session
	cutoff := time.Now().AddDate(0, 0, -cfg.SessionTTLDays)
	archivedSessions, err := mgr.ListArchivedSessions(ctx, cutoff)
	if err != nil {
		t.Fatalf("listing archived sessions: %v", err)
	}
	if len(archivedSessions) != 1 {
		t.Fatalf("expected 1 archived session, got %d", len(archivedSessions))
	}
	if archivedSessions[0].ID != originalSessionID {
		t.Errorf("expected archived session ID %d, got %d", originalSessionID, archivedSessions[0].ID)
	}
	if archivedSessions[0].ArchivedAt == nil {
		t.Error("expected archived session to have archived_at populated")
	}

	archivedTurns, err := mgr.GetTurns(ctx, originalSessionID)
	if err != nil {
		t.Fatalf("getting archived session turns: %v", err)
	}
	if len(archivedTurns) != 2 {
		t.Errorf("expected archived session to have 2 turns, got %d", len(archivedTurns))
	}

	// R5: /sessions
	router.HandleMessage(ctx, chatID, "/sessions")

	if len(fakeMessenger.sent) != 4 {
		t.Fatalf("expected 4 messenger messages after /sessions, got %d", len(fakeMessenger.sent))
	}
	sessionsListMsg := fakeMessenger.sent[3].Text
	if !strings.Contains(sessionsListMsg, fmt.Sprintf("%d", originalSessionID)) {
		t.Errorf("expected sessions list to contain original session ID %d, got %q", originalSessionID, sessionsListMsg)
	}
	if !strings.Contains(sessionsListMsg, "first user message") {
		t.Errorf("expected sessions list to contain original session title, got %q", sessionsListMsg)
	}

	// R6: Persistence verification (already partially checked above)
	// Verify archived session new_messages_json for both turns
	for i, turn := range archivedTurns {
		var msgs []map[string]interface{}
		if err := json.Unmarshal([]byte(turn.NewMessagesJSON), &msgs); err != nil {
			t.Fatalf("unmarshaling turn %d new_messages_json: %v", i+1, err)
		}
		if len(msgs) != 1 {
			t.Errorf("expected 1 message in turn %d, got %d", i+1, len(msgs))
		}
	}

	newSessionTurns, err := mgr.GetTurns(ctx, newSession.ID)
	if err != nil {
		t.Fatalf("getting new session turns: %v", err)
	}
	if len(newSessionTurns) != 0 {
		t.Errorf("expected new session to have 0 turns, got %d", len(newSessionTurns))
	}

	// R7: Restart
	mgr.Close()

	mgr2, restartedSession, err := startSessionManager(ctx, dbPath, cfg, logger)
	if err != nil {
		t.Fatalf("restarting session manager: %v", err)
	}
	defer mgr2.Close()

	if restartedSession.ID != newSession.ID {
		t.Errorf("expected restarted session ID %d, got %d", newSession.ID, restartedSession.ID)
	}

	restartTurnCount, err := mgr2.GetTurnCount(ctx, restartedSession.ID)
	if err != nil {
		t.Fatalf("getting turn count after restart: %v", err)
	}
	if restartTurnCount != 0 {
		t.Errorf("expected 0 turns after restart, got %d", restartTurnCount)
	}
}
