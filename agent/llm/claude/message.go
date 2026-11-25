package claude

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/tool"
)

// summaryHeader prefixes a compaction summary when rendered as user text.
const summaryHeader = "[context summary]\n"

// redactedThinkingID marks a ReasoningContent that carries a redacted thinking block: the opaque payload is
// stored in EncryptedContent.
const redactedThinkingID = "redacted_thinking"

func (p *Provider) newMessageParams(
	messages []*underlying.Message,
	options *underlying.GenOptions,
) (*anthropic.MessageNewParams, error) {
	params := &anthropic.MessageNewParams{
		Model:     anthropic.Model(p.modelID()),
		MaxTokens: p.maxTokens(),
		Messages:  make([]anthropic.MessageParam, 0, len(messages)),
	}

	if p.options.Instructions != "" {
		params.System = []anthropic.TextBlockParam{{Text: p.options.Instructions}}
	}

	for _, message := range messages {
		param, err := messageParamFrom(message)
		if err != nil {
			return nil, err
		}

		// A message that renders to no block (ex: only non-image data) is rejected by the api.
		if len(param.Content) > 0 {
			params.Messages = append(params.Messages, param)
		}
	}

	if options.OutputFormat != nil {
		schema, err := schemaToMap(options.OutputFormat.Schema)
		if err != nil {
			return nil, err
		}

		params.OutputConfig.Format.Schema = schema
	}

	tools, err := p.toolParamsFrom(options.Tools)
	if err != nil {
		return nil, err
	}

	params.Tools = tools
	return params, nil
}

func messageParamFrom(message *underlying.Message) (anthropic.MessageParam, error) {
	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(message.Contents))

	for _, content := range message.Contents {
		block, ok, err := blockFrom(message.Role, content)
		if err != nil {
			return anthropic.MessageParam{}, err
		}

		if ok {
			blocks = append(blocks, block)
		}
	}

	switch message.Role {
	case underlying.RoleAssistant:
		return anthropic.NewAssistantMessage(blocks...), nil
	// Tool results are carried by user messages in the messages api.
	case underlying.RoleUser, underlying.RoleTool:
		return anthropic.NewUserMessage(blocks...), nil
	default:
		return anthropic.MessageParam{}, fmt.Errorf("claude: unsupported message role %q", message.Role)
	}
}

// blockFrom converts one content into a block; ok is false when the content renders to nothing.
func blockFrom(
	role underlying.Role,
	content underlying.MessageContent,
) (block anthropic.ContentBlockParamUnion, ok bool, err error) {
	switch c := content.(type) {
	case *underlying.TextContent:
		// The api rejects empty text blocks.
		if c.Text == "" {
			return block, false, nil
		}

		return anthropic.NewTextBlock(c.Text), true, nil

	case *underlying.SummaryContent:
		return anthropic.NewTextBlock(summaryHeader + c.Text), true, nil

	case *underlying.DataContent:
		if !c.MediaType.Image() {
			return block, false, nil
		}

		encoded := base64.StdEncoding.EncodeToString(c.Data)

		return anthropic.NewImageBlockBase64(string(c.MediaType), encoded), true, nil

	case *underlying.URIContent:
		if !c.MediaType.Image() {
			return block, false, nil
		}

		return imageBlockFromURI(c), true, nil

	case *underlying.ReasoningContent:
		if role != underlying.RoleAssistant {
			return block, false, nil
		}

		return reasoningBlockFrom(c)

	case *underlying.ToolCallContent:
		input := json.RawMessage(c.Arguments)
		if strings.TrimSpace(c.Arguments) == "" {
			input = json.RawMessage("{}")
		}

		return anthropic.NewToolUseBlock(c.ID, input, c.Name), true, nil

	case *underlying.ToolResultContent:
		return anthropic.NewToolResultBlock(c.ToolCallID, c.Result, c.IsError), true, nil

	default:
		return block, false, fmt.Errorf("claude: unsupported message content %q", content.Kind())
	}
}

func imageBlockFromURI(c *underlying.URIContent) anthropic.ContentBlockParamUnion {
	// A data url carries the payload inline, which the api only accepts as a base64 source.
	if header, data, found := strings.Cut(c.URI, ","); found && strings.HasPrefix(header, "data:") {
		return anthropic.NewImageBlockBase64(string(c.MediaType), data)
	}

	return anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: c.URI})
}

// reasoningBlockFrom echoes the thinking back. The signature (stored in EncryptedContent) is mandatory: a
// thinking block without it is rejected, so such a block is dropped.
func reasoningBlockFrom(c *underlying.ReasoningContent) (anthropic.ContentBlockParamUnion, bool, error) {
	if c.EncryptedContent == "" {
		return anthropic.ContentBlockParamUnion{}, false, nil
	}

	if c.ID == redactedThinkingID {
		return anthropic.NewRedactedThinkingBlock(c.EncryptedContent), true, nil
	}

	return anthropic.NewThinkingBlock(c.EncryptedContent, c.Summary), true, nil
}

func (p *Provider) toolParamsFrom(tools []tool.Tool) ([]anthropic.ToolUnionParam, error) {
	params := make([]anthropic.ToolUnionParam, 0, len(tools))

	for _, t := range tools {
		schemaTool, ok := t.(tool.SchemaTool)
		if !ok {
			continue
		}

		// The tool search is a native item of the OpenAI Responses API only.
		if schemaTool.Name() == underlying.ToolSearchToolName {
			p.logger.Warn("tool search is not supported, skipped")
			continue
		}

		inputSchema, err := toolInputSchemaFrom(schemaTool.InSchema())
		if err != nil {
			return nil, fmt.Errorf("claude: schema of tool %q: %w", schemaTool.Name(), err)
		}

		param := anthropic.ToolUnionParamOfTool(inputSchema, schemaTool.Name())
		param.OfTool.Description = anthropic.String(schemaTool.Description())
		params = append(params, param)
	}

	return params, nil
}

// toolInputSchemaFrom splits a json schema into the fields the SDK models explicitly (properties, required)
// and everything else (ex: $defs), which is passed through as extra fields.
func toolInputSchemaFrom(schema *jsonschema.Schema) (anthropic.ToolInputSchemaParam, error) {
	result := anthropic.ToolInputSchemaParam{}
	if schema == nil {
		return result, nil
	}

	raw, err := schemaToMap(schema)
	if err != nil {
		return result, err
	}

	if properties, ok := raw["properties"]; ok {
		result.Properties = properties
		delete(raw, "properties")
	}

	if required, ok := raw["required"].([]any); ok {
		for _, name := range required {
			if s, isString := name.(string); isString {
				result.Required = append(result.Required, s)
			}
		}
	}

	delete(raw, "required")
	delete(raw, "type")
	delete(raw, "$schema")

	if len(raw) > 0 {
		result.ExtraFields = raw
	}

	return result, nil
}

func schemaToMap(schema *jsonschema.Schema) (map[string]any, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return result, nil
}
