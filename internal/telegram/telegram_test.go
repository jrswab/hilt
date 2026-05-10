package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func TestNewBot_EmptyToken(t *testing.T) {
	bot, err := NewBot("", nil)
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
	if bot != nil {
		t.Fatal("expected nil bot for empty token")
	}
}

func TestIsTextMessage(t *testing.T) {
	cases := []struct {
		name     string
		msg      *gotgbot.Message
		expected bool
	}{
		{"plain text", &gotgbot.Message{Text: "hello"}, true},
		{"empty text", &gotgbot.Message{Text: ""}, true},
		{"text with no Text field set", &gotgbot.Message{}, true}, // message object exists but empty
		{"photo", &gotgbot.Message{Photo: []gotgbot.PhotoSize{{FileId: "abc"}}}, false},
		{"voice", &gotgbot.Message{Voice: &gotgbot.Voice{FileId: "abc"}}, false},
		{"audio", &gotgbot.Message{Audio: &gotgbot.Audio{FileId: "abc"}}, false},
		{"video", &gotgbot.Message{Video: &gotgbot.Video{FileId: "abc"}}, false},
		{"document", &gotgbot.Message{Document: &gotgbot.Document{FileId: "abc"}}, false},
		{"sticker", &gotgbot.Message{Sticker: &gotgbot.Sticker{FileId: "abc"}}, false},
		{"location", &gotgbot.Message{Location: &gotgbot.Location{Latitude: 1.0, Longitude: 2.0}}, false},
		{"contact", &gotgbot.Message{Contact: &gotgbot.Contact{PhoneNumber: "+123", FirstName: "A"}}, false},
	}

	bot := &Bot{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := bot.isTextMessage(c.msg)
			if got != c.expected {
				t.Fatalf("expected %v, got %v", c.expected, got)
			}
		})
	}
}

type fakeBotClient struct {
	mu    sync.Mutex
	calls []apiCall
}

type apiCall struct {
	method string
	params map[string]any
}

func (f *fakeBotClient) recordCall(method string, params map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, apiCall{method: method, params: params})
}

func (f *fakeBotClient) RequestWithContext(_ context.Context, _ string, method string, params map[string]any, _ *gotgbot.RequestOpts) (json.RawMessage, error) {
	f.recordCall(method, params)
	switch method {
	case "getMe":
		raw, _ := json.Marshal(gotgbot.User{Id: 1, IsBot: true, FirstName: "TestBot"})
		return json.RawMessage(raw), nil
	case "sendMessage":
		raw, _ := json.Marshal(gotgbot.Message{MessageId: 1})
		return json.RawMessage(raw), nil
	case "sendChatAction":
		raw, _ := json.Marshal(true)
		return json.RawMessage(raw), nil
	case "deleteWebhook":
		raw, _ := json.Marshal(true)
		return json.RawMessage(raw), nil
	default:
		return nil, fmt.Errorf("unsupported method: %s", method)
	}
}

func (f *fakeBotClient) GetAPIURL(_ *gotgbot.RequestOpts) string {
	return "https://api.telegram.org/bottest"
}

func (f *fakeBotClient) FileURL(_ string, _ string, _ *gotgbot.RequestOpts) string {
	return ""
}

func newFakeBot() (*Bot, *fakeBotClient) {
	client := &fakeBotClient{}
	api := &gotgbot.Bot{
		Token:     "test",
		BotClient: client,
	}
	bot := &Bot{api: api, allowed: nil}
	return bot, client
}

