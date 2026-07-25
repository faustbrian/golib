package validate

import (
	"context"
	"strconv"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

type locatedParameter struct {
	value   jsonvalue.Value
	pointer string
	name    string
}

func validateParameters(
	ctx context.Context,
	document openapi.Document,
	options Options,
) []Diagnostic {
	dialect := document.SpecificationVersion().Dialect()
	if dialect == specversion.DialectSwagger20 {
		return validateSwaggerParameters(ctx, document, options)
	}
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	for _, parameter := range allParameterObjects(document) {
		diagnostics = append(
			diagnostics,
			validateParameter(parameter.value, parameter.pointer, version, dialect)...,
		)
	}
	diagnostics = append(
		diagnostics,
		validateParameterIdentityUniqueness(ctx, document, options)...,
	)
	if dialect == specversion.DialectOAS32 {
		diagnostics = append(
			diagnostics,
			validateQueryStringScopes(document.Raw(), version, dialect)...,
		)
	}
	return diagnostics
}

func validateSwaggerParameters(
	ctx context.Context,
	document openapi.Document,
	options Options,
) []Diagnostic {
	version := document.SpecificationVersion().String()
	resource := validationResource(document, options.ReferenceResourceURI)
	var diagnostics []Diagnostic
	for _, parameter := range swaggerParameterObjects(document.Raw()) {
		resolved, ok := resolveReferencedObjectWithPolicy(
			ctx,
			resource,
			parameter.value,
			options.ReferenceResolver,
			options.ReferenceLimits,
		)
		if !ok {
			continue
		}
		diagnostics = append(
			diagnostics,
			validateSwaggerParameter(resolved, parameter.pointer, version)...,
		)
	}
	diagnostics = append(
		diagnostics,
		validateSwaggerFileParameterConsumes(ctx, document, options)...,
	)
	diagnostics = append(
		diagnostics,
		validateParameterIdentityUniqueness(ctx, document, options)...,
	)
	return diagnostics
}

func swaggerParameterObjects(root jsonvalue.Value) []locatedParameter {
	var result []locatedParameter
	definitions, exists := root.Lookup("parameters")
	if exists && definitions.Kind() == jsonvalue.ObjectKind {
		members, _ := definitions.Members()
		for _, member := range members {
			result = appendSwaggerParameter(
				result,
				member.Value,
				"/parameters/"+escapePointer(member.Name),
			)
		}
	}
	paths, exists := root.Lookup("paths")
	if !exists || paths.Kind() != jsonvalue.ObjectKind {
		return result
	}
	members, _ := paths.Members()
	for _, member := range members {
		if !strings.HasPrefix(member.Name, "/") ||
			member.Value.Kind() != jsonvalue.ObjectKind {
			continue
		}
		pathPointer := "/paths/" + escapePointer(member.Name)
		result = appendSwaggerParameterArray(
			result,
			member.Value,
			pathPointer+"/parameters",
		)
		for _, operation := range operationsAt(
			member.Value,
			pathPointer,
			specversion.DialectSwagger20,
		) {
			result = appendSwaggerParameterArray(
				result,
				operation.value,
				operation.pointer+"/parameters",
			)
		}
	}
	return result
}

func appendSwaggerParameterArray(
	result []locatedParameter,
	container jsonvalue.Value,
	pointer string,
) []locatedParameter {
	parameters, exists := container.Lookup("parameters")
	if !exists || parameters.Kind() != jsonvalue.ArrayKind {
		return result
	}
	elements, _ := parameters.Elements()
	for index, element := range elements {
		result = appendSwaggerParameter(
			result,
			element,
			pointer+"/"+strconv.Itoa(index),
		)
	}
	return result
}

func appendSwaggerParameter(
	result []locatedParameter,
	value jsonvalue.Value,
	pointer string,
) []locatedParameter {
	if value.Kind() != jsonvalue.ObjectKind {
		return result
	}
	if referenceValue, referenced := value.Lookup("$ref"); referenced {
		rawReference, valid := referenceValue.Text()
		if !valid || strings.HasPrefix(rawReference, "#") {
			return result
		}
	}
	return append(result, locatedParameter{value: value, pointer: pointer})
}

func validateSwaggerParameter(
	parameter jsonvalue.Value,
	pointer string,
	version string,
) []Diagnostic {
	location, _ := stringMember(parameter, "in")
	parameterType, _ := stringMember(parameter, "type")
	collectionFormat, hasCollectionFormat := stringMember(parameter, "collectionFormat")
	var diagnostics []Diagnostic
	diagnostics = append(diagnostics, validateSwaggerTypedDefaults(
		parameter,
		pointer,
		version,
		"parameter-object",
	)...)
	if location != "body" && !validSwaggerParameterType(parameterType) {
		diagnostics = append(diagnostics, swaggerParameterDiagnostic(
			version,
			"swagger.parameter.type.invalid",
			pointer+"/type",
			"non-body parameter type must be string, number, integer, boolean, array, or file",
		))
	}
	if location == "path" {
		required, hasRequired := booleanMember(parameter, "required")
		if !hasRequired || !required {
			diagnostics = append(diagnostics, swaggerParameterDiagnostic(
				version,
				"openapi.path.parameter.not-required",
				pointer+"/required",
				"path parameter must set required to true",
			))
		}
	}
	if parameterType == "file" && location != "formData" {
		diagnostics = append(diagnostics, swaggerParameterDiagnostic(
			version,
			"swagger.parameter.file.location",
			pointer+"/in",
			"file parameters must use the formData location",
		))
	}
	if hasCollectionFormat {
		switch parameterType {
		case "array":
		default:
			diagnostics = append(diagnostics, swaggerParameterDiagnostic(
				version,
				"swagger.parameter.collection-format.non-array",
				pointer+"/collectionFormat",
				"collectionFormat applies only to array parameters",
			))
		}
	}
	if collectionFormat == "multi" && location != "query" && location != "formData" {
		diagnostics = append(diagnostics, swaggerParameterDiagnostic(
			version,
			"swagger.parameter.collection-format.multi-location",
			pointer+"/collectionFormat",
			"multi collectionFormat applies only to query or formData parameters",
		))
	}
	if _, hasAllowEmpty := parameter.Lookup("allowEmptyValue"); hasAllowEmpty &&
		location != "query" && location != "formData" {
		diagnostics = append(diagnostics, swaggerParameterDiagnostic(
			version,
			"swagger.parameter.allow-empty.invalid",
			pointer+"/allowEmptyValue",
			"allowEmptyValue applies only to query or formData parameters",
		))
	}
	return diagnostics
}

func validSwaggerParameterType(parameterType string) bool {
	switch parameterType {
	case "string", "number", "integer", "boolean", "array", "file":
		return true
	default:
		return false
	}
}

func validateSwaggerFileParameterConsumes(
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
	rootConsumes, _ := swaggerConsumes(root)
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	pathMembers, _ := paths.Members()
	for _, path := range pathMembers {
		if !strings.HasPrefix(path.Name, "/") ||
			path.Value.Kind() != jsonvalue.ObjectKind {
			continue
		}
		pathPointer := "/paths/" + escapePointer(path.Name)
		pathItem, pathItemResource, resolved :=
			resolveReferencedObjectResourceWithPolicy(
				ctx,
				resource,
				path.Value,
				options.ReferenceResolver,
				options.ReferenceLimits,
			)
		if !resolved {
			continue
		}
		inherited := resolvedSwaggerParameters(
			ctx,
			pathItemResource,
			pathItem,
			pathPointer+"/parameters",
			options.ReferenceResolver,
			options.ReferenceLimits,
		)
		for _, operation := range operationsAt(
			pathItem, pathPointer, specversion.DialectSwagger20,
		) {
			consumes, overridden := swaggerConsumes(operation.value)
			if !overridden {
				consumes = rootConsumes
			}
			effective := append([]locatedParameter(nil), inherited...)
			overrides := resolvedSwaggerParameters(
				ctx,
				pathItemResource,
				operation.value,
				operation.pointer+"/parameters",
				options.ReferenceResolver,
				options.ReferenceLimits,
			)
			effective = mergeSwaggerParameters(effective, overrides)
			bodyCount := 0
			hasFormData := false
			for _, parameter := range effective {
				location, _ := stringMember(parameter.value, "in")
				if location == "body" {
					bodyCount++
				}
				hasFormData = hasFormData || location == "formData"
			}
			if effectiveSwaggerBodyConflict(
				bodyCount,
				countSwaggerParametersAt(inherited, "body"),
				countSwaggerParametersAt(overrides, "body"),
			) {
				diagnostics = append(diagnostics, swaggerParameterDiagnostic(
					version,
					"swagger.parameter.body.multiple",
					operation.pointer+"/parameters",
					"effective operation parameters must not contain more than one body parameter",
				))
			}
			if bodyCount > 0 && hasFormData {
				diagnostics = append(diagnostics, swaggerParameterDiagnostic(
					version,
					"swagger.parameter.body-and-form-data",
					operation.pointer+"/parameters",
					"body and formData parameters must not share an operation",
				))
			}
			if validSwaggerFileConsumes(consumes) {
				continue
			}
			for _, parameter := range effective {
				parameterType, _ := stringMember(parameter.value, "type")
				location, _ := stringMember(parameter.value, "in")
				if parameterType != "file" || location != "formData" {
					continue
				}
				diagnostics = append(diagnostics, swaggerParameterDiagnostic(
					version,
					"swagger.parameter.file.consumes",
					parameter.pointer,
					"file parameters require only multipart/form-data or application/x-www-form-urlencoded consumes values",
				))
			}
		}
	}
	return diagnostics
}

func effectiveSwaggerBodyConflict(bodyCount, inheritedCount, overrideCount int) bool {
	if bodyCount <= 1 {
		return false
	}
	if inheritedCount > 1 {
		return false
	}
	if overrideCount > 1 {
		return false
	}
	return true
}

func resolvedSwaggerParameters(
	ctx context.Context,
	resource reference.Resource,
	container jsonvalue.Value,
	pointer string,
	resolver reference.Resolver,
	limits reference.Limits,
) []locatedParameter {
	parameters, exists := container.Lookup("parameters")
	if !exists || parameters.Kind() != jsonvalue.ArrayKind {
		return nil
	}
	elements, _ := parameters.Elements()
	result := make([]locatedParameter, 0, len(elements))
	for index, parameter := range elements {
		resolved, ok := resolveReferencedObjectWithPolicy(
			ctx, resource, parameter, resolver, limits,
		)
		if !ok {
			continue
		}
		result = append(result, locatedParameter{
			value:   resolved,
			pointer: pointer + "/" + strconv.Itoa(index),
		})
	}
	return result
}

func swaggerParameterIdentity(parameter jsonvalue.Value) string {
	location, _ := stringMember(parameter, "in")
	name, _ := stringMember(parameter, "name")
	return location + "\x00" + name
}

func countSwaggerParametersAt(parameters []locatedParameter, location string) int {
	count := 0
	for _, parameter := range parameters {
		actual, _ := stringMember(parameter.value, "in")
		if actual == location {
			count++
		}
	}
	return count
}

func mergeSwaggerParameters(
	inherited []locatedParameter,
	overrides []locatedParameter,
) []locatedParameter {
	positions := make(map[string]int)
	for index, parameter := range inherited {
		positions[swaggerParameterIdentity(parameter.value)] = index
	}
	for _, parameter := range overrides {
		identity := swaggerParameterIdentity(parameter.value)
		if index, exists := positions[identity]; exists {
			inherited[index] = parameter
			continue
		}
		positions[identity] = len(inherited)
		inherited = append(inherited, parameter)
	}
	return inherited
}

func swaggerConsumes(owner jsonvalue.Value) ([]string, bool) {
	value, exists := owner.Lookup("consumes")
	if !exists {
		return nil, false
	}
	if value.Kind() != jsonvalue.ArrayKind {
		return nil, true
	}
	elements, _ := value.Elements()
	result := make([]string, 0, len(elements))
	for _, element := range elements {
		mediaType, ok := element.Text()
		if !ok {
			return nil, true
		}
		result = append(result, mediaType)
	}
	return result, true
}

func validSwaggerFileConsumes(consumes []string) bool {
	if len(consumes) == 0 {
		return false
	}
	for _, mediaType := range consumes {
		if mediaType != "multipart/form-data" &&
			mediaType != "application/x-www-form-urlencoded" {
			return false
		}
	}
	return true
}

func swaggerParameterDiagnostic(
	version string,
	code string,
	pointer string,
	message string,
) Diagnostic {
	diagnostic := parameterDiagnostic(version, code, pointer, message)
	diagnostic.SpecificationSection = "swagger-parameter-object"
	return diagnostic
}

func validateParameterIdentityUniqueness(
	ctx context.Context,
	document openapi.Document,
	options Options,
) []Diagnostic {
	root := document.Raw()
	version := document.SpecificationVersion().String()
	dialect := document.SpecificationVersion().Dialect()
	resource := validationResource(document, options.ReferenceResourceURI)
	paths, exists := root.Lookup("paths")
	if !exists || paths.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	var diagnostics []Diagnostic
	members, _ := paths.Members()
	for _, member := range members {
		if !strings.HasPrefix(member.Name, "/") ||
			member.Value.Kind() != jsonvalue.ObjectKind {
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
		diagnostics = append(
			diagnostics,
			parameterIdentityDiagnostics(
				ctx,
				pathItemResource,
				pathItem,
				pathPointer+"/parameters",
				version,
				dialect,
				options.ReferenceResolver,
				options.ReferenceLimits,
			)...,
		)
		for _, operation := range operationsAt(pathItem, pathPointer, dialect) {
			diagnostics = append(
				diagnostics,
				parameterIdentityDiagnostics(
					ctx,
					pathItemResource,
					operation.value,
					operation.pointer+"/parameters",
					version,
					dialect,
					options.ReferenceResolver,
					options.ReferenceLimits,
				)...,
			)
		}
	}
	return diagnostics
}

func parameterIdentityDiagnostics(
	ctx context.Context,
	resource reference.Resource,
	container jsonvalue.Value,
	pointer string,
	version string,
	dialect specversion.Dialect,
	resolver reference.Resolver,
	limits reference.Limits,
) []Diagnostic {
	parameters, exists := container.Lookup("parameters")
	if !exists || parameters.Kind() != jsonvalue.ArrayKind {
		return nil
	}
	seen := make(map[string]struct{})
	bodySeen := false
	var diagnostics []Diagnostic
	elements, _ := parameters.Elements()
	for index, parameter := range elements {
		if parameter.Kind() != jsonvalue.ObjectKind {
			continue
		}
		resolved, ok := resolveReferencedObjectWithPolicy(
			ctx, resource, parameter, resolver, limits,
		)
		if !ok {
			continue
		}
		location, hasLocation := stringMember(resolved, "in")
		name, hasName := stringMember(resolved, "name")
		if !hasLocation || !hasName {
			continue
		}
		if location == "header" && ignoredHeaderParameter(name) {
			continue
		}
		if dialect == specversion.DialectSwagger20 && location == "body" {
			if bodySeen {
				diagnostics = append(diagnostics, swaggerParameterDiagnostic(
					version,
					"swagger.parameter.body.multiple",
					pointer+"/"+strconv.Itoa(index),
					"parameter list must not contain more than one body parameter",
				))
			}
			bodySeen = true
		}
		if location == "path" {
			// validatePaths reports path-specific duplicate diagnostics.
			continue
		}
		switch location {
		case "header":
			name = strings.ToLower(name)
		}
		identity := location + "\x00" + name
		if _, duplicate := seen[identity]; duplicate {
			diagnostics = append(diagnostics, parameterDiagnostic(
				version,
				"openapi.parameter.duplicate",
				pointer+"/"+strconv.Itoa(index),
				"parameter list contains a duplicate name and location",
			))
			continue
		}
		seen[identity] = struct{}{}
	}
	return diagnostics
}

type parameterScope map[string]string

func validateQueryStringScopes(
	root jsonvalue.Value,
	version string,
	dialect specversion.Dialect,
) []Diagnostic {
	paths, exists := root.Lookup("paths")
	if !exists || paths.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	var diagnostics []Diagnostic
	members, _ := paths.Members()
	for _, member := range members {
		if !strings.HasPrefix(member.Name, "/") ||
			member.Value.Kind() != jsonvalue.ObjectKind {
			continue
		}
		pathPointer := "/paths/" + escapePointer(member.Name)
		pathScope := parameterLocations(member.Value)
		diagnostics = append(
			diagnostics,
			queryStringScopeDiagnostics(version, pathPointer+"/parameters", pathScope)...,
		)
		for _, operation := range operationsAt(member.Value, pathPointer, dialect) {
			effective := make(parameterScope, len(pathScope))
			for identity, location := range pathScope {
				effective[identity] = location
			}
			for identity, location := range parameterLocations(operation.value) {
				effective[identity] = location
			}
			diagnostics = append(
				diagnostics,
				queryStringScopeDiagnostics(
					version,
					operation.pointer+"/parameters",
					effective,
				)...,
			)
		}
	}
	return diagnostics
}

func parameterLocations(container jsonvalue.Value) parameterScope {
	result := make(parameterScope)
	parameters, exists := container.Lookup("parameters")
	if !exists || parameters.Kind() != jsonvalue.ArrayKind {
		return result
	}
	elements, _ := parameters.Elements()
	for _, parameter := range elements {
		if parameter.Kind() != jsonvalue.ObjectKind {
			continue
		}
		if _, reference := parameter.Lookup("$ref"); reference {
			continue
		}
		location, exists := stringMember(parameter, "in")
		if !exists {
			continue
		}
		name, exists := stringMember(parameter, "name")
		if !exists {
			continue
		}
		if location == "header" {
			name = strings.ToLower(name)
		}
		result[location+"\x00"+name] = location
	}
	return result
}

func queryStringScopeDiagnostics(
	version string,
	pointer string,
	scope parameterScope,
) []Diagnostic {
	query := 0
	queryString := 0
	for _, location := range scope {
		switch location {
		case "query":
			query++
		case "querystring":
			queryString++
		}
	}
	var diagnostics []Diagnostic
	if queryString > 1 {
		diagnostics = append(diagnostics, parameterDiagnostic(
			version,
			"openapi.parameter.querystring.multiple",
			pointer,
			"querystring parameter must not appear more than once in an operation scope",
		))
	}
	if queryString > 0 && query > 0 {
		diagnostics = append(diagnostics, parameterDiagnostic(
			version,
			"openapi.parameter.querystring.with-query",
			pointer,
			"querystring and query parameters must not share an operation scope",
		))
	}
	return diagnostics
}

func parameterObjects(document openapi.Document) []locatedParameter {
	all := allParameterObjects(document)
	result := make([]locatedParameter, 0, len(all))
	for _, parameter := range all {
		if ignoredHeaderParameterObject(parameter.value) {
			continue
		}
		result = append(result, parameter)
	}
	return result
}

func allParameterObjects(document openapi.Document) []locatedParameter {
	root := document.Raw()
	var result []locatedParameter
	components, exists := root.Lookup("components")
	if exists && components.Kind() == jsonvalue.ObjectKind {
		parameters, exists := components.Lookup("parameters")
		if exists && parameters.Kind() == jsonvalue.ObjectKind {
			members, _ := parameters.Members()
			for _, member := range members {
				result = appendParameter(
					result,
					member.Value,
					"/components/parameters/"+escapePointer(member.Name),
				)
			}
		}
	}
	for _, pathItem := range documentPathItems(document) {
		result = appendParameterArray(
			result,
			pathItem.value,
			pathItem.pointer+"/parameters",
		)
	}
	for _, operation := range documentOperations(document) {
		result = appendParameterArray(
			result,
			operation.value,
			operation.pointer+"/parameters",
		)
	}
	return result
}

func appendParameterArray(
	result []locatedParameter,
	container jsonvalue.Value,
	pointer string,
) []locatedParameter {
	parameters, exists := container.Lookup("parameters")
	if !exists || parameters.Kind() != jsonvalue.ArrayKind {
		return result
	}
	elements, _ := parameters.Elements()
	for index, element := range elements {
		result = appendParameter(
			result,
			element,
			pointer+"/"+strconv.Itoa(index),
		)
	}
	return result
}

func appendParameter(
	result []locatedParameter,
	value jsonvalue.Value,
	pointer string,
) []locatedParameter {
	if value.Kind() != jsonvalue.ObjectKind {
		return result
	}
	if _, reference := value.Lookup("$ref"); reference {
		return result
	}
	return append(result, locatedParameter{value: value, pointer: pointer})
}

func validateParameter(
	parameter jsonvalue.Value,
	pointer string,
	version string,
	dialect specversion.Dialect,
) []Diagnostic {
	location, hasLocation := stringMember(parameter, "in")
	if !hasLocation {
		return nil
	}
	var diagnostics []Diagnostic
	name, _ := stringMember(parameter, "name")
	if location == "header" && ignoredHeaderParameter(name) {
		return []Diagnostic{{
			Code:                 "openapi.parameter.header.ignored",
			Message:              "Accept, Content-Type, and Authorization header parameters are ignored",
			Severity:             SeverityWarning,
			Source:               SourceDocument,
			InstanceLocation:     pointer,
			SpecificationVersion: version,
			SpecificationSection: "parameter-object",
		}}
	}
	if location == "path" {
		required, hasRequired := booleanMember(parameter, "required")
		if !hasRequired || !required {
			diagnostics = append(diagnostics, parameterDiagnostic(
				version,
				"openapi.path.parameter.not-required",
				pointer+"/required",
				"path parameter must set required to true",
			))
		}
	}
	if dialect == specversion.DialectOAS32 && location == "querystring" {
		for _, field := range []struct {
			name string
			code string
		}{
			{name: "style", code: "openapi.parameter.querystring.style"},
			{name: "explode", code: "openapi.parameter.querystring.explode"},
			{name: "allowReserved", code: "openapi.parameter.querystring.allow-reserved"},
			{name: "schema", code: "openapi.parameter.querystring.schema"},
		} {
			if _, exists := parameter.Lookup(field.name); exists {
				diagnostics = append(diagnostics, parameterDiagnostic(
					version,
					field.code,
					pointer+"/"+field.name,
					field.name+" must not be used with a querystring parameter",
				))
			}
		}
	}
	if style, hasStyle := stringMember(parameter, "style"); hasStyle &&
		location != "querystring" && !validStyle(location, style, dialect) {
		diagnostics = append(diagnostics, parameterDiagnostic(
			version,
			"openapi.parameter.style.invalid-for-location",
			pointer+"/style",
			"parameter style is not valid for "+safeValue(location),
		))
	}
	_, hasSchema := parameter.Lookup("schema")
	content, hasContent := parameter.Lookup("content")
	if location == "cookie" && hasSchema && cookieSchemaNonportable(version) {
		diagnostic := parameterDiagnostic(
			version,
			"openapi.parameter.cookie.schema.nonportable",
			pointer+"/schema",
			"cookie parameters should use text/plain content when schema serialization is not appropriate",
		)
		diagnostic.Severity = SeverityWarning
		diagnostics = append(diagnostics, diagnostic)
	}
	if hasSchema && hasContent {
		diagnostics = append(diagnostics, parameterDiagnostic(
			version,
			"openapi.parameter.schema-and-content",
			pointer,
			"parameter must not define both schema and content",
		))
	}
	if !hasSchema && !hasContent {
		diagnostics = append(diagnostics, parameterDiagnostic(
			version,
			"openapi.parameter.representation.missing",
			pointer,
			"parameter must define schema or content",
		))
	}
	if hasContent && content.Kind() == jsonvalue.ObjectKind {
		members, _ := content.Members()
		if len(members) != 1 {
			diagnostics = append(diagnostics, parameterDiagnostic(
				version,
				"openapi.parameter.content.multiple",
				pointer+"/content",
				"parameter content must contain exactly one media type",
			))
		}
	}
	if _, hasAllowReserved := parameter.Lookup("allowReserved"); hasAllowReserved &&
		location != "query" && location != "querystring" {
		diagnostics = append(diagnostics, parameterDiagnostic(
			version,
			"openapi.parameter.allow-reserved.invalid",
			pointer+"/allowReserved",
			"allowReserved applies only to query parameters",
		))
	}
	if _, hasAllowEmpty := parameter.Lookup("allowEmptyValue"); hasAllowEmpty {
		if location != "query" {
			diagnostics = append(diagnostics, parameterDiagnostic(
				version,
				"openapi.parameter.allow-empty.invalid",
				pointer+"/allowEmptyValue",
				"allowEmptyValue applies only to query parameters",
			))
		}
		if allowEmptyValueDeprecated(version) {
			diagnostic := parameterDiagnostic(
				version,
				"openapi.parameter.allow-empty.deprecated",
				pointer+"/allowEmptyValue",
				"allowEmptyValue is deprecated and not recommended",
			)
			diagnostic.Severity = SeverityWarning
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	_, hasExample := parameter.Lookup("example")
	_, hasExamples := parameter.Lookup("examples")
	if hasExample && hasExamples {
		diagnostics = append(diagnostics, parameterDiagnostic(
			version,
			"openapi.parameter.examples.conflict",
			pointer,
			"parameter must not define both example and examples",
		))
	}
	return diagnostics
}

func cookieSchemaNonportable(version string) bool {
	switch version {
	case "3.0.4", "3.1.1", "3.1.2":
		return true
	default:
		return false
	}
}

func allowEmptyValueDeprecated(version string) bool {
	return version != "3.0.0" && version != "3.0.1"
}

func ignoredHeaderParameter(name string) bool {
	return strings.EqualFold(name, "Accept") ||
		strings.EqualFold(name, "Content-Type") ||
		strings.EqualFold(name, "Authorization")
}

func ignoredHeaderParameterObject(parameter jsonvalue.Value) bool {
	location, hasLocation := stringMember(parameter, "in")
	name, hasName := stringMember(parameter, "name")
	return hasLocation && hasName && location == "header" &&
		ignoredHeaderParameter(name)
}

func validStyle(location string, style string, dialect specversion.Dialect) bool {
	styles := map[string]map[string]struct{}{
		"path": {
			"matrix": {}, "label": {}, "simple": {},
		},
		"query": {
			"form": {}, "spaceDelimited": {}, "pipeDelimited": {}, "deepObject": {},
		},
		"header": {"simple": {}},
		"cookie": {"form": {}},
	}
	if dialect == specversion.DialectOAS32 {
		styles["cookie"]["cookie"] = struct{}{}
	}
	allowed, exists := styles[location]
	if !exists {
		return false
	}
	_, exists = allowed[style]
	return exists
}

func parameterDiagnostic(
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
		SpecificationSection: "parameter-object",
	}
}
