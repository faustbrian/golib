// Package convert provides explicit, directional, loss-aware OpenAPI version
// migrations without mutating source documents.
package convert

import (
	"context"
	"errors"
	"fmt"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

var (
	// ErrInvalidInput reports a nil context or document, or an invalid target.
	ErrInvalidInput = errors.New("invalid OpenAPI conversion input")
	// ErrInvalidOptions reports unusable conversion limits.
	ErrInvalidOptions = errors.New("invalid OpenAPI conversion options")
	// ErrLimitExceeded reports conversion work beyond caller policy.
	ErrLimitExceeded = errors.New("OpenAPI conversion limit exceeded")
	// ErrUnsupportedConversion reports a direction without a complete policy.
	ErrUnsupportedConversion = errors.New("unsupported OpenAPI conversion")
)

// DiagnosticKind identifies the caller response required by a conversion.
type DiagnosticKind string

const (
	// Loss identifies source semantics that could not be represented.
	Loss DiagnosticKind = "loss"
	// ManualAction identifies semantics that require caller review.
	ManualAction DiagnosticKind = "manual-action"
)

// Diagnostic reports one structured conversion consequence.
type Diagnostic struct {
	Code    string
	Kind    DiagnosticKind
	Pointer string
	Message string
}

// Options bounds independently growing conversion work.
type Options struct {
	// MaxRootMembers limits top-level document members.
	MaxRootMembers int
	// MaxDocumentNodes limits transformed Swagger document objects.
	MaxDocumentNodes int
	// MaxSchemaNodes limits recursively translated Schema Objects.
	MaxSchemaNodes int
}

// DefaultOptions returns conservative limits for untrusted documents.
func DefaultOptions() Options {
	return Options{
		MaxRootMembers: 100_000, MaxDocumentNodes: 1_000_000,
		MaxSchemaNodes: 1_000_000,
	}
}

// Result retains both source provenance and the converted immutable document.
type Result struct {
	source      openapi.Document
	document    openapi.Document
	diagnostics []Diagnostic
}

// Source returns the original immutable document.
func (result Result) Source() openapi.Document {
	return result.source
}

// Document returns the converted immutable document.
func (result Result) Document() openapi.Document {
	return result.document
}

// Diagnostics returns caller-owned conversion diagnostics.
func (result Result) Diagnostics() []Diagnostic {
	return append([]Diagnostic(nil), result.diagnostics...)
}

// To converts a document under an explicit target and bounded policy.
//
// Patch revisions within one OpenAPI line are converted by replacing only the
// version marker, as patch releases do not change the feature set. Swagger 2.0
// may be upgraded to any supported OpenAPI line. OpenAPI 3.0 may be upgraded
// to 3.1 or 3.2 with its Schema Object dialect translated. OpenAPI 3.1 may be
// upgraded to 3.2; an absent schema dialect is made explicit so the target's
// default does not change Schema Object meaning. OpenAPI 3.1 may also be
// downgraded to 3.0, and OpenAPI 3.2 may be downgraded to either prior OpenAPI
// line, with explicit loss diagnostics. Every supported OpenAPI line may be
// downgraded to Swagger 2.0 through the same loss-aware transformations.
func To(
	ctx context.Context,
	source openapi.Document,
	target openapi.Version,
	options Options,
) (Result, error) {
	if ctx == nil || source == nil || target.String() == "" {
		return Result{}, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if options.MaxRootMembers < 0 || options.MaxDocumentNodes < 0 ||
		options.MaxSchemaNodes < 0 {
		return Result{}, ErrInvalidOptions
	}
	defaults := DefaultOptions()
	if options.MaxRootMembers == 0 {
		options.MaxRootMembers = defaults.MaxRootMembers
	}
	if options.MaxSchemaNodes == 0 {
		options.MaxSchemaNodes = defaults.MaxSchemaNodes
	}
	if options.MaxDocumentNodes == 0 {
		options.MaxDocumentNodes = defaults.MaxDocumentNodes
	}
	memberCount, _ := source.Raw().Length()
	if memberCount > options.MaxRootMembers {
		return Result{}, ErrLimitExceeded
	}
	if source.SpecificationVersion().String() == target.String() {
		return Result{source: source, document: source}, nil
	}

	sourceDialect := source.SpecificationVersion().Dialect()
	swaggerUpgrade := sourceDialect == openapi.DialectSwagger20 &&
		target.Dialect() != openapi.DialectSwagger20
	oas30Upgrade := sourceDialect == openapi.DialectOAS30 &&
		(target.Dialect() == openapi.DialectOAS31 ||
			target.Dialect() == openapi.DialectOAS32)
	oas31Downgrade := sourceDialect == openapi.DialectOAS31 &&
		target.Dialect() == openapi.DialectOAS30
	oas31Upgrade := sourceDialect == openapi.DialectOAS31 &&
		target.Dialect() == openapi.DialectOAS32
	oas32Downgrade := sourceDialect == openapi.DialectOAS32 &&
		(target.Dialect() == openapi.DialectOAS31 ||
			target.Dialect() == openapi.DialectOAS30)
	openAPISwaggerDowngrade := sourceDialect != openapi.DialectSwagger20 &&
		target.Dialect() == openapi.DialectSwagger20
	if sourceDialect != target.Dialect() && !swaggerUpgrade &&
		!oas30Upgrade && !oas31Downgrade && !oas32Downgrade &&
		!openAPISwaggerDowngrade && !oas31Upgrade {
		return Result{}, fmt.Errorf(
			"%w: %s to %s",
			ErrUnsupportedConversion,
			source.SpecificationVersion(),
			target,
		)
	}

	var (
		converted   jsonvalue.Value
		diagnostics []Diagnostic
		err         error
	)
	if swaggerUpgrade {
		swaggerTarget := target
		switch target.Dialect() {
		case openapi.DialectOAS30:
		default:
			swaggerTarget, _ = openapi.ParseVersion("3.0.4")
		}
		converted, diagnostics, err = convertSwagger20Root(
			ctx,
			source.Raw(),
			swaggerTarget,
			options.MaxDocumentNodes,
			options.MaxSchemaNodes,
		)
		if err != nil {
			return Result{}, err
		}
	} else if openAPISwaggerDowngrade {
		converted, diagnostics, err = convertOpenAPIToSwagger20(
			ctx, source.Raw(), sourceDialect, options,
		)
		if err != nil {
			return Result{}, err
		}
	} else {
		initialTarget := target
		switch sourceDialect {
		case openapi.DialectOAS30:
			switch target.Dialect() {
			case openapi.DialectOAS32:
				initialTarget, _ = openapi.ParseVersion("3.1.2")
			}
		case openapi.DialectOAS32:
			switch target.Dialect() {
			case openapi.DialectOAS30:
				initialTarget, _ = openapi.ParseVersion("3.1.2")
			}
		}
		members, _ := source.Raw().Members()
		converted, err = replaceVersion(members, initialTarget)
		if err != nil {
			return Result{}, err
		}
	}
	if swaggerUpgrade {
		switch target.Dialect() {
		case openapi.DialectOAS30:
		default:
			oas31Target := target
			switch target.Dialect() {
			case openapi.DialectOAS32:
				oas31Target, _ = openapi.ParseVersion("3.1.2")
			}
			convertedMembers, _ := converted.Members()
			converted, _ = replaceVersion(convertedMembers, oas31Target)
			var schemaDiagnostics []Diagnostic
			converted, schemaDiagnostics, err = convertOAS30Document(
				ctx, converted, options.MaxSchemaNodes,
			)
			diagnostics = append(diagnostics, schemaDiagnostics...)
			if err != nil {
				return Result{}, err
			}
		}
	}
	if sourceDialect == openapi.DialectOAS30 &&
		(target.Dialect() == openapi.DialectOAS31 ||
			target.Dialect() == openapi.DialectOAS32) {
		converted, diagnostics, err = convertOAS30Document(
			ctx, converted, options.MaxSchemaNodes,
		)
		if err != nil {
			return Result{}, err
		}
	}
	if oas31Downgrade {
		converted, diagnostics, err = convertOAS31Document(
			ctx, converted, options.MaxSchemaNodes,
		)
		if err != nil {
			return Result{}, err
		}
	}
	if oas32Downgrade {
		converted, diagnostics, err = convertOAS32Document(
			ctx, converted, options.MaxDocumentNodes,
		)
		if err != nil {
			return Result{}, err
		}
		if target.Dialect() == openapi.DialectOAS30 {
			convertedMembers, _ := converted.Members()
			converted, _ = replaceVersion(convertedMembers, target)
			var schemaDiagnostics []Diagnostic
			converted, schemaDiagnostics, err = convertOAS31Document(
				ctx, converted, options.MaxSchemaNodes,
			)
			diagnostics = append(diagnostics, schemaDiagnostics...)
			if err != nil {
				return Result{}, err
			}
		}
	}
	if (sourceDialect == openapi.DialectOAS31 ||
		sourceDialect == openapi.DialectOAS30 || swaggerUpgrade) &&
		target.Dialect() == openapi.DialectOAS32 {
		if swaggerUpgrade || sourceDialect == openapi.DialectOAS30 {
			convertedMembers, _ := converted.Members()
			converted, _ = replaceVersion(convertedMembers, target)
		}
		converted = preserveOAS31SchemaDialect(converted)
		diagnostics = append(diagnostics, Diagnostic{
			Code:    "openapi.convert.minor-version-review",
			Kind:    ManualAction,
			Pointer: "",
			Message: "review OpenAPI 3.2 semantic changes before publication",
		})
	}
	document, err := openapi.Decode(converted)
	if err != nil {
		return Result{}, fmt.Errorf("convert OpenAPI document: %w", err)
	}
	return Result{
		source:      source,
		document:    document,
		diagnostics: diagnostics,
	}, nil
}

func replaceVersion(
	members []jsonvalue.Member,
	target openapi.Version,
) (jsonvalue.Value, error) {
	version, _ := jsonvalue.String(target.String())
	for index := range members {
		if members[index].Name == "openapi" {
			members[index].Value = version
			return jsonvalue.Object(members)
		}
	}
	return jsonvalue.Value{}, fmt.Errorf(
		"convert OpenAPI document: %w: missing version marker",
		ErrInvalidInput,
	)
}

func preserveOAS31SchemaDialect(root jsonvalue.Value) jsonvalue.Value {
	if _, exists := root.Lookup("jsonSchemaDialect"); exists {
		return root
	}
	members, _ := root.Members()
	dialect, _ := jsonvalue.String("https://spec.openapis.org/oas/3.1/dialect/base")
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		result = append(result, member)
		if member.Name == "openapi" {
			result = append(result, jsonvalue.Member{
				Name:  "jsonSchemaDialect",
				Value: dialect,
			})
		}
	}
	converted, _ := jsonvalue.Object(result)
	return converted
}
