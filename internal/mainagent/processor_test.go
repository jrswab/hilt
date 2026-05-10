package mainagent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jrswab/axe/pkg/runner"
	"github.com/jrswab/hilt/internal/memory"
	"github.com/jrswab/hilt/internal/session"
)

// --- Shared test fakes ---

type fakeRunner struct {
	result *runner.Result
	err    error
}

func (f *fakeRunner) Run(_ context.Context, _ runner.Options) (*runner.Result, error) {
	return f.result, f.err
}

type fakeMessenger struct {
	lastChatID int64
	lastText   string
	err        error
}

func (f *fakeMessenger) SendMessage(_ context.Context, chatID int64, text string) error {
	f.lastChatID = chatID
	f.lastText = text
	return f.err
}

// fakeSessions implements ActiveSessionProvider
type fakeSessions struct {
	session       *session.Session
	getErr        error
	activityErr   error
	activityCalled bool
}

func (f *fakeSessions) GetActiveSession(_ context.Context) (*session.Session, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.session == nil {
		f.session = &session.Session{ID: 1}
	}
	return f.session, nil
}

func (f *fakeSessions) UpdateSessionActivity(_ context.Context) error {
	f.activityCalled = true
	return f.activityErr
}

// fakeTurns implements TurnStore
type fakeTurns struct {
	count        int
	countErr     error
	recorded     map[string]interface{}
	recordErr    error
}

func newFakeTurns() *fakeTurns {
	return &fakeTurns{recorded: make(map[string]interface{})}
}

func (f *fakeTurns) GetTurnCount(_ context.Context, _ int64) (int, error) {
	return f.count, f.countErr
}

func (f *fakeTurns) RecordTurn(_ context.Context, sessionID int64, turnNum int, userMessage string, newMessagesJSON string) error {
	if f.recordErr != nil {
		return f.recordErr
	}
	f.recorded["sessionID"] = sessionID
	f.recorded["turnNum"] = turnNum
	f.recorded["userMessage"] = userMessage
	f.recorded["newMessagesJSON"] = newMessagesJSON
	return nil
}

// fakeStore implements SessionStore
type fakeStore struct {
	tokens    map[string]int64
	titles    map[int64]string
	tokenErr  error
	titleErr  error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		tokens: make(map[string]int64),
		titles: make(map[int64]string),
	}
}

func (f *fakeStore) UpdateSessionTokens(_ context.Context, _ int64, inputTokens, outputTokens int64) error {
	if f.tokenErr != nil {
		return f.tokenErr
	}
	f.tokens["input"] = inputTokens
	f.tokens["output"] = outputTokens
	return nil
}

func (f *fakeStore) SetSessionTitle(_ context.Context, sessionID int64, title string) error {
	if f.titleErr != nil {
		return f.titleErr
	}
	f.titles[sessionID] = title
	return nil
}

// fakeHistoryBuilder implements history building for tests
type fakeHistoryBuilder struct {
	messages []runner.Message
	err      error
}

func (f *fakeHistoryBuilder) BuildMessages(_ context.Context, _ int64) ([]runner.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.messages, nil
}

// --- Context assembly tests ---

func TestAssembleContextAllSections(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)

	os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("# Agent Rules\n\nBe helpful."), 0644)
	os.MkdirAll(filepath.Join(workspace, "memory"), 0755)
	os.WriteFile(filepath.Join(workspace, "memory", "critical.md"), []byte("## Critical State\n\n- Active project: hilt"), 0644)

	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	os.WriteFile(filepath.Join(workspace, "memory", today+".md"), []byte("# "+today+"\n\nMet with team."), 0644)
	os.WriteFile(filepath.Join(workspace, "memory", yesterday+".md"), []byte("# "+yesterday+"\n\nReviewed PRs."), 0644)

	result, err := assembleContext(reader, "  Build me a feature  ")
	if err != nil {
		t.Fatalf("assembleContext: %v", err)
	}

	if !strings.Contains(result, "## Workspace Context") {
		t.Error("missing ## Workspace Context")
	}
	if !strings.Contains(result, "### Rules (AGENTS.md)") {
		t.Error("missing ### Rules (AGENTS.md)")
	}
	if !strings.Contains(result, "### Critical State") {
		t.Error("missing ### Critical State")
	}
	if !strings.Contains(result, "## Recent Memory") {
		t.Error("missing ## Recent Memory")
	}
	if !strings.Contains(result, "### Note: "+today) {
		t.Errorf("missing ### Note: %s", today)
	}
	if !strings.Contains(result, "### Note: "+yesterday) {
		t.Errorf("missing ### Note: %s", yesterday)
	}
	if !strings.Contains(result, "## Current Task") {
		t.Error("missing ## Current Task")
	}
	if !strings.HasSuffix(result, "Build me a feature") {
		t.Errorf("expected to end with trimmed user message, got:\n%s", result)
	}

	idxWorkspace := strings.Index(result, "## Workspace Context")
	idxRecent := strings.Index(result, "## Recent Memory")
	idxTask := strings.Index(result, "## Current Task")
	if idxWorkspace >= idxRecent || idxRecent >= idxTask {
		t.Error("sections are not in correct order")
	}
}

