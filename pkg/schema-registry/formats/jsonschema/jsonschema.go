// Package jsonschema integrates the provider-neutral registry contracts with
// golib's bounded JSON Schema compiler and validator.
package jsonschema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"github.com/deszhou/jcs"
	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

// ErrPayloadInvalid marks a value that does not satisfy the compiled JSON Schema.
var ErrPayloadInvalid = errors.New("schema registry json schema: invalid payload")

// Dialect is the explicit JSON Schema dialect selected for compilation.
type Dialect = jsonschema.Dialect

const (
	// Draft3 selects JSON Schema draft 3.
	Draft3 = jsonschema.Draft3
	// Draft4 selects JSON Schema draft 4.
	Draft4 = jsonschema.Draft4
	// Draft6 selects JSON Schema draft 6.
	Draft6 = jsonschema.Draft6
	// Draft7 selects JSON Schema draft 7.
	Draft7 = jsonschema.Draft7
	// Draft201909 selects JSON Schema draft 2019-09.
	Draft201909 = jsonschema.Draft201909
	// Draft202012 selects JSON Schema draft 2020-12.
	Draft202012 = jsonschema.Draft202012
)

// Config bounds schema compilation and payload validation.
type Config struct {
	Dialect             Dialect
	Resources           map[string][]byte
	MaxSchemaBytes      int
	MaxTotalSchemaBytes int
	MaxPayloadBytes     int
	MaxResources        int
}

// Adapter validates JSON Schema documents and JSON payloads without implicit
// resource loading.
type Adapter struct {
	compiler        *jsonschema.Compiler
	maxSchemaBytes  int
	maxPayloadBytes int
}

// New constructs an isolated adapter using Draft 2020-12 and no remote loader.
func New(config Config) (*Adapter, error) {
	if config.Dialect == "" {
		config.Dialect = Draft202012
	}
	if config.MaxSchemaBytes <= 0 || config.MaxTotalSchemaBytes <= 0 ||
		config.MaxPayloadBytes <= 0 || config.MaxResources <= 0 || len(config.Resources) > config.MaxResources {
		return nil, fmt.Errorf("json schema adapter limits must be positive")
	}
	resources := maps.Clone(config.Resources)
	totalBytes := 0
	for identifier, resource := range resources {
		if identifier == "" || len(resource) == 0 || len(resource) > config.MaxSchemaBytes {
			return nil, fmt.Errorf("invalid JSON Schema resource")
		}
		totalBytes += len(resource)
		if totalBytes > config.MaxTotalSchemaBytes {
			return nil, fmt.Errorf("JSON Schema resources exceed total byte limit")
		}
		resources[identifier] = append([]byte(nil), resource...)
	}
	limits := jsonschema.DefaultLimits()
	limits.MaxInputBytes = max(config.MaxSchemaBytes, config.MaxPayloadBytes)
	limits.MaxTotalSchemaBytes = config.MaxTotalSchemaBytes
	limits.MaxSchemaResources = config.MaxResources + 1
	options := []jsonschema.Option{jsonschema.WithDialect(config.Dialect), jsonschema.WithLimits(limits)}
	if len(resources) != 0 {
		options = append(options, jsonschema.WithResourceLoader(jsonschema.ResourceLoaderFunc(
			func(ctx context.Context, identifier string) ([]byte, error) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				resource, found := resources[identifier]
				if !found {
					return nil, jsonschema.ErrResourceNotFound
				}
				return append([]byte(nil), resource...), nil
			},
		)))
	}
	compiler, err := jsonschema.NewCompiler(options...)
	if err != nil {
		return nil, fmt.Errorf("construct JSON Schema compiler: %w", err)
	}
	return &Adapter{
		compiler:        compiler,
		maxSchemaBytes:  config.MaxSchemaBytes,
		maxPayloadBytes: config.MaxPayloadBytes,
	}, nil
}

// Canonicalize validates with golib/json-schema and applies RFC 8785 JCS. JCS
// deliberately restricts portable fingerprints to I-JSON number semantics.
func (adapter *Adapter) Canonicalize(
	ctx context.Context,
	definition schemaregistry.Definition,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if definition.Format != schemaregistry.FormatJSONSchema {
		return nil, fmt.Errorf("unsupported format %q", definition.Format)
	}
	if len(definition.Content) > adapter.maxSchemaBytes {
		return nil, fmt.Errorf("schema exceeds %d bytes", adapter.maxSchemaBytes)
	}
	if _, err := adapter.compiler.Compile(ctx, definition.Content); err != nil {
		return nil, fmt.Errorf("compile JSON Schema: %w", err)
	}
	canonical, err := jcs.Transform(definition.Content)
	if err != nil {
		return nil, fmt.Errorf("canonicalize JSON Schema: %w", err)
	}
	return canonical, nil
}

// Encode marshals and validates one bounded JSON payload.
func (adapter *Adapter) Encode(ctx context.Context, schema schemaregistry.Schema, value any) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal JSON payload: %w", err)
	}
	if len(payload) > adapter.maxPayloadBytes {
		return nil, fmt.Errorf("%w: payload exceeds %d bytes", schemaregistry.ErrLimitExceeded, adapter.maxPayloadBytes)
	}
	if err := adapter.validate(ctx, schema, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// Decode validates before unmarshaling into caller-owned target.
func (adapter *Adapter) Decode(
	ctx context.Context,
	schema schemaregistry.Schema,
	payload []byte,
	target any,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(payload) > adapter.maxPayloadBytes {
		return fmt.Errorf("%w: payload exceeds %d bytes", schemaregistry.ErrLimitExceeded, adapter.maxPayloadBytes)
	}
	if err := adapter.validate(ctx, schema, payload); err != nil {
		return err
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("unmarshal JSON payload: %w", err)
	}
	return nil
}

func (adapter *Adapter) validate(ctx context.Context, schema schemaregistry.Schema, payload []byte) error {
	definition := schema.Definition()
	if definition.Format != schemaregistry.FormatJSONSchema || len(definition.Content) > adapter.maxSchemaBytes {
		return fmt.Errorf("%w: schema format or size", schemaregistry.ErrInvalidSchema)
	}
	compiled, err := adapter.compiler.Compile(ctx, definition.Content)
	if err != nil {
		return fmt.Errorf("%w: compile schema: %v", schemaregistry.ErrInvalidSchema, err)
	}
	result, err := compiled.Validate(ctx, payload)
	if err != nil {
		return fmt.Errorf("%w: validate payload: %v", ErrPayloadInvalid, err)
	}
	if !result.Valid {
		return ErrPayloadInvalid
	}
	return nil
}
