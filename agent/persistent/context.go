package persistent

import (
	"time"
)

type TurnStatus string

const (
	TurnStatusIdle            TurnStatus = "idle"
	TurnStatusRunning         TurnStatus = "running"
	TurnStatusDone            TurnStatus = "done"
	TurnStatusFailed          TurnStatus = "failed"
	TurnStatusWaitingApproval TurnStatus = "waiting_approval"
	TurnStatusInterrupted     TurnStatus = "interrupted"
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
	Status  TurnStatus
}
