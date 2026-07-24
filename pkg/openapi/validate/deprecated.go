package validate

import (
	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func validateDeprecatedDeclarations(document openapi.Document) []Diagnostic {
	dialect := document.SpecificationVersion().Dialect()
	if dialect == specversion.DialectSwagger20 {
		return nil
	}
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	for _, operation := range documentOperations(document) {
		if trueMember(operation.value, "deprecated") {
			diagnostics = append(diagnostics, deprecatedDiagnostic(
				version,
				"openapi.operation.deprecated",
				operation.pointer+"/deprecated",
				"operation-object",
				"consumers should refrain from using this deprecated operation",
			))
		}
	}
	for _, parameter := range parameterObjects(document) {
		if trueMember(parameter.value, "deprecated") {
			diagnostics = append(diagnostics, deprecatedDiagnostic(
				version,
				"openapi.parameter.deprecated",
				parameter.pointer+"/deprecated",
				"parameter-object",
				"this deprecated parameter should be transitioned out of use",
			))
		}
	}
	for _, header := range headerObjects(document) {
		if trueMember(header.value, "deprecated") {
			diagnostics = append(diagnostics, deprecatedDiagnostic(
				version,
				"openapi.header.deprecated",
				header.pointer+"/deprecated",
				"header-object",
				"this deprecated header should be transitioned out of use",
			))
		}
	}
	if dialect == specversion.DialectOAS30 {
		diagnostics = append(
			diagnostics,
			deprecatedSchemaDiagnostics(document, version)...,
		)
	}
	if dialect == specversion.DialectOAS32 {
		diagnostics = append(
			diagnostics,
			deprecatedSecuritySchemeDiagnostics(document, version)...,
		)
	}
	return diagnostics
}

func deprecatedSchemaDiagnostics(
	document openapi.Document,
	version string,
) []Diagnostic {
	collector := schemaCollector{dialect: specversion.DialectOAS30}
	collector.document(document.Raw())
	var diagnostics []Diagnostic
	for _, root := range collector.locations {
		walkSchemaTree(root, func(schema schemaLocation) {
			if trueMember(schema.value, "deprecated") {
				diagnostics = append(diagnostics, deprecatedDiagnostic(
					version,
					"openapi.schema.deprecated",
					schema.pointer+"/deprecated",
					"schema-object",
					"this deprecated schema should be transitioned out of use",
				))
			}
		})
	}
	return diagnostics
}

func deprecatedSecuritySchemeDiagnostics(
	document openapi.Document,
	version string,
) []Diagnostic {
	definitions, pointer, exists := securitySchemeDefinitions(
		document.Raw(), specversion.DialectOAS32,
	)
	if !exists || definitions.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	members, _ := definitions.Members()
	var diagnostics []Diagnostic
	for _, member := range members {
		if !trueMember(member.Value, "deprecated") {
			continue
		}
		diagnostics = append(diagnostics, deprecatedDiagnostic(
			version,
			"openapi.security-scheme.deprecated",
			pointer+"/"+escapePointer(member.Name)+"/deprecated",
			"security-scheme-object",
			"consumers should refrain from using this deprecated security scheme",
		))
	}
	return diagnostics
}

func deprecatedDiagnostic(
	version string,
	code string,
	pointer string,
	section string,
	message string,
) Diagnostic {
	return Diagnostic{
		Code:                 code,
		Message:              message,
		Severity:             SeverityWarning,
		Source:               SourceDocument,
		InstanceLocation:     pointer,
		SpecificationVersion: version,
		SpecificationSection: section,
	}
}
