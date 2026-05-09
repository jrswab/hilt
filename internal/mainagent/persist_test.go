package mainagent

import (
	"encoding/json"
	"testing"

	"github.com/jrswab/axe/pkg/runner"
)

func TestPersistedMessageRoundTrip(t *testing.T) {
	t.Run("plain assistant message", func(t *testing.T) {
		original := []runner.Message{
			{Role: "assistant", Content: "Hello there"},
		}
		persisted := fromRunnerMessages(original)
		jsonBytes, err := json.Marshal(persisted)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var back []persistedMessage
		if err := json.Unmarshal(jsonBytes, &back); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		reconstructed := toRunnerMessages(back)
		if len(reconstructed) != 1 {
			t.Fatalf("expected 1 message, got %d", len(reconstructed))
		}
		if reconstructed[0].Role != "assistant" || reconstructed[0].Content != "Hello there" {
			t.Errorf("msg = %+v", reconstructed[0])
		}
	})

	t.Run("assistant with tool calls and tool results", func(t *testing.T) {
		original := []runner.Message{
			{
				Role:    "assistant",
				Content: "",
				ToolCalls: []runner.ToolCall{
					{ID: "call_1", Name: "calculator", Arguments: map[string]string{"expr": "1+1"}},
				},
			},
			{
				Role:        "tool",
				Content:     "",
				ToolResults: []runner.ToolResult{{CallID: "call_1", Content: "2", IsError: false}},
			},
			{
				Role:    "assistant",
				Content: "The answer is 2.",
			},
		}
		persisted := fromRunnerMessages(original)
		jsonBytes, err := json.Marshal(persisted)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var back []persistedMessage
		if err := json.Unmarshal(jsonBytes, &back); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		reconstructed := toRunnerMessages(back)
		if len(reconstructed) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(reconstructed))
		}

		// First message: assistant with tool calls
		msg := reconstructed[0]
		if msg.Role != "assistant" {
			t.Errorf("msg[0].role = %q, want assistant", msg.Role)
		}
		if len(msg.ToolCalls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
		}
		if msg.ToolCalls[0].ID != "call_1" || msg.ToolCalls[0].Name != "calculator" {
			t.Errorf("tool call = %+v", msg.ToolCalls[0])
		}
		if msg.ToolCalls[0].Arguments["expr"] != "1+1" {
			t.Errorf("arguments = %v", msg.ToolCalls[0].Arguments)
		}

		// Second message: tool with tool results
		msg = reconstructed[1]
		if msg.Role != "tool" {
			t.Errorf("msg[1].role = %q, want tool", msg.Role)
		}
		if len(msg.ToolResults) != 1 {
			t.Fatalf("expected 1 tool result, got %d", len(msg.ToolResults))
		}
		if msg.ToolResults[0].CallID != "call_1" || msg.ToolResults[0].Content != "2" || msg.ToolResults[0].IsError {
			t.Errorf("tool result = %+v", msg.ToolResults[0])
		}

		// Third message: final assistant reply
		msg = reconstructed[2]
		if msg.Role != "assistant" || msg.Content != "The answer is 2." {
			t.Errorf("msg[2] = %+v", msg)
		}
	})

	t.Run("backward compatibility with old role+content only", func(t *testing.T) {
		oldJSON := `[{"role":"assistant","content":"old format"}]`
		var back []persistedMessage
		if err := json.Unmarshal([]byte(oldJSON), &back); err != nil {
			t.Fatalf("unmarshal old format: %v", err)
		}

		reconstructed := toRunnerMessages(back)
		if len(reconstructed) != 1 {
			t.Fatalf("expected 1 message, got %d", len(reconstructed))
		}
		if reconstructed[0].Role != "assistant" || reconstructed[0].Content != "old format" {
			t.Errorf("msg = %+v", reconstructed[0])
		}
	})
}
