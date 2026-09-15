package tool

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
)

// Tool declares the abstract definition of a tool that can be made available to an agent.
type Tool interface {
	// Name returns the tool name.
	Name() string

	// Description returns the tool description.
	Description() string
}

type SchemaTool interface {
	Tool
	InSchema() *jsonschema.Schema
	OutSchema() *jsonschema.Schema
}

type FuncTool interface {
	SchemaTool

	// Call is the entrypoint of the function tool with raw json string.
	Call(ctx context.Context, args string) (any, error)
}

// SchemaFor reflects the JSON schema for T.
func SchemaFor[T any]() (*jsonschema.Schema, error) {
	return jsonschema.For[T](nil)
}
