package persistent

import (
	"encoding/json"

	"github.com/uptrace/bun"
)

// TurnModel maps a single conversation turn onto the `turns` table. Keep it in sync with
// migrations/05_turns.up.sql.
type TurnModel struct {
	bun.BaseModel `bun:"table:turns,alias:t"`

	ID int64 `bun:"id,pk,autoincrement"`

	// ConversationID identifies the multi-turn conversation the turn belongs to.
	ConversationID string `bun:"conversation_id,notnull"`

	// TurnSeq is the index of the turn within the conversation.
	TurnSeq int64 `bun:"turn_seq,notnull"`

	// UserMessageID is the message that opened the turn.
	UserMessageID string `bun:"user_message_id,notnull"`

	// Status is the lifecycle state of the turn, one of the TurnStatus constants.
	Status TurnStatus `bun:"status,notnull"`

	// LeaseUntil is the unix timestamp in seconds until which the current holder owns the turn.
	LeaseUntil int64 `bun:"lease_until,notnull"`

	// Epoch fences off stale holders: every takeover of the turn increments it.
	Epoch int64 `bun:"epoch,notnull"`

	// CancelRequested asks the holder instance to cancel the turn.
	CancelRequested bool `bun:"cancel_requested,notnull"`

	// Iterations counts the agent loop iterations the turn has run.
	Iterations int64 `bun:"iterations,notnull"`

	// TokensUsed accumulates the tokens the turn has consumed.
	TokensUsed int64 `bun:"tokens_used,notnull"`

	// LastMessageID is the most recent message persisted for the turn.
	LastMessageID string `bun:"last_message_id,notnull"`

	// PendingTools holds the IDs of the tool calls waiting for the user's response while the turn is in
	// waiting_approval; the calls themselves live in the messages.
	PendingTools json.RawMessage `bun:"pending_tools,notnull"`

	// CreatedAt is a unix timestamp in seconds, assigned by the application layer.
	CreatedAt int64 `bun:"created_at,notnull"`

	// UpdatedAt is a unix timestamp in seconds, assigned by the application layer.
	UpdatedAt int64 `bun:"updated_at,notnull"`
}
