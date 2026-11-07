package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"iter"
)

// sseEvent is one decoded server-sent event: the payload plus the event type carried by the optional `event:`
// field.
type sseEvent struct {
	Type string
	Data []byte
}

// sseFrames iterates the events of an SSE stream, one yield per event. The parsing rules follow the official
// SDK's ssestream decoder: `data:` fields of one event are joined with newlines, comment lines as well as
// `event:`/`id:`/`retry:` fields are handled per spec, CRLF line endings are tolerated, and an event is
// dispatched on a blank line. Three additions target non-standard gateways: physical lines that carry no SSE
// field prefix are appended verbatim so pretty-printed payloads survive; a trailing event without a blank line
// is dispatched at EOF instead of dropped; a leading UTF-8 BOM is stripped. A payload with a top-level `error`
// field surfaces as an error, mirroring the SDK's stream error handling. Unlike the SDK decoder, which caps one
// physical line at bufio.MaxScanTokenSize<<9, this decoder reads with a bufio.Reader and has no per-line cap,
// so arbitrarily large base64 image frames stream through.
func sseFrames(body io.Reader) iter.Seq2[sseEvent, error] {
	return func(yield func(sseEvent, error) bool) {
		reader := bufio.NewReaderSize(body, 64*1024)
		var frame bytes.Buffer
		var eventType string

		// Some gateways prepend a UTF-8 BOM to the stream; without stripping it the first line would be treated
		// as a payload continuation and corrupt the event.
		if leading, err := reader.Peek(3); err == nil && bytes.Equal(leading, []byte("\xef\xbb\xbf")) {
			_, _ = reader.Discard(3)
		}

		// flush dispatches the accumulated event; it reports whether iteration should continue (false on the
		// [DONE] sentinel, an error event, or a stopped consumer). The payload is cloned because the frame
		// buffer is reused across events and consumers may retain the yielded value.
		flush := func() bool {
			payload := bytes.Clone(bytes.TrimSpace(frame.Bytes()))
			frame.Reset()
			typ := eventType
			eventType = ""

			if len(payload) == 0 {
				return true
			}
			if bytes.Equal(payload, []byte("[DONE]")) {
				return false
			}

			var probe struct {
				Error json.RawMessage `json:"error"`
			}
			if err := json.Unmarshal(payload, &probe); err == nil &&
				len(probe.Error) > 0 && !bytes.Equal(probe.Error, []byte("null")) {
				return yield(sseEvent{}, fmt.Errorf("received error while streaming: %s", probe.Error))
			}

			return yield(sseEvent{Type: typ, Data: payload}, nil)
		}

		for {
			line, readErr := reader.ReadBytes('\n')
			line = bytes.TrimRight(line, "\r\n")

			switch {
			case len(line) == 0:
				// A blank line dispatches the accumulated event.
				if !flush() {
					return
				}
			case bytes.HasPrefix(line, []byte("data:")):
				value := bytes.TrimPrefix(line, []byte("data:"))
				value = bytes.TrimPrefix(value, []byte(" ")) // one optional space per spec
				frame.Write(value)
				frame.WriteByte('\n')
			case bytes.HasPrefix(line, []byte("event:")):
				eventType = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("event:"))))
			case bytes.HasPrefix(line, []byte("id:")) ||
				bytes.HasPrefix(line, []byte("retry:")) ||
				bytes.HasPrefix(line, []byte(":")):
				// `id` and `retry` fields and comment lines carry no payload.
			default:
				// Continuation of a pretty-printed multi-line data payload.
				frame.Write(line)
				frame.WriteByte('\n')
			}

			if readErr != nil {
				if readErr == io.EOF {
					// Flush a trailing event not terminated by a blank line.
					flush()
					return
				}

				yield(sseEvent{}, fmt.Errorf("read event stream: %w", readErr))
				return
			}
		}
	}
}
