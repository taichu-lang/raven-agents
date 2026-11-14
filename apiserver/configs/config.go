package configs

import (
	"errors"
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
	"github.com/taichu-lang/raven-agents/pkg/database"
)

type Prompt struct {
	TitleGen string `yaml:"title_gen"`
}

type Config struct {
	Instructions string                `yaml:"instructions"`
	DefaultModel string                `yaml:"default_model"`
	Store        *database.StoreConfig `yaml:"store"`
	Providers    []ModelProvider       `yaml:"providers"`

	// TODO(Leo): resolved from runtime?
	AppDir string `yaml:"app_dir"`
	Prompt Prompt `yaml:"prompt"`
}

func LoadConfig() (*Config, error) {
	c := &Config{}
	data, err := os.ReadFile("configs/config.yml")
	if err != nil {
		return nil, fmt.Errorf("config file not found, %w", err)
	}

	expanded := os.ExpandEnv(string(data))
	if err := yaml.Unmarshal([]byte(expanded), c); err != nil {
		return nil, fmt.Errorf("invalid yaml, %w", err)
	}

	if err := c.Validate(); err != nil {
		return nil, err
	}

	if c.AppDir == "" {
		c.AppDir, _ = os.UserHomeDir()
	}

	return c, nil
}

func (c *Config) Validate() error {
	if c.Store == nil {
		return errors.New("store is required")
	}

	if err := c.Store.Validate(); err != nil {
		return fmt.Errorf("invalid store config, %w", err)
	}

	if c.Prompt.TitleGen == "" {
		return errors.New("prompt.title_gen is required")
	}

	return nil
}
