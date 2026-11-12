package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/persistent"
	"github.com/taichu-lang/raven-agents/pkg/database"
)

func textMessage(id string, role underlying.Role, text string) *underlying.Message {
	return &underlying.Message{
		ID:       id,
		Role:     role,
		Contents: underlying.MessageContents{underlying.NewTextContent(text)},
	}
}

func assistantWithCalls(id string, calls ...*underlying.ToolCallContent) *underlying.Message {
	contents := make(underlying.MessageContents, 0, len(calls))
	for _, call := range calls {
		contents = append(contents, call)
	}

	return &underlying.Message{ID: id, Role: underlying.RoleAssistant, Contents: contents}
}

func toolMessage(id string, contents ...underlying.MessageContent) *underlying.Message {
	return &underlying.Message{ID: id, Role: underlying.RoleTool, Contents: contents}
}

// The cut must walk back to the assistant message of a split pair: dropping a call while keeping
// its result breaks the provider's pairing invariant.
func TestChooseCutPoint(t *testing.T) {
	tests := []struct {
		name       string
		messages   []*underlying.Message
		start      int
		keepRecent int
		wantCut    int
		wantOK     bool
	}{
		{
			name: "no pairs, cut at keepRecent boundary",
			messages: []*underlying.Message{
				textMessage("m1", underlying.RoleUser, "one"),
				textMessage("m2", underlying.RoleAssistant, "two"),
				textMessage("m3", underlying.RoleUser, "three"),
				textMessage("m4", underlying.RoleAssistant, "four"),
				textMessage("m5", underlying.RoleUser, "five"),
			},
			start:      0,
			keepRecent: 2,
			wantCut:    3,
			wantOK:     true,
		},
		{
			name: "cut walks back across a split tool call pair",
			messages: []*underlying.Message{
				textMessage("m1", underlying.RoleUser, "one"),
				assistantWithCalls("m2", underlying.NewToolCallContent("c1", "exec", "", "{}")),
				toolMessage("m3", underlying.NewToolResultContent("c1", "exec", "ok", false)),
				textMessage("m4", underlying.RoleUser, "two"),
				assistantWithCalls("m5", underlying.NewToolCallContent("c2", "exec", "", "{}")),
				toolMessage("m6", underlying.NewToolResultContent("c2", "exec", "ok", false)),
				textMessage("m7", underlying.RoleUser, "three"),
				textMessage("m8", underlying.RoleAssistant, "four"),
				textMessage("m9", underlying.RoleUser, "five"),
			},
			start:      0,
			keepRecent: 4,
			// raw cut = 5 splits c2 (call at m5, result at m6); walking back to 4 keeps the
			// pair whole inside the kept tail.
			wantCut: 4,
			wantOK:  true,
		},
		{
			name: "cut walks back across a split tool search pair, then minDrop rejects",
			messages: []*underlying.Message{
				textMessage("m1", underlying.RoleUser, "one"),
				{ID: "m2", Role: underlying.RoleAssistant, Contents: underlying.MessageContents{
					underlying.NewToolSearchCallContent("tsc_1", "s1", nil),
				}},
				toolMessage("m3", underlying.NewToolSearchResultContent("s1", nil)),
				textMessage("m4", underlying.RoleUser, "two"),
				textMessage("m5", underlying.RoleAssistant, "three"),
				textMessage("m6", underlying.RoleUser, "four"),
			},
			start:      0,
			keepRecent: 4,
			// The search call (declared on the assistant message) is paired with the result
			// in m3; the only safe boundary leaves too little to drop.
			wantCut: 0,
			wantOK:  false,
		},
		{
			name: "unanswered call at the head blocks every boundary",
			messages: []*underlying.Message{
				assistantWithCalls("m1", underlying.NewToolCallContent("c1", "exec", "", "{}")),
				textMessage("m2", underlying.RoleUser, "one"),
				textMessage("m3", underlying.RoleUser, "two"),
				textMessage("m4", underlying.RoleUser, "three"),
				textMessage("m5", underlying.RoleUser, "four"),
				toolMessage("m6", underlying.NewToolResultContent("c1", "exec", "ok", false)),
			},
			start:      0,
			keepRecent: 2,
			wantCut:    0,
			wantOK:     false,
		},
		{
			name: "summary head folds into the dropped span from start=1",
			messages: []*underlying.Message{
				{ID: "head", Role: underlying.RoleUser, Contents: underlying.MessageContents{
					underlying.NewSummaryContent("old summary"),
				}},
				textMessage("m1", underlying.RoleUser, "one"),
				textMessage("m2", underlying.RoleAssistant, "two"),
				textMessage("m3", underlying.RoleUser, "three"),
				textMessage("m4", underlying.RoleAssistant, "four"),
				textMessage("m5", underlying.RoleUser, "five"),
			},
			start:      1,
			keepRecent: 3,
			wantCut:    3,
			wantOK:     true,
		},
		{
			name: "too little to drop",
			messages: []*underlying.Message{
				textMessage("m1", underlying.RoleUser, "one"),
				textMessage("m2", underlying.RoleAssistant, "two"),
				textMessage("m3", underlying.RoleUser, "three"),
			},
			start:      0,
			keepRecent: 2,
			wantCut:    0,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cut, ok := chooseCutPoint(tt.messages, tt.start, tt.keepRecent)
			if ok != tt.wantOK {
				t.Fatalf("expected ok=%v, got %v (cut=%d)", tt.wantOK, ok, cut)
			}
			if cut != tt.wantCut {
				t.Fatalf("expected cut=%d, got %d", tt.wantCut, cut)
			}
		})
	}
}

