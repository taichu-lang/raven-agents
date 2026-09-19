package functool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/taichu-lang/raven-agents/tool"
)

// Config represents the base metadata of a function tool.
type Config struct {
	Name        string
	Description string
}

type HandlerT[In, Out any] func(context.Context, In) (Out, error)

type funcTool struct {
	cfg          Config
	call         func(ctx context.Context, args string) (any, error)
	inputSchema  *jsonschema.Schema
	outputSchema *jsonschema.Schema
	wrapped      bool
}

func New[In, Out any](cfg Config, h HandlerT[In, Out]) (tool.FuncTool, error) {
	if h == nil {
		return nil, errors.New("handler of function tool is required")
	}

	inputSchema, wrapped, err := getInputFormat[In]()
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema of handler's input: %w", err)
	}

	outputSchema, err := tool.SchemaFor[Out]()
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema of handler's output: %w", err)
	}

	resolvedInput, err := inputSchema.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
	if err != nil {
		return nil, fmt.Errorf("schema of handler's input can not be resolved: %w", err)
	}

	resolvedOutput, err := outputSchema.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
	if err != nil {
		return nil, fmt.Errorf("schema of handler's output can not be resolved: %w", err)
	}

	t := &funcTool{
		cfg:          cfg,
		inputSchema:  inputSchema,
		outputSchema: outputSchema,
		wrapped:      wrapped,
	}
	t.call = func(ctx context.Context, args string) (any, error) {
		// Unmarshal args into a arbitrary type to validate it, ex: map[string]interface{}.
		var validating any
		if err := json.Unmarshal([]byte(args), &validating); err != nil {
			return nil, fmt.Errorf("failed to unmarshal args: %w", err)
		}

		if err := resolvedInput.Validate(validating); err != nil {
			return nil, fmt.Errorf("validate args against input schema: %w", err)
		}

		var in In
		if t.wrapped {
			var objected inObject[In]
			if err := json.Unmarshal([]byte(args), &objected); err != nil {
				return nil, fmt.Errorf("failed to unmarshal args into input type: %w", err)
			}

			in = objected.Arg
		} else {
			if err := json.Unmarshal([]byte(args), &in); err != nil {
				return nil, fmt.Errorf("failed to unmarshal args into input type: %w", err)
			}
		}

		out, err := h(ctx, in)
		if err != nil {
			return nil, err
		}

		if err := resolvedOutput.Validate(out); err != nil {
			return nil, fmt.Errorf("validate output against output schema: %w", err)
		}

		return out, nil
	}

	return t, nil
}

func (t *funcTool) Name() string {
	return t.cfg.Name
}

func (t *funcTool) Description() string {
	return t.cfg.Description
}

func (t *funcTool) InSchema() *jsonschema.Schema {
	return t.inputSchema
}

func (t *funcTool) OutSchema() *jsonschema.Schema {
	return t.outputSchema
}

func (t *funcTool) Call(ctx context.Context, args string) (any, error) {
	return t.call(ctx, args)
}

// According to llm spec, the input schema should be an object. inObject is used
// to wrap the input if its schema is not an object.
type inObject[T any] struct {
	Arg T
}

func getInputFormat[T any]() (schema *jsonschema.Schema, wrapped bool, err error) {
	t := reflect.TypeFor[T]()
	if t == reflect.TypeFor[any]() {
		return nil, false, errors.New("input type can not be 'any'")
	}

	elem := t
	for elem.Kind() == reflect.Pointer {
		elem = elem.Elem()
	}

	if elem.Kind() == reflect.Struct {
		schema, err = tool.SchemaFor[T]()
		if err != nil {
			return nil, false, err
		}
		return schema, false, nil
	}

	schema, err = tool.SchemaFor[inObject[T]]()
	if err != nil {
		return nil, false, err
	}
	return schema, true, nil
}
