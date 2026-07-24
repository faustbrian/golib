package convert

import (
	"bytes"
	"context"
	"net/url"
	"strconv"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

type oas30SwaggerConverter struct {
	ctx              context.Context
	maxNodes         int
	nodes            int
	diagnostics      []Diagnostic
	requestBodyNames map[string]string
	requestBodies    map[string]jsonvalue.Value
	securitySchemes  map[string]bool
	parameters       map[string]bool
}

func convertOpenAPIToSwagger20(
	ctx context.Context,
	root jsonvalue.Value,
	sourceDialect openapi.Dialect,
	options Options,
) (jsonvalue.Value, []Diagnostic, error) {
	converted := root
	var diagnostics []Diagnostic
	if sourceDialect == openapi.DialectOAS32 {
		target, _ := openapi.ParseVersion("3.1.2")
		members, _ := converted.Members()
		converted, _ = replaceVersion(members, target)
		var err error
		var stageDiagnostics []Diagnostic
		converted, stageDiagnostics, err = convertOAS32Document(
			ctx, converted, options.MaxDocumentNodes,
		)
		diagnostics = append(diagnostics, stageDiagnostics...)
		if err != nil {
			return jsonvalue.Value{}, nil, err
		}
	}
	if sourceDialect == openapi.DialectOAS31 ||
		sourceDialect == openapi.DialectOAS32 {
		target, _ := openapi.ParseVersion("3.0.4")
		members, _ := converted.Members()
		converted, _ = replaceVersion(members, target)
		var err error
		var stageDiagnostics []Diagnostic
		converted, stageDiagnostics, err = convertOAS31Document(
			ctx, converted, options.MaxSchemaNodes,
		)
		diagnostics = append(diagnostics, stageDiagnostics...)
		if err != nil {
			return jsonvalue.Value{}, nil, err
		}
	}
	converted, stageDiagnostics, err := convertOAS30ToSwagger20(
		ctx, converted, options.MaxDocumentNodes,
	)
	diagnostics = append(diagnostics, stageDiagnostics...)
	return converted, diagnostics, err
}

func convertOAS30ToSwagger20(
	ctx context.Context,
	root jsonvalue.Value,
	maxNodes int,
) (jsonvalue.Value, []Diagnostic, error) {
	converter := oas30SwaggerConverter{ctx: ctx, maxNodes: maxNodes}
	converted, err := converter.document(root)
	return converted, converter.diagnostics, err
}

func (converter *oas30SwaggerConverter) visit() error {
	if err := converter.ctx.Err(); err != nil {
		return err
	}
	converter.nodes++
	if converter.nodes > converter.maxNodes {
		return ErrLimitExceeded
	}
	return nil
}

func (converter *oas30SwaggerConverter) document(
	root jsonvalue.Value,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	converter.indexRequestBodyNames(root)
	converter.indexSecuritySchemes(root)
	converter.indexParameters(root)
	members, _ := root.Members()
	result := make([]jsonvalue.Member, 0, len(members)+3)
	for _, member := range members {
		switch member.Name {
		case "openapi":
			version, _ := jsonvalue.String("2.0")
			result = append(result, jsonvalue.Member{Name: "swagger", Value: version})
		case "servers":
			serverMembers, err := converter.server(member.Value, "/servers")
			if err != nil {
				return jsonvalue.Value{}, err
			}
			result = append(result, serverMembers...)
		case "components":
			componentMembers, err := converter.components(member.Value, "/components")
			if err != nil {
				return jsonvalue.Value{}, err
			}
			result = append(result, componentMembers...)
		case "paths":
			converted, err := converter.pathMap(member.Value, "/paths")
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
			result = append(result, member)
		case "security":
			member.Value = converter.securityRequirements(member.Value, "/security")
			result = append(result, member)
		default:
			result = append(result, member)
		}
	}
	return jsonvalue.Object(result)
}

func (converter *oas30SwaggerConverter) indexParameters(root jsonvalue.Value) {
	converter.parameters = make(map[string]bool)
	components, exists := root.Lookup("components")
	if !exists {
		return
	}
	parameters, exists := components.Lookup("parameters")
	if !exists {
		return
	}
	members, _ := parameters.Members()
	for _, member := range members {
		converter.parameters["#/components/parameters/"+escapePointer(member.Name)] =
			swaggerParameterSupported(member.Value)
	}
}

func swaggerParameterSupported(value jsonvalue.Value) bool {
	if value.Kind() != jsonvalue.ObjectKind {
		return false
	}
	if _, reference := value.Lookup("$ref"); reference {
		return false
	}
	location, _ := stringMember(value, "in")
	if location == "cookie" {
		return false
	}
	schema, exists := value.Lookup("schema")
	if !exists {
		content, _ := value.Lookup("content")
		members, _ := content.Members()
		if len(members) != 0 {
			schema, _ = members[0].Value.Lookup("schema")
		}
	}
	if _, reference := schema.Lookup("$ref"); reference {
		return false
	}
	typeName, _ := stringMember(schema, "type")
	return typeName != "object"
}

func (converter *oas30SwaggerConverter) indexSecuritySchemes(
	root jsonvalue.Value,
) {
	converter.securitySchemes = make(map[string]bool)
	components, exists := root.Lookup("components")
	if !exists {
		return
	}
	schemes, exists := components.Lookup("securitySchemes")
	if !exists {
		return
	}
	members, _ := schemes.Members()
	for _, member := range members {
		converter.securitySchemes[member.Name] = swaggerSecuritySchemeSupported(
			member.Value,
		)
	}
}

func (converter *oas30SwaggerConverter) indexRequestBodyNames(
	root jsonvalue.Value,
) {
	converter.requestBodyNames = make(map[string]string)
	converter.requestBodies = make(map[string]jsonvalue.Value)
	components, exists := root.Lookup("components")
	if !exists {
		return
	}
	used := make(map[string]struct{})
	if parameters, exists := components.Lookup("parameters"); exists {
		members, _ := parameters.Members()
		for _, member := range members {
			used[member.Name] = struct{}{}
		}
	}
	requestBodies, exists := components.Lookup("requestBodies")
	if !exists {
		return
	}
	members, _ := requestBodies.Members()
	for _, member := range members {
		reference := "#/components/requestBodies/" + escapePointer(member.Name)
		converter.requestBodies[reference] = member.Value
		if _, form := swaggerFormMediaType(member.Value); form {
			continue
		}
		target := member.Name
		if _, collision := used[target]; collision {
			base := member.Name + "RequestBody"
			target = base
			if _, taken := used[target]; taken {
				for suffix := 2; ; suffix++ {
					candidate := base + strconv.Itoa(suffix)
					if _, candidateTaken := used[candidate]; !candidateTaken {
						target = candidate
						break
					}
				}
			}
			converter.diagnostics = append(converter.diagnostics, Diagnostic{
				Code: "openapi.convert.component-renamed", Kind: ManualAction,
				Pointer: "/components/requestBodies/" + escapePointer(member.Name),
				Message: "the request body was renamed to avoid a Swagger parameter collision",
			})
		}
		used[target] = struct{}{}
		converter.requestBodyNames[reference] = target
	}
}

func (converter *oas30SwaggerConverter) server(
	value jsonvalue.Value,
	pointer string,
) ([]jsonvalue.Member, error) {
	if err := converter.visit(); err != nil {
		return nil, err
	}
	servers, ok := value.Elements()
	if !ok || len(servers) == 0 {
		return nil, nil
	}
	for offset := range servers[1:] {
		index := offset + 1
		converter.loss(
			pointer+"/"+strconv.Itoa(index),
			"openapi.convert.server-removed",
			"Swagger 2.0 can represent only one effective server",
		)
	}
	raw, ok := stringMember(servers[0], "url")
	if description, exists := servers[0].Lookup("description"); exists {
		if text, valid := description.Text(); !valid || text != "" {
			converter.loss(
				pointer+"/0/description",
				"openapi.convert.server-description-removed",
				"Swagger 2.0 cannot represent server descriptions",
			)
		}
	}
	if variables, exists := servers[0].Lookup("variables"); exists {
		raw = expandServerURL(raw, variables)
		converter.loss(
			pointer+"/0/variables",
			"openapi.convert.server-variables-removed",
			"Swagger 2.0 retained only server variable defaults",
		)
	}
	serverFields, _ := servers[0].Members()
	for _, field := range serverFields {
		if strings.HasPrefix(strings.ToLower(field.Name), "x-") {
			converter.loss(
				pointer+"/0/"+escapePointer(field.Name),
				"openapi.convert.server-extension-removed",
				"Swagger 2.0 cannot attach extensions to an individual server",
			)
		}
	}
	if !ok || strings.Contains(raw, "{") {
		converter.loss(
			pointer+"/0/url",
			"openapi.convert.server-removed",
			"Swagger 2.0 cannot represent this server URL",
		)
		return nil, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || (parsed.Host == "" && parsed.Scheme != "") ||
		(parsed.Host == "" && parsed.Path != "" &&
			!strings.HasPrefix(parsed.Path, "/")) {
		converter.loss(
			pointer+"/0/url",
			"openapi.convert.server-removed",
			"Swagger 2.0 cannot represent this server URL",
		)
		return nil, nil
	}
	result := make([]jsonvalue.Member, 0, 3)
	if parsed.Host != "" {
		host, _ := jsonvalue.String(parsed.Host)
		result = append(result, jsonvalue.Member{Name: "host", Value: host})
	}
	if parsed.Scheme != "" {
		scheme, _ := jsonvalue.String(parsed.Scheme)
		schemes, _ := jsonvalue.Array([]jsonvalue.Value{scheme})
		result = append(result, jsonvalue.Member{Name: "schemes", Value: schemes})
	}
	if parsed.EscapedPath() != "" {
		basePath, _ := jsonvalue.String(parsed.EscapedPath())
		result = append(result, jsonvalue.Member{Name: "basePath", Value: basePath})
	}
	return result, nil
}

func expandServerURL(raw string, variables jsonvalue.Value) string {
	members, _ := variables.Members()
	for _, member := range members {
		defaultValue, exists := member.Value.Lookup("default")
		defaultText, valid := defaultValue.Text()
		if exists && valid {
			raw = strings.ReplaceAll(raw, "{"+member.Name+"}", defaultText)
		}
	}
	return raw
}

func (converter *oas30SwaggerConverter) components(
	value jsonvalue.Value,
	pointer string,
) ([]jsonvalue.Member, error) {
	if err := converter.visit(); err != nil {
		return nil, err
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return nil, nil
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members)+2)
	var consumes []jsonvalue.Value
	var produces []jsonvalue.Value
	for _, member := range members {
		switch member.Name {
		case "schemas":
			converted, err := converter.schemaMap(
				member.Value, pointer+"/schemas",
			)
			if err != nil {
				return nil, err
			}
			result = append(result, jsonvalue.Member{
				Name: "definitions", Value: converted,
			})
		case "parameters":
			converted, err := converter.parameterMap(
				member.Value, pointer+"/parameters",
			)
			if err != nil {
				return nil, err
			}
			result = append(result, jsonvalue.Member{
				Name: "parameters", Value: converted,
			})
		case "requestBodies":
			converted, mediaTypes, err := converter.requestBodyMap(
				member.Value, pointer+"/requestBodies",
			)
			if err != nil {
				return nil, err
			}
			result = append(result, jsonvalue.Member{
				Name: "parameters", Value: converted,
			})
			consumes = append(consumes, mediaTypes...)
		case "responses":
			converted, mediaTypes, err := converter.responseMap(
				member.Value, pointer+"/responses",
			)
			if err != nil {
				return nil, err
			}
			result = append(result, jsonvalue.Member{
				Name: "responses", Value: converted,
			})
			produces = append(produces, mediaTypes...)
		case "securitySchemes":
			converted, err := converter.securitySchemeMap(
				member.Value, pointer+"/securitySchemes",
			)
			if err != nil {
				return nil, err
			}
			result = append(result, jsonvalue.Member{
				Name: "securityDefinitions", Value: converted,
			})
		default:
			converter.loss(
				pointer+"/"+escapePointer(member.Name),
				"openapi.convert.component-removed",
				"this OpenAPI component registry is not yet representable in Swagger 2.0",
			)
		}
	}
	result = mergeNamedObjectMembers(result, "parameters")
	if len(consumes) > 0 {
		value, _ := jsonvalue.Array(uniqueStrings(consumes))
		result = append(result, jsonvalue.Member{Name: "consumes", Value: value})
	}
	if len(produces) > 0 {
		value, _ := jsonvalue.Array(uniqueStrings(produces))
		result = append(result, jsonvalue.Member{Name: "produces", Value: value})
	}
	return result, nil
}

func mergeNamedObjectMembers(
	members []jsonvalue.Member,
	name string,
) []jsonvalue.Member {
	result := make([]jsonvalue.Member, 0, len(members))
	var combined []jsonvalue.Member
	for _, member := range members {
		if member.Name != name {
			result = append(result, member)
			continue
		}
		objectMembers, _ := member.Value.Members()
		combined = append(combined, objectMembers...)
	}
	if len(combined) > 0 {
		value, _ := jsonvalue.Object(combined)
		result = append(result, jsonvalue.Member{Name: name, Value: value})
	}
	return result
}

func (converter *oas30SwaggerConverter) parameterMap(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		converted, err := converter.parameter(
			member.Value,
			pointer+"/"+escapePointer(member.Name),
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		if converted.Kind() == jsonvalue.InvalidKind {
			continue
		}
		member.Value = converted
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (converter *oas30SwaggerConverter) requestBodyMap(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, []jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil, nil
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	var mediaTypes []jsonvalue.Value
	for _, member := range members {
		reference := "#/components/requestBodies/" +
			escapePointer(member.Name)
		if _, form := swaggerFormMediaType(member.Value); form {
			converter.diagnostics = append(converter.diagnostics, Diagnostic{
				Code:    "openapi.convert.form-request-body-inlined",
				Kind:    ManualAction,
				Pointer: pointer + "/" + escapePointer(member.Name),
				Message: "the reusable form request body was inlined at known operation uses",
			})
			continue
		}
		targetName := converter.requestBodyNames[reference]
		if targetName == "" {
			targetName = member.Name
		}
		converted, bodyMediaTypes, err := converter.requestBody(
			member.Value,
			pointer+"/"+escapePointer(member.Name),
		)
		if err != nil {
			return jsonvalue.Value{}, nil, err
		}
		converted = replaceObjectString(converted, "name", targetName)
		member.Name = targetName
		member.Value = converted
		result = append(result, member)
		mediaTypes = append(mediaTypes, bodyMediaTypes...)
	}
	converted, err := jsonvalue.Object(result)
	return converted, mediaTypes, err
}

func replaceObjectString(
	value jsonvalue.Value,
	name string,
	text string,
) jsonvalue.Value {
	members, _ := value.Members()
	replacement, _ := jsonvalue.String(text)
	for index := range members {
		if members[index].Name == name {
			members[index].Value = replacement
			converted, _ := jsonvalue.Object(members)
			return converted
		}
	}
	members = append(members, jsonvalue.Member{Name: name, Value: replacement})
	converted, _ := jsonvalue.Object(members)
	return converted
}

func (converter *oas30SwaggerConverter) pathMap(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	for index := range members {
		converted, err := converter.pathItem(
			members[index].Value,
			pointer+"/"+escapePointer(members[index].Name),
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		members[index].Value = converted
	}
	return jsonvalue.Object(members)
}

func (converter *oas30SwaggerConverter) pathItem(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		memberPointer := pointer + "/" + escapePointer(member.Name)
		switch member.Name {
		case "get", "put", "post", "delete", "options", "head", "patch":
			converted, err := converter.operation(member.Value, memberPointer)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "trace":
			converter.loss(
				memberPointer,
				"openapi.convert.operation-removed",
				"Swagger 2.0 cannot represent TRACE operations",
			)
			continue
		case "summary", "description":
			converter.loss(
				memberPointer,
				"openapi.convert.path-field-removed",
				"Swagger 2.0 cannot represent path item metadata",
			)
			continue
		case "servers":
			converter.loss(
				memberPointer,
				"openapi.convert.server-removed",
				"Swagger 2.0 cannot represent path-level servers",
			)
			continue
		case "parameters":
			converted, err := converter.parameterArray(member.Value, memberPointer)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		}
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (converter *oas30SwaggerConverter) operation(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members)+2)
	var produces []jsonvalue.Value
	var consumes []jsonvalue.Value
	var parameters []jsonvalue.Value
	hasParameters := false
	for _, member := range members {
		memberPointer := pointer + "/" + escapePointer(member.Name)
		switch member.Name {
		case "responses":
			converted, mediaTypes, err := converter.responseMap(
				member.Value, memberPointer,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
			produces = append(produces, mediaTypes...)
		case "parameters":
			hasParameters = true
			converted, err := converter.parameterArray(member.Value, memberPointer)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			parameters, _ = converted.Elements()
			continue
		case "requestBody":
			bodyParameters, mediaTypes, err := converter.operationRequestBody(
				member.Value, memberPointer,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			parameters = append(parameters, bodyParameters...)
			consumes = append(consumes, mediaTypes...)
			continue
		case "security":
			member.Value = converter.securityRequirements(
				member.Value, memberPointer,
			)
		case "servers", "callbacks":
			converter.loss(
				memberPointer,
				"openapi.convert.operation-field-removed",
				"Swagger 2.0 cannot represent this operation field",
			)
			continue
		}
		result = append(result, member)
	}
	if hasParameters || len(parameters) != 0 {
		array, _ := jsonvalue.Array(parameters)
		result = append(result, jsonvalue.Member{Name: "parameters", Value: array})
	}
	if len(consumes) != 0 {
		array, _ := jsonvalue.Array(uniqueStrings(consumes))
		result = append(result, jsonvalue.Member{Name: "consumes", Value: array})
	}
	if len(produces) != 0 {
		array, _ := jsonvalue.Array(uniqueStrings(produces))
		result = append(result, jsonvalue.Member{Name: "produces", Value: array})
	}
	return jsonvalue.Object(result)
}

func (converter *oas30SwaggerConverter) operationRequestBody(
	value jsonvalue.Value,
	pointer string,
) ([]jsonvalue.Value, []jsonvalue.Value, error) {
	if referenceValue, reference := value.Lookup("$ref"); reference {
		raw, _ := referenceValue.Text()
		if target, local := converter.requestBodies[raw]; local {
			if mediaType, form := swaggerFormMediaType(target); form {
				return converter.operationFormRequestBody(target, mediaType, pointer)
			}
		}
	}
	content, exists := value.Lookup("content")
	contentMembers, _ := content.Members()
	if exists && len(contentMembers) != 0 &&
		isSwaggerFormMediaType(contentMembers[0].Name) {
		return converter.operationFormRequestBody(
			value, contentMembers[0], pointer,
		)
	}
	body, mediaTypes, err := converter.requestBody(value, pointer)
	if err != nil || body.Kind() == jsonvalue.InvalidKind {
		return nil, mediaTypes, err
	}
	return []jsonvalue.Value{body}, mediaTypes, nil
}

func (converter *oas30SwaggerConverter) operationFormRequestBody(
	value jsonvalue.Value,
	selected jsonvalue.Member,
	pointer string,
) ([]jsonvalue.Value, []jsonvalue.Value, error) {
	parameters, mediaTypes, err := converter.formRequestBody(
		value, selected, pointer,
	)
	if err != nil {
		return nil, nil, err
	}
	content, _ := value.Lookup("content")
	members, _ := content.Members()
	for _, member := range members[1:] {
		if isSwaggerFormMediaType(member.Name) &&
			sameJSONValue(selected.Value, member.Value) {
			mediaType, _ := jsonvalue.String(member.Name)
			mediaTypes = append(mediaTypes, mediaType)
			continue
		}
		converter.loss(
			pointer+"/content/"+escapePointer(member.Name),
			"openapi.convert.request-media-type-removed",
			"Swagger 2.0 cannot combine this media type with formData parameters",
		)
	}
	return parameters, mediaTypes, nil
}

func swaggerFormMediaType(value jsonvalue.Value) (jsonvalue.Member, bool) {
	content, exists := value.Lookup("content")
	members, _ := content.Members()
	if !exists || len(members) == 0 || !isSwaggerFormMediaType(members[0].Name) {
		return jsonvalue.Member{}, false
	}
	return members[0], true
}

func isSwaggerFormMediaType(mediaType string) bool {
	return mediaType == "multipart/form-data" ||
		mediaType == "application/x-www-form-urlencoded"
}

func (converter *oas30SwaggerConverter) formRequestBody(
	value jsonvalue.Value,
	mediaType jsonvalue.Member,
	pointer string,
) ([]jsonvalue.Value, []jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return nil, nil, err
	}
	mediaPointer := pointer + "/content/" + escapePointer(mediaType.Name)
	if _, description := value.Lookup("description"); description {
		converter.loss(
			pointer+"/description",
			"openapi.convert.request-body-description-removed",
			"Swagger 2.0 form parameters cannot retain a shared description",
		)
	}
	requestMembers, _ := value.Members()
	for _, member := range requestMembers {
		if strings.HasPrefix(strings.ToLower(member.Name), "x-") {
			converter.loss(
				pointer+"/"+escapePointer(member.Name),
				"openapi.convert.request-body-extension-removed",
				"Swagger 2.0 form parameters cannot retain shared extensions",
			)
		}
	}
	schema, exists := mediaType.Value.Lookup("schema")
	properties, hasProperties := schema.Lookup("properties")
	propertyMembers, _ := properties.Members()
	if !exists || !hasProperties {
		converter.loss(
			mediaPointer+"/schema",
			"openapi.convert.form-schema-removed",
			"Swagger 2.0 form conversion requires an object property schema",
		)
		mediaValue, _ := jsonvalue.String(mediaType.Name)
		return nil, []jsonvalue.Value{mediaValue}, nil
	}
	required := stringValueSet(schema, "required")
	if enabledBoolean(value, "required") && len(required) == 0 {
		converter.loss(
			pointer+"/required",
			"openapi.convert.request-body-required-removed",
			"Swagger 2.0 cannot require a form body without required fields",
		)
	}
	encodings, _ := mediaType.Value.Lookup("encoding")
	schemaMembers, _ := schema.Members()
	for _, member := range schemaMembers {
		if member.Name != "type" && member.Name != "properties" &&
			member.Name != "required" {
			converter.loss(
				mediaPointer+"/schema/"+escapePointer(member.Name),
				"openapi.convert.form-schema-field-removed",
				"Swagger 2.0 cannot represent this shared form schema field",
			)
		}
	}
	parameters := make([]jsonvalue.Value, 0, len(propertyMembers))
	for _, property := range propertyMembers {
		propertyPointer := mediaPointer + "/schema/properties/" +
			escapePointer(property.Name)
		converted, err := converter.formParameter(
			property.Name,
			property.Value,
			encodings,
			propertyPointer,
			required[property.Name],
		)
		if err != nil {
			return nil, nil, err
		}
		if converted.Kind() != jsonvalue.InvalidKind {
			parameters = append(parameters, converted)
		}
	}
	mediaMembers, _ := mediaType.Value.Members()
	for _, member := range mediaMembers {
		if member.Name != "schema" && member.Name != "encoding" {
			converter.loss(
				mediaPointer+"/"+escapePointer(member.Name),
				"openapi.convert.media-type-field-removed",
				"Swagger 2.0 cannot represent this form media field",
			)
		}
	}
	mediaValue, _ := jsonvalue.String(mediaType.Name)
	return parameters, []jsonvalue.Value{mediaValue}, nil
}

func (converter *oas30SwaggerConverter) formParameter(
	name string,
	schema jsonvalue.Value,
	encodings jsonvalue.Value,
	pointer string,
	required bool,
) (jsonvalue.Value, error) {
	typeName, _ := stringMember(schema, "type")
	if typeName == "object" {
		converter.loss(
			pointer,
			"openapi.convert.form-property-removed",
			"Swagger 2.0 cannot represent nested form objects",
		)
		return jsonvalue.Value{}, nil
	}
	converted, err := converter.schema(schema, pointer)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	schemaMembers, _ := converted.Members()
	nameValue, _ := jsonvalue.String(name)
	location, _ := jsonvalue.String("formData")
	result := []jsonvalue.Member{
		{Name: "name", Value: nameValue},
		{Name: "in", Value: location},
	}
	file := typeName == "string"
	format, _ := stringMember(schema, "format")
	file = file && format == "binary"
	for _, member := range schemaMembers {
		if file && member.Name == "type" {
			fileType, _ := jsonvalue.String("file")
			member.Value = fileType
		}
		if file && member.Name == "format" {
			continue
		}
		if !swaggerParameterSchemaField(member.Name) {
			converter.loss(
				pointer+"/"+escapePointer(member.Name),
				"openapi.convert.form-property-field-removed",
				"Swagger 2.0 form parameters cannot use this schema field",
			)
			continue
		}
		result = append(result, member)
	}
	if required {
		result = append(result, jsonvalue.Member{
			Name: "required", Value: jsonvalue.Boolean(true),
		})
	}
	if typeName == "array" {
		encoding, _ := encodings.Lookup(name)
		style, _ := stringMember(encoding, "style")
		explode := enabledBoolean(encoding, "explode")
		_, hasExplode := encoding.Lookup("explode")
		format, representable := swaggerCollectionFormat(
			"query", style, explode, hasExplode, true,
		)
		if representable && format != "" {
			formatValue, _ := jsonvalue.String(format)
			result = append(result, jsonvalue.Member{
				Name: "collectionFormat", Value: formatValue,
			})
		} else if !representable {
			converter.loss(
				pointer+"/encoding",
				"openapi.convert.form-encoding-removed",
				"Swagger 2.0 cannot represent this form array encoding",
			)
		}
	}
	if encoding, exists := encodings.Lookup(name); exists {
		members, _ := encoding.Members()
		for _, member := range members {
			if member.Name != "style" && member.Name != "explode" {
				converter.loss(
					pointer+"/encoding/"+escapePointer(member.Name),
					"openapi.convert.form-encoding-removed",
					"Swagger 2.0 cannot represent this form encoding field",
				)
			}
		}
	}
	return jsonvalue.Object(result)
}

func swaggerParameterSchemaField(name string) bool {
	if strings.HasPrefix(strings.ToLower(name), "x-") {
		return true
	}
	switch name {
	case "type", "format", "description", "items", "default", "maximum",
		"exclusiveMaximum", "minimum", "exclusiveMinimum", "maxLength",
		"minLength", "pattern", "maxItems", "minItems", "uniqueItems",
		"enum", "multipleOf":
		return true
	default:
		return false
	}
}

func stringValueSet(value jsonvalue.Value, name string) map[string]bool {
	result := make(map[string]bool)
	array, exists := value.Lookup(name)
	elements, _ := array.Elements()
	if !exists {
		return result
	}
	for _, element := range elements {
		if text, ok := element.Text(); ok {
			result[text] = true
		}
	}
	return result
}

func (converter *oas30SwaggerConverter) parameterArray(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	elements, ok := value.Elements()
	if !ok {
		return value, nil
	}
	result := make([]jsonvalue.Value, 0, len(elements))
	for index := range elements {
		converted, err := converter.parameter(
			elements[index], pointer+"/"+strconv.Itoa(index),
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		if converted.Kind() != jsonvalue.InvalidKind {
			result = append(result, converted)
		}
	}
	return jsonvalue.Array(result)
}

func (converter *oas30SwaggerConverter) parameter(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	if reference, exists := value.Lookup("$ref"); exists {
		if raw, valid := reference.Text(); valid {
			if supported, local := converter.parameters[raw]; local && !supported {
				converter.loss(
					pointer+"/$ref",
					"openapi.convert.parameter-reference-removed",
					"the referenced parameter is unavailable in Swagger 2.0",
				)
				return jsonvalue.Value{}, nil
			}
		}
		converted := converter.reference(reference, pointer+"/$ref")
		return jsonvalue.Object([]jsonvalue.Member{{Name: "$ref", Value: converted}})
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members)+4)
	location, _ := stringMember(value, "in")
	if location == "cookie" {
		converter.loss(
			pointer+"/in",
			"openapi.convert.parameter-location-removed",
			"Swagger 2.0 cannot represent cookie parameters",
		)
		return jsonvalue.Value{}, nil
	}
	style, _ := stringMember(value, "style")
	explode := enabledBoolean(value, "explode")
	_, hasExplode := value.Lookup("explode")
	schemaValue, _ := value.Lookup("schema")
	contentValue, hasContent := value.Lookup("content")
	if schemaValue.Kind() == jsonvalue.InvalidKind && hasContent {
		contentMembers, _ := contentValue.Members()
		if len(contentMembers) > 0 {
			schemaValue, _ = contentMembers[0].Value.Lookup("schema")
		}
	}
	schemaType, _ := stringMember(schemaValue, "type")
	if schemaType == "object" {
		converter.loss(
			pointer+"/schema",
			"openapi.convert.parameter-schema-removed",
			"Swagger 2.0 cannot represent object-valued non-body parameters",
		)
		return jsonvalue.Value{}, nil
	}
	if _, reference := schemaValue.Lookup("$ref"); reference {
		converter.loss(
			pointer+"/schema/$ref",
			"openapi.convert.parameter-schema-removed",
			"Swagger 2.0 non-body parameters cannot reference schemas",
		)
		return jsonvalue.Value{}, nil
	}
	for _, member := range members {
		memberPointer := pointer + "/" + escapePointer(member.Name)
		switch member.Name {
		case "schema":
			converted, err := converter.schema(member.Value, memberPointer)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			schemaMembers, ok := converted.Members()
			if ok {
				result = append(result, schemaMembers...)
			}
		case "content":
			converter.loss(
				memberPointer,
				"openapi.convert.parameter-content-removed",
				"Swagger 2.0 cannot retain parameter content negotiation",
			)
			if schemaValue.Kind() == jsonvalue.InvalidKind {
				return jsonvalue.Value{}, nil
			}
			converted, err := converter.schema(schemaValue, memberPointer)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			schemaMembers, _ := converted.Members()
			result = append(result, schemaMembers...)
		case "style", "explode":
			continue
		case "deprecated", "allowReserved":
			if enabled, valid := member.Value.Bool(); valid && !enabled {
				continue
			}
			converter.loss(
				memberPointer,
				"openapi.convert.parameter-field-removed",
				"Swagger 2.0 cannot represent this Parameter Object field",
			)
			continue
		case "example", "examples":
			converter.loss(
				memberPointer,
				"openapi.convert.parameter-field-removed",
				"Swagger 2.0 cannot represent this Parameter Object field",
			)
			continue
		default:
			result = append(result, member)
		}
	}
	if format, representable := swaggerCollectionFormat(
		location, style, explode, hasExplode, schemaType == "array",
	); format != "" {
		value, _ := jsonvalue.String(format)
		result = append(result, jsonvalue.Member{
			Name: "collectionFormat", Value: value,
		})
	} else if !representable {
		converter.loss(
			pointer+"/style",
			"openapi.convert.parameter-style-removed",
			"Swagger 2.0 cannot represent this parameter serialization style",
		)
	}
	return jsonvalue.Object(result)
}

func swaggerCollectionFormat(
	location string,
	style string,
	explode bool,
	hasExplode bool,
	array bool,
) (string, bool) {
	if !array {
		switch style {
		case "", "form", "simple":
			return "", true
		default:
			return "", false
		}
	}
	if style == "" {
		switch location {
		case "query":
			style = "form"
			explode = true
		case "path", "header":
			style = "simple"
			explode = false
		}
	}
	if !hasExplode && style == "form" {
		explode = true
	}
	switch style {
	case "form":
		if location != "query" {
			return "", false
		}
		if explode {
			return "multi", true
		}
		return "csv", true
	case "spaceDelimited":
		if location == "query" && !explode {
			return "ssv", true
		}
	case "pipeDelimited":
		if location == "query" && !explode {
			return "pipes", true
		}
	case "simple":
		if (location == "path" || location == "header") && !explode {
			return "csv", true
		}
	}
	return "", false
}

func (converter *oas30SwaggerConverter) requestBody(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, []jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, nil, err
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return jsonvalue.Value{}, nil, nil
	}
	if _, exists := value.Lookup("$ref"); exists {
		reference, _ := value.Lookup("$ref")
		converted := converter.reference(reference, pointer+"/$ref")
		body, err := jsonvalue.Object([]jsonvalue.Member{{
			Name: "$ref", Value: converted,
		}})
		return body, nil, err
	}
	name, _ := jsonvalue.String("body")
	location, _ := jsonvalue.String("body")
	result := []jsonvalue.Member{
		{Name: "name", Value: name},
		{Name: "in", Value: location},
	}
	if description, exists := value.Lookup("description"); exists {
		result = append(result, jsonvalue.Member{
			Name: "description", Value: description,
		})
	}
	if required, exists := value.Lookup("required"); exists {
		result = append(result, jsonvalue.Member{Name: "required", Value: required})
	}
	members, _ := value.Members()
	for _, member := range members {
		if strings.HasPrefix(strings.ToLower(member.Name), "x-") {
			result = append(result, member)
		}
	}
	content, exists := value.Lookup("content")
	contentMembers, ok := content.Members()
	if !exists || !ok {
		body, err := jsonvalue.Object(result)
		return body, nil, err
	}
	mediaTypes := make([]jsonvalue.Value, 0, len(contentMembers))
	var selectedSchema jsonvalue.Value
	for _, mediaType := range contentMembers {
		mediaTypeValue, _ := jsonvalue.String(mediaType.Name)
		mediaTypes = append(mediaTypes, mediaTypeValue)
		if isSwaggerFormMediaType(mediaType.Name) {
			converter.loss(
				pointer+"/content/"+escapePointer(mediaType.Name),
				"openapi.convert.form-media-type-removed",
				"Swagger 2.0 form semantics require formData parameters",
			)
		}
		converter.mediaTypeLosses(
			mediaType.Value,
			pointer+"/content/"+escapePointer(mediaType.Name),
			false,
		)
		schema, exists := mediaType.Value.Lookup("schema")
		if !exists {
			continue
		}
		converted, err := converter.schema(
			schema,
			pointer+"/content/"+escapePointer(mediaType.Name)+"/schema",
		)
		if err != nil {
			return jsonvalue.Value{}, nil, err
		}
		if selectedSchema.Kind() == jsonvalue.InvalidKind {
			selectedSchema = converted
			result = append(result, jsonvalue.Member{Name: "schema", Value: converted})
			continue
		}
		if !sameJSONValue(selectedSchema, converted) {
			converter.loss(
				pointer+"/content/"+escapePointer(mediaType.Name)+"/schema",
				"openapi.convert.request-media-schema-removed",
				"Swagger 2.0 supports only one body schema across media types",
			)
		}
	}
	body, err := jsonvalue.Object(result)
	return body, mediaTypes, err
}

func (converter *oas30SwaggerConverter) responseMap(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, []jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil, nil
	}
	members, _ := value.Members()
	var mediaTypes []jsonvalue.Value
	for index := range members {
		converted, responseMediaTypes, err := converter.response(
			members[index].Value,
			pointer+"/"+escapePointer(members[index].Name),
		)
		if err != nil {
			return jsonvalue.Value{}, nil, err
		}
		members[index].Value = converted
		mediaTypes = append(mediaTypes, responseMediaTypes...)
	}
	converted, err := jsonvalue.Object(members)
	return converted, mediaTypes, err
}

func (converter *oas30SwaggerConverter) response(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, []jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, nil, err
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil, nil
	}
	if reference, exists := value.Lookup("$ref"); exists {
		converted := converter.reference(reference, pointer+"/$ref")
		object, err := jsonvalue.Object([]jsonvalue.Member{{Name: "$ref", Value: converted}})
		return object, nil, err
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	var mediaTypes []jsonvalue.Value
	var examples []jsonvalue.Member
	for _, member := range members {
		switch member.Name {
		case "headers":
			converted, err := converter.headerMap(
				member.Value, pointer+"/headers",
			)
			if err != nil {
				return jsonvalue.Value{}, nil, err
			}
			member.Value = converted
			result = append(result, member)
			continue
		case "links":
			converter.loss(
				pointer+"/links",
				"openapi.convert.response-links-removed",
				"Swagger 2.0 cannot represent response links",
			)
			continue
		case "content":
		default:
			result = append(result, member)
			continue
		}
		contentMembers, ok := member.Value.Members()
		if !ok {
			continue
		}
		var selectedSchema jsonvalue.Value
		for _, mediaType := range contentMembers {
			text, _ := jsonvalue.String(mediaType.Name)
			mediaTypes = append(mediaTypes, text)
			if example, exists := mediaType.Value.Lookup("example"); exists {
				examples = append(examples, jsonvalue.Member{
					Name: mediaType.Name, Value: example,
				})
			}
			converter.mediaTypeLosses(
				mediaType.Value,
				pointer+"/content/"+escapePointer(mediaType.Name),
				true,
			)
			schema, exists := mediaType.Value.Lookup("schema")
			if !exists {
				continue
			}
			converted, err := converter.schema(
				schema,
				pointer+"/content/"+escapePointer(mediaType.Name)+"/schema",
			)
			if err != nil {
				return jsonvalue.Value{}, nil, err
			}
			if selectedSchema.Kind() == jsonvalue.InvalidKind {
				selectedSchema = converted
				result = append(result, jsonvalue.Member{Name: "schema", Value: converted})
				continue
			}
			if !sameJSONValue(selectedSchema, converted) {
				converter.loss(
					pointer+"/content/"+escapePointer(mediaType.Name)+"/schema",
					"openapi.convert.response-media-schema-removed",
					"Swagger 2.0 supports only one response schema across media types",
				)
			}
		}
	}
	if len(examples) != 0 {
		value, _ := jsonvalue.Object(examples)
		result = append(result, jsonvalue.Member{Name: "examples", Value: value})
	}
	converted, err := jsonvalue.Object(result)
	return converted, mediaTypes, err
}

func (converter *oas30SwaggerConverter) headerMap(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	for index := range members {
		memberPointer := pointer + "/" + escapePointer(members[index].Name)
		header := members[index].Value
		if required, exists := header.Lookup("required"); exists {
			if enabled, _ := required.Bool(); enabled {
				converter.loss(
					memberPointer+"/required",
					"openapi.convert.header-field-removed",
					"Swagger 2.0 response headers cannot be required",
				)
			}
		}
		location, _ := jsonvalue.String("header")
		header, _ = appendObjectMember(header, "in", location)
		converted, err := converter.parameter(
			header, memberPointer,
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		converted, _ = withoutNamedFields(converted, "in", "required")
		members[index].Value = converted
	}
	return jsonvalue.Object(members)
}

func (converter *oas30SwaggerConverter) mediaTypeLosses(
	value jsonvalue.Value,
	pointer string,
	preserveExample bool,
) {
	members, _ := value.Members()
	for _, member := range members {
		if member.Name == "schema" || (member.Name == "example" && preserveExample) {
			continue
		}
		converter.loss(
			pointer+"/"+escapePointer(member.Name),
			"openapi.convert.media-type-field-removed",
			"Swagger 2.0 cannot represent this Media Type Object field",
		)
	}
}

func withoutNamedFields(
	value jsonvalue.Value,
	names ...string,
) (jsonvalue.Value, error) {
	dropped := make(map[string]struct{}, len(names))
	for _, name := range names {
		dropped[name] = struct{}{}
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		if _, drop := dropped[member.Name]; !drop {
			result = append(result, member)
		}
	}
	return jsonvalue.Object(result)
}

func sameJSONValue(left jsonvalue.Value, right jsonvalue.Value) bool {
	leftJSON, leftErr := left.MarshalJSON()
	rightJSON, rightErr := right.MarshalJSON()
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func (converter *oas30SwaggerConverter) schemaMap(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	for index := range members {
		converted, err := converter.schema(
			members[index].Value,
			pointer+"/"+escapePointer(members[index].Name),
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		members[index].Value = converted
	}
	return jsonvalue.Object(members)
}

func (converter *oas30SwaggerConverter) securitySchemeMap(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		converted, keep, err := converter.securityScheme(
			member.Value,
			pointer+"/"+escapePointer(member.Name),
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		if !keep {
			continue
		}
		member.Value = converted
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (converter *oas30SwaggerConverter) securityScheme(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, bool, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, false, err
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return value, false, nil
	}
	if _, reference := value.Lookup("$ref"); reference {
		converter.loss(
			pointer+"/$ref",
			"openapi.convert.security-scheme-removed",
			"Swagger 2.0 security definitions cannot be references",
		)
		return jsonvalue.Value{}, false, nil
	}
	typeName, _ := stringMember(value, "type")
	if typeName == "apiKey" {
		location, _ := stringMember(value, "in")
		switch location {
		case "header", "query":
			return value, true, nil
		}
		converter.loss(
			pointer+"/in",
			"openapi.convert.security-scheme-removed",
			"Swagger 2.0 API keys cannot use this location",
		)
		return jsonvalue.Value{}, false, nil
	}
	if typeName == "oauth2" {
		converted, ok, err := converter.oauth2SecurityScheme(value, pointer)
		return converted, ok, err
	}
	if typeName != "http" {
		converter.loss(
			pointer+"/type",
			"openapi.convert.security-scheme-removed",
			"this OpenAPI security scheme needs manual Swagger translation",
		)
		return jsonvalue.Value{}, false, nil
	}
	scheme, _ := stringMember(value, "scheme")
	if !strings.EqualFold(scheme, "basic") {
		converter.loss(
			pointer+"/scheme",
			"openapi.convert.security-scheme-removed",
			"Swagger 2.0 cannot represent this HTTP authentication scheme",
		)
		return jsonvalue.Value{}, false, nil
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		switch member.Name {
		case "type":
			basic, _ := jsonvalue.String("basic")
			member.Value = basic
			result = append(result, member)
		case "scheme", "bearerFormat":
			continue
		default:
			result = append(result, member)
		}
	}
	converted, err := jsonvalue.Object(result)
	return converted, true, err
}

func swaggerSecuritySchemeSupported(value jsonvalue.Value) bool {
	if value.Kind() != jsonvalue.ObjectKind {
		return false
	}
	if _, reference := value.Lookup("$ref"); reference {
		return false
	}
	typeName, _ := stringMember(value, "type")
	switch typeName {
	case "apiKey":
		location, _ := stringMember(value, "in")
		switch location {
		case "header", "query":
			return true
		default:
			return false
		}
	case "http":
		scheme, _ := stringMember(value, "scheme")
		return strings.EqualFold(scheme, "basic")
	case "oauth2":
		flows, exists := value.Lookup("flows")
		if !exists {
			return false
		}
		members, _ := flows.Members()
		if len(members) == 0 {
			return false
		}
		switch members[0].Name {
		case "implicit", "password", "clientCredentials", "authorizationCode":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func (converter *oas30SwaggerConverter) securityRequirements(
	value jsonvalue.Value,
	pointer string,
) jsonvalue.Value {
	elements, ok := value.Elements()
	if !ok {
		return value
	}
	result := make([]jsonvalue.Value, 0, len(elements))
	for index, element := range elements {
		members, object := element.Members()
		if !object {
			result = append(result, element)
			continue
		}
		supported := true
		for _, member := range members {
			known, exists := converter.securitySchemes[member.Name]
			if exists && !known {
				converter.loss(
					pointer+"/"+strconv.Itoa(index)+"/"+escapePointer(member.Name),
					"openapi.convert.security-requirement-removed",
					"the security alternative uses a scheme unavailable in Swagger 2.0",
				)
				supported = false
			}
		}
		if supported {
			result = append(result, element)
		}
	}
	converted, _ := jsonvalue.Array(result)
	return converted
}

func (converter *oas30SwaggerConverter) oauth2SecurityScheme(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, bool, error) {
	flows, exists := value.Lookup("flows")
	flowMembers, ok := flows.Members()
	if !exists || !ok || len(flowMembers) == 0 {
		converter.loss(
			pointer+"/flows",
			"openapi.convert.security-scheme-removed",
			"Swagger 2.0 requires one OAuth flow",
		)
		return jsonvalue.Value{}, false, nil
	}
	flowNames := map[string]string{
		"implicit":          "implicit",
		"password":          "password",
		"clientCredentials": "application",
		"authorizationCode": "accessCode",
	}
	selected := flowMembers[0]
	flowName, supported := flowNames[selected.Name]
	if !supported {
		converter.loss(
			pointer+"/flows/"+escapePointer(selected.Name),
			"openapi.convert.security-scheme-removed",
			"Swagger 2.0 cannot represent this OAuth flow",
		)
		return jsonvalue.Value{}, false, nil
	}
	for _, extra := range flowMembers[1:] {
		converter.loss(
			pointer+"/flows/"+escapePointer(extra.Name),
			"openapi.convert.oauth-flow-removed",
			"Swagger 2.0 can represent only one OAuth flow per scheme",
		)
	}
	typeValue, _ := jsonvalue.String("oauth2")
	flowValue, _ := jsonvalue.String(flowName)
	result := []jsonvalue.Member{
		{Name: "type", Value: typeValue},
		{Name: "flow", Value: flowValue},
	}
	schemeFields, _ := value.Members()
	for _, field := range schemeFields {
		if field.Name == "description" ||
			strings.HasPrefix(strings.ToLower(field.Name), "x-") {
			result = append(result, field)
		}
	}
	flowFields, _ := selected.Value.Members()
	for _, field := range flowFields {
		switch field.Name {
		case "authorizationUrl", "tokenUrl", "scopes":
			result = append(result, field)
		case "refreshUrl":
			converter.loss(
				pointer+"/flows/"+escapePointer(selected.Name)+"/refreshUrl",
				"openapi.convert.oauth-field-removed",
				"Swagger 2.0 cannot represent OAuth refresh URLs",
			)
		default:
			if strings.HasPrefix(strings.ToLower(field.Name), "x-") {
				result = append(result, field)
			}
		}
	}
	converted, err := jsonvalue.Object(result)
	return converted, true, err
}

func (converter *oas30SwaggerConverter) schema(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		memberPointer := pointer + "/" + escapePointer(member.Name)
		switch member.Name {
		case "$ref":
			member.Value = converter.reference(member.Value, memberPointer)
		case "properties":
			converted, err := converter.schemaMap(member.Value, memberPointer)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "items", "additionalProperties":
			converted, err := converter.schema(member.Value, memberPointer)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "allOf":
			converted, err := converter.schemaArray(member.Value, memberPointer)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "discriminator":
			propertyName, exists := member.Value.Lookup("propertyName")
			_, mapping := member.Value.Lookup("mapping")
			if !exists {
				converter.loss(
					memberPointer,
					"openapi.convert.discriminator-removed",
					"Swagger 2.0 requires a discriminator property name",
				)
				continue
			}
			member.Value = propertyName
			if mapping {
				converter.loss(
					memberPointer+"/mapping",
					"openapi.convert.discriminator-mapping-removed",
					"Swagger 2.0 cannot represent discriminator mappings",
				)
			}
		case "nullable", "writeOnly", "deprecated":
			if enabled, valid := member.Value.Bool(); valid && !enabled {
				continue
			}
			converter.loss(
				memberPointer,
				"openapi.convert.schema-keyword-removed",
				"Swagger 2.0 cannot represent this OpenAPI Schema keyword",
			)
			continue
		case "not", "oneOf", "anyOf":
			converter.loss(
				memberPointer,
				"openapi.convert.schema-keyword-removed",
				"Swagger 2.0 cannot represent this OpenAPI Schema keyword",
			)
			continue
		}
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (converter *oas30SwaggerConverter) schemaArray(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	elements, ok := value.Elements()
	if !ok {
		return value, nil
	}
	for index := range elements {
		converted, err := converter.schema(
			elements[index], pointer+"/"+strconv.Itoa(index),
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		elements[index] = converted
	}
	return jsonvalue.Array(elements)
}

func (converter *oas30SwaggerConverter) reference(
	value jsonvalue.Value,
	pointer string,
) jsonvalue.Value {
	raw, ok := value.Text()
	if !ok {
		return value
	}
	if !strings.HasPrefix(raw, "#") {
		converter.diagnostics = append(converter.diagnostics, Diagnostic{
			Code: "openapi.convert.external-reference", Kind: ManualAction,
			Pointer: pointer,
			Message: "verify that the external resource is also converted to Swagger 2.0",
		})
	}
	if target, exists := converter.requestBodyNames[raw]; exists {
		converted, _ := jsonvalue.String("#/parameters/" + escapePointer(target))
		return converted
	}
	for source, target := range map[string]string{
		"#/components/schemas/":       "#/definitions/",
		"#/components/parameters/":    "#/parameters/",
		"#/components/requestBodies/": "#/parameters/",
		"#/components/responses/":     "#/responses/",
	} {
		if strings.HasPrefix(raw, source) {
			converted, _ := jsonvalue.String(target + strings.TrimPrefix(raw, source))
			return converted
		}
	}
	return value
}

func (converter *oas30SwaggerConverter) loss(
	pointer string,
	code string,
	message string,
) {
	converter.diagnostics = append(converter.diagnostics, Diagnostic{
		Code: code, Kind: Loss, Pointer: pointer, Message: message,
	})
}

func uniqueStrings(values []jsonvalue.Value) []jsonvalue.Value {
	seen := make(map[string]struct{}, len(values))
	result := make([]jsonvalue.Value, 0, len(values))
	for _, value := range values {
		text, ok := value.Text()
		if !ok {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		result = append(result, value)
	}
	return result
}
