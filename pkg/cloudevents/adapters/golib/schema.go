package golib

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"reflect"
	"strings"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryjsonschema "github.com/faustbrian/golib/pkg/schema-registry/formats/jsonschema"
)

var (
	// ErrSchemaViolation reports a valid JSON instance that does not satisfy the
	// explicitly selected schema.
	ErrSchemaViolation = errors.New("cloudevents golib adapter: schema violation")
	// ErrSchemaMapping reports an absent, conflicting, or unsupported explicit
	// schema selection.
	ErrSchemaMapping = errors.New("cloudevents golib adapter: schema mapping")
)

// JSONSchemaValidator adapts one caller-compiled Golib JSON Schema to the
// core CloudEvents opt-in SchemaValidator boundary. It performs no lookup.
type JSONSchemaValidator struct {
	URI    string
	Schema *jsonschema.Schema
}

// Validate implements cloudevents.SchemaValidator.
func (validator JSONSchemaValidator) Validate(
	ctx context.Context,
	uri string,
	contentType string,
	data []byte,
) error {
	if validator.URI == "" || validator.Schema == nil || uri != validator.URI {
		return ErrSchemaMapping
	}
	if !isJSONContentType(contentType) {
		return fmt.Errorf("%w: non-JSON content type", ErrSchemaMapping)
	}
	result, err := validator.Schema.Validate(ctx, data)
	if err != nil {
		return err
	}
	if !result.Valid {
		return ErrSchemaViolation
	}
	return nil
}

// LookupSchema maps an absolute CloudEvents dataschema URI to one exact
// provider-neutral registry lookup. The application owns this mapping.
type LookupSchema func(uri string) (schemaregistry.Lookup, error)

// RegistryJSONSchemaValidator performs registry resolution only when the
// caller explicitly passes it to cloudevents.ValidateSchema. Event receipt and
// decoding never invoke this adapter.
type RegistryJSONSchemaValidator struct {
	Resolver schemaregistry.Resolver
	Adapter  *registryjsonschema.Adapter
	Lookup   LookupSchema
}

// Validate implements cloudevents.SchemaValidator with explicit lookup and
// bounded Golib JSON Schema validation.
func (validator RegistryJSONSchemaValidator) Validate(
	ctx context.Context,
	uri string,
	contentType string,
	data []byte,
) error {
	if interfaceIsNil(validator.Resolver) || validator.Adapter == nil || validator.Lookup == nil {
		return ErrSchemaMapping
	}
	if !isJSONContentType(contentType) {
		return fmt.Errorf("%w: non-JSON content type", ErrSchemaMapping)
	}
	lookup, err := validator.Lookup(uri)
	if err != nil {
		return fmt.Errorf("%w: URI lookup: %w", ErrSchemaMapping, err)
	}
	resolved, err := validator.Resolver.Resolve(ctx, lookup)
	if err != nil {
		return err
	}
	if resolved.Schema.Definition().Format != schemaregistry.FormatJSONSchema {
		return fmt.Errorf("%w: registry format", ErrSchemaMapping)
	}
	var target any
	if err := validator.Adapter.Decode(ctx, resolved.Schema, data, &target); err != nil {
		if errors.Is(err, registryjsonschema.ErrPayloadInvalid) {
			return fmt.Errorf("%w: %w", ErrSchemaViolation, err)
		}
		return err
	}
	return nil
}

func interfaceIsNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}
