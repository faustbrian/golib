package validate

import (
	"mime"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

var swaggerSchemes = map[string]struct{}{
	"http": {}, "https": {}, "ws": {}, "wss": {},
}

func validateSwaggerTransport(document openapi.Document) []Diagnostic {
	if document.SpecificationVersion().Dialect() != specversion.DialectSwagger20 {
		return nil
	}
	version := document.SpecificationVersion().String()
	root := document.Raw()
	var diagnostics []Diagnostic
	if host, ok := stringMember(root, "host"); ok {
		if !validSwaggerHost(host) {
			diagnostics = append(diagnostics, swaggerTransportDiagnostic(
				version, "openapi.swagger.host.invalid", "/host",
				"host must contain only a host name or IP address and optional port",
			))
		}
	}
	if basePath, ok := stringMember(root, "basePath"); ok {
		if !validSwaggerBasePath(basePath) {
			diagnostics = append(diagnostics, swaggerTransportDiagnostic(
				version, "openapi.swagger.base-path.invalid", "/basePath",
				"basePath must start with a slash and must not contain a template, query, or fragment",
			))
		}
	}
	diagnostics = append(diagnostics, validateSwaggerSchemes(
		version, root, "schemes", "/schemes",
	)...)
	diagnostics = append(diagnostics, validateSwaggerMediaTypes(
		version, root, "consumes", "/consumes",
	)...)
	diagnostics = append(diagnostics, validateSwaggerMediaTypes(
		version, root, "produces", "/produces",
	)...)
	for _, operation := range documentOperations(document) {
		if summary, exists := stringMember(operation.value, "summary"); exists {
			if utf8.RuneCountInString(summary) >= 120 {
				diagnostic := swaggerTransportDiagnostic(
					version,
					"openapi.swagger.operation.summary.long",
					operation.pointer+"/summary",
					"operation summary should contain fewer than 120 characters",
				)
				diagnostic.Severity = SeverityWarning
				diagnostic.SpecificationSection = "operation-object"
				diagnostics = append(diagnostics, diagnostic)
			}
		}
		diagnostics = append(diagnostics, validateSwaggerSchemes(
			version, operation.value, "schemes", operation.pointer+"/schemes",
		)...)
		diagnostics = append(diagnostics, validateSwaggerMediaTypes(
			version, operation.value, "consumes", operation.pointer+"/consumes",
		)...)
		diagnostics = append(diagnostics, validateSwaggerMediaTypes(
			version, operation.value, "produces", operation.pointer+"/produces",
		)...)
	}
	return diagnostics
}

func validSwaggerHost(host string) bool {
	if host == "" {
		return false
	}
	if strings.ContainsAny(host, "/{}?# \t\r\n") {
		return false
	}
	parsed, err := url.Parse("//" + host)
	if err != nil {
		return false
	}
	if parsed.Host != host {
		return false
	}
	if parsed.Hostname() == "" {
		return false
	}
	return true
}

func validSwaggerBasePath(basePath string) bool {
	if !strings.HasPrefix(basePath, "/") {
		return false
	}
	return !strings.ContainsAny(basePath, "{}?#")
}

func validateSwaggerSchemes(
	version string,
	owner jsonvalue.Value,
	name string,
	pointer string,
) []Diagnostic {
	values, exists := owner.Lookup(name)
	if !exists {
		return nil
	}
	if values.Kind() != jsonvalue.ArrayKind {
		return nil
	}
	elements, _ := values.Elements()
	var diagnostics []Diagnostic
	for index, element := range elements {
		scheme, text := element.Text()
		if text {
			if _, valid := swaggerSchemes[scheme]; !valid {
				diagnostics = append(diagnostics, swaggerTransportDiagnostic(
					version, "openapi.swagger.scheme.invalid",
					pointer+"/"+strconv.Itoa(index),
					"scheme must be http, https, ws, or wss",
				))
			}
		}
	}
	return diagnostics
}

func validateSwaggerMediaTypes(
	version string,
	owner jsonvalue.Value,
	name string,
	pointer string,
) []Diagnostic {
	values, exists := owner.Lookup(name)
	if !exists {
		return nil
	}
	if values.Kind() != jsonvalue.ArrayKind {
		return nil
	}
	elements, _ := values.Elements()
	var diagnostics []Diagnostic
	for index, element := range elements {
		mediaType, text := element.Text()
		if text {
			if !validSwaggerMediaType(mediaType) {
				diagnostics = append(diagnostics, swaggerTransportDiagnostic(
					version, "openapi.swagger.media-type.invalid",
					pointer+"/"+strconv.Itoa(index),
					"value must be an RFC 6838 media type",
				))
			}
		}
	}
	return diagnostics
}

func validSwaggerMediaType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 {
		return false
	}
	return parts[1] != ""
}

func swaggerTransportDiagnostic(
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
		SpecificationSection: "swagger-transport",
	}
}
