package mcp

import "time"

const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
)

const (
	defaultConnectTimeout = 30 * time.Second
	defaultCallTimeout    = 2 * time.Minute
	defaultIdleTimeout    = 10 * time.Minute
)

// ServerConfig is the resolved connection config of one MCP server: every secret placeholder is already
// rendered, so it must never be logged.
type ServerConfig struct {
	// Name is the user visible server name, lower case letters, digits and single hyphens.
	Name      string
	Transport string

	// Command, Args and Env configure a stdio server.
	Command string
	Args    []string
	Env     map[string]string

	// URL and Headers configure an http server.
	URL     string
	Headers map[string]string

	// ConnectTimeout bounds one connection attempt, defaults to 30 seconds.
	ConnectTimeout time.Duration
}
