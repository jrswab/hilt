package mainagent

import (
	"context"
	"fmt"
	"testing"

	"github.com/jrswab/axe/pkg/runner"
	"github.com/jrswab/hilt/internal/session"
)

type fakeTurnReader struct {
	turns []session.TurnRow
	err   error
}

func (f *fakeTurnReader) GetTurns(_ context.Context, _ int64) ([]session.TurnRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.turns, nil
}

func TestBuildMessages(t *testing.T) {
	t.Run("zero turns returns empty non-nil slice", func(t *testing.T) {
		hb := NewHistoryBuilder(&fakeTurnReader{turns: nil})
		msgs, err := hb.BuildMessages(context.Background(), 1)
		if err != nil {
			t.Fatalf("BuildMessages: %v", err)
		}
		if msgs == nil {
			t.Error("expected non-nil empty slice, got nil")
		}
		if len(msgs) != 0 {
			t.Errorf("expected 0 messages, got %d", len(msgs))
		}
	})

	t.Run("one turn returns user + assistant", func(t *testing.T) {
		hb := NewHistoryBuilder(&fakeTurnReader{
			turns: []session.TurnRow{
				{TurnNumber: 1, UserMessage: "Hello", NewMessagesJSON: `[{"role":"assistant","content":"Hi"}]`},
			},
		})
		msgs, err := hb.BuildMessages(context.Background(), 1)
		if err != nil {
			t.Fatalf("BuildMessages: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(msgs))
		}
		if msgs[0].Role != "user" || msgs[0].Content != "Hello" {
			t.Errorf("msg[0] = %+v", msgs[0])
		}
		if msgs[1].Role != "assistant" || msgs[1].Content != "Hi" {
			t.Errorf("msg[1] = %+v", msgs[1])
		}
	})

	t.Run("two turns with interleaved messages", func(t *testing.T) {
		hb := NewHistoryBuilder(&fakeTurnReader{
			turns: []session.TurnRow{
				{TurnNumber: 1, UserMessage: "A", NewMessagesJSON: `[{"role":"assistant","content":"B"}]`},
				{TurnNumber: 2, UserMessage: "C", NewMessagesJSON: `[{"role":"assistant","content":"D"}]`},
			},
		})
		msgs, err := hb.BuildMessages(context.Background(), 1)
		if err != nil {
			t.Fatalf("BuildMessages: %v", err)
		}
		if len(msgs) != 4 {
			t.Fatalf("expected 4 messages, got %d", len(msgs))
		}
		expected := []runner.Message{
			{Role: "user", Content: "A"},
			{Role: "assistant", Content: "B"},
			{Role: "user", Content: "C"},
			{Role: "assistant", Content: "D"},
		}
		for i, exp := range expected {
			if msgs[i].Role != exp.Role || msgs[i].Content != exp.Content {
				t.Errorf("msg[%d] = %+v, want %+v", i, msgs[i], exp)
			}
		}
	})

	t.Run("tool call round-trip", func(t *testing.T) {
		json := `[{"role":"assistant","content":"","tool_calls":[{"id":"call_1","name":"calc","arguments":{"x":"1"}}]},{"role":"tool","content":"","tool_results":[{"call_id":"call_1","content":"2","is_error":false}]},{"role":"assistant","content":"Result is 2."}]`
		hb := NewHistoryBuilder(&fakeTurnReader{
			turns: []session.TurnRow{
				{TurnNumber: 1, UserMessage: "calc", NewMessagesJSON: json},
			},
		})
		msgs, err := hb.BuildMessages(context.Background(), 1)
		if err != nil {
			t.Fatalf("BuildMessages: %v", err)
		}
		if len(msgs) != 4 {
			t.Fatalf("expected 4 messages, got %d", len(msgs))
		}
		if msgs[0].Role != "user" || msgs[0].Content != "calc" {
			t.Errorf("msg[0] = %+v", msgs[0])
		}
		if msgs[1].Role != "assistant" || len(msgs[1].ToolCalls) != 1 || msgs[1].ToolCalls[0].ID != "call_1" {
			t.Errorf("msg[1] = %+v", msgs[1])
		}
		if msgs[2].Role != "tool" || len(msgs[2].ToolResults) != 1 || msgs[2].ToolResults[0].CallID != "call_1" {
			t.Errorf("msg[2] = %+v", msgs[2])
		}
		if msgs[3].Role != "assistant" || msgs[3].Content != "Result is 2." {
			t.Errorf("msg[3] = %+v", msgs[3])
		}
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		hb := NewHistoryBuilder(&fakeTurnReader{
			turns: []session.TurnRow{
				{TurnNumber: 1, UserMessage: "x", NewMessagesJSON: `{not json}`},
			},
		})
		_, err := hb.BuildMessages(context.Background(), 1)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("unrecognized role returns error", func(t *testing.T) {
		hb := NewHistoryBuilder(&fakeTurnReader{
			turns: []session.TurnRow{
				{TurnNumber: 1, UserMessage: "x", NewMessagesJSON: `[{"role":"system","content":"bad"}]`},
			},
		})
		_, err := hb.BuildMessages(context.Background(), 1)
		if err == nil {
			t.Fatal("expected error for unrecognized role")
		}
	})

	t.Run("reader error propagates", func(t *testing.T) {
		hb := NewHistoryBuilder(&fakeTurnReader{err: fmt.Errorf("db down")})
		_, err := hb.BuildMessages(context.Background(), 1)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
