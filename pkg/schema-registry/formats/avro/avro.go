// Package avro provides Apache Avro parsing canonical form through goavro.
package avro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	"github.com/linkedin/goavro/v2"
)

const maxNestingDepth = 256

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
	if err := checkNesting(ctx, definition.Content); err != nil {
		return nil, err
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

func checkNesting(ctx context.Context, content []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	depth := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("parse Avro schema JSON: %w", err)
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			continue
		}
		switch delimiter {
		case '{', '[':
			depth++
			if depth > maxNestingDepth {
				return fmt.Errorf("avro schema nesting exceeds %d", maxNestingDepth)
			}
		case '}', ']':
			depth--
		}
	}
}
