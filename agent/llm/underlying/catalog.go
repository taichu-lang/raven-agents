package underlying

type ReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type Modalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type Limit struct {
	Context uint64 `json:"context"`
	Input   uint64 `json:"input,omitzero"`
	Output  uint64 `json:"output"`
}

type Catalog struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Family           string            `json:"family"`
	Reasoning        bool              `json:"reasoning"`
	ReasoningOptions []ReasoningOption `json:"reasoning_options,omitzero"`
	ToolCall         bool              `json:"tool_call,omitzero"`
	StructuredOutput bool              `json:"structured_output,omitzero"`
	Modalities       Modalities        `json:"modalities"`
	Limit            Limit             `json:"limit"`
	Protocols        []Protocol        `json:"protocols"`
	Provider         string            `json:"provider"`
}