func TestCompactThreshold(t *testing.T) {
	compactor := &Compactor{options: CompactOptions{SafetyFactor: 0.8}}

	cm := &ContextManager{
		compactor: compactor,
		catalog:   &underlying.Catalog{Limit: underlying.Limit{Context: 10000}},
	}
	if got := cm.compactThreshold(); got != 8000 {
		t.Fatalf("catalog window times safety factor, got %d", got)
	}

	cm.catalog.Limit.Output = 1000
	if got := cm.compactThreshold(); got != 8000 {
		t.Fatalf("a small output reserve keeps the safety factor threshold, got %d", got)
	}

	cm.catalog.Limit.Output = 4000
	if got := cm.compactThreshold(); got != 6000 {
		t.Fatalf("a large output reserve must lower the threshold, got %d", got)
	}

	cm.catalog.Limit.Output = 10000
	if got := cm.compactThreshold(); got != 8000 {
		t.Fatalf("an output limit covering the window must be ignored, got %d", got)
	}

	cm.catalog.Limit = underlying.Limit{}
	if got := cm.compactThreshold(); got != 0 {
		t.Fatalf("no window size disables compaction, got %d", got)
	}
}

func TestEstimateTokens(t *testing.T) {
	messages := []*underlying.Message{
		textMessage("m1", underlying.RoleUser, strings.Repeat("a", 100)),
		assistantWithCalls("m2", underlying.NewToolCallContent("c1", "exec", "", strings.Repeat("b", 40))),
		toolMessage("m3", underlying.NewToolResultContent("c1", "exec", strings.Repeat("c", 200), false)),
	}

	// (100 + 40 + 200) / 4 = 85
	if got := estimateTokens(messages); got != 85 {
		t.Fatalf("unexpected estimate: %d", got)
	}
}

type fakeStrategy struct {
	gotPrior string
	gotSpan  int
	text     string
	usage    *underlying.UsageContent
}

func (f *fakeStrategy) Summarize(
	ctx context.Context,
	prior string,
	messages []*underlying.Message,
) (string, *underlying.UsageContent, error) {
	f.gotPrior = prior
	f.gotSpan = len(messages)
	return f.text, f.usage, nil
}

func newTestCompactor(strategy CompactStrategy) *Compactor {
	options := (&CompactOptions{}).withDefaults()
	options.Strategy = strategy
	return &Compactor{strategy: strategy, options: options}
}

func TestCompactorCompactViewAndRow(t *testing.T) {
	strategy := &fakeStrategy{text: "folded history", usage: &underlying.UsageContent{Total: 120}}
	compactor := newTestCompactor(strategy)
	compactor.options.KeepRecent = 3

	messages := []*underlying.Message{
		textMessage("m1", underlying.RoleUser, "one"),
		assistantWithCalls("m2", underlying.NewToolCallContent("c1", "exec", "", "{}")),
		toolMessage("m3", underlying.NewToolResultContent("c1", "exec", "ok", false)),
		textMessage("m4", underlying.RoleUser, "two"),
		textMessage("m5", underlying.RoleAssistant, "three"),
		textMessage("m6", underlying.RoleUser, "four"),
	}

	result, err := compactor.Compact(context.Background(), messages)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected a compaction result")
	}

	if result.Dropped != 3 {
		t.Fatalf("expected 3 dropped messages, got %d", result.Dropped)
	}
	if len(result.View) != 4 {
		t.Fatalf("expected the view to hold 4 messages, got %d", len(result.View))
	}

	head := result.View[0]
	if head.Role != underlying.RoleUser || len(head.Contents) != 1 {
		t.Fatalf("unexpected view head: %+v", head)
	}
	summary, ok := head.Contents[0].(*underlying.SummaryContent)
	if !ok {
		t.Fatalf("the view head must carry a SummaryContent, got %T", head.Contents[0])
	}
	if summary.Text != "folded history" {
		t.Fatalf("unexpected summary text: %q", summary.Text)
	}
	if result.View[1].ID != "m4" || result.View[3].ID != "m6" {
		t.Fatalf("the kept tail must be the verbatim last 3 messages: %+v", result.View[1:])
	}

	if result.Row.CoversFromMessageID != "m1" || result.Row.CoversToMessageID != "m3" {
		t.Fatalf("unexpected coverage: %+v", result.Row)
	}
	if result.Row.Tokens != 120 {
		t.Fatalf("the summarization spend must be recorded, got %d", result.Row.Tokens)
	}
}

