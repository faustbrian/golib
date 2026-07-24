package validate

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

type pathParameter struct {
	name    string
	pointer string
}

type operationLocation struct {
	value    jsonvalue.Value
	pathItem jsonvalue.Value
	resource reference.Resource
	pointer  string
	method   string
}

var fixedOperationMethods = map[string]struct{}{
	"GET": {}, "PUT": {}, "POST": {}, "DELETE": {}, "OPTIONS": {},
	"HEAD": {}, "PATCH": {}, "TRACE": {}, "QUERY": {},
}

func validateAdditionalOperations(document openapi.Document) []Diagnostic {
	if document.SpecificationVersion().Dialect() != specversion.DialectOAS32 {
		return nil
	}
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	for _, pathItem := range documentPathItems(document) {
		additional, exists := objectMember(pathItem.value, "additionalOperations")
		if !exists {
			continue
		}
		members, _ := additional.Members()
		for _, member := range members {
			if _, fixed := fixedOperationMethods[member.Name]; !fixed {
				continue
			}
			diagnostics = append(diagnostics, Diagnostic{
				Code:                 "openapi.path.additional-operation.fixed",
				Message:              "additionalOperations must not redefine a fixed operation method",
				Severity:             SeverityError,
				Source:               SourceDocument,
				InstanceLocation:     pathItem.pointer + "/additionalOperations/" + escapePointer(member.Name),
				SpecificationVersion: version,
				SpecificationSection: "path-item-object",
			})
		}
	}
	return diagnostics
}

func validatePaths(
	ctx context.Context,
	document openapi.Document,
	options Options,
) []Diagnostic {
	root := document.Raw()
	resource := validationResource(document, options.ReferenceResourceURI)
	paths, exists := root.Lookup("paths")
	if !exists || paths.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	version := document.SpecificationVersion().String()
	dialect := document.SpecificationVersion().Dialect()
	members, _ := paths.Members()
	var diagnostics []Diagnostic
	normalizedPaths := make(map[string]string)
	for _, member := range members {
		if !strings.HasPrefix(member.Name, "/") {
			if !strings.HasPrefix(member.Name, "x-") {
				diagnostics = append(diagnostics, pathDiagnostic(
					version,
					"openapi.path.key.invalid",
					"/paths/"+escapePointer(member.Name),
					"path field name must begin with a slash",
				))
			}
			continue
		}
		if member.Value.Kind() != jsonvalue.ObjectKind {
			continue
		}
		pathPointer := "/paths/" + escapePointer(member.Name)
		pathItem, pathItemResource, resolved :=
			resolveReferencedObjectResourceWithPolicy(
				ctx,
				resource,
				member.Value,
				options.ReferenceResolver,
				options.ReferenceLimits,
			)
		if !resolved {
			continue
		}
		templateNames, normalized, repeated, templateErr := parsePathTemplate(member.Name)
		if templateErr != nil {
			diagnostics = append(diagnostics, pathDiagnostic(
				version,
				"openapi.path.template.invalid",
				pathPointer,
				"path template has unmatched or empty braces",
			))
		} else if prior, duplicate := normalizedPaths[normalized]; duplicate && prior != member.Name {
			diagnostics = append(diagnostics, pathDiagnostic(
				version,
				"openapi.path.template.ambiguous",
				pathPointer,
				"path template has the same hierarchy as "+safeValue(prior),
			))
		} else {
			normalizedPaths[normalized] = member.Name
		}
		if templateErr == nil && repeated && dialect == specversion.DialectOAS32 {
			diagnostics = append(diagnostics, pathDiagnostic(
				version,
				"openapi.path.template.duplicate",
				pathPointer,
				"path template must not repeat a template expression",
			))
		}

		inherited, inheritedDiagnostics := parametersAt(
			ctx,
			pathItemResource,
			options.ReferenceResolver,
			options.ReferenceLimits,
			pathItem,
			pathPointer+"/parameters",
			version,
		)
		diagnostics = append(diagnostics, inheritedDiagnostics...)
		for _, operation := range operationsAt(pathItem, pathPointer, dialect) {
			operationParameters, parameterDiagnostics := parametersAt(
				ctx,
				pathItemResource,
				options.ReferenceResolver,
				options.ReferenceLimits,
				operation.value,
				operation.pointer+"/parameters",
				version,
			)
			diagnostics = append(diagnostics, parameterDiagnostics...)
			effective := make(map[string]pathParameter, len(inherited))
			for _, parameter := range inherited {
				effective[parameter.name] = parameter
			}
			for _, parameter := range operationParameters {
				effective[parameter.name] = parameter
			}
			if templateErr == nil {
				diagnostics = append(
					diagnostics,
					validateOperationPathParameters(
						templateNames, effective, operation.pointer, version,
					)...,
				)
			}
		}
	}
	return diagnostics
}

