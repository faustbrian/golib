package golib

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"mime"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/cloudevents"
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
	if ctx == nil {
		return cloudevents.ErrContextRequired
	}
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

// RegistryJSONSchemaConfig configures an opt-in registry-backed validator.
// SchemaLookups is cloned during construction and is never read afterward.
type RegistryJSONSchemaConfig struct {
	Cache              *schemaregistry.ResolveCache
	SchemaLookups      map[string]schemaregistry.Lookup
	Adapter            *registryjsonschema.Adapter
	AvailabilityPolicy schemaregistry.AvailabilityPolicy
	Timeout            time.Duration
}

// RegistryJSONSchemaValidator performs registry resolution only when the
// caller explicitly passes it to cloudevents.ValidateSchema. Event receipt and
// decoding never invoke this adapter. Its private schema lookup snapshot is a
// static allowlist: an event-controlled dataschema URI cannot select a registry
// target that the caller did not configure.
type RegistryJSONSchemaValidator struct {
	cache              *schemaregistry.ResolveCache
	schemaLookups      map[string]schemaregistry.Lookup
	adapter            *registryjsonschema.Adapter
	availabilityPolicy schemaregistry.AvailabilityPolicy
	timeout            time.Duration
	configured         bool
}

// NewRegistryJSONSchemaValidator validates the required resolution policy and
// takes an immutable snapshot of the caller-owned dataschema mapping.
func NewRegistryJSONSchemaValidator(config RegistryJSONSchemaConfig) (RegistryJSONSchemaValidator, error) {
	if config.Cache == nil || len(config.SchemaLookups) == 0 || config.Adapter == nil ||
		!validAvailabilityPolicy(config.AvailabilityPolicy) || config.Timeout <= 0 {
		return RegistryJSONSchemaValidator{}, fmt.Errorf("%w: validator configuration", ErrSchemaMapping)
	}
	for uri, lookup := range config.SchemaLookups {
		if uri == "" || lookup.Kind() == "" {
			return RegistryJSONSchemaValidator{}, fmt.Errorf("%w: dataschema mapping", ErrSchemaMapping)
		}
	}
	return RegistryJSONSchemaValidator{
		cache:              config.Cache,
		schemaLookups:      maps.Clone(config.SchemaLookups),
		adapter:            config.Adapter,
		availabilityPolicy: config.AvailabilityPolicy,
		timeout:            config.Timeout,
		configured:         true,
	}, nil
}

// Validate implements cloudevents.SchemaValidator with an exact allowlisted
// lookup, bounded cached resolution, and bounded Golib JSON Schema validation.
func (validator RegistryJSONSchemaValidator) Validate(
	ctx context.Context,
	uri string,
	contentType string,
	data []byte,
) error {
	if ctx == nil {
		return cloudevents.ErrContextRequired
	}
	if !validator.configured {
		return ErrSchemaMapping
	}
	if !isJSONContentType(contentType) {
		return fmt.Errorf("%w: non-JSON content type", ErrSchemaMapping)
	}
	lookup, found := validator.schemaLookups[uri]
	if !found {
		return fmt.Errorf("%w: dataschema URI", ErrSchemaMapping)
	}
	resolveCtx, cancel := context.WithTimeout(ctx, validator.timeout)
	defer cancel()
	resolution, err := validator.cache.Resolve(resolveCtx, lookup, validator.availabilityPolicy)
	if err != nil {
		return err
	}
	resolved := resolution.Result
	if resolved.Schema.Definition().Format != schemaregistry.FormatJSONSchema {
		return fmt.Errorf("%w: registry format", ErrSchemaMapping)
	}
	var target any
	if err := validator.adapter.Decode(resolveCtx, resolved.Schema, data, &target); err != nil {
		if errors.Is(err, registryjsonschema.ErrPayloadInvalid) {
			return fmt.Errorf("%w: %w", ErrSchemaViolation, err)
		}
		return err
	}
	return nil
}

func validAvailabilityPolicy(policy schemaregistry.AvailabilityPolicy) bool {
	switch policy {
	case schemaregistry.FailClosed,
		schemaregistry.AllowStale,
		schemaregistry.CacheOnly,
		schemaregistry.ReturnUnavailable:
		return true
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
