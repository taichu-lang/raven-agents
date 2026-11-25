package configs

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
	"github.com/taichu-lang/raven-agents/pkg/database"
	"github.com/taichu-lang/raven-agents/pkg/webcrawler"
)

type Prompt struct {
	TitleGen string `yaml:"title_gen"`
}

// MCP configures the MCP integration. The servers themselves live in the database registry.
type MCP struct {
	// CredentialKey is the base64 encoded 32 byte key sealing the per-user credentials of the MCP servers. It
	// may be empty: servers declaring credentials are then unavailable, the others are not affected.
	CredentialKey string `yaml:"credential_key"`
}

type Config struct {
	Instructions string                `yaml:"instructions"`
	DefaultModel string                `yaml:"default_model"`
	Store        *database.StoreConfig `yaml:"store"`
	Providers    []ModelProvider       `yaml:"providers"`
	WebCrawler   *webcrawler.Config    `yaml:"webcrawler,omitempty"`
	MCP          MCP                   `yaml:"mcp"`

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

	if c.MCP.CredentialKey != "" {
		key, err := base64.StdEncoding.DecodeString(c.MCP.CredentialKey)
		if err != nil || len(key) != 32 {
			return errors.New("mcp.credential_key must be a base64 encoded 32 byte key")
		}
	}

	return nil
}
