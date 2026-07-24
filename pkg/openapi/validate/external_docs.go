package validate

import (
	"net/url"
	"strconv"
	"unicode"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func validateExternalDocumentation(document openapi.Document) []Diagnostic {
	version := document.SpecificationVersion().String()
	dialect := document.SpecificationVersion().Dialect()
	var diagnostics []Diagnostic
	diagnostics = appendExternalDocumentationDiagnostics(
		diagnostics, document.Raw(), "", version, dialect,
	)
	for _, operation := range documentOperations(document) {
		diagnostics = appendExternalDocumentationDiagnostics(
			diagnostics, operation.value, operation.pointer, version, dialect,
		)
	}
	if tags, exists := document.Raw().Lookup("tags"); exists &&
		tags.Kind() == jsonvalue.ArrayKind {
		elements, _ := tags.Elements()
		for index, tag := range elements {
			diagnostics = appendExternalDocumentationDiagnostics(
				diagnostics, tag, "/tags/"+strconv.Itoa(index), version, dialect,
			)
		}
	}
	collector := schemaCollector{dialect: document.SpecificationVersion().Dialect()}
	collector.document(document.Raw())
	for _, root := range collector.locations {
		walkSchemaTree(root, func(schema schemaLocation) {
			diagnostics = appendExternalDocumentationDiagnostics(
				diagnostics, schema.value, schema.pointer, version, dialect,
			)
		})
	}
	return diagnostics
}

func appendExternalDocumentationDiagnostics(
	diagnostics []Diagnostic,
	owner jsonvalue.Value,
	pointer string,
	version string,
	dialect specversion.Dialect,
) []Diagnostic {
	externalDocs, exists := objectMember(owner, "externalDocs")
	if !exists {
		return diagnostics
	}
	target, exists := stringMember(externalDocs, "url")
	if !exists || validMetadataURL(target, dialect) {
		return diagnostics
	}
	return append(diagnostics, Diagnostic{
		Code:                 "openapi.external-docs.url.invalid",
		Message:              "external documentation URL must be a valid URI reference",
		Severity:             SeverityError,
		Source:               SourceDocument,
		InstanceLocation:     pointer + "/externalDocs/url",
		SpecificationVersion: version,
		SpecificationSection: "external-documentation-object",
	})
}

func validAbsoluteURI(value string) bool {
	if !validURIReference(value) {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.IsAbs()
}

func validURIReference(value string) bool {
	if value == "" || containsUnicodeSpace(value) {
		return false
	}
	_, err := url.Parse(value)
	return err == nil
}

func containsUnicodeSpace(value string) bool {
	for _, character := range value {
		if unicode.IsSpace(character) {
			return true
		}
	}
	return false
}

func walkSchemaTree(root schemaLocation, visit func(schemaLocation)) {
	stack := []schemaLocation{root}
	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		visit(current)
		children := schemaChildren(current)
		for index := len(children) - 1; index >= 0; index-- {
			stack = append(stack, children[index])
		}
	}
}

func schemaChildren(parent schemaLocation) []schemaLocation {
	if parent.value.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	members, _ := parent.value.Members()
	var result []schemaLocation
	for _, member := range members {
		pointer := parent.pointer + "/" + escapePointer(member.Name)
		switch member.Name {
		case "$defs", "definitions", "properties", "patternProperties", "dependentSchemas":
			result = appendSchemaMap(result, member.Value, pointer)
		case "allOf", "anyOf", "oneOf", "prefixItems":
			result = appendSchemaArray(result, member.Value, pointer)
		case "items":
			if member.Value.Kind() == jsonvalue.ArrayKind {
				result = appendSchemaArray(result, member.Value, pointer)
			} else {
				result = appendSchemaValue(result, member.Value, pointer)
			}
		case "additionalItems", "additionalProperties", "unevaluatedProperties",
			"propertyNames", "contains", "unevaluatedItems", "contentSchema",
			"if", "then", "else", "not":
			result = appendSchemaValue(result, member.Value, pointer)
		}
	}
	return result
}

func appendSchemaMap(
	result []schemaLocation,
	value jsonvalue.Value,
	pointer string,
) []schemaLocation {
	if value.Kind() != jsonvalue.ObjectKind {
		return result
	}
	members, _ := value.Members()
	for _, member := range members {
		result = appendSchemaValue(
			result, member.Value, pointer+"/"+escapePointer(member.Name),
		)
	}
	return result
}

func appendSchemaArray(
	result []schemaLocation,
	value jsonvalue.Value,
	pointer string,
) []schemaLocation {
	if value.Kind() != jsonvalue.ArrayKind {
		return result
	}
	elements, _ := value.Elements()
	for index, element := range elements {
		result = appendSchemaValue(
			result, element, pointer+"/"+strconv.Itoa(index),
		)
	}
	return result
}

func appendSchemaValue(
	result []schemaLocation,
	value jsonvalue.Value,
	pointer string,
) []schemaLocation {
	switch value.Kind() {
	case jsonvalue.ObjectKind, jsonvalue.BooleanKind:
	default:
		return result
	}
	return append(result, schemaLocation{value: value, pointer: pointer})
}
