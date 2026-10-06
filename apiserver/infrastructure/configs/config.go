package configs

import (
	"errors"
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
	"github.com/taichu-lang/raven-agents/internal/database"
)

type Config struct {
	ApiKey       string                `yaml:"api_key"`
	Endpoint     string                `yaml:"endpoint"`
	Instructions string                `yaml:"instructions"`
	Store        *database.StoreConfig `yaml:"store"`
}

func (c *Config) Load() error {
	data, err := os.ReadFile("configs/config.yml")
	if err != nil {
		return fmt.Errorf("config file not found, %w", err)
	}

	if err := yaml.Unmarshal(data, c); err != nil {
		return fmt.Errorf("invalid yaml, %w", err)
	}

	if c.Store == nil {
		return errors.New("store is required")
	}

	if err := c.Store.Validate(); err != nil {
		return fmt.Errorf("invalid store config, %w", err)
	}

	return nil
}