func TestAssembleContextMissingFilesPresentHeaders(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)

	result, err := assembleContext(reader, "Do something")
	if err != nil {
		t.Fatalf("assembleContext: %v", err)
	}

	if !strings.Contains(result, "### Rules (AGENTS.md)") {
		t.Error("missing Rules header even when AGENTS.md is missing")
	}
	if !strings.Contains(result, "### Critical State") {
		t.Error("missing Critical State header even when critical.md is missing")
	}
}

func TestAssembleContextNoYesterdayNote(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)

	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")

	os.MkdirAll(filepath.Join(workspace, "memory"), 0755)
	os.WriteFile(filepath.Join(workspace, "memory", today+".md"), []byte("# "+today+"\n\nDaily entry."), 0644)

	result, err := assembleContext(reader, "Task")
	if err != nil {
		t.Fatalf("assembleContext: %v", err)
	}

	if !strings.Contains(result, "### Note: "+today) {
		t.Errorf("missing today note")
	}
	if strings.Contains(result, "### Note: "+yesterday) {
		t.Errorf("yesterday note should be omitted when file is missing")
	}
}

func TestAssembleContextCreatesSkeleton(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)

	today := time.Now().UTC().Format("2006-01-02")

	result, err := assembleContext(reader, "New task")
	if err != nil {
		t.Fatalf("assembleContext: %v", err)
	}

	path := filepath.Join(workspace, "memory", today+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("skeleton not created: %v", err)
	}
	if !strings.Contains(string(data), "# "+today) {
		t.Errorf("skeleton missing heading, got:\n%s", string(data))
	}

	if !strings.Contains(result, "## Wing: Work") {
		t.Error("assembled context missing skeleton content")
	}
}

func TestAssembleContextWhitespaceTrim(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)

	result, err := assembleContext(reader, "  \n  some task  \n  ")
	if err != nil {
		t.Fatalf("assembleContext: %v", err)
	}

	if !strings.HasSuffix(result, "some task") {
		t.Errorf("expected trimmed user message, got suffix:\n%s", result[len(result)-50:])
	}
}

// --- truncateTitle tests ---

func TestTruncateTitle(t *testing.T) {
	t.Run("truncates ASCII over limit", func(t *testing.T) {
		input := strings.Repeat("a", 50)
		got := truncateTitle(input, 40)
		if len(got) != 40 {
			t.Errorf("len=%d, want 40", len(got))
		}
	})

	t.Run("keeps short string unchanged", func(t *testing.T) {
		input := "short title"
		got := truncateTitle(input, 40)
		if got != input {
			t.Errorf("got %q, want %q", got, input)
		}
	})

	t.Run("truncate at exactly 40 runes with multi-byte", func(t *testing.T) {
		input := strings.Repeat("🔥", 50)
		got := truncateTitle(input, 40)
		if utf8.RuneCountInString(got) != 40 {
			t.Errorf("rune count = %d, want 40", utf8.RuneCountInString(got))
		}
	})
}

// --- ProcessTurn tests ---

