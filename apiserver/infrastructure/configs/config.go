package configs

import (
	"errors"
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
	"github.com/taichu-lang/raven-agents/internal/database"
)

type Prompt struct {
	TitleGen string `yaml:"title_gen"`
}

type Config struct {
	ApiKey       string                `yaml:"api_key"`
	Endpoint     string                `yaml:"endpoint"`
	Instructions string                `yaml:"instructions"`
	DefaultModel string                `yaml:"default_model"`
	Store        *database.StoreConfig `yaml:"store"`

	// TODO(Leo): resolved from runtime?
	AppDir string `yaml:"app_dir"`
	Prompt Prompt `yaml:"prompt"`
}

func (c *Config) Load() error {
	data, err := os.ReadFile("configs/config.yml")
	if err != nil {
		return fmt.Errorf("config file not found, %w", err)
	}

	expanded := os.ExpandEnv(string(data))
	if err := yaml.Unmarshal([]byte(expanded), c); err != nil {
		return fmt.Errorf("invalid yaml, %w", err)
	}

	if c.Store == nil {
		return errors.New("store is required")
	}

	if err := c.Store.Validate(); err != nil {
		return fmt.Errorf("invalid store config, %w", err)
	}

	if c.AppDir == "" {
		c.AppDir, _ = os.UserHomeDir()
	}

	if c.Prompt.TitleGen == "" {
		return errors.New("prompt.title_gen is required")
	}

	return nil
}
