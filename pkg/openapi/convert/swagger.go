package convert

import (
	"context"
	"strconv"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

type swagger20Converter struct {
	ctx              context.Context
	maxDocumentNodes int
	documentNodes    int
	maxSchemaNodes   int
	schemaNodes      int
	diagnostics      []Diagnostic
	bodyRefs         map[string]struct{}
	formRefs         map[string]jsonvalue.Value
	parameterRefs    map[string]jsonvalue.Value
}

type swaggerParameterInput struct {
	value   jsonvalue.Value
	pointer string
}

func (converter *swagger20Converter) loss(
	pointer string,
	code string,
	message string,
) {
	converter.diagnostics = append(converter.diagnostics, Diagnostic{
		Code: code, Kind: Loss, Pointer: pointer, Message: message,
	})
}

func (converter *swagger20Converter) manual(
	pointer string,
	code string,
	message string,
) {
	converter.diagnostics = append(converter.diagnostics, Diagnostic{
		Code: code, Kind: ManualAction, Pointer: pointer, Message: message,
	})
}

func (converter *swagger20Converter) defaultMediaType(pointer string) {
	converter.manual(
		pointer,
		"openapi.convert.swagger-default-media-type",
		"application/json was selected because Swagger media types were absent",
	)
}

func (converter *swagger20Converter) referenceSiblingLoss(
	value jsonvalue.Value,
	pointer string,
) {
	members, _ := value.Members()
	for _, member := range members {
		if member.Name != "$ref" {
			converter.loss(
				pointer+"/"+escapePointer(member.Name),
				"openapi.convert.swagger-reference-sibling",
				"this ignored Swagger reference sibling was removed",
			)
		}
	}
}

func (converter *swagger20Converter) externalReference(
	reference string,
	pointer string,
	subject string,
) {
	if strings.HasPrefix(reference, "#") {
		return
	}
	converter.manual(
		pointer+"/$ref",
		"openapi.convert.swagger-external-reference",
		"verify that the external "+subject+" was converted from Swagger",
	)
}

func convertSwagger20Root(
	ctx context.Context,
	root jsonvalue.Value,
	target openapi.Version,
	maxDocumentNodes int,
	maxSchemaNodes int,
) (jsonvalue.Value, []Diagnostic, error) {
	converter := swagger20Converter{
		ctx: ctx, maxDocumentNodes: maxDocumentNodes,
		maxSchemaNodes: maxSchemaNodes,
	}
	converted, err := converter.document(root, target)
	return converted, converter.diagnostics, err
}

func (converter *swagger20Converter) visit() error {
	if err := converter.ctx.Err(); err != nil {
		return err
	}
	converter.documentNodes++
	if converter.documentNodes > converter.maxDocumentNodes {
		return ErrLimitExceeded
	}
	return nil
}

func (converter *swagger20Converter) document(
	root jsonvalue.Value,
	target openapi.Version,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	converter.indexBodyParameters(root)
	members, _ := root.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		switch member.Name {
		case "swagger":
			version, _ := jsonvalue.String(target.String())
			result = append(result, jsonvalue.Member{
				Name: "openapi", Value: version,
			})
		case "host", "basePath", "schemes", "consumes", "produces",
			"definitions", "parameters", "responses", "securityDefinitions":
			continue
		case "components":
			converter.loss(
				"/components",
				"openapi.convert.swagger-component-collision",
				"the non-standard Swagger components member was replaced",
			)
			continue
		case "paths":
			converted, err := converter.paths(member.Value, root, "/paths")
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
			result = append(result, member)
		default:
			result = append(result, member)
		}
	}
	if servers, exists := converter.servers(root); exists {
		result = append(result, jsonvalue.Member{Name: "servers", Value: servers})
	}
	components, exists, err := converter.components(root)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	if exists {
		result = append(result, jsonvalue.Member{
			Name: "components", Value: components,
		})
	}
	return jsonvalue.Object(result)
}