func TestProcessTurnSuccess(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	store := newFakeStore()
	messenger := &fakeMessenger{}
	runner := &fakeRunner{
		result: &runner.Result{
			Content:      "Hello from the LLM",
			InputTokens:  100,
			OutputTokens: 50,
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, &fakeHistoryBuilder{}, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "  Hello LLM  ")
	if err != nil {
		t.Fatalf("ProcessTurn: %v", err)
	}

	if messenger.lastChatID != 12345 {
		t.Errorf("chatID = %d, want 12345", messenger.lastChatID)
	}
	if messenger.lastText != "Hello from the LLM" {
		t.Errorf("text = %q, want \"Hello from the LLM\"", messenger.lastText)
	}

	if turns.recorded["sessionID"] != int64(1) {
		t.Errorf("sessionID = %v, want 1", turns.recorded["sessionID"])
	}
	if turns.recorded["turnNum"] != 1 {
		t.Errorf("turnNum = %v, want 1", turns.recorded["turnNum"])
	}
	if turns.recorded["userMessage"] != "Hello LLM" {
		t.Errorf("userMessage = %v, want \"Hello LLM\"", turns.recorded["userMessage"])
	}

	if store.tokens["input"] != 100 {
		t.Errorf("input tokens = %d, want 100", store.tokens["input"])
	}
	if store.tokens["output"] != 50 {
		t.Errorf("output tokens = %d, want 50", store.tokens["output"])
	}

	if store.titles[1] != "Hello LLM" {
		t.Errorf("title = %q, want \"Hello LLM\"", store.titles[1])
	}

	if !sessions.activityCalled {
		t.Error("UpdateSessionActivity was not called")
	}

	// Verify delta JSON contains assistant message
	deltaJSON := turns.recorded["newMessagesJSON"].(string)
	if !strings.Contains(deltaJSON, `"role":"assistant"`) {
		t.Errorf("delta JSON missing assistant role: %s", deltaJSON)
	}
	if !strings.Contains(deltaJSON, "Hello from the LLM") {
		t.Errorf("delta JSON missing content: %s", deltaJSON)
	}
}

func TestProcessTurnRunnerError(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	store := newFakeStore()
	messenger := &fakeMessenger{}
	runner := &fakeRunner{err: fmt.Errorf("API rate limit")}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, &fakeHistoryBuilder{}, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "Hello")
	if err != nil {
		t.Fatalf("ProcessTurn returned error: %v", err)
	}

	want := "I couldn't process that request. Please try again."
	if messenger.lastText != want {
		t.Errorf("expected generic fallback %q, got %q", want, messenger.lastText)
	}
	// Must not contain raw error text
	if strings.Contains(messenger.lastText, "API rate limit") {
		t.Errorf("message must not contain raw error text, got %q", messenger.lastText)
	}

	// No turn should be recorded on failure
	if len(turns.recorded) > 0 {
		t.Error("no turn should be recorded when runner fails")
	}
}

func TestProcessTurnNilResult(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	store := newFakeStore()
	messenger := &fakeMessenger{}
	runner := &fakeRunner{result: nil}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, &fakeHistoryBuilder{}, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "Hello")
	if err != nil {
		t.Fatalf("ProcessTurn returned error: %v", err)
	}

	want := "I couldn't process that request. Please try again."
	if messenger.lastText != want {
		t.Errorf("expected generic fallback %q, got %q", want, messenger.lastText)
	}
	// Must not contain raw "unexpected empty result" text
	if strings.Contains(messenger.lastText, "unexpected empty result") {
		t.Errorf("message must not contain raw nil-result text, got %q", messenger.lastText)
	}
}

func TestProcessTurnRecordTurnErrorStillSendsReply(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	turns.recordErr = fmt.Errorf("disk full")
	store := newFakeStore()
	messenger := &fakeMessenger{}
	runner := &fakeRunner{result: &runner.Result{Content: "Reply content", InputTokens: 10, OutputTokens: 5}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, &fakeHistoryBuilder{}, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "Hello")
	if err != nil {
		t.Fatalf("ProcessTurn returned error: %v", err)
	}

	if messenger.lastText != "Reply content" {
		t.Errorf("expected reply even after DB failure, got %q", messenger.lastText)
	}
}

func TestProcessTurnMessengerErrorPropagates(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	store := newFakeStore()
	messenger := &fakeMessenger{err: fmt.Errorf("network down")}
	runner := &fakeRunner{result: &runner.Result{Content: "Reply", InputTokens: 10, OutputTokens: 5}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, &fakeHistoryBuilder{}, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "Hello")
	if err == nil {
		t.Fatal("expected Telegram dispatch error to propagate")
	}
}