func validateOperationIDs(
	ctx context.Context,
	document openapi.Document,
	options Options,
) []Diagnostic {
	version := document.SpecificationVersion().String()
	identifiers := make(map[string]string)
	var diagnostics []Diagnostic
	operations := append(
		documentOperations(document),
		externalDocumentOperations(ctx, document, options)...,
	)
	for _, operation := range operations {
		identifier, ok := stringMember(operation.value, "operationId")
		if !ok {
			continue
		}
		pointer := operation.pointer + "/operationId"
		if !portableOperationID(identifier) {
			diagnostic := pathDiagnostic(
				version,
				"openapi.operation-id.nonportable",
				pointer,
				"operationId should use a portable programming identifier",
			)
			diagnostic.Severity = SeverityWarning
			diagnostic.SpecificationSection = "operation-object"
			diagnostics = append(diagnostics, diagnostic)
		}
		if prior, duplicate := identifiers[identifier]; duplicate {
			diagnostics = append(diagnostics, pathDiagnostic(
				version,
				"openapi.operation-id.duplicate",
				pointer,
				"operationId is already used at "+safeValue(prior),
			))
			continue
		}
		identifiers[identifier] = pointer
	}
	return diagnostics
}

var portableOperationIDPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func portableOperationID(identifier string) bool {
	return portableOperationIDPattern.MatchString(identifier)
}

func parsePathTemplate(path string) (map[string]struct{}, string, bool, error) {
	names := make(map[string]struct{})
	var normalized strings.Builder
	repeated := false
	for index := 0; index < len(path); {
		switch path[index] {
		case '}':
			return nil, "", false, fmt.Errorf("unmatched closing brace")
		case '{':
			closing := strings.IndexByte(path[index+1:], '}')
			if closing == -1 {
				return nil, "", false, fmt.Errorf("unmatched opening brace")
			}
			closing += index + 1
			name := path[index+1 : closing]
			if !validTemplateName(name) {
				return nil, "", false, fmt.Errorf("invalid template name")
			}
			_, repeatedName := names[name]
			repeated = repeated || repeatedName
			names[name] = struct{}{}
			normalized.WriteString("{}")
			index = closing + 1
		default:
			normalized.WriteByte(path[index])
			index++
		}
	}
	return names, normalized.String(), repeated, nil
}

func validTemplateName(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	for _, character := range name {
		if character < 0x20 {
			return false
		}
		switch character {
		case 0x7f, '{', '}', '/':
			return false
		}
	}
	return true
}

func parametersAt(
	ctx context.Context,
	resource reference.Resource,
	resolver reference.Resolver,
	limits reference.Limits,
	container jsonvalue.Value,
	pointer string,
	version string,
) ([]pathParameter, []Diagnostic) {
	raw, exists := container.Lookup("parameters")
	if !exists || raw.Kind() != jsonvalue.ArrayKind {
		return nil, nil
	}
	elements, _ := raw.Elements()
	seen := make(map[string]struct{})
	var parameters []pathParameter
	var diagnostics []Diagnostic
	for index, element := range elements {
		if element.Kind() != jsonvalue.ObjectKind {
			continue
		}
		resolved, ok := resolveReferencedObjectWithPolicy(
			ctx, resource, element, resolver, limits,
		)
		if !ok {
			continue
		}
		name, hasName := stringMember(resolved, "name")
		location, hasLocation := stringMember(resolved, "in")
		if !hasName || !hasLocation || location != "path" {
			continue
		}
		parameterPointer := pointer + "/" + strconv.Itoa(index)
		key := location + "\x00" + name
		if _, duplicate := seen[key]; duplicate {
			diagnostics = append(diagnostics, pathDiagnostic(
				version,
				"openapi.path.parameter.duplicate",
				parameterPointer,
				"parameter duplicates name and location "+safeValue(name),
			))
		} else {
			seen[key] = struct{}{}
		}
		required, _ := booleanMember(resolved, "required")
		if !required {
			diagnostics = append(diagnostics, pathDiagnostic(
				version,
				"openapi.path.parameter.not-required",
				parameterPointer+"/required",
				"path parameter must set required to true",
			))
		}
		parameters = append(parameters, pathParameter{
			name: name, pointer: parameterPointer,
		})
	}
	return parameters, diagnostics
}