func TestCompactorCompactComposesPriorSummary(t *testing.T) {
	strategy := &fakeStrategy{text: "extended summary"}
	compactor := newTestCompactor(strategy)
	compactor.options.KeepRecent = 3

	messages := []*underlying.Message{
		{ID: "head", Role: underlying.RoleUser, Contents: underlying.MessageContents{
			underlying.NewSummaryContent("old summary"),
		}},
		textMessage("m1", underlying.RoleUser, "one"),
		textMessage("m2", underlying.RoleAssistant, "two"),
		textMessage("m3", underlying.RoleUser, "three"),
		textMessage("m4", underlying.RoleAssistant, "four"),
		textMessage("m5", underlying.RoleUser, "five"),
	}

	result, err := compactor.Compact(context.Background(), messages)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected a compaction result")
	}

	if strategy.gotPrior != "old summary" {
		t.Fatalf("the prior summary must seed the strategy, got %q", strategy.gotPrior)
	}
	if strategy.gotSpan != 2 {
		t.Fatalf("the dropped span excludes the folded head, got %d messages", strategy.gotSpan)
	}
	if result.Row.CoversFromMessageID != "m1" {
		t.Fatalf("coverage starts at the first original message, got %q", result.Row.CoversFromMessageID)
	}
}

func TestCompactorCompactDeclines(t *testing.T) {
	strategy := &fakeStrategy{text: "unused"}
	compactor := newTestCompactor(strategy)

	messages := []*underlying.Message{
		textMessage("m1", underlying.RoleUser, "one"),
		textMessage("m2", underlying.RoleAssistant, "two"),
	}

	result, err := compactor.Compact(context.Background(), messages)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatalf("expected compaction to decline, got %+v", result)
	}
}

type fakeRepository struct {
	messages []*underlying.Message
	after    map[string][]*underlying.Message
	summary  *persistent.SummaryModel
	afterErr error
}

func (f *fakeRepository) AddTurn(
	ctx context.Context,
	turnContext *persistent.TurnContext,
	userMessageID string,
) (*database.TurnModel, error) {
	return nil, nil
}

func (f *fakeRepository) UpdateTurn(ctx context.Context, turnContext *persistent.TurnContext) error {
	return nil
}

func (f *fakeRepository) LoadLatestTurn(
	ctx context.Context,
	conversationID string,
) (*database.TurnModel, error) {
	return nil, nil
}

func (f *fakeRepository) StoreMessages(
	ctx context.Context,
	turnContext *persistent.TurnContext,
	messages []*underlying.Message,
) error {
	return nil
}

func (f *fakeRepository) LoadMessages(
	ctx context.Context,
	conversationID string,
) ([]*underlying.Message, error) {
	return f.messages, nil
}

func (f *fakeRepository) LoadMessagesAfter(
	ctx context.Context,
	conversationID string,
	afterMessageID string,
) ([]*underlying.Message, error) {
	if f.afterErr != nil {
		return nil, f.afterErr
	}
	return f.after[afterMessageID], nil
}

func (f *fakeRepository) StoreSummary(
	ctx context.Context,
	turnContext *persistent.TurnContext,
	summary *persistent.SummaryModel,
) error {
	return nil
}

func (f *fakeRepository) LoadLatestSummary(
	ctx context.Context,
	conversationID string,
) (*persistent.SummaryModel, error) {
	return f.summary, nil
}

func (f *fakeRepository) Close() error {
	return nil
}

func TestLoadCompactedView(t *testing.T) {
	summary := &persistent.SummaryModel{
		ID:                7,
		CoversToMessageID: "m3",
		Summary:           "folded history",
	}
	tail := []*underlying.Message{
		textMessage("m4", underlying.RoleUser, "two"),
		textMessage("m5", underlying.RoleAssistant, "three"),
	}

	c := ContextManager{store: &fakeRepository{after: map[string][]*underlying.Message{"m3": tail}}}
	view, err := c.loadCompactedView(context.Background(), "conv", summary)
	if err != nil {
		t.Fatal(err)
	}

	if len(view) != 3 {
		t.Fatalf("expected [head] + 2 tail messages, got %d", len(view))
	}
	head, ok := view[0].Contents[0].(*underlying.SummaryContent)
	if !ok {
		t.Fatalf("the view head must carry a SummaryContent, got %T", view[0].Contents[0])
	}
	if head.Text != "folded history" {
		t.Fatalf("unexpected head text: %q", head.Text)
	}
	if view[1].ID != "m4" || view[2].ID != "m5" {
		t.Fatalf("unexpected tail: %+v", view[1:])
	}
}

func TestLoadCompactedViewFallsBackWhenAnchorMissing(t *testing.T) {
	full := []*underlying.Message{
		textMessage("m1", underlying.RoleUser, "one"),
		textMessage("m2", underlying.RoleAssistant, "two"),
	}

	c := &ContextManager{
		store: &fakeRepository{
			messages: full,
			afterErr: fmt.Errorf("anchor: %w", persistent.ErrMessageNotFound),
		},
	}

	view, err := c.loadCompactedView(
		context.Background(),
		"conv",
		&persistent.SummaryModel{CoversToMessageID: "gone"},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(view) != len(full) {
		t.Fatalf("expected the full history as fallback, got %d messages", len(view))
	}
}