func TestProcessTurnSessionTokensErrorStillSendsReply(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	store := newFakeStore()
	store.tokenErr = fmt.Errorf("db locked")
	messenger := &fakeMessenger{}
	runner := &fakeRunner{result: &runner.Result{Content: "LLM says hi", InputTokens: 5, OutputTokens: 5}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, &fakeHistoryBuilder{}, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "Hello")
	if err != nil {
		t.Fatalf("ProcessTurn returned error: %v", err)
	}

	if messenger.lastText != "LLM says hi" {
		t.Errorf("expected reply even when token update fails, got %q", messenger.lastText)
	}
}

func TestProcessTurnNoTitleWhenAlreadySet(t *testing.T) {
	title := "Existing Title"
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{session: &session.Session{ID: 1, Title: &title}}
	turns := newFakeTurns()
	store := newFakeStore()
	messenger := &fakeMessenger{}
	runner := &fakeRunner{result: &runner.Result{Content: "ok", InputTokens: 1, OutputTokens: 1}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, &fakeHistoryBuilder{}, &AxeErrorMapper{})

	ctx := context.Background()
	p.ProcessTurn(ctx, 1, "Message 1")

	if _, ok := store.titles[1]; ok {
		t.Error("title should not be updated when session already has a title")
	}
}

// --- Turn 2+ tests ---

func TestProcessTurnTurn2PlusSuccess(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	turns.count = 1
	store := newFakeStore()
	messenger := &fakeMessenger{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	fakeHistory := &fakeHistoryBuilder{
		messages: []runner.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
	}

	// Runner returns the full conversation including the new turn
	runner := &fakeRunner{
		result: &runner.Result{
			Messages: []runner.Message{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there"},
				{Role: "user", Content: "How are you?"},
				{Role: "assistant", Content: "I'm fine, thanks."},
			},
			Content:      "I'm fine, thanks.",
			InputTokens:  50,
			OutputTokens: 25,
		},
	}

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, fakeHistory, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "How are you?")
	if err != nil {
		t.Fatalf("ProcessTurn: %v", err)
	}

	if messenger.lastText != "I'm fine, thanks." {
		t.Errorf("reply = %q, want \"I'm fine, thanks.\"", messenger.lastText)
	}

	if turns.recorded["turnNum"] != 2 {
		t.Errorf("turnNum = %v, want 2", turns.recorded["turnNum"])
	}
	if turns.recorded["userMessage"] != "How are you?" {
		t.Errorf("userMessage = %v, want \"How are you?\"", turns.recorded["userMessage"])
	}

	// Verify the delta persisted is the new assistant response only
	// (the user message is already in the sent slice, not the delta)
	deltaJSON := turns.recorded["newMessagesJSON"].(string)
	if strings.Contains(deltaJSON, `"role":"user"`) {
		t.Errorf("delta should not contain user role (it's already in sent): %s", deltaJSON)
	}
	if !strings.Contains(deltaJSON, `"role":"assistant"`) {
		t.Errorf("delta missing assistant role: %s", deltaJSON)
	}

	if store.tokens["input"] != 50 {
		t.Errorf("input tokens = %d, want 50", store.tokens["input"])
	}
	if store.tokens["output"] != 25 {
		t.Errorf("output tokens = %d, want 25", store.tokens["output"])
	}
	if !sessions.activityCalled {
		t.Error("UpdateSessionActivity was not called")
	}
}

func TestProcessTurnTurn2PlusWithToolCalls(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	turns.count = 1
	store := newFakeStore()
	messenger := &fakeMessenger{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	fakeHistory := &fakeHistoryBuilder{
		messages: []runner.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
	}

	// Runner returns result with tool calls in the delta
	runner := &fakeRunner{
		result: &runner.Result{
			Messages: []runner.Message{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there"},
				{Role: "user", Content: "Run the calc"},
				{Role: "assistant", Content: "", ToolCalls: []runner.ToolCall{{ID: "call_1", Name: "calc", Arguments: map[string]string{"x": "1"}}}},
				{Role: "tool", Content: "", ToolResults: []runner.ToolResult{{CallID: "call_1", Content: "2", IsError: false}}},
				{Role: "assistant", Content: "Result is 2."},
			},
			Content: "Result is 2.",
		},
	}

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, fakeHistory, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "Run the calc")
	if err != nil {
		t.Fatalf("ProcessTurn: %v", err)
	}

	if messenger.lastText != "Result is 2." {
		t.Errorf("reply = %q, want \"Result is 2.\"", messenger.lastText)
	}

	deltaJSON := turns.recorded["newMessagesJSON"].(string)
	if !strings.Contains(deltaJSON, `"tool_calls"`) {
		t.Errorf("delta missing tool_calls: %s", deltaJSON)
	}
	if !strings.Contains(deltaJSON, `"tool_results"`) {
		t.Errorf("delta missing tool_results: %s", deltaJSON)
	}
	if !strings.Contains(deltaJSON, `"id":"call_1"`) {
		t.Errorf("delta missing tool call id: %s", deltaJSON)
	}
	if !strings.Contains(deltaJSON, `"call_id":"call_1"`) {
		t.Errorf("delta missing tool result call_id: %s", deltaJSON)
	}
}

