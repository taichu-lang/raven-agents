package configs

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

type ModelCatalogSlice = []*underlying.Catalog

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
