package mainagent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jrswab/axe/pkg/runner"
	"github.com/jrswab/hilt/internal/session"
)

// TurnReader reads all turns for a session.
type TurnReader interface {
	GetTurns(ctx context.Context, sessionID int64) ([]session.TurnRow, error)
}

// HistoryBuilder reconstructs []runner.Message from persisted turns.
type HistoryBuilder interface {
	BuildMessages(ctx context.Context, sessionID int64) ([]runner.Message, error)
}

// historyBuilder implements HistoryBuilder.
type historyBuilder struct {
	reader TurnReader
}

// NewHistoryBuilder creates a HistoryBuilder backed by the given TurnReader.
func NewHistoryBuilder(reader TurnReader) HistoryBuilder {
	return &historyBuilder{reader: reader}
}

// BuildMessages reconstructs the full conversation history for a session.
func (hb *historyBuilder) BuildMessages(ctx context.Context, sessionID int64) ([]runner.Message, error) {
	turns, err := hb.reader.GetTurns(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	out := make([]runner.Message, 0)
	for _, t := range turns {
		// User message for this turn
		out = append(out, runner.Message{Role: userRole, Content: t.UserMessage})

		// Messages produced during this turn (the delta)
		var persisted []persistedMessage
		if err := json.Unmarshal([]byte(t.NewMessagesJSON), &persisted); err != nil {
			return nil, fmt.Errorf("invalid new_messages_json at turn %d: %w", t.TurnNumber, err)
		}
		for _, pm := range persisted {
			if !validRoles[pm.Role] {
				return nil, fmt.Errorf("unrecognized role %q in turn %d", pm.Role, t.TurnNumber)
			}
		}
		runnerMsgs := toRunnerMessages(persisted)
		out = append(out, runnerMsgs...)
	}

	return out, nil
}