func TestProcessTurnTurn2PlusNilMessagesFallback(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	turns.count = 1
	store := newFakeStore()
	messenger := &fakeMessenger{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	fakeHistory := &fakeHistoryBuilder{
		messages: []runner.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
	}

	// Runner returns nil Messages, should fallback to Content
	runner := &fakeRunner{
		result: &runner.Result{
			Messages: nil,
			Content:  "Fallback reply",
		},
	}

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, fakeHistory, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "How are you?")
	if err != nil {
		t.Fatalf("ProcessTurn: %v", err)
	}

	if messenger.lastText != "Fallback reply" {
		t.Errorf("reply = %q, want \"Fallback reply\"", messenger.lastText)
	}

	deltaJSON := turns.recorded["newMessagesJSON"].(string)
	if !strings.Contains(deltaJSON, `"role":"assistant"`) {
		t.Errorf("fallback delta missing assistant role: %s", deltaJSON)
	}
	if !strings.Contains(deltaJSON, "Fallback reply") {
		t.Errorf("fallback delta missing content: %s", deltaJSON)
	}
}

func TestProcessTurnTurn2PlusHistoryError(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	turns.count = 1
	store := newFakeStore()
	messenger := &fakeMessenger{}
	runner := &fakeRunner{result: &runner.Result{Content: "should not run"}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	fakeHistory := &fakeHistoryBuilder{err: fmt.Errorf("corrupted db")}

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, fakeHistory, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "How are you?")
	if err != nil {
		t.Fatalf("ProcessTurn should not return error: %v", err)
	}

	if messenger.lastText == "" {
		t.Errorf("expected error message, got empty")
	}
	// Accept either context-specific fallback or generic mapped fallback;
	// the key requirement is that raw error text is NOT leaked.
	if strings.Contains(messenger.lastText, "corrupted db") {
		t.Errorf("message must not leak raw DB error, got %q", messenger.lastText)
	}

	// No turn should be recorded
	if len(turns.recorded) > 0 {
		t.Error("no turn should be recorded when history fails")
	}
}

func TestProcessTurnTurn2PlusTokenAccumulation(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	turns.count = 2
	store := newFakeStore()
	messenger := &fakeMessenger{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	fakeHistory := &fakeHistoryBuilder{
		messages: []runner.Message{
			{Role: "user", Content: "A"},
			{Role: "assistant", Content: "B"},
			{Role: "user", Content: "C"},
			{Role: "assistant", Content: "D"},
		},
	}

	runner := &fakeRunner{
		result: &runner.Result{
			Messages: []runner.Message{
				{Role: "user", Content: "A"},
				{Role: "assistant", Content: "B"},
				{Role: "user", Content: "C"},
				{Role: "assistant", Content: "D"},
				{Role: "user", Content: "E"},
				{Role: "assistant", Content: "F"},
			},
			Content:      "F",
			InputTokens:  30,
			OutputTokens: 15,
		},
	}

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, fakeHistory, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "E")
	if err != nil {
		t.Fatalf("ProcessTurn: %v", err)
	}

	if turns.recorded["turnNum"] != 3 {
		t.Errorf("turnNum = %v, want 3", turns.recorded["turnNum"])
	}
}

