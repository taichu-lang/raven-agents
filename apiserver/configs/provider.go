package configs

type ModelTransform struct {
	// Model is the standard model id.
	Model string `json:"model" yaml:"model"`

	// Alias is the model id used in the provider.
	Alias string `json:"alias" yaml:"alias"`
}

type ModelProvider struct {
	Name       string           `json:"name"                yaml:"name"`
	ApiKey     string           `json:"api_key"             yaml:"api_key"`
	ChatURL    string           `json:"chat_url"            yaml:"chat_url"`
	Transforms []ModelTransform `json:"transforms,omitzero" yaml:"transforms,omitempty"`
}

func (mp *ModelProvider) GetAlias(model string) string {
	for _, t := range mp.Transforms {
		if t.Model == model {
			return t.Alias
		}
	}

	return ""
}
