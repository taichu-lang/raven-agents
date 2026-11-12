package persistent

import (
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/uptrace/bun"
)

// SummaryModel maps a context compaction summary onto the `message_summaries` table. Keep it in sync with
// migrations/01_messages.up.sql.
type SummaryModel struct {
	bun.BaseModel `bun:"table:message_summaries,alias:ms"`

	ID int64 `bun:"id,pk,autoincrement"`

	// ConversationID identifies the multi-turn conversation the summary belongs to.
	ConversationID string `bun:"conversation_id,notnull"`

	// TurnSeq is the index of the turn that produced the summary.
	TurnSeq int64 `bun:"turn_seq,notnull"`

	// CoversFromMessageID is the message_id of the first message folded into the summary.
	CoversFromMessageID string `bun:"covers_from_message_id,notnull"`

	// CoversToMessageID is the message_id of the last message folded into the summary; the compacted view
	// resumes right after it.
	CoversToMessageID string `bun:"covers_to_message_id,notnull"`

	// Summary is the compaction text that replaces the covered messages in the LLM view.
	Summary string `bun:"summary,notnull"`

	// Tokens is the token spend of the summarization call that produced the summary.
	Tokens int64 `bun:"tokens,notnull"`

	// CreatedAt is a unix timestamp in seconds, assigned by the application layer.
	CreatedAt int64 `bun:"created_at,notnull"`
}

// ToMessage synthesizes the head message of the compacted LLM view: the summary re-enters the conversation as a
// user message carrying a SummaryContent, which each provider adapter renders in the wrapping format its model
// family prefers.
func (m *SummaryModel) ToMessage() *underlying.Message {
	return underlying.NewMessage(underlying.RoleUser, underlying.MessageContents{
		underlying.NewSummaryContent(m.Summary),
	})
}
