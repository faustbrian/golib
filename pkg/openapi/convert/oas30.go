package convert

import (
	"context"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

type oas30SchemaConverter struct {
	ctx         context.Context
	maxNodes    int
	nodes       int
	diagnostics []Diagnostic
}

func convertOAS30Document(
	ctx context.Context,
	root jsonvalue.Value,
	maxNodes int,
) (jsonvalue.Value, []Diagnostic, error) {
	locations := make(map[string]struct{})
	references := make(map[string]struct{})
	collector := oas30SchemaCollector{
		locations: locations, references: references,
	}
	collector.document(root)
	converter := oas30SchemaConverter{ctx: ctx, maxNodes: maxNodes}
	converted, err := converter.document(root, "", locations, references)
	return converted, converter.diagnostics, err
}

func (converter *oas30SchemaConverter) document(
	value jsonvalue.Value,
	pointer string,
	locations map[string]struct{},
	references map[string]struct{},
) (jsonvalue.Value, error) {
	if _, schema := locations[pointer]; schema {
		return converter.schema(value, pointer)
	}
	if _, reference := references[pointer]; reference {
		return converter.reference(value, pointer, false)
	}
	switch value.Kind() {
	case jsonvalue.ObjectKind:
		members, _ := value.Members()
		for index := range members {
			converted, err := converter.document(
				members[index].Value,
				pointer+"/"+escapePointer(members[index].Name),
				locations,
				references,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			members[index].Value = converted
		}
		return jsonvalue.Object(members)
	case jsonvalue.ArrayKind:
		elements, _ := value.Elements()
		for index := range elements {
			converted, err := converter.document(
				elements[index],
				pointer+"/"+strconv.Itoa(index),
				locations,
				references,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			elements[index] = converted
		}
		return jsonvalue.Array(elements)
	default:
		return value, nil
	}
}

func (converter *oas30SchemaConverter) schema(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.ctx.Err(); err != nil {
		return jsonvalue.Value{}, err
	}
	converter.nodes++
	if converter.nodes > converter.maxNodes {
		return jsonvalue.Value{}, ErrLimitExceeded
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	if _, exists := value.Lookup("$ref"); exists {
		return converter.reference(value, pointer, true)
	}
	minimum, hasMinimum := value.Lookup("minimum")
	maximum, hasMaximum := value.Lookup("maximum")
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		memberPointer := pointer + "/" + escapePointer(member.Name)
		switch member.Name {
		case "type":
			if enabledBoolean(value, "nullable") {
				if typeName, scalar := member.Value.Text(); scalar && typeName != "null" {
					typeNameValue, _ := jsonvalue.String(typeName)
					nullName, _ := jsonvalue.String("null")
					member.Value, _ = jsonvalue.Array([]jsonvalue.Value{
						typeNameValue, nullName,
					})
				}
			}
		case "nullable":
			if _, valid := member.Value.Bool(); !valid {
				converter.loss(
					memberPointer,
					"openapi.convert.invalid-schema-keyword",
					"non-boolean nullable cannot be represented in OpenAPI 3.1",
				)
			}
			continue
		case "minimum":
			if enabledBoolean(value, "exclusiveMinimum") {
				continue
			}
		case "maximum":
			if enabledBoolean(value, "exclusiveMaximum") {
				continue
			}
		case "exclusiveMinimum":
			enabled, valid := member.Value.Bool()
			if !valid {
				converter.loss(
					memberPointer,
					"openapi.convert.invalid-schema-keyword",
					"non-boolean exclusiveMinimum is invalid in OpenAPI 3.0",
				)
				continue
			}
			if !enabled {
				continue
			}
			if hasMinimum && minimum.Kind() == jsonvalue.NumberKind {
				member.Value = minimum
			} else {
				converter.loss(
					memberPointer,
					"openapi.convert.exclusive-bound-without-value",
					"exclusiveMinimum has no numeric minimum to preserve",
				)
				continue
			}
		case "exclusiveMaximum":
			enabled, valid := member.Value.Bool()
			if !valid {
				converter.loss(
					memberPointer,
					"openapi.convert.invalid-schema-keyword",
					"non-boolean exclusiveMaximum is invalid in OpenAPI 3.0",
				)
				continue
			}
			if !enabled {
				continue
			}
			if hasMaximum && maximum.Kind() == jsonvalue.NumberKind {
				member.Value = maximum
			} else {
				converter.loss(
					memberPointer,
					"openapi.convert.exclusive-bound-without-value",
					"exclusiveMaximum has no numeric maximum to preserve",
				)
				continue
			}
		}
		converted, err := converter.schemaChild(
			member.Name, member.Value, memberPointer,
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		member.Value = converted
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (converter *oas30SchemaConverter) reference(
	value jsonvalue.Value,
	pointer string,
	schema bool,
) (jsonvalue.Value, error) {
	members, _ := value.Members()
	reference, _ := value.Lookup("$ref")
	if target, valid := reference.Text(); valid && !strings.HasPrefix(target, "#") {
		message := "verify that the external component uses OpenAPI 3.1 semantics"
		code := "openapi.convert.external-reference"
		if schema {
			message = "verify that the external schema uses OpenAPI 3.1 semantics"
			code = "openapi.convert.external-schema-reference"
		}
		converter.diagnostics = append(converter.diagnostics, Diagnostic{
			Code: code, Kind: ManualAction, Pointer: pointer + "/$ref", Message: message,
		})
	}
	result := make([]jsonvalue.Member, 0, 1)
	for _, member := range members {
		if member.Name == "$ref" {
			result = append(result, member)
			continue
		}
		converter.loss(
			pointer+"/"+escapePointer(member.Name),
			"openapi.convert.ignored-reference-sibling",
			"OpenAPI 3.0 ignores this Reference Object sibling",
		)
	}
	return jsonvalue.Object(result)
}

func (converter *oas30SchemaConverter) schemaChild(
	name string,
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	switch name {
	case "properties":
		return converter.schemaMap(value, pointer)
	case "allOf", "anyOf", "oneOf":
		return converter.schemaArray(value, pointer)
	case "items", "additionalProperties", "not":
		return converter.schema(value, pointer)
	default:
		return value, nil
	}
}

func (converter *oas30SchemaConverter) schemaMap(
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

func (converter *oas30SchemaConverter) schemaArray(
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

func (converter *oas30SchemaConverter) loss(
	pointer string,
	code string,
	message string,
) {
	converter.diagnostics = append(converter.diagnostics, Diagnostic{
		Code: code, Kind: Loss, Pointer: pointer, Message: message,
	})
}

func enabledBoolean(value jsonvalue.Value, name string) bool {
	member, exists := value.Lookup(name)
	enabled, valid := member.Bool()
	return exists && valid && enabled
}

type oas30SchemaCollector struct {
	locations  map[string]struct{}
	references map[string]struct{}
	pathItems  map[string]struct{}
}

func (collector oas30SchemaCollector) document(root jsonvalue.Value) {
	collector.mapObjects(root, "paths", "/paths", collector.pathItem)
	collector.mapObjects(root, "webhooks", "/webhooks", collector.pathItem)
	components, exists := objectMember(root, "components")
	if !exists {
		return
	}
	collector.mapValues(components, "schemas", "/components/schemas", collector.schema)
	collector.mapReferences(components, "parameters", "/components/parameters", collector.parameter)
	collector.mapReferences(components, "headers", "/components/headers", collector.parameter)
	collector.mapReferences(components, "requestBodies", "/components/requestBodies", collector.requestBody)
	collector.mapReferences(components, "responses", "/components/responses", collector.response)
	collector.mapReferences(components, "callbacks", "/components/callbacks", collector.callback)
	collector.mapReferences(components, "examples", "/components/examples", nil)
	collector.mapReferences(components, "securitySchemes", "/components/securitySchemes", nil)
	collector.mapReferences(components, "links", "/components/links", nil)
	collector.mapReferences(components, "pathItems", "/components/pathItems", collector.pathItem)
}

func (collector oas30SchemaCollector) pathItem(value jsonvalue.Value, pointer string) {
	if collector.pathItems != nil {
		collector.pathItems[pointer] = struct{}{}
	}
	if isReference(value) {
		return
	}
	collector.parameters(value, pointer+"/parameters")
	for _, method := range []string{
		"get", "put", "post", "delete", "options", "head", "patch", "trace",
	} {
		if operation, exists := value.Lookup(method); exists &&
			operation.Kind() == jsonvalue.ObjectKind {
			collector.operation(operation, pointer+"/"+method)
		}
	}
}

func (collector oas30SchemaCollector) operation(value jsonvalue.Value, pointer string) {
	collector.parameters(value, pointer+"/parameters")
	if requestBody, exists := value.Lookup("requestBody"); exists {
		collector.referenceOrVisit(
			requestBody, pointer+"/requestBody", collector.requestBody,
		)
	}
	collector.mapReferences(value, "responses", pointer+"/responses", collector.response)
	collector.mapReferences(value, "callbacks", pointer+"/callbacks", collector.callback)
}

func (collector oas30SchemaCollector) parameters(value jsonvalue.Value, pointer string) {
	parameters, exists := value.Lookup("parameters")
	if !exists || parameters.Kind() != jsonvalue.ArrayKind {
		return
	}
	elements, _ := parameters.Elements()
	for index, parameter := range elements {
		collector.referenceOrVisit(
			parameter, pointer+"/"+strconv.Itoa(index), collector.parameter,
		)
	}
}

func (collector oas30SchemaCollector) parameter(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind || isReference(value) {
		return
	}
	if schema, exists := value.Lookup("schema"); exists {
		collector.schema(schema, pointer+"/schema")
	}
	collector.content(value, pointer+"/content")
	collector.mapReferences(value, "examples", pointer+"/examples", nil)
}

func (collector oas30SchemaCollector) requestBody(value jsonvalue.Value, pointer string) {
	if value.Kind() == jsonvalue.ObjectKind && !isReference(value) {
		collector.content(value, pointer+"/content")
	}
}

func (collector oas30SchemaCollector) response(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind || isReference(value) {
		return
	}
	collector.content(value, pointer+"/content")
	collector.mapReferences(value, "headers", pointer+"/headers", collector.parameter)
	collector.mapReferences(value, "links", pointer+"/links", nil)
}

func (collector oas30SchemaCollector) content(value jsonvalue.Value, pointer string) {
	content, exists := objectMember(value, "content")
	if !exists {
		return
	}
	members, _ := content.Members()
	for _, member := range members {
		if member.Value.Kind() != jsonvalue.ObjectKind {
			continue
		}
		if schema, exists := member.Value.Lookup("schema"); exists {
			collector.schema(
				schema, pointer+"/"+escapePointer(member.Name)+"/schema",
			)
		}
		collector.mapReferences(
			member.Value,
			"examples",
			pointer+"/"+escapePointer(member.Name)+"/examples",
			nil,
		)
		if encoding, exists := objectMember(member.Value, "encoding"); exists {
			encodingMembers, _ := encoding.Members()
			for _, encodingMember := range encodingMembers {
				collector.mapReferences(
					encodingMember.Value,
					"headers",
					pointer+"/"+escapePointer(member.Name)+"/encoding/"+
						escapePointer(encodingMember.Name)+"/headers",
					collector.parameter,
				)
			}
		}
	}
}

func (collector oas30SchemaCollector) referenceOrVisit(
	value jsonvalue.Value,
	pointer string,
	visit func(jsonvalue.Value, string),
) {
	if value.Kind() != jsonvalue.ObjectKind {
		return
	}
	if isReference(value) {
		collector.references[pointer] = struct{}{}
		return
	}
	if visit != nil {
		visit(value, pointer)
	}
}

func (collector oas30SchemaCollector) callback(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind || isReference(value) {
		return
	}
	members, _ := value.Members()
	for _, member := range members {
		collector.pathItem(member.Value, pointer+"/"+escapePointer(member.Name))
	}
}

func (collector oas30SchemaCollector) schema(value jsonvalue.Value, pointer string) {
	if value.Kind() == jsonvalue.ObjectKind || value.Kind() == jsonvalue.BooleanKind {
		collector.locations[pointer] = struct{}{}
	}
}

func (collector oas30SchemaCollector) mapValues(
	container jsonvalue.Value,
	name string,
	pointer string,
	visit func(jsonvalue.Value, string),
) {
	values, exists := objectMember(container, name)
	if !exists {
		return
	}
	members, _ := values.Members()
	for _, member := range members {
		visit(member.Value, pointer+"/"+escapePointer(member.Name))
	}
}

func (collector oas30SchemaCollector) mapObjects(
	container jsonvalue.Value,
	name string,
	pointer string,
	visit func(jsonvalue.Value, string),
) {
	collector.mapValues(container, name, pointer, func(value jsonvalue.Value, pointer string) {
		if value.Kind() == jsonvalue.ObjectKind {
			visit(value, pointer)
		}
	})
}

func (collector oas30SchemaCollector) mapReferences(
	container jsonvalue.Value,
	name string,
	pointer string,
	visit func(jsonvalue.Value, string),
) {
	collector.mapValues(container, name, pointer, func(value jsonvalue.Value, pointer string) {
		collector.referenceOrVisit(value, pointer, visit)
	})
}

func objectMember(value jsonvalue.Value, name string) (jsonvalue.Value, bool) {
	member, exists := value.Lookup(name)
	return member, exists && member.Kind() == jsonvalue.ObjectKind
}

func isReference(value jsonvalue.Value) bool {
	_, exists := value.Lookup("$ref")
	return exists
}

func escapePointer(value string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(value)
}
