package configs

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

type Config struct {
	ApiKey       string `yaml:"api_key"`
	Endpoint     string `yaml:"endpoint"`
	Instructions string `yaml:"instructions"`
}

func (c *Config) Load() error {
	data, err := os.ReadFile("configs/config.yml")
	if err != nil {
		return fmt.Errorf("config file not found, %w", err)
	}

	if err := yaml.Unmarshal(data, c); err != nil {
		return fmt.Errorf("invalid yaml, %w", err)
	}

	return nil
}
