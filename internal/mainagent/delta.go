package mainagent

import (
	"log/slog"

	"github.com/jrswab/axe/pkg/runner"
)

// computeDelta returns the suffix of result.Messages that was not in sent.
// Falls back to a single assistant message containing result.Content.
func computeDelta(sent []runner.Message, result *runner.Result) []runner.Message {
	returned := result.Messages

	if returned == nil || len(returned) == 0 {
		slog.Warn("result.Messages is nil/empty, falling back to result.Content")
		return []runner.Message{{Role: assistantRole, Content: result.Content}}
	}

	if len(returned) < len(sent) {
		slog.Error("result.Messages shorter than sent messages, may indicate corruption", slog.Int("sent", len(sent)), slog.Int("returned", len(returned)))
		return []runner.Message{{Role: assistantRole, Content: result.Content}}
	}

	return returned[len(sent):]
}

// extractReply returns the assistant reply text from a runner.Result.
// It walks backward through result.Messages past tool messages to find
// the most recent assistant message. Falls back to result.Content.
func extractReply(result *runner.Result) string {
	msgs := result.Messages

	if len(msgs) == 0 {
		return result.Content
	}

	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == assistantRole {
			return msgs[i].Content
		}
	}

	return result.Content
}