func TestHandleUpdate_Unauthorized(t *testing.T) {
	bot, client := newFakeBot()
	bot.allowed = []int64{100}

	handlerCalled := false
	handler := func(_ context.Context, _ int64, _ string) error {
		handlerCalled = true
		return nil
	}

	msg := &gotgbot.Message{
		Text: "hello",
		From: &gotgbot.User{Id: 999},
		Chat: gotgbot.Chat{Id: 1},
	}
	update := &gotgbot.Update{Message: msg}
	ctx := ext.NewContext(bot.api, update, nil)

	err := bot.handleUpdate(context.Background(), ctx, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handlerCalled {
		t.Fatal("handler should not be called for unauthorized user")
	}
	if len(client.calls) > 0 {
		t.Fatalf("expected no API calls for unauthorized user, got %d", len(client.calls))
	}
}

func TestHandleUpdate_AuthorizedText(t *testing.T) {
	bot, client := newFakeBot()
	bot.allowed = []int64{100}

	var gotChatID int64
	var gotText string
	handler := func(_ context.Context, chatID int64, text string) error {
		gotChatID = chatID
		gotText = text
		return nil
	}

	msg := &gotgbot.Message{
		Text: "hello world",
		From: &gotgbot.User{Id: 100},
		Chat: gotgbot.Chat{Id: 42},
	}
	update := &gotgbot.Update{Message: msg}
	ctx := ext.NewContext(bot.api, update, nil)

	err := bot.handleUpdate(context.Background(), ctx, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotChatID != 42 {
		t.Fatalf("expected chatID 42, got %d", gotChatID)
	}
	if gotText != "hello world" {
		t.Fatalf("expected text 'hello world', got %q", gotText)
	}

	// Give the background typing goroutine a moment to see the cancelled context and exit.
	time.Sleep(50 * time.Millisecond)

	client.mu.Lock()
	calls := make([]apiCall, len(client.calls))
	copy(calls, client.calls)
	client.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 API call (typing indicator), got %d", len(calls))
	}
	if calls[0].method != "sendChatAction" {
		t.Fatalf("expected sendChatAction, got %s", calls[0].method)
	}
	if calls[0].params["action"] != "typing" {
		t.Fatalf("expected action 'typing', got %v", calls[0].params["action"])
	}
}

func TestHandleUpdate_AuthorizedUnsupported(t *testing.T) {
	bot, client := newFakeBot()
	bot.allowed = []int64{100}

	handlerCalled := false
	handler := func(_ context.Context, _ int64, _ string) error {
		handlerCalled = true
		return nil
	}

	msg := &gotgbot.Message{
		Photo: []gotgbot.PhotoSize{{FileId: "abc"}},
		From:  &gotgbot.User{Id: 100},
		Chat:  gotgbot.Chat{Id: 42},
	}
	update := &gotgbot.Update{Message: msg}
	ctx := ext.NewContext(bot.api, update, nil)

	err := bot.handleUpdate(context.Background(), ctx, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handlerCalled {
		t.Fatal("handler should not be called for unsupported message")
	}

	client.mu.Lock()
	calls := make([]apiCall, len(client.calls))
	copy(calls, client.calls)
	client.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 API call, got %d", len(calls))
	}
	call := calls[0]
	if call.method != "sendMessage" {
		t.Fatalf("expected sendMessage, got %s", call.method)
	}
	if call.params["text"] != "Hilt only processes text and voice messages." {
		t.Fatalf("expected auto-reply text, got %q", call.params["text"])
	}
	chatID, ok := call.params["chat_id"].(int64)
	if !ok || chatID != 42 {
		t.Fatalf("expected chat_id 42, got %v", call.params["chat_id"])
	}
}

func TestHandleUpdate_NilMessage(t *testing.T) {
	bot, client := newFakeBot()

	handler := func(_ context.Context, _ int64, _ string) error {
		t.Fatal("handler should not be called")
		return nil
	}

	// Update with no message.
	update := &gotgbot.Update{}
	ctx := ext.NewContext(bot.api, update, nil)

	err := bot.handleUpdate(context.Background(), ctx, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	client.mu.Lock()
	callCount := len(client.calls)
	client.mu.Unlock()

	if callCount > 0 {
		t.Fatalf("expected no API calls, got %d", callCount)
	}
}

func TestHandleUpdate_NilFrom(t *testing.T) {
	bot, client := newFakeBot()

	handler := func(_ context.Context, _ int64, _ string) error {
		t.Fatal("handler should not be called")
		return nil
	}

	msg := &gotgbot.Message{
		Text: "hello",
		From: nil,
		Chat: gotgbot.Chat{Id: 1},
	}
	// Need to use ChannelPost to get EffectiveMessage with nil From,
	// because NewContext for Message type copies From to user.
	update := &gotgbot.Update{ChannelPost: msg}
	ctx := ext.NewContext(bot.api, update, nil)

	err := bot.handleUpdate(context.Background(), ctx, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	client.mu.Lock()
	callCount := len(client.calls)
	client.mu.Unlock()

	if callCount > 0 {
		t.Fatalf("expected no API calls, got %d", callCount)
	}
}

func TestSendMessage_Success(t *testing.T) {
	bot, client := newFakeBot()

	err := bot.SendMessage(context.Background(), 42, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	client.mu.Lock()
	calls := make([]apiCall, len(client.calls))
	copy(calls, client.calls)
	client.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 API call, got %d", len(calls))
	}
	call := calls[0]
	if call.method != "sendMessage" {
		t.Fatalf("expected sendMessage, got %s", call.method)
	}
	if call.params["text"] != "hello" {
		t.Fatalf("expected text 'hello', got %q", call.params["text"])
	}
	chatID, ok := call.params["chat_id"].(int64)
	if !ok || chatID != 42 {
		t.Fatalf("expected chat_id 42, got %v", call.params["chat_id"])
	}
}

type brokenClient struct{}

func (brokenClient) RequestWithContext(_ context.Context, _ string, method string, _ map[string]any, _ *gotgbot.RequestOpts) (json.RawMessage, error) {
	if method == "sendMessage" {
		return nil, fmt.Errorf("telegram API error: chat not found")
	}
	raw, _ := json.Marshal(gotgbot.Message{MessageId: 1})
	return json.RawMessage(raw), nil
}

func (brokenClient) GetAPIURL(_ *gotgbot.RequestOpts) string                   { return "" }
func (brokenClient) FileURL(_ string, _ string, _ *gotgbot.RequestOpts) string { return "" }

func TestSendTyping_Success(t *testing.T) {
	bot, client := newFakeBot()

	err := bot.SendTyping(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	client.mu.Lock()
	calls := make([]apiCall, len(client.calls))
	copy(calls, client.calls)
	client.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 API call, got %d", len(calls))
	}
	call := calls[0]
	if call.method != "sendChatAction" {
		t.Fatalf("expected sendChatAction, got %s", call.method)
	}
	if call.params["action"] != "typing" {
		t.Fatalf("expected action 'typing', got %v", call.params["action"])
	}
	chatID, ok := call.params["chat_id"].(int64)
	if !ok || chatID != 42 {
		t.Fatalf("expected chat_id 42, got %v", call.params["chat_id"])
	}
}

func TestSendMessage_Error(t *testing.T) {
	api := &gotgbot.Bot{
		Token:     "test",
		BotClient: brokenClient{},
	}
	bot := &Bot{api: api, allowed: nil}

	err := bot.SendMessage(context.Background(), 99, "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "telegram API error") {
		t.Fatalf("expected wrapped telegram API error, got: %v", err)
	}
}

func TestNewBot_Integration(t *testing.T) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		t.Skip("TELEGRAM_BOT_TOKEN not set, skipping integration test")
	}

	// Valid token should succeed.
	bot, err := NewBot(token, nil)
	if err != nil {
		t.Fatalf("expected valid token to succeed: %v", err)
	}
	if bot == nil {
		t.Fatal("expected non-nil bot")
	}

	// Invalid token should fail.
	_, err = NewBot("123456:invalid_token_for_testing_only", nil)
	if err == nil {
		t.Fatal("expected invalid token to fail")
	}
}

type startTestClient struct {
	mu    sync.Mutex
	calls []apiCall
}

func (s *startTestClient) recordCall(method string, params map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, apiCall{method: method, params: params})
}

