package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/util"
)

type Provider struct {
	client  *http.Client
	options *llm.ProviderOptions
	logger  *slog.Logger
}

func NewOpenAIProvider(options *llm.ProviderOptions) llm.Provider {
	return &Provider{
		client:  util.NewStreamClient(),
		options: options,
		logger:  slog.Default().With("llm", "openai"),
	}
}

func (p *Provider) Gen(
	ctx context.Context,
	model string,
	messages []*llm.Message,
	options ...llm.WithGenOption,
) agent.ResponseStream {
	return func(yield func(*llm.ResponseChunk, error) bool) {
		opts := llm.ApplyGenOptions(options)
		params := buildResponsesMessage(model, messages, opts)
		body, _ := json.Marshal(params)

		p.logger.Debug("request body of generation", "model", model, "options", opts, "body", string(body))

		url, err := util.UriAppend(p.options.BaseURL, p.options.Endpoint)
		if err != nil {
			p.logger.Error("invalid endpoint", "err", err)
			yield(nil, err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			p.logger.Error("failed to create generation request", "err", err)
			yield(nil, err)
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.options.ApiKey)
		if opts.Stream {
			streaming := p.getStreaming(req)
			defer func() {
				streaming.Close()
			}()

			for streaming.Next() {
				chunk, err := streamEventToResponse(streaming.Current())
				if err != nil {
					p.logger.Error("failed to convert stream event to response chunk", "err", err)
					yield(nil, err)
					return
				}

				// Discard some stream events.
				if chunk == nil {
					continue
				}

				if !yield(chunk, nil) {
					return
				}
			}

			if streaming.Err() != nil {
				yield(nil, streaming.Err())
			}
		} else {
			chunk, err := p.get(req)
			if err != nil {
				p.logger.Error("failed to get generation response in non-stream mode", "err", err)
				yield(nil, err)
			} else {
				if !yield(chunk, nil) {
					return
				}
			}
		}
	}
}

func (p *Provider) get(req *http.Request) (*llm.ResponseChunk, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		p.logger.Error(
			"response of generation request is not ok",
			"status",
			resp.StatusCode,
			"err",
			string(body),
		)
		return nil, errors.New("unexpected status code")
	}

	var response responses.Response
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	chunk := &llm.ResponseChunk{
		Role: llm.RoleAssistant,
	}

	for _, out := range response.Output {
		switch out := out.AsAny().(type) {
		case responses.ResponseOutputMessage:
			chunk.Contents = responsesToMessageContents(out.Content, chunk.Contents)
		}
	}

	if usage := toUsageContent(response.Usage); usage != nil {
		chunk.Contents = append(chunk.Contents, usage)
	}

	return chunk, nil
}

func (p *Provider) getStreaming(
	req *http.Request,
) *ssestream.Stream[responses.ResponseStreamEventUnion] {
	req.Header.Set("Accept", "text/event-stream")
	resp, err := p.client.Do(req)
	return ssestream.NewStream[responses.ResponseStreamEventUnion](ssestream.NewDecoder(resp), err)
}

func buildResponsesMessage(
	model string,
	messages []*llm.Message,
	options *llm.GenOptions,
) *ResponsesParams {
	params := &ResponsesParams{
		Model:        model,
		Stream:       options.Stream,
		Instructions: options.Instructions,
		Input:        make(ResponseInput, 0, len(messages)),
	}

	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleUser:
			contents := make([]ResponseInputContent, 0, len(msg.Contents))
			for _, mc := range msg.Contents {
				contents = buildInputContent(mc, contents)
			}
			params.Input = append(params.Input, ResponseInputItem{
				Role:    RoleUser,
				Content: contents,
			})

		case llm.RoleSystem:
			// Use instructions option ?
		}
	}

	return params
}

func buildInputContent(content llm.MessageContent, inputs []ResponseInputContent) []ResponseInputContent {
	switch c := content.(type) {
	case *llm.TextContent:
		return append(inputs, ResponseInputContent{
			Type: InputTypeText,
			ResponseInputText: &ResponseInputText{
				Text: c.Raw(),
			},
		})

	default:
		panic("unsupported message content")
	}
}
