package openapi

import (
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/oas30"
	"github.com/faustbrian/golib/pkg/openapi/oas31"
	"github.com/faustbrian/golib/pkg/openapi/oas32"
	"github.com/faustbrian/golib/pkg/openapi/swagger20"
)

// ErrInvalidDocument reports a value that cannot unambiguously select one
// supported OpenAPI or Swagger document model.
var ErrInvalidDocument = errors.New("invalid OpenAPI document")

// Document is the shared immutable contract implemented by every versioned
// root model.
type Document interface {
	Raw() jsonvalue.Value
	SpecificationVersion() Version
}

// Decode selects the exact version-specific immutable document model. It
// preserves nested type errors and unknown fields for phased validation, but
// requires an unambiguous supported root version marker.
func Decode(raw jsonvalue.Value) (Document, error) {
	if raw.Kind() != jsonvalue.ObjectKind {
		return nil, fmt.Errorf("%w: root is not an object", ErrInvalidDocument)
	}
	openAPIValue, hasOpenAPI := raw.Lookup("openapi")
	swaggerValue, hasSwagger := raw.Lookup("swagger")
	if hasOpenAPI == hasSwagger {
		return nil, fmt.Errorf("%w: exactly one version marker is required", ErrInvalidDocument)
	}

	selected := openAPIValue
	if hasSwagger {
		selected = swaggerValue
	}
	versionText, ok := selected.Text()
	if !ok {
		return nil, fmt.Errorf("%w: version marker is not a string", ErrInvalidDocument)
	}
	version, err := ParseVersion(versionText)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}

	// ParseVersion only returns dialects registered below.
	return documentDecoders[version.Dialect()](raw)
}

var documentDecoders = map[Dialect]func(jsonvalue.Value) (Document, error){
	DialectOAS32: func(raw jsonvalue.Value) (Document, error) {
		document, err := oas32.Decode(raw)
		return wrapDocumentResult(document, err)
	},
	DialectOAS31: func(raw jsonvalue.Value) (Document, error) {
		document, err := oas31.Decode(raw)
		return wrapDocumentResult(document, err)
	},
	DialectOAS30: func(raw jsonvalue.Value) (Document, error) {
		document, err := oas30.Decode(raw)
		return wrapDocumentResult(document, err)
	},
	DialectSwagger20: func(raw jsonvalue.Value) (Document, error) {
		document, err := swagger20.Decode(raw)
		return wrapDocumentResult(document, err)
	},
}

func wrapDocumentResult[T Document](document T, err error) (Document, error) {
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}
	return document, nil
}