func TestNewProcessorPanicsOnNil(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fs := &fakeSessions{}
	ft := newFakeTurns()
	fst := newFakeStore()
	fr := &fakeRunner{}
	fm := &fakeMessenger{}
	reader := memory.NewReader(t.TempDir())
	fh := &fakeHistoryBuilder{}

	tests := []struct {
		name string
		fn   func()
	}{
		{"sessions nil", func() { NewProcessor(nil, ft, fst, reader, fr, fm, "", "", logger, fh, &AxeErrorMapper{}) }},
		{"turns nil", func() { NewProcessor(fs, nil, fst, reader, fr, fm, "", "", logger, fh, &AxeErrorMapper{}) }},
		{"store nil", func() { NewProcessor(fs, ft, nil, reader, fr, fm, "", "", logger, fh, &AxeErrorMapper{}) }},
		{"reader nil", func() { NewProcessor(fs, ft, fst, nil, fr, fm, "", "", logger, fh, &AxeErrorMapper{}) }},
		{"runner nil", func() { NewProcessor(fs, ft, fst, reader, nil, fm, "", "", logger, fh, &AxeErrorMapper{}) }},
		{"messenger nil", func() { NewProcessor(fs, ft, fst, reader, fr, nil, "", "", logger, fh, &AxeErrorMapper{}) }},
		{"history nil", func() { NewProcessor(fs, ft, fst, reader, fr, fm, "", "", logger, nil, &AxeErrorMapper{}) }},
		{"mapper nil", func() { NewProcessor(fs, ft, fst, reader, fr, fm, "", "", logger, fh, nil) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic for %s", tt.name)
				}
			}()
			tt.fn()
		})
	}
}

func TestProcessTurnRunnerConfigError(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	store := newFakeStore()
	messenger := &fakeMessenger{}
	runner := &fakeRunner{err: &runner.ConfigError{Msg: "missing agent"}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, &fakeHistoryBuilder{}, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "Hello")
	if err != nil {
		t.Fatalf("ProcessTurn returned error: %v", err)
	}

	want := "Configuration issue: please check your agent files, API keys, and model settings."
	if messenger.lastText != want {
		t.Errorf("expected config message %q, got %q", want, messenger.lastText)
	}
	// No turn recorded
	if len(turns.recorded) > 0 {
		t.Error("no turn should be recorded on config error")
	}
}

func TestProcessTurnRunnerBudgetExceededError(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	store := newFakeStore()
	messenger := &fakeMessenger{}
	runner := &fakeRunner{err: &runner.BudgetExceededError{Used: 150, Max: 100}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, &fakeHistoryBuilder{}, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "Hello")
	if err != nil {
		t.Fatalf("ProcessTurn returned error: %v", err)
	}

	if !strings.Contains(messenger.lastText, "⚠️ Token budget exceeded") {
		t.Errorf("expected budget exceeded prefix, got %q", messenger.lastText)
	}
	// No turn recorded
	if len(turns.recorded) > 0 {
		t.Error("no turn should be recorded on budget error")
	}
}

func TestProcessTurnRunnerRuntimeError(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	store := newFakeStore()
	messenger := &fakeMessenger{}
	// RuntimeError without ProviderError → generic runtime fallback
	runner := &fakeRunner{err: &runner.RuntimeError{Msg: "runtime failed", Err: fmt.Errorf("boom")}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, &fakeHistoryBuilder{}, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "Hello")
	if err != nil {
		t.Fatalf("ProcessTurn returned error: %v", err)
	}

	want := "Something went wrong. Please try again."
	if messenger.lastText != want {
		t.Errorf("expected runtime fallback %q, got %q", want, messenger.lastText)
	}
	// Must not contain raw error text
	if strings.Contains(messenger.lastText, "runtime failed") || strings.Contains(messenger.lastText, "boom") {
		t.Errorf("message must not contain raw error text, got %q", messenger.lastText)
	}
	if len(turns.recorded) > 0 {
		t.Error("no turn should be recorded on runtime error")
	}
}

func TestReportErrorMessengerFailureLogged(t *testing.T) {
	workspace := t.TempDir()
	reader := memory.NewReader(workspace)
	sessions := &fakeSessions{}
	turns := newFakeTurns()
	store := newFakeStore()
	messenger := &fakeMessenger{err: fmt.Errorf("network down")}
	runner := &fakeRunner{err: fmt.Errorf("runner failed")}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	p := NewProcessor(sessions, turns, store, reader, runner, messenger, "/tmp/agents", "test/model", logger, &fakeHistoryBuilder{}, &AxeErrorMapper{})

	ctx := context.Background()
	err := p.ProcessTurn(ctx, 12345, "Hello")
	if err != nil {
		t.Fatalf("ProcessTurn returned error: %v", err)
	}

	// messenger failed but processTurn returns nil (no propagation)
	if messenger.lastText != "I couldn't process that request. Please try again." {
		t.Errorf("expected fallback message attempt, got %q", messenger.lastText)
	}
}