func validateOperationPathParameters(
	templateNames map[string]struct{},
	parameters map[string]pathParameter,
	operationPointer string,
	version string,
) []Diagnostic {
	var diagnostics []Diagnostic
	for _, name := range sortedParameterNames(templateNames) {
		if _, exists := parameters[name]; !exists {
			diagnostics = append(diagnostics, pathDiagnostic(
				version,
				"openapi.path.parameter.missing",
				operationPointer,
				"path template parameter is not declared: "+safeValue(name),
			))
		}
	}
	parameterNames := make([]string, 0, len(parameters))
	for name := range parameters {
		parameterNames = append(parameterNames, name)
	}
	sort.Strings(parameterNames)
	for _, name := range parameterNames {
		parameter := parameters[name]
		if _, exists := templateNames[name]; !exists {
			diagnostics = append(diagnostics, pathDiagnostic(
				version,
				"openapi.path.parameter.unused",
				parameter.pointer,
				"path parameter is absent from the template: "+safeValue(name),
			))
		}
	}
	return diagnostics
}

func sortedParameterNames(parameters map[string]struct{}) []string {
	names := make([]string, 0, len(parameters))
	for name := range parameters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func operationsAt(
	pathItem jsonvalue.Value,
	pointer string,
	dialect specversion.Dialect,
) []operationLocation {
	methods := []string{"get", "put", "post", "delete", "options", "head", "patch"}
	switch dialect {
	case specversion.DialectOAS30, specversion.DialectOAS31, specversion.DialectOAS32:
		methods = append(methods, "trace")
	}
	switch dialect {
	case specversion.DialectOAS32:
		methods = append(methods, "query")
	}
	var operations []operationLocation
	for _, method := range methods {
		operation, exists := pathItem.Lookup(method)
		if exists && operation.Kind() == jsonvalue.ObjectKind {
			operations = append(operations, operationLocation{
				value: operation, pointer: pointer + "/" + method, method: method,
			})
		}
	}
	switch dialect {
	case specversion.DialectOAS32:
		additional, exists := pathItem.Lookup("additionalOperations")
		if exists && additional.Kind() == jsonvalue.ObjectKind {
			members, _ := additional.Members()
			for _, member := range members {
				if member.Value.Kind() == jsonvalue.ObjectKind {
					operations = append(operations, operationLocation{
						value:   member.Value,
						pointer: pointer + "/additionalOperations/" + escapePointer(member.Name),
						method:  member.Name,
					})
				}
			}
		}
	}
	return operations
}

func stringMember(value jsonvalue.Value, name string) (string, bool) {
	member, exists := value.Lookup(name)
	if !exists {
		return "", false
	}
	return member.Text()
}

func booleanMember(value jsonvalue.Value, name string) (bool, bool) {
	member, exists := value.Lookup(name)
	if !exists {
		return false, false
	}
	return member.Bool()
}

func pathDiagnostic(version string, code string, pointer string, message string) Diagnostic {
	return Diagnostic{
		Code:                 code,
		Message:              message,
		Severity:             SeverityError,
		Source:               SourceDocument,
		InstanceLocation:     pointer,
		SpecificationVersion: version,
		SpecificationSection: "paths",
	}
}

func escapePointer(value string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(value)
}

func safeValue(value string) string {
	characters := []rune(value)
	if len(characters) > 80 {
		value = string(characters[:80]) + "..."
	}
	return strconv.Quote(value)
}
