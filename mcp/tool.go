package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// ToolNamePrefix starts the name of every bridged tool: mcp__<server>__<tool>.
	ToolNamePrefix = "mcp__"

	// maxToolNameLength is the function name limit of the OpenAI Responses API.
	maxToolNameLength = 64

	// maxResultBytes caps the text handed to the model, like the exec tool does for its output.
	maxResultBytes = 64 * 1024

	truncatedMarker = "\n[output truncated]"
)

var invalidToolNameChars = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// caller is the part of the manager a bridged tool needs to reach its server.
type caller interface {
	callTool(ctx context.Context, name string, args json.RawMessage) (*mcpsdk.CallToolResult, error)
}

// mcpTool bridges one tool of an MCP server to tool.FuncTool. The schema is taken from the server as is, so
// functool.New, which derives schemas by reflection, is not used.
type mcpTool struct {
	server      caller
	name        string
	rawName     string
	description string
	schema      *jsonschema.Schema
}

// ToolName builds the model facing name of a server tool. The server name never contains "__" (checked on
// registration), so the name stays reversible. Characters outside [A-Za-z0-9_-] of the tool name become "_",
// and a name beyond the limit is cut with an FNV-1a digest appended to keep it unique.
func ToolName(serverName, rawName string) string {
	name := ToolNamePrefix + serverName + "__" + invalidToolNameChars.ReplaceAllString(rawName, "_")
	if len(name) <= maxToolNameLength {
		return name
	}

	digest := fnv.New32a()
	_, _ = digest.Write([]byte(name))
	return fmt.Sprintf("%s%08x", name[:maxToolNameLength-8], digest.Sum32())
}

// newTool converts the wire definition. It fails when the input schema cannot be converted, so the caller can
// skip the tool and keep the server.
func newTool(server caller, serverName string, def *mcpsdk.Tool) (*mcpTool, error) {
	schema, err := convertSchema(def.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("input schema of %q: %w", def.Name, err)
	}

	description := def.Description
	if description == "" {
		description = def.Title
	}

	return &mcpTool{
		server:      server,
		name:        ToolName(serverName, def.Name),
		rawName:     def.Name,
		description: description,
		schema:      schema,
	}, nil
}

// convertSchema maps the wire schema (a map[string]any) onto *jsonschema.Schema. A missing schema becomes an
// empty object schema, as the Responses API requires parameters for each function.
func convertSchema(raw any) (*jsonschema.Schema, error) {
	if raw == nil {
		return &jsonschema.Schema{Type: "object"}, nil
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	schema := &jsonschema.Schema{}
	if err := json.Unmarshal(data, schema); err != nil {
		return nil, err
	}

	if schema.Type == "" && schema.Types == nil {
		schema.Type = "object"
	}

	return schema, nil
}

func (t *mcpTool) Name() string                  { return t.name }
func (t *mcpTool) Description() string           { return t.description }
func (t *mcpTool) InSchema() *jsonschema.Schema  { return t.schema }
func (t *mcpTool) OutSchema() *jsonschema.Schema { return nil }

func (t *mcpTool) Call(ctx context.Context, args string) (any, error) {
	var arguments json.RawMessage
	if strings.TrimSpace(args) != "" {
		arguments = json.RawMessage(args)
	}

	result, err := t.server.callTool(ctx, t.rawName, arguments)
	if err != nil {
		return nil, err
	}

	return renderResult(result)
}

// renderResult maps a call result to the value the agent encodes for the model: the structured content when
// present, else the joined text. An IsError result becomes an error, which the agent renders as a failed tool
// result for the model to self-correct on.
func renderResult(result *mcpsdk.CallToolResult) (any, error) {
	var text strings.Builder
	omitted := 0
	for _, content := range result.Content {
		textContent, ok := content.(*mcpsdk.TextContent)
		if !ok {
			omitted++
			continue
		}

		if text.Len() > 0 {
			text.WriteString("\n")
		}

		text.WriteString(textContent.Text)
	}

	if omitted > 0 {
		if text.Len() > 0 {
			text.WriteString("\n")
		}

		fmt.Fprintf(&text, "[%d non-text content omitted]", omitted)
	}

	if result.IsError {
		return nil, fmt.Errorf("%s", truncate(text.String()))
	}

	if result.StructuredContent != nil {
		encoded, err := json.Marshal(result.StructuredContent)
		if err == nil && len(encoded) <= maxResultBytes {
			return json.RawMessage(encoded), nil
		}
	}

	return truncate(text.String()), nil
}

func truncate(text string) string {
	if len(text) <= maxResultBytes {
		return text
	}

	cut := maxResultBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}

	return text[:cut] + truncatedMarker
}
