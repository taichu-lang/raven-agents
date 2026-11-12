package persistent

import (
	"time"

	"github.com/taichu-lang/raven-agents/pkg/database"
)

type TurnContext struct {
	UserID     string
	StartedAt  time.Time
	Iterations int64
	TokenUsage int64
	TokenCost  float64
	Model      string

	// ConversationID identifies a multi-turn conversation. On some platforms, it is also referred to as
	// SessionID, but this can easily be confused with login authentication scenarios.
	ConversationID string

	// TurnSeq is assigned after turn started.
	TurnSeq int64
	Status  database.TurnStatus

	// PendingTools is the IDs of the tool calls waiting for the user's response.
	PendingTools []string
}
