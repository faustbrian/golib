// Package validate performs version-aware OpenAPI document validation and
// returns stable, machine-readable diagnostics.
package validate

import (
	"context"
	"errors"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	openapischema "github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

// ErrLimitExceeded reports a validation input or work bound exhaustion.
var ErrLimitExceeded = errors.New("OpenAPI validation limit exceeded")

// Source identifies the validation subsystem that produced a diagnostic.
type Source string

const (
	// SourceDocument identifies OpenAPI data-model validation.
	SourceDocument Source = "document"
	// SourceSchema identifies validation of an OpenAPI Schema Object.
	SourceSchema Source = "schema"
	// SourceReference identifies URI-reference target validation.
	SourceReference Source = "reference"
)

// Severity identifies diagnostic impact.
type Severity string

const (
	// SeverityError prevents a document from being valid.
	SeverityError Severity = "error"
	// SeverityWarning reports a non-fatal interoperability concern.
	SeverityWarning Severity = "warning"
)

// Diagnostic is one stable validation finding.
type Diagnostic struct {
	Code                    string
	Message                 string
	Severity                Severity
	Source                  Source
	InstanceLocation        string
	KeywordLocation         string
	AbsoluteKeywordLocation string
	SpecificationVersion    string
	SpecificationSection    string
}

// ExternalExampleResource is one explicitly retrieved serialized example.
type ExternalExampleResource struct {
	RetrievalURI string
	ContentType  string
	Data         []byte
}

// ExternalExampleResolver retrieves serialized example bytes through an
// application-owned authorization and transport policy.
type ExternalExampleResolver interface {
	ResolveExternalExample(context.Context, string) (ExternalExampleResource, error)
}

// ExternalExampleResolverFunc adapts a function to ExternalExampleResolver.
type ExternalExampleResolverFunc func(
	context.Context,
	string,
) (ExternalExampleResource, error)

// ResolveExternalExample implements ExternalExampleResolver.
func (resolver ExternalExampleResolverFunc) ResolveExternalExample(
	ctx context.Context,
	identifier string,
) (ExternalExampleResource, error) {
	return resolver(ctx, identifier)
}

// MediaTypeExampleCodec serializes and parses one application-owned media
// representation. Implementations own format policy and perform no implicit
// I/O through the validator.
type MediaTypeExampleCodec interface {
	Encode(context.Context, jsonvalue.Value) ([]byte, error)
	Decode(context.Context, []byte) (jsonvalue.Value, error)
}

// MediaTypeExampleCodecResolver selects an explicitly configured codec for a
// Media Type Object. Returning nil leaves that representation unchecked.
type MediaTypeExampleCodecResolver interface {
	ResolveMediaTypeExampleCodec(
		context.Context,
		string,
		jsonvalue.Value,
	) (MediaTypeExampleCodec, error)
}

// MediaTypeExampleCodecResolverFunc adapts a function to a codec resolver.
type MediaTypeExampleCodecResolverFunc func(
	context.Context,
	string,
	jsonvalue.Value,
) (MediaTypeExampleCodec, error)

// ResolveMediaTypeExampleCodec implements MediaTypeExampleCodecResolver.
func (resolver MediaTypeExampleCodecResolverFunc) ResolveMediaTypeExampleCodec(
	ctx context.Context,
	mediaType string,
	mediaTypeObject jsonvalue.Value,
) (MediaTypeExampleCodec, error) {
	return resolver(ctx, mediaType, mediaTypeObject)
}

// Options controls diagnostic collection without changing rule semantics.
type Options struct {
	FailFast       bool
	MaxDiagnostics int
	// MaxDocumentNodes limits semantic values visited in the input document.
	MaxDocumentNodes int
	// MaxDocumentDepth limits input document nesting, counting the root as one.
	MaxDocumentDepth int
	// SchemaResourceLoader explicitly authorizes non-pinned Schema Object
	// dialects and resources. Nil keeps external schema loading disabled.
	SchemaResourceLoader openapischema.ResourceLoader
	// ReferenceResourceURI supplies the retrieval URI used as the document base.
	ReferenceResourceURI string
	// ReferenceResolver explicitly authorizes external OpenAPI references. Nil
	// leaves them unresolved without making an otherwise valid document fail.
	ReferenceResolver reference.Resolver
	// ReferenceLimits bound reference scanning and target resolution.
	ReferenceLimits reference.Limits
	// MaxReferences limits resolved reference occurrences in one document.
	MaxReferences int
	// ExternalExampleResolver explicitly authorizes serialized example
	// retrieval. Nil keeps retrieval disabled.
	ExternalExampleResolver ExternalExampleResolver
	// MediaTypeExampleCodecResolver explicitly supplies application-owned
	// serialization for non-JSON media examples.
	MediaTypeExampleCodecResolver MediaTypeExampleCodecResolver
	// MaxExternalExampleBytes bounds each retrieved serialized example.
	MaxExternalExampleBytes int
	schemaCompilerFactory   func(openapi.Document, ...openapischema.Option) (*openapischema.Compiler, error)
	schemaMarshaller        func(jsonvalue.Value) ([]byte, error)
	schemaValidator         func(*openapischema.Compiler, context.Context, jsonvalue.Value) (openapischema.OutputUnit, error)
}

// DefaultOptions returns bounded collect-all validation policy.
func DefaultOptions() Options {
	return Options{
		MaxDiagnostics:          10_000,
		MaxDocumentNodes:        1_000_000,
		MaxDocumentDepth:        256,
		ReferenceLimits:         reference.DefaultLimits(),
		MaxReferences:           100_000,
		MaxExternalExampleBytes: 16 << 20,
	}
}

// Report is an immutable validation result.
type Report struct {
	diagnostics []Diagnostic
}

// Valid reports whether no error diagnostics were produced.
func (report Report) Valid() bool {
	for _, diagnostic := range report.diagnostics {
		if diagnostic.Severity == SeverityError {
			return false
		}
	}
	return true
}

// Diagnostics returns a caller-owned copy in deterministic evaluation order.
func (report Report) Diagnostics() []Diagnostic {
	return append([]Diagnostic(nil), report.diagnostics...)
}
