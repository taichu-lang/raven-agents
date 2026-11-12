package llm

import (
	"context"
	"testing"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

// The tag middleware re-tags only the final chunk of the compaction stream; deltas and usage
// pass through so the shared chain (ex: metering) still sees them under their usual types.
func TestSummaryTagMiddleware(t *testing.T) {
	next := func(
		ctx context.Context,
		messages []*underlying.Message,
		options *underlying.GenOptions,
	) underlying.ResponseStream {
		return func(yield func(*underlying.ResponseChunk, error) bool) {
			chunks := []*underlying.ResponseChunk{
				{Type: underlying.ResponseChunkTypeDelta},
				{Type: underlying.ResponseChunkTypeUsage},
				{Type: underlying.ResponseChunkTypeFinal, FinishReason: underlying.FinishReasonDone},
			}
			for _, chunk := range chunks {
				if !yield(chunk, nil) {
					return
				}
			}
		}
	}

	stream := NewSummaryTagMiddleware().Run(next, context.Background(), nil, nil)

	var types []underlying.ResponseChunkType
	for chunk, err := range stream {
		if err != nil {
			t.Fatal(err)
		}
		types = append(types, chunk.Type)
	}

	expected := []underlying.ResponseChunkType{
		underlying.ResponseChunkTypeDelta,
		underlying.ResponseChunkTypeUsage,
		underlying.ResponseChunkTypeSummary,
	}
	if len(types) != len(expected) {
		t.Fatalf("expected %d chunks, got %d", len(expected), len(types))
	}
	for i, chunkType := range expected {
		if types[i] != chunkType {
			t.Fatalf("chunk %d: expected type %q, got %q", i, chunkType, types[i])
		}
	}
}
