package validate

import (
	"net/url"
	"path"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func validateRoot(document openapi.Document, options Options) []Diagnostic {
	dialect := document.SpecificationVersion().Dialect()
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	if dialect != specversion.DialectSwagger20 &&
		!standardEntryDocumentName(options.ReferenceResourceURI) {
		diagnostics = append(diagnostics, Diagnostic{
			Code:                 "openapi.document.entry-name.nonstandard",
			Message:              "entry document should be named openapi.json or openapi.yaml",
			Severity:             SeverityWarning,
			Source:               SourceDocument,
			SpecificationVersion: version,
			SpecificationSection: "openapi-description-structure",
		})
	}
	if dialect != specversion.DialectOAS31 && dialect != specversion.DialectOAS32 {
		return diagnostics
	}
	root := document.Raw()
	_, hasComponents := root.Lookup("components")
	_, hasPaths := root.Lookup("paths")
	_, hasWebhooks := root.Lookup("webhooks")
	if !hasComponents && !hasPaths && !hasWebhooks {
		diagnostics = append(diagnostics, rootDiagnostic(
			version,
			"openapi.root.surface.missing",
			"",
			"OpenAPI requires components, paths, or webhooks",
		))
	}
	if dialect == specversion.DialectOAS32 {
		if self, exists := stringMember(root, "$self"); exists && self != "" &&
			!validURIReference(self) {
			diagnostics = append(diagnostics, rootDiagnostic(
				version,
				"openapi.root.self.invalid",
				"/$self",
				"$self must be a valid URI reference",
			))
		}
	}
	if schemaDialect, exists := stringMember(root, "jsonSchemaDialect"); exists &&
		!validAbsoluteURI(schemaDialect) {
		diagnostics = append(diagnostics, rootDiagnostic(
			version,
			"openapi.root.schema-dialect.non-absolute",
			"/jsonSchemaDialect",
			"jsonSchemaDialect must be a non-relative URI",
		))
	}
	return diagnostics
}

func standardEntryDocumentName(resourceURI string) bool {
	if resourceURI == "" {
		return true
	}
	parsed, err := url.Parse(resourceURI)
	if err != nil {
		return true
	}
	name := path.Base(parsed.Path)
	return name == "openapi.json" || name == "openapi.yaml"
}

func rootDiagnostic(
	version string,
	code string,
	pointer string,
	message string,
) Diagnostic {
	return Diagnostic{
		Code:                 code,
		Message:              message,
		Severity:             SeverityError,
		Source:               SourceDocument,
		InstanceLocation:     pointer,
		SpecificationVersion: version,
		SpecificationSection: "openapi-object",
	}
}