func (converter *swagger20Converter) servers(
	root jsonvalue.Value,
) (jsonvalue.Value, bool) {
	host, hasHost := stringMember(root, "host")
	basePath, hasBasePath := stringMember(root, "basePath")
	schemesValue, hasSchemes := root.Lookup("schemes")
	schemes, validSchemes := schemesValue.Elements()
	if !hasHost && !hasBasePath {
		if hasSchemes {
			converter.loss(
				"/schemes",
				"openapi.convert.swagger-schemes-without-host",
				"schemes need a host before they can become server URLs",
			)
		}
		return jsonvalue.Value{}, false
	}
	if !hasHost {
		if hasSchemes {
			converter.loss(
				"/schemes",
				"openapi.convert.swagger-schemes-without-host",
				"schemes need a host before they can become server URLs",
			)
		}
		server := serverValue(basePath)
		servers, _ := jsonvalue.Array([]jsonvalue.Value{server})
		return servers, true
	}
	if !hasSchemes || !validSchemes || len(schemes) == 0 {
		server := serverValue("//" + host + basePath)
		servers, _ := jsonvalue.Array([]jsonvalue.Value{server})
		return servers, true
	}
	servers := make([]jsonvalue.Value, 0, len(schemes))
	for _, schemeValue := range schemes {
		scheme, valid := schemeValue.Text()
		if !valid {
			continue
		}
		servers = append(servers, serverValue(scheme+"://"+host+basePath))
	}
	if len(servers) == 0 {
		return jsonvalue.Value{}, false
	}
	value, _ := jsonvalue.Array(servers)
	return value, true
}

func (converter *swagger20Converter) serversForSchemes(
	root jsonvalue.Value,
	schemesValue jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, bool) {
	host, hasHost := stringMember(root, "host")
	if !hasHost {
		converter.manual(
			pointer,
			"openapi.convert.swagger-schemes-without-host",
			"operation schemes need a host before they can become server URLs",
		)
		return jsonvalue.Value{}, false
	}
	basePath, _ := stringMember(root, "basePath")
	schemes, valid := schemesValue.Elements()
	if !valid {
		return jsonvalue.Value{}, false
	}
	servers := make([]jsonvalue.Value, 0, len(schemes))
	for _, schemeValue := range schemes {
		scheme, valid := schemeValue.Text()
		if valid {
			servers = append(servers, serverValue(
				scheme+"://"+host+basePath,
			))
		}
	}
	if len(servers) == 0 {
		return jsonvalue.Value{}, false
	}
	value, _ := jsonvalue.Array(servers)
	return value, true
}

func (converter *swagger20Converter) components(
	root jsonvalue.Value,
) (jsonvalue.Value, bool, error) {
	result := make([]jsonvalue.Member, 0, 2)
	if definitions, exists := root.Lookup("definitions"); exists {
		converted, err := converter.schemaMap(definitions, "/definitions")
		if err != nil {
			return jsonvalue.Value{}, false, err
		}
		result = append(result, jsonvalue.Member{
			Name: "schemas", Value: converted,
		})
	}
	if parameters, exists := root.Lookup("parameters"); exists {
		regular, requestBodies, err := converter.reusableParameters(
			parameters, stringArrayMember(root, "consumes"), "/parameters",
		)
		if err != nil {
			return jsonvalue.Value{}, false, err
		}
		if regular.Kind() == jsonvalue.ObjectKind {
			result = append(result, jsonvalue.Member{
				Name: "parameters", Value: regular,
			})
		}
		if requestBodies.Kind() == jsonvalue.ObjectKind {
			result = append(result, jsonvalue.Member{
				Name: "requestBodies", Value: requestBodies,
			})
		}
	}
	if responses, exists := root.Lookup("responses"); exists {
		converted, err := converter.responseMap(
			responses, stringArrayMember(root, "produces"), "/responses",
		)
		if err != nil {
			return jsonvalue.Value{}, false, err
		}
		result = append(result, jsonvalue.Member{
			Name: "responses", Value: converted,
		})
	}
	if definitions, exists := root.Lookup("securityDefinitions"); exists {
		converted, err := converter.securitySchemes(
			definitions, "/securityDefinitions",
		)
		if err != nil {
			return jsonvalue.Value{}, false, err
		}
		result = append(result, jsonvalue.Member{
			Name: "securitySchemes", Value: converted,
		})
	}
	if len(result) == 0 {
		return jsonvalue.Value{}, false, nil
	}
	value, err := jsonvalue.Object(result)
	return value, true, err
}

func (converter *swagger20Converter) indexBodyParameters(root jsonvalue.Value) {
	converter.bodyRefs = make(map[string]struct{})
	converter.formRefs = make(map[string]jsonvalue.Value)
	converter.parameterRefs = make(map[string]jsonvalue.Value)
	parameters, exists := root.Lookup("parameters")
	if !exists || parameters.Kind() != jsonvalue.ObjectKind {
		return
	}
	members, _ := parameters.Members()
	for _, member := range members {
		reference := "#/parameters/" + escapePointer(member.Name)
		converter.parameterRefs[reference] = member.Value
		location, _ := stringMember(member.Value, "in")
		switch location {
		case "body":
			converter.bodyRefs[reference] = struct{}{}
		case "formData":
			converter.formRefs[reference] = member.Value
		}
	}
}

