package validate

import (
	"math/big"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

func validateSwaggerTypedDefaults(
	value jsonvalue.Value,
	pointer string,
	version string,
	section string,
) []Diagnostic {
	var diagnostics []Diagnostic
	if section == "items-object" || section == "header-object" {
		typeName, hasType := stringMember(value, "type")
		if !hasType || !validSwaggerCollectionType(typeName) {
			code := "swagger.items.type.invalid"
			message := "items type must be string, number, integer, boolean, or array"
			if section == "header-object" {
				code = "swagger.header.type.invalid"
				message = "header type must be string, number, integer, boolean, or array"
			}
			diagnostics = append(diagnostics, Diagnostic{
				Code:                 code,
				Message:              message,
				Severity:             SeverityError,
				Source:               SourceDocument,
				InstanceLocation:     pointer + "/type",
				SpecificationVersion: version,
				SpecificationSection: section,
			})
		}
	}
	if typeName, hasType := stringMember(value, "type"); hasType {
		if defaultValue, hasDefault := value.Lookup("default"); hasDefault &&
			!swaggerValueMatchesType(typeName, defaultValue) {
			diagnostics = append(diagnostics, Diagnostic{
				Code:                 "openapi.swagger.default.type",
				Message:              "default must conform to the declared type",
				Severity:             SeverityError,
				Source:               SourceDocument,
				InstanceLocation:     pointer + "/default",
				SpecificationVersion: version,
				SpecificationSection: section,
			})
		}
	}
	items, exists := value.Lookup("items")
	if !exists || items.Kind() != jsonvalue.ObjectKind {
		return diagnostics
	}
	return append(
		diagnostics,
		validateSwaggerTypedDefaults(
			items,
			pointer+"/items",
			version,
			"items-object",
		)...,
	)
}

func validSwaggerCollectionType(typeName string) bool {
	switch typeName {
	case "string", "number", "integer", "boolean", "array":
		return true
	default:
		return false
	}
}

func swaggerValueMatchesType(typeName string, value jsonvalue.Value) bool {
	switch typeName {
	case "array":
		return value.Kind() == jsonvalue.ArrayKind
	case "boolean":
		return value.Kind() == jsonvalue.BooleanKind
	case "integer":
		if value.Kind() != jsonvalue.NumberKind {
			return false
		}
		number, _ := value.NumberText()
		rational, valid := new(big.Rat).SetString(number)
		return valid && rational.IsInt()
	case "number":
		return value.Kind() == jsonvalue.NumberKind
	case "object":
		return value.Kind() == jsonvalue.ObjectKind
	case "string", "file":
		return value.Kind() == jsonvalue.StringKind
	default:
		return true
	}
}

func validateSwaggerHeaders(document openapi.Document) []Diagnostic {
	root := document.Raw()
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	diagnostics = appendSwaggerResponseHeaderDefaults(
		diagnostics,
		root,
		"responses",
		"/responses",
		version,
	)
	paths, exists := root.Lookup("paths")
	if !exists || paths.Kind() != jsonvalue.ObjectKind {
		return diagnostics
	}
	pathMembers, _ := paths.Members()
	for _, path := range pathMembers {
		if !strings.HasPrefix(path.Name, "/") ||
			path.Value.Kind() != jsonvalue.ObjectKind {
			continue
		}
		pathPointer := "/paths/" + escapePointer(path.Name)
		for _, operation := range operationsAt(
			path.Value,
			pathPointer,
			openapi.DialectSwagger20,
		) {
			diagnostics = appendSwaggerResponseHeaderDefaults(
				diagnostics,
				operation.value,
				"responses",
				operation.pointer+"/responses",
				version,
			)
		}
	}
	return diagnostics
}

func appendSwaggerResponseHeaderDefaults(
	diagnostics []Diagnostic,
	owner jsonvalue.Value,
	field string,
	pointer string,
	version string,
) []Diagnostic {
	responses, exists := owner.Lookup(field)
	if !exists || responses.Kind() != jsonvalue.ObjectKind {
		return diagnostics
	}
	responseMembers, _ := responses.Members()
	for _, response := range responseMembers {
		if response.Value.Kind() != jsonvalue.ObjectKind ||
			isReference(response.Value) {
			continue
		}
		headers, exists := response.Value.Lookup("headers")
		if !exists || headers.Kind() != jsonvalue.ObjectKind {
			continue
		}
		headerMembers, _ := headers.Members()
		for _, header := range headerMembers {
			diagnostics = append(diagnostics, validateSwaggerTypedDefaults(
				header.Value,
				pointer+"/"+escapePointer(response.Name)+"/headers/"+
					escapePointer(header.Name),
				version,
				"header-object",
			)...)
		}
	}
	return diagnostics
}
