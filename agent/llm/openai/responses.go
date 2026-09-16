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
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/util"
)

type Provider struct {
	client  *http.Client
	options *llm.ProviderOptions
	logger  *slog.Logger
}

func NewProvider(options *llm.ProviderOptions) llm.Provider {
	return &Provider{
		client:  util.NewStreamClient(),
		options: options,
		logger:  slog.Default().With("llm", "openai"),
	}
}

func (p *Provider) Gen(
	ctx context.Context,
	messages []*llm.Message,
	options ...llm.WithGenOption,
) llm.ResponseStream {
	return func(yield func(*llm.ResponseChunk, error) bool) {
		opts := llm.ApplyGenOptions(options)
		params := p.newResponsesParams(p.options.Model, messages, opts)
		body, _ := json.Marshal(params)

		p.logger.Debug(
			"request body of generation",
			"model",
			p.options.Model,
			"options",
			opts,
			"body",
			string(body),
		)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.options.Endpoint, bytes.NewReader(body))
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
			response, err := p.get(req)
			if err != nil {
				p.logger.Error("failed to get generation response in non-stream mode", "err", err)
				yield(nil, err)
				return
			}

			for _, out := range response.Output {
				switch out := out.AsAny().(type) {
				case responses.ResponseOutputMessage:
					chunk := &llm.ResponseChunk{
						Type: llm.ResponseChunkTypeFinal,
						Role: llm.RoleAssistant,
					}
					chunk.Contents = responsesToMessageContents(out.Content, chunk.Contents)
					yield(chunk, nil)
				}
			}

			// TODO(Leo): event order, can usage event before final event for all providers ?
			if usage := toUsageContent(response.Usage); usage != nil {
				yield(&llm.ResponseChunk{
					Type:     llm.ResponseChunkTypeUsage,
					Role:     llm.RoleAssistant,
					Contents: llm.MessageContents{usage},
				}, nil)
			}

		}
	}
}

func (p *Provider) get(req *http.Request) (*responses.Response, error) {
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

	return &response, nil
}

func (p *Provider) getStreaming(
	req *http.Request,
) *ssestream.Stream[responses.ResponseStreamEventUnion] {
	req.Header.Set("Accept", "text/event-stream")
	resp, err := p.client.Do(req)
	return ssestream.NewStream[responses.ResponseStreamEventUnion](ssestream.NewDecoder(resp), err)
}

func (p *Provider) newResponsesParams(
	model string,
	messages []*llm.Message,
	options *llm.GenOptions,
) *ResponsesParams {
	params := &ResponsesParams{
		Model:        model,
		Stream:       options.Stream,
		Instructions: p.options.Instructions,
		Input:        make(ResponsesInput, 0, len(messages)),
	}

	for _, msg := range messages {
		params.Input = append(params.Input, inputFromMessage(msg))
	}

	return params
}
