package configs

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

type Cost struct {
	Input      float32 `json:"input"       yaml:"input"`
	Output     float32 `json:"output"      yaml:"output"`
	CacheRead  float32 `json:"cache_read"  yaml:"cache_read"`
	CacheWrite float32 `json:"cache_write" yaml:"cache_write"`
}

type ModelCatalog struct {
	underlying.Catalog
	Cost
}

type ModelCatalogSlice = []ModelCatalog

func LoadModelCatalog() (ModelCatalogSlice, error) {
	catalog := make(ModelCatalogSlice, 0, 1)
	data, err := os.ReadFile("configs/catalog.json")
	if err != nil {
		return nil, fmt.Errorf("catalog file not found, %w", err)
	}

	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("invalid catalog, %w", err)
	}

	return catalog, nil
}
