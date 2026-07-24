package validate

import (
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func validateHeaders(document openapi.Document) []Diagnostic {
	if document.SpecificationVersion().Dialect() == specversion.DialectSwagger20 {
		return validateSwaggerHeaders(document)
	}
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	for _, header := range headerObjects(document) {
		if strings.EqualFold(header.name, "Content-Type") {
			diagnostic := headerDiagnostic(
				version,
				"openapi.header.content-type.ignored",
				header.pointer,
				"Content-Type header definitions are ignored",
			)
			diagnostic.Severity = SeverityWarning
			diagnostics = append(diagnostics, diagnostic)
		}
		diagnostics = append(
			diagnostics,
			validateHeaderTraits(header, version)...,
		)
		content, exists := header.value.Lookup("content")
		if !exists || content.Kind() != jsonvalue.ObjectKind {
			continue
		}
		members, _ := content.Members()
		if len(members) == 1 {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Code:                 "openapi.header.content.multiple",
			Message:              "header content must contain exactly one media type",
			Severity:             SeverityError,
			Source:               SourceDocument,
			InstanceLocation:     header.pointer + "/content",
			SpecificationVersion: version,
			SpecificationSection: "header-object",
		})
	}
	return diagnostics
}

func validateHeaderTraits(header locatedParameter, version string) []Diagnostic {
	var diagnostics []Diagnostic
	_, hasExample := header.value.Lookup("example")
	_, hasExamples := header.value.Lookup("examples")
	if hasExample && hasExamples {
		diagnostics = append(diagnostics, headerDiagnostic(
			version,
			"openapi.header.examples.conflict",
			header.pointer,
			"header must not define both example and examples",
		))
	}
	for _, field := range []struct {
		name    string
		code    string
		message string
	}{
		{"name", "openapi.header.name.present", "header name comes from its containing map"},
		{"in", "openapi.header.location.present", "header location is implicitly header"},
		{"allowEmptyValue", "openapi.header.allow-empty.invalid", "allowEmptyValue does not apply to headers"},
		{"allowReserved", "openapi.header.allow-reserved.invalid", "allowReserved does not apply to headers"},
	} {
		if _, exists := header.value.Lookup(field.name); !exists {
			continue
		}
		diagnostics = append(diagnostics, headerDiagnostic(
			version,
			field.code,
			header.pointer+"/"+field.name,
			field.message,
		))
	}
	if style, exists := stringMember(header.value, "style"); exists && style != "simple" {
		diagnostics = append(diagnostics, headerDiagnostic(
			version,
			"openapi.header.style.invalid",
			header.pointer+"/style",
			"header style must be simple",
		))
	}
	return diagnostics
}

func headerDiagnostic(
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
		SpecificationSection: "header-object",
	}
}

func headerObjects(document openapi.Document) []locatedParameter {
	root := document.Raw()
	var headers []locatedParameter
	var contentContainers []locatedParameter
	components, hasComponents := objectMember(root, "components")
	if hasComponents {
		headers, contentContainers = appendHeaderMap(
			headers,
			contentContainers,
			components,
			"headers",
			"/components/headers",
		)
		responses := componentObjects(
			components,
			"responses",
			"/components/responses",
		)
		for _, response := range responses {
			contentContainers = append(contentContainers, response)
			headers, contentContainers = appendHeaderMap(
				headers,
				contentContainers,
				response.value,
				"headers",
				response.pointer+"/headers",
			)
		}
	}
	contentContainers = append(contentContainers, parameterObjects(document)...)
	contentContainers = append(contentContainers, requestBodyObjects(document)...)
	for _, operation := range documentOperations(document) {
		responses, exists := objectMember(operation.value, "responses")
		if !exists {
			continue
		}
		members, _ := responses.Members()
		for _, member := range members {
			if member.Value.Kind() != jsonvalue.ObjectKind || isReference(member.Value) {
				continue
			}
			response := locatedParameter{
				value:   member.Value,
				pointer: operation.pointer + "/responses/" + escapePointer(member.Name),
			}
			contentContainers = append(contentContainers, response)
			headers, contentContainers = appendHeaderMap(
				headers,
				contentContainers,
				response.value,
				"headers",
				response.pointer+"/headers",
			)
		}
	}
	if hasComponents {
		switch document.SpecificationVersion().Dialect() {
		case specversion.DialectOAS32:
			mediaTypes, exists := objectMember(components, "mediaTypes")
			if exists {
				members, _ := mediaTypes.Members()
				for _, member := range members {
					headers, contentContainers = appendEncodingHeaders(
						headers,
						contentContainers,
						member.Value,
						"/components/mediaTypes/"+escapePointer(member.Name),
					)
				}
			}
		}
	}
	for index := 0; index < len(contentContainers); index++ {
		container := contentContainers[index]
		content, exists := objectMember(container.value, "content")
		if !exists {
			continue
		}
		mediaTypes, _ := content.Members()
		for _, mediaType := range mediaTypes {
			headers, contentContainers = appendEncodingHeaders(
				headers,
				contentContainers,
				mediaType.Value,
				container.pointer+"/content/"+escapePointer(mediaType.Name),
			)
		}
	}
	return headers
}

func componentObjects(
	components jsonvalue.Value,
	field string,
	pointer string,
) []locatedParameter {
	objects, exists := objectMember(components, field)
	if !exists {
		return nil
	}
	var result []locatedParameter
	members, _ := objects.Members()
	for _, member := range members {
		if member.Value.Kind() != jsonvalue.ObjectKind || isReference(member.Value) {
			continue
		}
		result = append(result, locatedParameter{
			value:   member.Value,
			pointer: pointer + "/" + escapePointer(member.Name),
		})
	}
	return result
}

func appendHeaderMap(
	headers []locatedParameter,
	contentContainers []locatedParameter,
	container jsonvalue.Value,
	field string,
	pointer string,
) ([]locatedParameter, []locatedParameter) {
	headerMap, exists := objectMember(container, field)
	if !exists {
		return headers, contentContainers
	}
	members, _ := headerMap.Members()
	for _, member := range members {
		if member.Value.Kind() != jsonvalue.ObjectKind || isReference(member.Value) {
			continue
		}
		header := locatedParameter{
			value:   member.Value,
			pointer: pointer + "/" + escapePointer(member.Name),
			name:    member.Name,
		}
		headers = append(headers, header)
		contentContainers = append(contentContainers, header)
	}
	return headers, contentContainers
}

func appendEncodingHeaders(
	headers []locatedParameter,
	contentContainers []locatedParameter,
	mediaType jsonvalue.Value,
	pointer string,
) ([]locatedParameter, []locatedParameter) {
	if mediaType.Kind() != jsonvalue.ObjectKind || isReference(mediaType) {
		return headers, contentContainers
	}
	encodings, exists := objectMember(mediaType, "encoding")
	if !exists {
		return headers, contentContainers
	}
	members, _ := encodings.Members()
	for _, member := range members {
		headers, contentContainers = appendHeaderMap(
			headers,
			contentContainers,
			member.Value,
			"headers",
			pointer+"/encoding/"+escapePointer(member.Name)+"/headers",
		)
	}
	return headers, contentContainers
}
