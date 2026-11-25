package mcp

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoInput struct {
	Text string `json:"text"`
}

// newTestManager returns a manager whose dial starts a fresh in-process echo server per connection and counts
// the connections.
func newTestManager(t *testing.T) (*Manager, *atomic.Int32) {
	t.Helper()

	connections := &atomic.Int32{}
	manager := NewManager()
	t.Cleanup(manager.Close)

	manager.dial = func(*ServerConfig) (mcpsdk.Transport, error) {
		connections.Add(1)
		server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "echo", Version: "1"}, nil)
		mcpsdk.AddTool(
			server,
			&mcpsdk.Tool{Name: "say.hello", Description: "Echo the text back."},
			func(_ context.Context, _ *mcpsdk.CallToolRequest, in echoInput) (*mcpsdk.CallToolResult, any, error) {
				if in.Text == "boom" {
					return &mcpsdk.CallToolResult{
						IsError: true,
						Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "exploded"}},
					}, nil, nil
				}

				return &mcpsdk.CallToolResult{
					Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "echo: " + in.Text}},
				}, nil, nil
			},
		)

		clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
		if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
			return nil, err
		}

		return clientTransport, nil
	}

	return manager, connections
}

func TestToolsBridgeAndCall(t *testing.T) {
	manager, _ := newTestManager(t)
	ctx := context.Background()
	cfg := ServerConfig{Name: "echo", Transport: TransportStdio, Command: "unused"}

	tools, err := manager.Tools(ctx, "1/echo", cfg)
	if err != nil {
		t.Fatalf("tools: %v", err)
	}

	if len(tools) != 1 || tools[0].Name() != "mcp__echo__say_hello" {
		t.Fatalf("unexpected tools: %v", tools)
	}

	if tools[0].InSchema() == nil || tools[0].InSchema().Properties["text"] == nil {
		t.Fatalf("schema not carried over: %+v", tools[0].InSchema())
	}

	out, err := tools[0].Call(ctx, `{"text":"hi"}`)
	if err != nil || out != "echo: hi" {
		t.Fatalf("call = %v, %v", out, err)
	}

	// The server side name is forwarded, and an IsError result surfaces as an error.
	if _, err := tools[0].Call(ctx, `{"text":"boom"}`); err == nil || err.Error() != "exploded" {
		t.Fatalf("want exploded error, got %v", err)
	}
}

func TestSessionIsReusedAndReconnectedAfterDrop(t *testing.T) {
	manager, connections := newTestManager(t)
	ctx := context.Background()
	cfg := ServerConfig{Name: "echo", Transport: TransportStdio, Command: "unused"}

	tools, err := manager.Tools(ctx, "1/echo", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Tools(ctx, "1/echo", cfg); err != nil {
		t.Fatal(err)
	}

	if connections.Load() != 1 {
		t.Fatalf("want one shared connection, got %d", connections.Load())
	}

	// Kill the session behind the tool: the next call reconnects once and succeeds.
	manager.servers["1/echo"].close()
	if out, err := tools[0].Call(ctx, `{"text":"again"}`); err != nil || out != "echo: again" {
		t.Fatalf("call after drop = %v, %v", out, err)
	}

	if connections.Load() != 2 {
		t.Fatalf("want a reconnect, got %d connections", connections.Load())
	}
}

func TestChangedConfigReplacesSession(t *testing.T) {
	manager, connections := newTestManager(t)
	ctx := context.Background()

	cfg := ServerConfig{
		Name:      "echo",
		Transport: TransportHTTP,
		URL:       "http://a",
		Headers:   map[string]string{"k": "1"},
	}
	if _, err := manager.Tools(ctx, "1/echo", cfg); err != nil {
		t.Fatal(err)
	}

	cfg.Headers = map[string]string{"k": "2"}
	if _, err := manager.Tools(ctx, "1/echo", cfg); err != nil {
		t.Fatal(err)
	}

	if connections.Load() != 2 {
		t.Fatalf("a rotated credential must reconnect, got %d connections", connections.Load())
	}
}

func TestToolNameSanitizesAndTruncates(t *testing.T) {
	if got := ToolName("git-hub", "list.issues v2"); got != "mcp__git-hub__list_issues_v2" {
		t.Fatalf("got %q", got)
	}

	long := strings.Repeat("x", 100)
	a, b := ToolName("srv", long+"a"), ToolName("srv", long+"b")
	if len(a) != maxToolNameLength || len(b) != maxToolNameLength || a == b {
		t.Fatalf("truncated names must be 64 bytes and unique: %q %q", a, b)
	}
}

func TestTruncateKeepsUTF8(t *testing.T) {
	text := strings.Repeat("好", maxResultBytes)
	got := truncate(text)
	if !strings.HasSuffix(got, truncatedMarker) || strings.ContainsRune(got, '�') {
		t.Fatal("truncation must cut on a rune boundary and mark the output")
	}
}