func (s *startTestClient) RequestWithContext(ctx context.Context, _ string, method string, params map[string]any, _ *gotgbot.RequestOpts) (json.RawMessage, error) {
	s.recordCall(method, params)
	switch method {
	case "getMe":
		raw, _ := json.Marshal(gotgbot.User{Id: 1, IsBot: true, FirstName: "TestBot"})
		return json.RawMessage(raw), nil
	case "sendMessage":
		raw, _ := json.Marshal(gotgbot.Message{MessageId: 1})
		return json.RawMessage(raw), nil
	case "sendChatAction":
		raw, _ := json.Marshal(true)
		return json.RawMessage(raw), nil
	case "deleteWebhook":
		raw, _ := json.Marshal(true)
		return json.RawMessage(raw), nil
	case "getUpdates":
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
			raw, _ := json.Marshal([]gotgbot.Update{})
			return json.RawMessage(raw), nil
		}
	default:
		return nil, fmt.Errorf("unsupported method: %s", method)
	}
}

func (s *startTestClient) GetAPIURL(_ *gotgbot.RequestOpts) string                   { return "" }
func (s *startTestClient) FileURL(_ string, _ string, _ *gotgbot.RequestOpts) string { return "" }

func TestStart_StopsCleanly(t *testing.T) {
	client := &startTestClient{}
	api := &gotgbot.Bot{
		Token:     "test",
		BotClient: client,
	}
	bot := &Bot{api: api, allowed: nil}

	ctx, cancel := context.WithCancel(context.Background())
	var startErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		handler := func(_ context.Context, _ int64, _ string) error { return nil }
		startErr = bot.Start(ctx, handler)
	}()

	// Give StartPolling time to start, then cancel.
	time.Sleep(200 * time.Millisecond)
	cancel()

	// Wait for Start to return with a timeout.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if startErr != nil {
			t.Fatalf("Start returned an error: %v", startErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return within timeout after context cancellation")
	}
}

func TestIsAuthorized(t *testing.T) {
	// Empty allowed list = unrestricted.
	bot := &Bot{allowed: nil}
	if !bot.isAuthorized(123) {
		t.Fatal("expected authorized when allowed list is nil")
	}
	bot = &Bot{allowed: []int64{}}
	if !bot.isAuthorized(456) {
		t.Fatal("expected authorized when allowed list is empty")
	}

	// Populated allowed list.
	bot = &Bot{allowed: []int64{100, 200, 300}}
	if !bot.isAuthorized(200) {
		t.Fatal("expected authorized for matching user ID")
	}
	if bot.isAuthorized(999) {
		t.Fatal("expected unauthorized for unknown user ID")
	}
}
