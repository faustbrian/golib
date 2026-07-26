package jsonschema

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

// ErrInvalidDialectAdvisory reports an invalid Schema Object or resource use.
var ErrInvalidDialectAdvisory = errors.New("invalid dialect advisory")

// SchemaResourceUse identifies the document context of a Schema Object.
type SchemaResourceUse uint8

const (
	// CompleteOpenAPIDocumentSchema is a Schema Object embedded in a complete
	// OpenAPI document, where jsonSchemaDialect supplies the default dialect.
	CompleteOpenAPIDocumentSchema SchemaResourceUse = iota
	// StandaloneSchema is a Schema Object used outside an OpenAPI document.
	StandaloneSchema
	// IncompleteOpenAPIDocumentSchema is a Schema Object extracted from an
	// incomplete OpenAPI document or document fragment.
	IncompleteOpenAPIDocumentSchema
)

// NeedsExplicitDialect reports whether a Schema Object should be accompanied
// by an explicit dialect declaration. Boolean schemas cannot contain $schema,
// so true means their caller must carry the dialect out of band.
func NeedsExplicitDialect(
	schema jsonvalue.Value,
	use SchemaResourceUse,
) (bool, error) {
	if use > IncompleteOpenAPIDocumentSchema {
		return false, fmt.Errorf("%w: unknown schema resource use", ErrInvalidDialectAdvisory)
	}
	if schema.Kind() != jsonvalue.ObjectKind &&
		schema.Kind() != jsonvalue.BooleanKind {
		return false, fmt.Errorf("%w: schema must be an object or boolean", ErrInvalidDialectAdvisory)
	}
	if use == CompleteOpenAPIDocumentSchema {
		return false, nil
	}
	if schema.Kind() == jsonvalue.BooleanKind {
		return true, nil
	}

	declaration, exists := schema.Lookup("$schema")
	if !exists {
		return true, nil
	}
	identifier, text := declaration.Text()
	if !text || identifier == "" {
		return false, fmt.Errorf("%w: $schema must be a non-empty string", ErrInvalidDialectAdvisory)
	}
	parsed, err := url.Parse(identifier)
	if err != nil || !parsed.IsAbs() || parsed.Fragment != "" {
		return false, fmt.Errorf("%w: $schema must be an absolute URI without a fragment", ErrInvalidDialectAdvisory)
	}

	return false, nil
}
