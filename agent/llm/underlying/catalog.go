package underlying

type ReasoningOption struct {
	Type   string   `json:"type"   yaml:"type"`
	Values []string `json:"values" yaml:"values"`
}

type Modalities struct {
	Input  []string `json:"input"  yaml:"input"`
	Output []string `json:"output" yaml:"output"`
}

type Limit struct {
	Context uint64 `json:"context"        yaml:"context"`
	Input   uint64 `json:"input,omitzero" yaml:"input,omitempty"`
	Output  uint64 `json:"output"         yaml:"output"`
}

type Catalog struct {
	ID               string            `json:"id"                         yaml:"id"`
	Name             string            `json:"name"                       yaml:"name"`
	Description      string            `json:"description"                yaml:"description"`
	Family           string            `json:"family"                     yaml:"family"`
	Reasoning        bool              `json:"reasoning"                  yaml:"reasoning"`
	ReasoningOptions []ReasoningOption `json:"reasoning_options,omitzero" yaml:"reasoning_options,omitempty"`
	ToolCall         bool              `json:"tool_call,omitzero"         yaml:"tool_call,omitempty"`
	StructuredOutput bool              `json:"structured_output,omitzero" yaml:"structured_output,omitempty"`
	Modalities       Modalities        `json:"modalities"                 yaml:"modalities"`
	Limit            Limit             `json:"limit"                      yaml:"limit"`
	Protocols        []Protocol        `json:"protocols"                  yaml:"protocols"`
	Provider         string            `json:"provider,omitempty"         yaml:"provider,omitempty"`
	Publisher        string            `json:"publisher,omitempty"        yaml:"publisher,omitempty"`
}
