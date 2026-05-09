package mainagent

import (
	"testing"

	"github.com/jrswab/axe/pkg/runner"
)

func TestComputeDelta(t *testing.T) {
	t.Run("exact suffix match", func(t *testing.T) {
		sent := []runner.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		}
		result := &runner.Result{
			Messages: []runner.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
				{Role: "user", Content: "how are you?"},
				{Role: "assistant", Content: "fine"},
			},
			Content: "fine",
		}
		delta := computeDelta(sent, result)
		if len(delta) != 2 {
			t.Fatalf("expected 2 delta messages, got %d", len(delta))
		}
		if delta[0].Role != "user" || delta[0].Content != "how are you?" {
			t.Errorf("delta[0] = %+v", delta[0])
		}
		if delta[1].Role != "assistant" || delta[1].Content != "fine" {
			t.Errorf("delta[1] = %+v", delta[1])
		}
	})

	t.Run("nil Messages fallback", func(t *testing.T) {
		sent := []runner.Message{{Role: "user", Content: "hello"}}
		result := &runner.Result{Messages: nil, Content: "fallback"}
		delta := computeDelta(sent, result)
		if len(delta) != 1 || delta[0].Role != "assistant" || delta[0].Content != "fallback" {
			t.Errorf("delta = %+v", delta)
		}
	})

	t.Run("empty Messages fallback", func(t *testing.T) {
		sent := []runner.Message{{Role: "user", Content: "hello"}}
		result := &runner.Result{Messages: []runner.Message{}, Content: "fallback"}
		delta := computeDelta(sent, result)
		if len(delta) != 1 || delta[0].Role != "assistant" || delta[0].Content != "fallback" {
			t.Errorf("delta = %+v", delta)
		}
	})

	t.Run("returned shorter than sent fallback", func(t *testing.T) {
		sent := []runner.Message{
			{Role: "user", Content: "a"},
			{Role: "assistant", Content: "b"},
			{Role: "user", Content: "c"},
		}
		result := &runner.Result{
			Messages: []runner.Message{
				{Role: "user", Content: "a"},
				{Role: "assistant", Content: "b"},
			},
			Content: "fallback",
		}
		delta := computeDelta(sent, result)
		if len(delta) != 1 || delta[0].Content != "fallback" {
			t.Errorf("delta = %+v", delta)
		}
	})
}

func TestExtractReply(t *testing.T) {
	t.Run("last message is assistant", func(t *testing.T) {
		result := &runner.Result{
			Messages: []runner.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "reply"},
			},
			Content: "reply",
		}
		if got := extractReply(result); got != "reply" {
			t.Errorf("got %q, want reply", got)
		}
	})

	t.Run("last message is tool walks back", func(t *testing.T) {
		result := &runner.Result{
			Messages: []runner.Message{
				{Role: "assistant", Content: "first reply", ToolCalls: []runner.ToolCall{{ID: "c1"}}},
				{Role: "tool", Content: "", ToolResults: []runner.ToolResult{{CallID: "c1", Content: "2"}}},
				{Role: "assistant", Content: "final reply"},
			},
			Content: "final reply",
		}
		if got := extractReply(result); got != "final reply" {
			t.Errorf("got %q, want 'final reply'", got)
		}
	})

	t.Run("tool result is last message walks back to prior assistant", func(t *testing.T) {
		result := &runner.Result{
			Messages: []runner.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "tool call", ToolCalls: []runner.ToolCall{{ID: "c1"}}},
				{Role: "tool", Content: "", ToolResults: []runner.ToolResult{{CallID: "c1", Content: "2"}}},
			},
			Content: "fallback",
		}
		if got := extractReply(result); got != "tool call" {
			t.Errorf("got %q, want 'tool call'", got)
		}
	})

	t.Run("empty Messages fallback to Content", func(t *testing.T) {
		result := &runner.Result{Messages: nil, Content: "fallback"}
		if got := extractReply(result); got != "fallback" {
			t.Errorf("got %q, want fallback", got)
		}
	})
}
