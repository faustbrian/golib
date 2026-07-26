package convert

import (
	"context"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

const oas32BaseDialect = "https://spec.openapis.org/oas/3.2/dialect/2025-09-17"

type oas32DocumentConverter struct {
	ctx         context.Context
	maxNodes    int
	nodes       int
	diagnostics []Diagnostic
	mediaTypes  map[string]jsonvalue.Value
	mediaCache  map[string]jsonvalue.Value
	resolving   map[string]struct{}
}

func convertOAS32Document(
	ctx context.Context,
	root jsonvalue.Value,
	maxNodes int,
) (jsonvalue.Value, []Diagnostic, error) {
	converter := oas32DocumentConverter{
		ctx: ctx, maxNodes: maxNodes,
		mediaTypes: make(map[string]jsonvalue.Value),
		mediaCache: make(map[string]jsonvalue.Value),
		resolving:  make(map[string]struct{}),
	}
	converter.indexMediaTypes(root)
	converted, err := converter.document(root)
	return converted, converter.diagnostics, err
}

func (converter *oas32DocumentConverter) indexMediaTypes(root jsonvalue.Value) {
	components, exists := root.Lookup("components")
	if !exists {
		return
	}
	mediaTypes, exists := components.Lookup("mediaTypes")
	if !exists || mediaTypes.Kind() != jsonvalue.ObjectKind {
		return
	}
	members, _ := mediaTypes.Members()
	for _, member := range members {
		converter.mediaTypes["#/components/mediaTypes/"+escapePointer(member.Name)] =
			member.Value
	}
}

func (converter *oas32DocumentConverter) visit() error {
	if err := converter.ctx.Err(); err != nil {
		return err
	}
	converter.nodes++
	if converter.nodes > converter.maxNodes {
		return ErrLimitExceeded
	}
	return nil
}

func (converter *oas32DocumentConverter) document(
	root jsonvalue.Value,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	members, _ := root.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	hasDialect := hasObjectMember(members, "jsonSchemaDialect")
	for _, member := range members {
		switch member.Name {
		case "$self":
			converter.loss(
				"/$self", "openapi.convert.self-uri-removed",
				"OpenAPI 3.1 cannot use $self as the document base URI",
			)
			continue
		case "servers":
			converted, err := converter.serverArray(member.Value, "/servers")
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "paths", "webhooks":
			converted, err := converter.pathMap(
				member.Value, "/"+member.Name,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "components":
			converted, err := converter.components(member.Value, "/components")
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "tags":
			converted, err := converter.tagArray(member.Value, "/tags")
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		}
		result = append(result, member)
		switch member.Name {
		case "openapi":
			if !hasDialect {
				dialect, _ := jsonvalue.String(oas32BaseDialect)
				result = append(result, jsonvalue.Member{
					Name: "jsonSchemaDialect", Value: dialect,
				})
				hasDialect = true
			}
		}
	}
	return jsonvalue.Object(result)
}

func (converter *oas32DocumentConverter) serverArray(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	return converter.objectArray(value, pointer, converter.server)
}

func (converter *oas32DocumentConverter) server(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	return converter.withoutFields(value, pointer, map[string]string{
		"name": "OpenAPI 3.1 does not define Server Object names",
	})
}

func (converter *oas32DocumentConverter) pathMap(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	return converter.objectMap(value, pointer, converter.pathItem)
}

func (converter *oas32DocumentConverter) pathItem(
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
		case "query", "additionalOperations":
			converter.loss(
				memberPointer,
				"openapi.convert.operation-removed",
				"OpenAPI 3.1 cannot represent this OpenAPI 3.2 operation",
			)
			continue
		case "get", "put", "post", "delete", "options", "head", "patch", "trace":
			converted, err := converter.operation(member.Value, memberPointer)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "servers":
			converted, err := converter.serverArray(member.Value, memberPointer)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "parameters":
			converted, err := converter.objectArray(
				member.Value, memberPointer, converter.parameter,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		}
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (converter *oas32DocumentConverter) operation(
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
	for index := range members {
		memberPointer := pointer + "/" + escapePointer(members[index].Name)
		switch members[index].Name {
		case "servers":
			converted, err := converter.serverArray(
				members[index].Value, memberPointer,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			members[index].Value = converted
		case "responses":
			converted, err := converter.responseMap(
				members[index].Value, memberPointer,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			members[index].Value = converted
		case "parameters":
			converted, err := converter.objectArray(
				members[index].Value, memberPointer, converter.parameter,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			members[index].Value = converted
		case "requestBody":
			converted, err := converter.requestBody(
				members[index].Value, memberPointer,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			members[index].Value = converted
		case "callbacks":
			converted, err := converter.objectMap(
				members[index].Value, memberPointer, converter.callbackMap,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			members[index].Value = converted
		}
	}
	return jsonvalue.Object(members)
}

func (converter *oas32DocumentConverter) responseMap(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	return converter.objectMap(value, pointer, converter.response)
}

func (converter *oas32DocumentConverter) response(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	if isReference(value) {
		return value, nil
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	hasDescription := hasObjectMember(members, "description")
	hasSummary := hasObjectMember(members, "summary")
	for _, member := range members {
		memberPointer := pointer + "/" + escapePointer(member.Name)
		switch member.Name {
		case "summary":
			if !hasDescription {
				member.Name = "description"
				result = append(result, member)
				continue
			}
			converter.loss(
				memberPointer,
				"openapi.convert.object-field-removed",
				"OpenAPI 3.1 does not define Response Object summaries",
			)
			continue
		case "content":
			converted, err := converter.content(member.Value, memberPointer)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "headers":
			converted, err := converter.objectMap(
				member.Value, memberPointer, converter.parameter,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		}
		result = append(result, member)
	}
	if !hasDescription && !hasSummary {
		description, _ := jsonvalue.String("")
		result = append(result, jsonvalue.Member{
			Name: "description", Value: description,
		})
		converter.diagnostics = append(converter.diagnostics, Diagnostic{
			Code: "openapi.convert.response-description-added",
			Kind: ManualAction, Pointer: pointer + "/description",
			Message: "review the empty description required by OpenAPI 3.1",
		})
	}
	return jsonvalue.Object(result)
}

func (converter *oas32DocumentConverter) callbackMap(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	return converter.objectMap(value, pointer, converter.pathItem)
}

func (converter *oas32DocumentConverter) components(
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
		case "mediaTypes":
			converter.loss(
				memberPointer,
				"openapi.convert.media-type-components-removed",
				"OpenAPI 3.1 cannot store reusable Media Type Objects",
			)
			continue
		case "securitySchemes":
			converted, err := converter.objectMap(
				member.Value, memberPointer, converter.securityScheme,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "responses":
			converted, err := converter.responseMap(
				member.Value, memberPointer,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "parameters", "headers":
			converted, err := converter.objectMap(
				member.Value, memberPointer, converter.parameter,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "requestBodies":
			converted, err := converter.objectMap(
				member.Value, memberPointer, converter.requestBody,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "examples":
			converted, err := converter.objectMap(
				member.Value, memberPointer, converter.example,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "callbacks":
			converted, err := converter.objectMap(
				member.Value, memberPointer, converter.callbackMap,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "pathItems":
			converted, err := converter.pathMap(
				member.Value, memberPointer,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		}
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (converter *oas32DocumentConverter) parameter(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	if isReference(value) || value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	for index := range members {
		memberPointer := pointer + "/" + escapePointer(members[index].Name)
		switch members[index].Name {
		case "content":
			converted, err := converter.content(
				members[index].Value, memberPointer,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			members[index].Value = converted
		case "examples":
			converted, err := converter.exampleMap(
				members[index].Value, memberPointer,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			members[index].Value = converted
		}
	}
	return jsonvalue.Object(members)
}

func (converter *oas32DocumentConverter) requestBody(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	if isReference(value) || value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	for index := range members {
		if members[index].Name != "content" {
			continue
		}
		converted, err := converter.content(
			members[index].Value, pointer+"/content",
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		members[index].Value = converted
	}
	return jsonvalue.Object(members)
}

func (converter *oas32DocumentConverter) content(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	return converter.objectMap(value, pointer, converter.mediaType)
}

func (converter *oas32DocumentConverter) mediaType(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if reference, exists := referenceText(value); exists {
		members, _ := value.Members()
		for _, member := range members {
			if member.Name != "$ref" {
				converter.loss(
					pointer+"/"+escapePointer(member.Name),
					"openapi.convert.media-type-reference-sibling-removed",
					"the Media Type reference sibling could not be inlined",
				)
			}
		}
		if target, local := converter.mediaTypes[reference]; local {
			if cached, exists := converter.mediaCache[reference]; exists {
				return cached, nil
			}
			if _, cycle := converter.resolving[reference]; cycle {
				converter.loss(
					pointer+"/$ref",
					"openapi.convert.media-type-reference-cycle",
					"the reusable Media Type reference cycle was removed",
				)
				return jsonvalue.Object(nil)
			}
			converter.resolving[reference] = struct{}{}
			converted, err := converter.mediaType(
				target,
				"/components/mediaTypes/"+
					strings.TrimPrefix(reference, "#/components/mediaTypes/"),
			)
			delete(converter.resolving, reference)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			converter.mediaCache[reference] = converted
			return converted, nil
		}
		converter.loss(
			pointer+"/$ref",
			"openapi.convert.media-type-reference-removed",
			"OpenAPI 3.1 cannot retain this Media Type Object reference",
		)
		return jsonvalue.Object(nil)
	}
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
		case "itemSchema", "prefixEncoding", "itemEncoding":
			converter.loss(
				memberPointer,
				"openapi.convert.media-type-field-removed",
				"OpenAPI 3.1 does not define this Media Type field",
			)
			continue
		case "examples":
			converted, err := converter.exampleMap(member.Value, memberPointer)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		case "encoding":
			converted, err := converter.encodingMap(member.Value, memberPointer)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		}
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (converter *oas32DocumentConverter) exampleMap(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	return converter.objectMap(value, pointer, converter.example)
}

func (converter *oas32DocumentConverter) example(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if err := converter.visit(); err != nil {
		return jsonvalue.Value{}, err
	}
	if isReference(value) || value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	hasValue := hasObjectMember(members, "value")
	for _, member := range members {
		memberPointer := pointer + "/" + escapePointer(member.Name)
		switch member.Name {
		case "dataValue":
			if hasValue {
				converter.loss(
					memberPointer,
					"openapi.convert.example-data-value-removed",
					"dataValue was removed in favor of the existing value",
				)
				continue
			}
			member.Name = "value"
		case "serializedValue":
			converter.loss(
				memberPointer,
				"openapi.convert.serialized-example-removed",
				"OpenAPI 3.1 cannot represent a serialized example separately",
			)
			continue
		}
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (converter *oas32DocumentConverter) encodingMap(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	return converter.objectMap(value, pointer, converter.encoding)
}

func (converter *oas32DocumentConverter) encoding(
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
		case "encoding", "prefixEncoding", "itemEncoding":
			converter.loss(
				memberPointer,
				"openapi.convert.encoding-field-removed",
				"OpenAPI 3.1 cannot represent nested Encoding Objects",
			)
			continue
		case "headers":
			converted, err := converter.objectMap(
				member.Value, memberPointer, converter.parameter,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		}
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (converter *oas32DocumentConverter) securityScheme(
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
		case "deprecated", "oauth2MetadataUrl":
			converter.loss(
				memberPointer,
				"openapi.convert.security-field-removed",
				"OpenAPI 3.1 does not define this Security Scheme field",
			)
			continue
		case "flows":
			converted, err := converter.oauthFlows(member.Value, memberPointer)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
		}
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (converter *oas32DocumentConverter) oauthFlows(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		memberPointer := pointer + "/" + escapePointer(member.Name)
		if member.Name == "deviceAuthorization" {
			converter.loss(
				memberPointer,
				"openapi.convert.oauth-flow-removed",
				"OpenAPI 3.1 cannot represent the device authorization flow",
			)
			continue
		}
		converted, err := converter.withoutFields(
			member.Value, memberPointer, map[string]string{
				"deviceAuthorizationUrl": "OpenAPI 3.1 does not define device authorization URLs",
			},
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		member.Value = converted
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (converter *oas32DocumentConverter) tagArray(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	return converter.objectArray(value, pointer, converter.tag)
}

func (converter *oas32DocumentConverter) tag(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	return converter.withoutFields(value, pointer, map[string]string{
		"summary": "OpenAPI 3.1 does not define Tag Object summaries",
		"kind":    "OpenAPI 3.1 does not define Tag Object kinds",
		"parent":  "OpenAPI 3.1 does not define Tag Object parents",
	})
}

func (converter *oas32DocumentConverter) withoutFields(
	value jsonvalue.Value,
	pointer string,
	fields map[string]string,
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
		message, remove := fields[member.Name]
		if remove {
			converter.loss(
				pointer+"/"+escapePointer(member.Name),
				"openapi.convert.object-field-removed", message,
			)
			continue
		}
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (converter *oas32DocumentConverter) objectMap(
	value jsonvalue.Value,
	pointer string,
	convert func(jsonvalue.Value, string) (jsonvalue.Value, error),
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	for index := range members {
		converted, err := convert(
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

func (converter *oas32DocumentConverter) objectArray(
	value jsonvalue.Value,
	pointer string,
	convert func(jsonvalue.Value, string) (jsonvalue.Value, error),
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ArrayKind {
		return value, nil
	}
	elements, _ := value.Elements()
	for index := range elements {
		converted, err := convert(
			elements[index], pointer+"/"+strconv.Itoa(index),
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		elements[index] = converted
	}
	return jsonvalue.Array(elements)
}

func (converter *oas32DocumentConverter) loss(
	pointer string,
	code string,
	message string,
) {
	converter.diagnostics = append(converter.diagnostics, Diagnostic{
		Code: code, Kind: Loss, Pointer: pointer, Message: message,
	})
}

func hasObjectMember(members []jsonvalue.Member, name string) bool {
	for _, member := range members {
		if member.Name == name {
			return true
		}
	}
	return false
}
