package openapi

import (
	"context"
	"fmt"
	"io"

	representation "github.com/faustbrian/golib/pkg/openapi/parse"
)

// ParseJSON performs bounded strict JSON parsing and selects the exact
// version-specific immutable document model.
func ParseJSON(
	ctx context.Context,
	reader io.Reader,
	limits representation.Limits,
) (Document, error) {
	value, err := representation.JSON(ctx, reader, limits)
	if err != nil {
		return nil, fmt.Errorf("openapi: parse JSON: %w", err)
	}
	return Decode(value)
}

// ParseYAML performs bounded strict JSON-equivalent YAML parsing and selects
// the exact version-specific immutable document model.
func ParseYAML(
	ctx context.Context,
	reader io.Reader,
	limits representation.Limits,
) (Document, error) {
	value, err := representation.YAML(ctx, reader, limits)
	if err != nil {
		return nil, fmt.Errorf("openapi: parse YAML: %w", err)
	}
	return Decode(value)
}
