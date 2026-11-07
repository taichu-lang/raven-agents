package openai

import (
	"strings"
	"testing"
)

func collectEvents(t *testing.T, raw string) []sseEvent {
	t.Helper()
	events := make([]sseEvent, 0)
	for event, err := range sseFrames(strings.NewReader(raw)) {
		if err != nil {
			t.Fatal(err)
		}

		events = append(events, event)
	}

	return events
}

func collectFrames(t *testing.T, raw string) []string {
	t.Helper()
	payloads := make([]string, 0)
	for _, event := range collectEvents(t, raw) {
		payloads = append(payloads, string(event.Data))
	}

	return payloads
}

func TestSSEFramesCompact(t *testing.T) {
	raw := "data: {\"type\":\"a\"}\n\ndata: {\"type\":\"b\"}\n\ndata: [DONE]\n\n"
	payloads := collectFrames(t, raw)
	if len(payloads) != 2 || payloads[0] != `{"type":"a"}` || payloads[1] != `{"type":"b"}` {
		t.Fatalf("unexpected payloads: %v", payloads)
	}
}

// TestSSEFramesPrettyPrinted reproduces the gateway layout observed in production: the JSON payload starts on
// the data line and continues across indented physical lines that carry no SSE field prefix.
func TestSSEFramesPrettyPrinted(t *testing.T) {
	raw := "event: image_edit.completed\n" +
		"data: {\"type\":\"image_edit.completed\",\"b64_json\":\"QUJD\",\n" +
		"      \"usage\": {\n" +
		"        \"total_tokens\": 535\n" +
		"      }\n" +
		"}\n\n"
	payloads := collectFrames(t, raw)
	want := `{"type":"image_edit.completed","b64_json":"QUJD",` +
		"\n      \"usage\": {\n        \"total_tokens\": 535\n      }\n}"
	if len(payloads) != 1 || payloads[0] != want {
		t.Fatalf("unexpected payloads: %q", payloads)
	}
}

func TestSSEFramesCommentsAndCRLF(t *testing.T) {
	raw := ": ping\r\n\r\ndata: {\"type\":\"a\"}\r\n\r\n"
	payloads := collectFrames(t, raw)
	if len(payloads) != 1 || payloads[0] != `{"type":"a"}` {
		t.Fatalf("unexpected payloads: %v", payloads)
	}
}

func TestSSEFramesEOFWithoutBlankLine(t *testing.T) {
	payloads := collectFrames(t, "data: {\"type\":\"a\"}")
	if len(payloads) != 1 || payloads[0] != `{"type":"a"}` {
		t.Fatalf("unexpected payloads: %v", payloads)
	}
}

func TestSSEFramesEmptyStream(t *testing.T) {
	if payloads := collectFrames(t, ""); len(payloads) != 0 {
		t.Fatalf("unexpected payloads: %v", payloads)
	}
}

func TestSSEFramesEventType(t *testing.T) {
	raw := "event: image_generation.completed\ndata: {\"type\":\"image_generation.completed\"}\n\n"
	events := collectEvents(t, raw)
	if len(events) != 1 || events[0].Type != "image_generation.completed" {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestSSEFramesBOM(t *testing.T) {
	payloads := collectFrames(t, "\xef\xbb\xbfdata: {\"type\":\"a\"}\n\n")
	if len(payloads) != 1 || payloads[0] != `{"type":"a"}` {
		t.Fatalf("unexpected payloads: %v", payloads)
	}
}

// TestSSEFramesErrorEvent mirrors the SDK's stream behavior: a payload with a
// top-level error field surfaces as an error instead of being silently dropped.
func TestSSEFramesErrorEvent(t *testing.T) {
	raw := "data: {\"error\":{\"message\":\"content_policy_violation\"}}\n\n"

	var streamErr error
	for _, err := range sseFrames(strings.NewReader(raw)) {
		if err != nil {
			streamErr = err
			break
		}
	}

	if streamErr == nil || !strings.Contains(streamErr.Error(), "content_policy_violation") {
		t.Fatalf("expected stream error, got %v", streamErr)
	}
}

// TestSSEFramesNullErrorField ensures an explicit null error field does not
// masquerade as an error event.
func TestSSEFramesNullErrorField(t *testing.T) {
	payloads := collectFrames(t, "data: {\"error\":null,\"type\":\"a\"}\n\n")
	if len(payloads) != 1 || payloads[0] != `{"error":null,"type":"a"}` {
		t.Fatalf("unexpected payloads: %v", payloads)
	}
}
