package persistent_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/persistent"
	"github.com/taichu-lang/raven-agents/pkg/database"
)

func newTestStore(t *testing.T) *persistent.StoreRef {
	t.Helper()

	store, err := database.NewSqliteStore(&database.SqliteConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DB().Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return persistent.NewStoreRef(store.DB(), logger, false)
}

func textMessage(id string, role underlying.Role, text string) *underlying.Message {
	return &underlying.Message{
		ID:       id,
		Role:     role,
		Contents: underlying.MessageContents{underlying.NewTextContent(text)},
	}
}

func storeTurn(
	t *testing.T,
	ctx context.Context,
	store *persistent.StoreRef,
	conversationID string,
	messages ...*underlying.Message,
) *persistent.TurnContext {
	t.Helper()

	turnContext := &persistent.TurnContext{UserID: "user", ConversationID: conversationID}
	if _, err := store.AddTurn(ctx, turnContext, messages[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreMessages(ctx, turnContext, messages); err != nil {
		t.Fatal(err)
	}

	return turnContext
}

// The summary lifecycle against a real sqlite database: coverage anchors resolve to row ids, the
// newest summary wins, and originals stay untouched in the messages table.
func TestSummaryLifecycle(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	conversationID := uuid.NewString()

	turn1 := storeTurn(t, ctx, store, conversationID,
		textMessage("m1", underlying.RoleUser, "one"),
		textMessage("m2", underlying.RoleAssistant, "two"),
	)
	turn2 := storeTurn(t, ctx, store, conversationID,
		textMessage("m3", underlying.RoleUser, "three"),
		textMessage("m4", underlying.RoleAssistant, "four"),
	)

	first := &persistent.SummaryModel{
		CoversFromMessageID: "m1",
		CoversToMessageID:   "m2",
		Summary:             "first summary",
		Tokens:              42,
	}
	if err := store.StoreSummary(ctx, turn1, first); err != nil {
		t.Fatal(err)
	}
	if first.ConversationID != conversationID || first.TurnSeq != turn1.TurnSeq || first.CreatedAt == 0 {
		t.Fatalf("store must fill the scope fields, got %+v", first)
	}

	second := &persistent.SummaryModel{
		CoversFromMessageID: "m3",
		CoversToMessageID:   "m4",
		Summary:             "second summary",
	}
	if err := store.StoreSummary(ctx, turn2, second); err != nil {
		t.Fatal(err)
	}

	latest, err := store.LoadLatestSummary(ctx, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.ID != second.ID || latest.Summary != "second summary" {
		t.Fatalf("the newest summary must win, got %+v", latest)
	}

	// A conversation without summaries gets nil, not an error.
	if latest, err := store.LoadLatestSummary(ctx, uuid.NewString()); err != nil || latest != nil {
		t.Fatalf("expected nil summary for a fresh conversation, got %+v (%v)", latest, err)
	}

	// The anchor resolves to the stored row order: messages after m2 are m3 and m4.
	tail, err := store.LoadMessagesAfter(ctx, conversationID, "m2")
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 2 || tail[0].ID != "m3" || tail[1].ID != "m4" {
		t.Fatalf("unexpected tail after m2: %+v", tail)
	}

	// An empty anchor behaves like LoadMessages; the originals are never rewritten.
	all, err := store.LoadMessagesAfter(ctx, conversationID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 || all[0].ID != "m1" || all[3].ID != "m4" {
		t.Fatalf("empty anchor must load the full history: %+v", all)
	}

	// A missing anchor is an explicit error so the caller falls back to full history.
	if _, err := store.LoadMessagesAfter(ctx, conversationID, "gone"); !errors.Is(err, persistent.ErrMessageNotFound) {
		t.Fatalf("expected ErrMessageNotFound, got %v", err)
	}
}

// The summary row renders the view head the restore path prepends to the tail.
func TestSummaryModelToMessage(t *testing.T) {
	summary := &persistent.SummaryModel{Summary: "folded history"}

	message := summary.ToMessage()
	if message.Role != underlying.RoleUser || len(message.Contents) != 1 {
		t.Fatalf("unexpected synthesized message: %+v", message)
	}
	content, ok := message.Contents[0].(*underlying.SummaryContent)
	if !ok {
		t.Fatalf("the head must carry a SummaryContent, got %T", message.Contents[0])
	}
	if content.Text != "folded history" {
		t.Fatalf("unexpected summary text: %q", content.Text)
	}
}