func (converter *swagger20Converter) paths(
	value jsonvalue.Value,
	root jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	for index := range members {
		converted, err := converter.pathItem(
			members[index].Value,
			root,
			pointer+"/"+escapePointer(members[index].Name),
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		members[index].Value = converted
	}
	return jsonvalue.Object(members)
}

func (converter *swagger20Converter) pathItem(
	value jsonvalue.Value,
	root jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	var (
		inherited      []swaggerParameterInput
		pathParameters jsonvalue.Value
	)
	if parameters, exists := value.Lookup("parameters"); exists {
		var err error
		pathParameters, inherited, err = converter.splitPathParameters(
			parameters, pointer+"/parameters",
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		var (
			converted jsonvalue.Value
			err       error
		)
		switch member.Name {
		case "$ref":
			if reference, valid := member.Value.Text(); valid {
				converter.externalReference(reference, pointer, "path item")
			}
			result = append(result, member)
		case "parameters":
			converted = pathParameters
		case "get", "put", "post", "delete", "options", "head", "patch":
			converted, err = converter.operation(
				member.Value,
				root,
				pointer+"/"+member.Name,
				inherited,
			)
		default:
			result = append(result, member)
			continue
		}
		if err != nil {
			return jsonvalue.Value{}, err
		}
		if converted.Kind() != jsonvalue.InvalidKind {
			member.Value = converted
			result = append(result, member)
		}
	}
	return jsonvalue.Object(result)
}

func (converter *swagger20Converter) operation(
	value jsonvalue.Value,
	root jsonvalue.Value,
	pointer string,
	inherited []swaggerParameterInput,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	consumes := effectiveStringArray(value, root, "consumes")
	produces := effectiveStringArray(value, root, "produces")
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	var requestBody jsonvalue.Value
	hasParameters := false
	for _, member := range members {
		switch member.Name {
		case "consumes", "produces":
			continue
		case "schemes":
			if servers, exists := converter.serversForSchemes(
				root, member.Value, pointer+"/schemes",
			); exists {
				result = append(result, jsonvalue.Member{
					Name: "servers", Value: servers,
				})
			}
			continue
		case "parameters":
			hasParameters = true
			parameters, body, err := converter.operationParameters(
				member.Value, consumes, pointer+"/parameters", inherited,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			if parameters.Kind() == jsonvalue.ArrayKind {
				member.Value = parameters
				result = append(result, member)
			}
			requestBody = body
			continue
		case "responses":
			converted, err := converter.responseMap(
				member.Value, produces, pointer+"/responses",
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		}
		result = append(result, member)
	}
	if !hasParameters && len(inherited) != 0 {
		parameters, body, err := converter.operationParameters(
			jsonvalue.Value{}, consumes, pointer+"/parameters", inherited,
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		if parameters.Kind() == jsonvalue.ArrayKind {
			result = append(result, jsonvalue.Member{
				Name: "parameters", Value: parameters,
			})
		}
		requestBody = body
	}
	if requestBody.Kind() != jsonvalue.InvalidKind {
		result = append(result, jsonvalue.Member{
			Name: "requestBody", Value: requestBody,
		})
	}
	return jsonvalue.Object(result)
}

func (converter *swagger20Converter) splitPathParameters(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, []swaggerParameterInput, error) {
	if value.Kind() != jsonvalue.ArrayKind {
		return value, nil, nil
	}
	elements, _ := value.Elements()
	regular := make([]jsonvalue.Value, 0, len(elements))
	special := make([]swaggerParameterInput, 0, len(elements))
	for index, element := range elements {
		elementPointer := pointer + "/" + strconv.Itoa(index)
		if converter.isRequestParameter(element) {
			special = append(special, swaggerParameterInput{
				value: element, pointer: elementPointer,
			})
			continue
		}
		converted, err := converter.parameter(
			element, elementPointer,
		)
		if err != nil {
			return jsonvalue.Value{}, nil, err
		}
		regular = append(regular, converted)
	}
	if len(regular) == 0 {
		return jsonvalue.Value{}, special, nil
	}
	converted, err := jsonvalue.Array(regular)
	return converted, special, err
}

func (converter *swagger20Converter) isRequestParameter(
	value jsonvalue.Value,
) bool {
	if reference, exists := referenceText(value); exists {
		if _, body := converter.bodyRefs[reference]; body {
			return true
		}
		_, form := converter.formRefs[reference]
		return form
	}
	location, _ := stringMember(value, "in")
	return location == "body" || location == "formData"
}

func (converter *swagger20Converter) parameterIdentity(
	value jsonvalue.Value,
) string {
	if reference, exists := referenceText(value); exists {
		if target, local := converter.parameterRefs[reference]; local {
			value = target
		} else {
			return "$ref\x00" + reference
		}
	}
	name, _ := stringMember(value, "name")
	location, _ := stringMember(value, "in")
	return location + "\x00" + name
}

func (converter *swagger20Converter) operationParameters(
	value jsonvalue.Value,
	consumes []string,
	pointer string,
	inherited []swaggerParameterInput,
) (jsonvalue.Value, jsonvalue.Value, error) {
	inputs := append([]swaggerParameterInput(nil), inherited...)
	if value.Kind() == jsonvalue.ArrayKind {
		elements, _ := value.Elements()
		own := make([]swaggerParameterInput, 0, len(elements))
		overrides := make(map[string]struct{}, len(elements))
		for index, element := range elements {
			input := swaggerParameterInput{
				value: element, pointer: pointer + "/" + strconv.Itoa(index),
			}
			own = append(own, input)
			overrides[converter.parameterIdentity(element)] = struct{}{}
		}
		kept := inputs[:0]
		for _, input := range inputs {
			if _, overridden := overrides[converter.parameterIdentity(input.value)]; !overridden {
				kept = append(kept, input)
			}
		}
		inputs = append(kept, own...)
	} else if value.Kind() != jsonvalue.InvalidKind {
		return value, jsonvalue.Value{}, nil
	}
	parameters := make([]jsonvalue.Value, 0, len(inputs))
	formParameters := make([]swaggerParameterInput, 0, len(inputs))
	var requestBody jsonvalue.Value
	for _, input := range inputs {
		element := input.value
		elementPointer := input.pointer
		if reference, isReference := referenceText(element); isReference {
			converter.referenceSiblingLoss(element, elementPointer)
			converter.externalReference(reference, elementPointer, "parameter")
			if _, body := converter.bodyRefs[reference]; body {
				if requestBody.Kind() != jsonvalue.InvalidKind {
					converter.loss(
						elementPointer,
						"openapi.convert.swagger-multiple-request-bodies",
						"only the first Swagger body parameter was converted",
					)
					continue
				}
				requestBody = referenceValue(swaggerParameterReference(reference, true))
				continue
			}
			if formParameter, form := converter.formRefs[reference]; form {
				formParameters = append(formParameters, swaggerParameterInput{
					value: formParameter, pointer: elementPointer,
				})
				continue
			}
			parameters = append(parameters,
				referenceValue(swaggerParameterReference(reference, false)))
			continue
		}
		location, _ := stringMember(element, "in")
		if location == "body" {
			if requestBody.Kind() != jsonvalue.InvalidKind {
				converter.loss(
					elementPointer,
					"openapi.convert.swagger-multiple-request-bodies",
					"only the first Swagger body parameter was converted",
				)
				continue
			}
			converted, err := converter.requestBody(
				element, consumes, elementPointer,
			)
			if err != nil {
				return jsonvalue.Value{}, jsonvalue.Value{}, err
			}
			requestBody = converted
			continue
		}
		if location == "formData" {
			formParameters = append(formParameters, input)
			continue
		}
		converted, err := converter.parameter(element, elementPointer)
		if err != nil {
			return jsonvalue.Value{}, jsonvalue.Value{}, err
		}
		parameters = append(parameters, converted)
	}
	if len(formParameters) > 0 {
		if requestBody.Kind() != jsonvalue.InvalidKind {
			converter.loss(
				pointer,
				"openapi.convert.swagger-body-form-conflict",
				"form parameters were not converted beside a body parameter",
			)
			if len(parameters) == 0 {
				return jsonvalue.Value{}, requestBody, nil
			}
			converted, err := jsonvalue.Array(parameters)
			return converted, requestBody, err
		}
		converted, err := converter.formRequestBody(
			formParameters, consumes, pointer,
		)
		if err != nil {
			return jsonvalue.Value{}, jsonvalue.Value{}, err
		}
		requestBody = converted
	}
	if len(parameters) == 0 {
		return jsonvalue.Value{}, requestBody, nil
	}
	converted, err := jsonvalue.Array(parameters)
	return converted, requestBody, err
}

func (converter *swagger20Converter) reusableParameters(
	value jsonvalue.Value,
	consumes []string,
	pointer string,
) (jsonvalue.Value, jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, jsonvalue.Value{}, nil
	}
	members, _ := value.Members()
	parameters := make([]jsonvalue.Member, 0, len(members))
	requestBodies := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		memberPointer := pointer + "/" + escapePointer(member.Name)
		location, _ := stringMember(member.Value, "in")
		if location == "body" {
			converted, err := converter.requestBody(
				member.Value, consumes, memberPointer,
			)
			if err != nil {
				return jsonvalue.Value{}, jsonvalue.Value{}, err
			}
			member.Value = converted
			requestBodies = append(requestBodies, member)
			continue
		}
		if location == "formData" {
			converter.manual(
				memberPointer,
				"openapi.convert.swagger-reusable-form-parameter",
				"the reusable form parameter was inlined at known operation uses",
			)
			continue
		}
		converted, err := converter.parameter(member.Value, memberPointer)
		if err != nil {
			return jsonvalue.Value{}, jsonvalue.Value{}, err
		}
		member.Value = converted
		parameters = append(parameters, member)
	}
	var regular, bodies jsonvalue.Value
	if len(parameters) != 0 {
		regular, _ = jsonvalue.Object(parameters)
	}
	if len(requestBodies) != 0 {
		bodies, _ = jsonvalue.Object(requestBodies)
	}
	return regular, bodies, nil
}

func (converter *swagger20Converter) parameter(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	if reference, exists := referenceText(value); exists {
		converter.referenceSiblingLoss(value, pointer)
		converter.externalReference(reference, pointer, "parameter")
		return referenceValue(swaggerParameterReference(reference, false)), nil
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	location, _ := stringMember(value, "in")
	typeName, _ := stringMember(value, "type")
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members)+3)
	schemaMembers := make([]jsonvalue.Member, 0, len(members))
	collectionFormat, hasCollectionFormat := stringMember(value, "collectionFormat")
	for _, member := range members {
		switch member.Name {
		case "name", "in", "description", "required", "allowEmptyValue":
			result = append(result, member)
		case "collectionFormat":
			continue
		default:
			if strings.HasPrefix(strings.ToLower(member.Name), "x-") {
				result = append(result, member)
			} else {
				schemaMembers = append(schemaMembers, member)
			}
		}
	}
	if len(schemaMembers) != 0 {
		schema, _ := jsonvalue.Object(schemaMembers)
		var err error
		schema, err = converter.schema(schema, pointer)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		result = append(result, jsonvalue.Member{Name: "schema", Value: schema})
	}
	if typeName == "array" {
		style, explode := parameterCollectionStyle(
			location, collectionFormat, hasCollectionFormat,
		)
		if style != "" {
			styleValue, _ := jsonvalue.String(style)
			result = append(result, jsonvalue.Member{
				Name: "style", Value: styleValue,
			}, jsonvalue.Member{
				Name: "explode", Value: jsonvalue.Boolean(explode),
			})
		} else if collectionFormat != "" {
			converter.manual(
				pointer+"/collectionFormat",
				"openapi.convert.swagger-collection-format",
				"translate this Swagger collection format manually",
			)
		}
	}
	return jsonvalue.Object(result)
}

func (converter *swagger20Converter) requestBody(
	value jsonvalue.Value,
	consumes []string,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, 3)
	for _, member := range members {
		if member.Name == "description" || member.Name == "required" ||
			strings.HasPrefix(strings.ToLower(member.Name), "x-") {
			result = append(result, member)
		}
	}
	schema, exists := value.Lookup("schema")
	if exists {
		converted, err := converter.schema(schema, pointer+"/schema")
		if err != nil {
			return jsonvalue.Value{}, err
		}
		if len(consumes) == 0 {
			converter.defaultMediaType(pointer + "/schema")
		}
		content := contentValue(converted, consumes, jsonvalue.Value{})
		result = append(result, jsonvalue.Member{Name: "content", Value: content})
	}
	return jsonvalue.Object(result)
}

func (converter *swagger20Converter) formRequestBody(
	parameters []swaggerParameterInput,
	consumes []string,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	properties := make([]jsonvalue.Member, 0, len(parameters))
	required := make([]jsonvalue.Value, 0, len(parameters))
	encodings := make([]jsonvalue.Member, 0, len(parameters))
	requestRequired := false
	hasFile := false
	for index, input := range parameters {
		parameter := input.value
		name, _ := stringMember(parameter, "name")
		parameterPointer := input.pointer
		if parameterPointer == "" {
			parameterPointer = pointer + "/" + strconv.Itoa(index)
		}
		converted, err := converter.parameter(
			parameter, parameterPointer,
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		schema, _ := converted.Lookup("schema")
		if description, exists := parameter.Lookup("description"); exists {
			schema, _ = appendObjectMember(schema, "description", description)
		}
		parameterMembers, _ := parameter.Members()
		for _, member := range parameterMembers {
			if strings.HasPrefix(strings.ToLower(member.Name), "x-") {
				schema, _ = appendObjectMember(schema, member.Name, member.Value)
			}
		}
		properties = append(properties, jsonvalue.Member{Name: name, Value: schema})
		if enabledBoolean(parameter, "required") {
			nameValue, _ := jsonvalue.String(name)
			required = append(required, nameValue)
			requestRequired = true
		}
		typeName, _ := stringMember(parameter, "type")
		if typeName == "file" {
			hasFile = true
		}
		if typeName == "array" {
			format, hasFormat := stringMember(parameter, "collectionFormat")
			style, explode := parameterCollectionStyle("query", format, hasFormat)
			if style != "" {
				styleValue, _ := jsonvalue.String(style)
				encoding, _ := jsonvalue.Object([]jsonvalue.Member{
					{Name: "style", Value: styleValue},
					{Name: "explode", Value: jsonvalue.Boolean(explode)},
				})
				encodings = append(encodings, jsonvalue.Member{
					Name: name, Value: encoding,
				})
			}
		}
	}
	typeValue, _ := jsonvalue.String("object")
	propertyMap, _ := jsonvalue.Object(properties)
	schemaMembers := []jsonvalue.Member{
		{Name: "type", Value: typeValue},
		{Name: "properties", Value: propertyMap},
	}
	if len(required) != 0 {
		requiredValue, _ := jsonvalue.Array(required)
		schemaMembers = append(schemaMembers, jsonvalue.Member{
			Name: "required", Value: requiredValue,
		})
	}
	schema, _ := jsonvalue.Object(schemaMembers)
	if len(consumes) == 0 {
		if hasFile {
			consumes = []string{"multipart/form-data"}
		} else {
			consumes = []string{"application/x-www-form-urlencoded"}
		}
	}
	content := make([]jsonvalue.Member, 0, len(consumes))
	for _, mediaType := range consumes {
		mediaMembers := []jsonvalue.Member{{Name: "schema", Value: schema}}
		if len(encodings) != 0 {
			encodingValue, _ := jsonvalue.Object(encodings)
			mediaMembers = append(mediaMembers, jsonvalue.Member{
				Name: "encoding", Value: encodingValue,
			})
		}
		mediaValue, _ := jsonvalue.Object(mediaMembers)
		content = append(content, jsonvalue.Member{
			Name: mediaType, Value: mediaValue,
		})
	}
	contentValue, _ := jsonvalue.Object(content)
	return jsonvalue.Object([]jsonvalue.Member{
		{Name: "required", Value: jsonvalue.Boolean(requestRequired)},
		{Name: "content", Value: contentValue},
	})
}

func (converter *swagger20Converter) responseMap(
	value jsonvalue.Value,
	produces []string,
	pointer string,
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	for index := range members {
		converted, err := converter.response(
			members[index].Value,
			produces,
			pointer+"/"+escapePointer(members[index].Name),
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		members[index].Value = converted
	}
	return jsonvalue.Object(members)
}

func (converter *swagger20Converter) response(
	value jsonvalue.Value,
	produces []string,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	if reference, exists := referenceText(value); exists {
		converter.referenceSiblingLoss(value, pointer)
		converter.externalReference(reference, pointer, "response")
		return referenceValue(swaggerResponseReference(reference)), nil
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	var schema, examples jsonvalue.Value
	for _, member := range members {
		switch member.Name {
		case "schema":
			converted, err := converter.schema(
				member.Value, pointer+"/schema",
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			schema = converted
		case "examples":
			examples = member.Value
		case "headers":
			converted, err := converter.headers(
				member.Value, pointer+"/headers",
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
			result = append(result, member)
		default:
			result = append(result, member)
		}
	}
	if schema.Kind() != jsonvalue.InvalidKind ||
		examples.Kind() == jsonvalue.ObjectKind {
		if len(produces) == 0 && schema.Kind() != jsonvalue.InvalidKind &&
			examples.Kind() != jsonvalue.ObjectKind {
			converter.defaultMediaType(pointer + "/schema")
		}
		mediaTypes := append([]string(nil), produces...)
		if exampleMembers, valid := examples.Members(); valid {
			for _, example := range exampleMembers {
				mediaTypes = appendUnique(mediaTypes, example.Name)
			}
		}
		content := contentValue(schema, mediaTypes, examples)
		result = append(result, jsonvalue.Member{Name: "content", Value: content})
	}
	return jsonvalue.Object(result)
}

func (converter *swagger20Converter) headers(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	for index := range members {
		converted, err := converter.parameter(
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

func contentValue(
	schema jsonvalue.Value,
	mediaTypes []string,
	examples jsonvalue.Value,
) jsonvalue.Value {
	if len(mediaTypes) == 0 {
		mediaTypes = []string{"application/json"}
	}
	content := make([]jsonvalue.Member, 0, len(mediaTypes))
	for _, mediaType := range mediaTypes {
		mediaMembers := make([]jsonvalue.Member, 0, 2)
		if schema.Kind() != jsonvalue.InvalidKind {
			mediaMembers = append(mediaMembers, jsonvalue.Member{
				Name: "schema", Value: schema,
			})
		}
		if example, exists := examples.Lookup(mediaType); exists {
			mediaMembers = append(mediaMembers, jsonvalue.Member{
				Name: "example", Value: example,
			})
		}
		mediaValue, _ := jsonvalue.Object(mediaMembers)
		content = append(content, jsonvalue.Member{
			Name: mediaType, Value: mediaValue,
		})
	}
	value, _ := jsonvalue.Object(content)
	return value
}

func parameterCollectionStyle(
	location string,
	collectionFormat string,
	hasCollectionFormat bool,
) (string, bool) {
	if !hasCollectionFormat {
		collectionFormat = "csv"
	}
	switch collectionFormat {
	case "multi":
		if location != "query" && location != "formData" {
			return "", false
		}
		return "form", true
	case "ssv":
		if location != "query" {
			return "", false
		}
		return "spaceDelimited", false
	case "pipes":
		if location != "query" {
			return "", false
		}
		return "pipeDelimited", false
	case "csv":
		if location == "query" {
			return "form", false
		}
		return "simple", false
	default:
		return "", false
	}
}

func (converter *swagger20Converter) schemaMap(
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

func (converter *swagger20Converter) schema(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.ctx.Err(); err != nil {
		return jsonvalue.Value{}, err
	}
	converter.schemaNodes++
	if converter.schemaNodes > converter.maxSchemaNodes {
		return jsonvalue.Value{}, ErrLimitExceeded
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	if reference, exists := referenceText(value); exists {
		converter.referenceSiblingLoss(value, pointer)
		converter.externalReference(reference, pointer, "schema")
		return referenceValue(swaggerReference(reference)), nil
	}
	members, _ := value.Members()
	typeName, _ := stringMember(value, "type")
	fileSchema := typeName == "file"
	_, hasFormat := value.Lookup("format")
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		switch member.Name {
		case "discriminator":
			if property, valid := member.Value.Text(); valid {
				propertyName, _ := jsonvalue.String(property)
				member.Value, _ = jsonvalue.Object([]jsonvalue.Member{{
					Name: "propertyName", Value: propertyName,
				}})
			}
		case "type":
			if fileSchema {
				member.Value, _ = jsonvalue.String("string")
			}
		case "format":
			if fileSchema {
				member.Value, _ = jsonvalue.String("binary")
			}
		case "properties":
			converted, err := converter.schemaMap(
				member.Value, pointer+"/properties",
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "items", "additionalProperties":
			converted, err := converter.schema(
				member.Value, pointer+"/"+escapePointer(member.Name),
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "allOf":
			converted, err := converter.schemaArray(
				member.Value, pointer+"/allOf",
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		}
		result = append(result, member)
	}
	if fileSchema && !hasFormat {
		format, _ := jsonvalue.String("binary")
		result = append(result, jsonvalue.Member{
			Name: "format", Value: format,
		})
	}
	return jsonvalue.Object(result)
}

func (converter *swagger20Converter) schemaArray(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ArrayKind {
		return value, nil
	}
	elements, _ := value.Elements()
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

func (converter *swagger20Converter) securitySchemes(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	for index := range members {
		converted, err := converter.securityScheme(
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

func (converter *swagger20Converter) securityScheme(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	typeName, _ := stringMember(value, "type")
	switch typeName {
	case "basic":
		return basicSecurityScheme(value)
	case "apiKey":
		return value, nil
	case "oauth2":
		return converter.oauth2SecurityScheme(value, pointer)
	default:
		converter.manual(
			pointer+"/type",
			"openapi.convert.swagger-security-type",
			"translate this Swagger security scheme type manually",
		)
		return value, nil
	}
}

func basicSecurityScheme(value jsonvalue.Value) (jsonvalue.Value, error) {
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		if member.Name == "type" {
			member.Value, _ = jsonvalue.String("http")
			result = append(result, member)
			scheme, _ := jsonvalue.String("basic")
			result = append(result, jsonvalue.Member{
				Name: "scheme", Value: scheme,
			})
			continue
		}
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (converter *swagger20Converter) oauth2SecurityScheme(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		switch member.Name {
		case "flow", "authUrl", "tokenUrl", "scopes":
			continue
		default:
			result = append(result, member)
		}
	}
	swaggerFlow, _ := stringMember(value, "flow")
	flowName := map[string]string{
		"implicit": "implicit", "password": "password",
		"application": "clientCredentials", "accessCode": "authorizationCode",
	}[swaggerFlow]
	if flowName == "" {
		converter.loss(
			pointer+"/flow",
			"openapi.convert.swagger-oauth-flow",
			"the Swagger OAuth flow could not be represented",
		)
	}
	flowMembers := make([]jsonvalue.Member, 0, 3)
	if authorizationURL, exists := value.Lookup("authUrl"); exists {
		flowMembers = append(flowMembers, jsonvalue.Member{
			Name: "authorizationUrl", Value: authorizationURL,
		})
	}
	if (swaggerFlow == "implicit" || swaggerFlow == "accessCode") &&
		!hasMember(value, "authUrl") {
		converter.loss(
			pointer+"/authUrl",
			"openapi.convert.swagger-oauth-authorization-url",
			"the OAuth flow is missing its required authorization URL",
		)
	}
	if tokenURL, exists := value.Lookup("tokenUrl"); exists {
		flowMembers = append(flowMembers, jsonvalue.Member{
			Name: "tokenUrl", Value: tokenURL,
		})
	}
	if (swaggerFlow == "password" || swaggerFlow == "application" ||
		swaggerFlow == "accessCode") && !hasMember(value, "tokenUrl") {
		converter.loss(
			pointer+"/tokenUrl",
			"openapi.convert.swagger-oauth-token-url",
			"the OAuth flow is missing its required token URL",
		)
	}
	if scopes, exists := value.Lookup("scopes"); exists {
		flowMembers = append(flowMembers, jsonvalue.Member{
			Name: "scopes", Value: scopes,
		})
	} else {
		converter.loss(
			pointer+"/scopes",
			"openapi.convert.swagger-oauth-scopes",
			"missing OAuth scopes were replaced with an empty object",
		)
		empty, _ := jsonvalue.Object(nil)
		flowMembers = append(flowMembers, jsonvalue.Member{
			Name: "scopes", Value: empty,
		})
	}
	flow, _ := jsonvalue.Object(flowMembers)
	flowEntries := make([]jsonvalue.Member, 0, 1)
	if flowName != "" {
		flowEntries = append(flowEntries, jsonvalue.Member{
			Name: flowName, Value: flow,
		})
	}
	flows, _ := jsonvalue.Object(flowEntries)
	result = append(result, jsonvalue.Member{Name: "flows", Value: flows})
	return jsonvalue.Object(result)
}

func swaggerReference(reference string) string {
	return strings.Replace(
		reference, "#/definitions/", "#/components/schemas/", 1,
	)
}

func swaggerParameterReference(reference string, body bool) string {
	component := "parameters"
	if body {
		component = "requestBodies"
	}
	return strings.Replace(
		reference, "#/parameters/", "#/components/"+component+"/", 1,
	)
}

func swaggerResponseReference(reference string) string {
	return strings.Replace(
		reference, "#/responses/", "#/components/responses/", 1,
	)
}

func referenceText(value jsonvalue.Value) (string, bool) {
	reference, exists := value.Lookup("$ref")
	if !exists {
		return "", false
	}
	text, valid := reference.Text()
	return text, valid
}

func referenceValue(reference string) jsonvalue.Value {
	value, _ := jsonvalue.String(reference)
	result, _ := jsonvalue.Object([]jsonvalue.Member{{
		Name: "$ref", Value: value,
	}})
	return result
}

func stringArrayMember(value jsonvalue.Value, name string) []string {
	member, exists := value.Lookup(name)
	if !exists {
		return nil
	}
	elements, valid := member.Elements()
	if !valid {
		return nil
	}
	result := make([]string, 0, len(elements))
	for _, element := range elements {
		if text, valid := element.Text(); valid {
			result = append(result, text)
		}
	}
	return result
}

func effectiveStringArray(
	local jsonvalue.Value,
	root jsonvalue.Value,
	name string,
) []string {
	if _, exists := local.Lookup(name); exists {
		return stringArrayMember(local, name)
	}
	return stringArrayMember(root, name)
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendObjectMember(
	value jsonvalue.Value,
	name string,
	memberValue jsonvalue.Value,
) (jsonvalue.Value, error) {
	members, valid := value.Members()
	if !valid {
		return value, nil
	}
	for _, member := range members {
		if member.Name == name {
			return value, nil
		}
	}
	return jsonvalue.Object(append(members, jsonvalue.Member{
		Name: name, Value: memberValue,
	}))
}

func stringMember(value jsonvalue.Value, name string) (string, bool) {
	member, exists := value.Lookup(name)
	if !exists {
		return "", false
	}
	text, valid := member.Text()
	return text, valid
}

func hasMember(value jsonvalue.Value, name string) bool {
	_, exists := value.Lookup(name)
	return exists
}

func serverValue(url string) jsonvalue.Value {
	urlValue, _ := jsonvalue.String(url)
	server, _ := jsonvalue.Object([]jsonvalue.Member{{
		Name: "url", Value: urlValue,
	}})
	return server
}
