package validate

import (
	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func validateRequestBodies(document openapi.Document) []Diagnostic {
	version := document.SpecificationVersion().String()
	dialect := document.SpecificationVersion().Dialect()
	if dialect == specversion.DialectSwagger20 {
		return nil
	}
	var diagnostics []Diagnostic
	for _, operation := range documentOperations(document) {
		if _, exists := operation.value.Lookup("requestBody"); !exists ||
			!undefinedRequestBodyMethod(operation.method) {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Code:                 "openapi.operation.request-body.undefined",
			Message:              "request body semantics are not well defined for this HTTP method",
			Severity:             SeverityWarning,
			Source:               SourceDocument,
			InstanceLocation:     operation.pointer + "/requestBody",
			SpecificationVersion: version,
			SpecificationSection: "operation-object",
		})
	}
	if version != "3.1.2" && dialect != specversion.DialectOAS32 {
		return diagnostics
	}
	for _, requestBody := range requestBodyObjects(document) {
		content, exists := requestBody.value.Lookup("content")
		if !exists || content.Kind() != jsonvalue.ObjectKind {
			continue
		}
		members, _ := content.Members()
		if len(members) != 0 {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Code:                 "openapi.request-body.content.empty",
			Message:              "request body content should contain at least one media type",
			Severity:             SeverityWarning,
			Source:               SourceDocument,
			InstanceLocation:     requestBody.pointer + "/content",
			SpecificationVersion: version,
			SpecificationSection: "request-body-object",
		})
	}
	return diagnostics
}

func undefinedRequestBodyMethod(method string) bool {
	switch method {
	case "get", "head", "delete":
		return true
	default:
		return false
	}
}

func consumerIgnoresRequestBody(
	dialect specversion.Dialect,
	method string,
) bool {
	if dialect != specversion.DialectOAS30 {
		return false
	}
	switch method {
	case "put", "post", "patch":
		return false
	default:
		return true
	}
}

func requestBodyObjects(document openapi.Document) []locatedParameter {
	root := document.Raw()
	var result []locatedParameter
	if components, exists := objectMember(root, "components"); exists {
		result = append(
			result,
			componentObjects(
				components,
				"requestBodies",
				"/components/requestBodies",
			)...,
		)
	}
	for _, operation := range documentOperations(document) {
		if consumerIgnoresRequestBody(
			document.SpecificationVersion().Dialect(),
			operation.method,
		) {
			continue
		}
		requestBody, exists := operation.value.Lookup("requestBody")
		if !exists || requestBody.Kind() != jsonvalue.ObjectKind || isReference(requestBody) {
			continue
		}
		result = append(result, locatedParameter{
			value:   requestBody,
			pointer: operation.pointer + "/requestBody",
		})
	}
	return result
}
