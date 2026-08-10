// Package avro provides Apache Avro parsing canonical form through goavro.
package avro

import (
	"context"
	"fmt"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	"github.com/linkedin/goavro/v2"
)

// Canonicalizer validates and canonicalizes bounded standalone Avro schemas.
type Canonicalizer struct{ maxSchemaBytes int }

// New constructs an Avro canonicalizer. External named-schema references must
// be resolved and inlined explicitly before this focused adapter is called.
func New(maxSchemaBytes int) *Canonicalizer {
	return &Canonicalizer{maxSchemaBytes: maxSchemaBytes}
}

// Canonicalize returns Avro parsing canonical form.
func (canonicalizer *Canonicalizer) Canonicalize(
	ctx context.Context,
	definition schemaregistry.Definition,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if canonicalizer == nil || canonicalizer.maxSchemaBytes <= 0 {
		return nil, fmt.Errorf("invalid Avro canonicalizer")
	}
	if definition.Format != schemaregistry.FormatAvro {
		return nil, fmt.Errorf("unsupported format %q", definition.Format)
	}
	if len(definition.Content) > canonicalizer.maxSchemaBytes {
		return nil, fmt.Errorf("schema exceeds %d bytes", canonicalizer.maxSchemaBytes)
	}
	codec, err := goavro.NewCodec(string(definition.Content))
	if err != nil {
		return nil, fmt.Errorf("parse Avro schema: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []byte(codec.CanonicalSchema()), nil
}
