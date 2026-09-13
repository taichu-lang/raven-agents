package agent

import (
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/openai"
)

func NewProvider(options *llm.ProviderOptions) llm.Provider {
	if options.Protocol == "" {
		options.Protocol = llm.ProtocolOpenAI
	}

	if options.ApiKey == "" {
		panic("api key is required")
	}

	if options.Endpoint == "" {
		panic("endpoint is required")
	}

	switch options.Protocol {
	case llm.ProtocolOpenAI:
		return openai.NewProvider(options)

	default:
		return nil
	}
}
