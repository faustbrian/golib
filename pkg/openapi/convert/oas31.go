package convert

import (
	"context"
	"math/big"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

type oas31SchemaConverter struct {
	ctx         context.Context
	maxNodes    int
	nodes       int
	diagnostics []Diagnostic
}

func convertOAS31Document(
	ctx context.Context,
	root jsonvalue.Value,
	maxNodes int,
) (jsonvalue.Value, []Diagnostic, error) {
	locations := make(map[string]struct{})
	references := make(map[string]struct{})
	pathItems := make(map[string]struct{})
	collector := oas30SchemaCollector{
		locations: locations, references: references, pathItems: pathItems,
	}
	collector.document(root)
	converter := oas31SchemaConverter{ctx: ctx, maxNodes: maxNodes}
	converted, err := converter.document(
		root, "", locations, references, pathItems,
	)
	return converted, converter.diagnostics, err
}

func (converter *oas31SchemaConverter) document(
	value jsonvalue.Value,
	pointer string,
	locations map[string]struct{},
	references map[string]struct{},
	pathItems map[string]struct{},
) (jsonvalue.Value, error) {
	if err := converter.ctx.Err(); err != nil {
		return jsonvalue.Value{}, err
	}
	if _, schema := locations[pointer]; schema {
		return converter.schema(value, pointer)
	}
	if _, reference := references[pointer]; reference {
		return converter.reference(value, pointer)
	}
	switch value.Kind() {
	case jsonvalue.ObjectKind:
		members, _ := value.Members()
		result := make([]jsonvalue.Member, 0, len(members))
		for _, member := range members {
			memberPointer := pointer + "/" + escapePointer(member.Name)
			if pointer == "" {
				switch member.Name {
				case "jsonSchemaDialect":
					converter.loss(
						memberPointer,
						"openapi.convert.schema-dialect-removed",
						"OpenAPI 3.0 cannot declare a JSON Schema dialect",
					)
					continue
				case "webhooks":
					converter.loss(
						memberPointer,
						"openapi.convert.webhooks-removed",
						"OpenAPI 3.0 cannot represent top-level webhooks",
					)
					continue
				}
			}
			if pointer == "/components" && member.Name == "pathItems" {
				converter.loss(
					memberPointer,
					"openapi.convert.path-items-removed",
					"OpenAPI 3.0 cannot store reusable Path Item Objects",
				)
				continue
			}
			if (pointer == "/info" && member.Name == "summary") ||
				(pointer == "/info/license" && member.Name == "identifier") {
				converter.loss(
					memberPointer,
					"openapi.convert.object-field-removed",
					"OpenAPI 3.0 does not define this object field",
				)
				continue
			}
			if pointer == "/components/securitySchemes" &&
				isSecurityType(member.Value, "mutualTLS") {
				converter.loss(
					memberPointer,
					"openapi.convert.mutual-tls-removed",
					"OpenAPI 3.0 cannot represent mutual TLS security schemes",
				)
				continue
			}
			if _, pathItem := pathItems[pointer]; pathItem &&
				(member.Name == "summary" || member.Name == "description") {
				converter.loss(
					memberPointer,
					"openapi.convert.path-item-field-removed",
					"OpenAPI 3.0 does not define this Path Item field",
				)
				continue
			}
			converted, err := converter.document(
				member.Value, memberPointer, locations, references, pathItems,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = converted
			result = append(result, member)
		}
		return jsonvalue.Object(result)
	case jsonvalue.ArrayKind:
		elements, _ := value.Elements()
		for index := range elements {
			converted, err := converter.document(
				elements[index], pointer+"/"+strconv.Itoa(index),
				locations, references, pathItems,
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

func (converter *oas31SchemaConverter) schema(
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
	if boolean, valid := value.Bool(); valid {
		if boolean {
			return jsonvalue.Object(nil)
		}
		empty, _ := jsonvalue.Object(nil)
		return jsonvalue.Object([]jsonvalue.Member{{Name: "not", Value: empty}})
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	if reference, exists := value.Lookup("$ref"); exists {
		if target, valid := reference.Text(); valid &&
			!strings.HasPrefix(target, "#") {
			converter.diagnostics = append(converter.diagnostics, Diagnostic{
				Code: "openapi.convert.external-schema-reference",
				Kind: ManualAction, Pointer: pointer + "/$ref",
				Message: "verify that the external schema uses OpenAPI 3.0 semantics",
			})
		}
		members, _ := value.Members()
		for _, member := range members {
			if member.Name != "$ref" {
				converter.loss(
					pointer+"/"+escapePointer(member.Name),
					"openapi.convert.reference-sibling-removed",
					"OpenAPI 3.0 ignores Reference Object siblings",
				)
			}
		}
		return jsonvalue.Object([]jsonvalue.Member{{Name: "$ref", Value: reference}})
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members)+2)
	generatedNullable := schemaTypeContainsNull(value)
	typeUsesAllOf := schemaTypeNeedsAllOf(value)
	_, hasConst := value.Lookup("const")
	constConflict := constEnumConflict(value)
	_, hasNot := value.Lookup("not")
	_, hasExample := value.Lookup("example")
	lowerValue, lowerExclusive, lowerConverted := downgradeExclusiveBound(
		value, "minimum", "exclusiveMinimum", true,
	)
	upperValue, upperExclusive, upperConverted := downgradeExclusiveBound(
		value, "maximum", "exclusiveMaximum", false,
	)
	lowerWritten := false
	upperWritten := false
	for _, member := range members {
		memberPointer := pointer + "/" + escapePointer(member.Name)
		switch member.Name {
		case "type":
			converted, additions, err := converter.schemaType(
				value, member.Value, memberPointer,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			if converted.Kind() != jsonvalue.InvalidKind {
				member.Value = converted
				result = append(result, member)
			}
			result = append(result, additions...)
			continue
		case "nullable":
			if generatedNullable {
				continue
			}
		case "minimum", "exclusiveMinimum":
			if lowerConverted {
				if !lowerWritten {
					result = append(result,
						jsonvalue.Member{Name: "minimum", Value: lowerValue},
						jsonvalue.Member{
							Name:  "exclusiveMinimum",
							Value: jsonvalue.Boolean(lowerExclusive),
						},
					)
					lowerWritten = true
				}
				continue
			}
		case "maximum", "exclusiveMaximum":
			if upperConverted {
				if !upperWritten {
					result = append(result,
						jsonvalue.Member{Name: "maximum", Value: upperValue},
						jsonvalue.Member{
							Name:  "exclusiveMaximum",
							Value: jsonvalue.Boolean(upperExclusive),
						},
					)
					upperWritten = true
				}
				continue
			}
		case "const":
			if constConflict {
				if !hasNot {
					empty, _ := jsonvalue.Object(nil)
					result = append(result, jsonvalue.Member{
						Name: "not", Value: empty,
					})
				}
				continue
			}
			enumeration, _ := jsonvalue.Array([]jsonvalue.Value{member.Value})
			result = append(result, jsonvalue.Member{Name: "enum", Value: enumeration})
			continue
		case "enum":
			if hasConst && !constConflict {
				continue
			}
		case "not":
			if constConflict {
				empty, _ := jsonvalue.Object(nil)
				member.Value = empty
				result = append(result, member)
				continue
			}
		case "allOf":
			if typeUsesAllOf {
				continue
			}
		case "examples":
			if hasExample {
				converter.loss(
					memberPointer,
					"openapi.convert.schema-examples-removed",
					"Schema examples were removed in favor of the existing example",
				)
				continue
			}
			elements, valid := member.Value.Elements()
			if !valid || len(elements) == 0 {
				converter.loss(
					memberPointer,
					"openapi.convert.schema-examples-removed",
					"Schema examples could not become one OpenAPI 3.0 example",
				)
				continue
			}
			if len(elements) > 1 {
				converter.loss(
					memberPointer,
					"openapi.convert.schema-examples-truncated",
					"only the first Schema example was retained",
				)
			}
			result = append(result, jsonvalue.Member{Name: "example", Value: elements[0]})
			continue
		case "xml":
			member.Value = converter.downgradeXML(member.Value, memberPointer)
		case "discriminator":
			member.Value = converter.withoutObjectField(
				member.Value,
				memberPointer,
				"defaultMapping",
				"OpenAPI 3.0 cannot represent discriminator default mappings",
			)
		case "$schema", "$id", "$anchor", "$dynamicAnchor", "$dynamicRef",
			"$vocabulary", "$comment", "$defs", "definitions",
			"additionalItems", "dependencies",
			"unevaluatedProperties", "unevaluatedItems",
			"prefixItems", "contains", "minContains", "maxContains",
			"dependentSchemas", "dependentRequired", "propertyNames",
			"patternProperties", "contentEncoding", "contentMediaType",
			"contentSchema", "if", "then", "else":
			converter.loss(
				memberPointer,
				"openapi.convert.unsupported-schema-keyword",
				"OpenAPI 3.0 cannot represent this JSON Schema keyword",
			)
			continue
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

func (converter *oas31SchemaConverter) downgradeXML(
	value jsonvalue.Value,
	pointer string,
) jsonvalue.Value {
	if value.Kind() != jsonvalue.ObjectKind {
		return value
	}
	nodeTypeValue, hasNodeType := value.Lookup("nodeType")
	nodeType, validNodeType := nodeTypeValue.Text()
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		if member.Name == "nodeType" {
			if !validNodeType || (nodeType != "element" && nodeType != "attribute") {
				converter.loss(
					pointer+"/nodeType",
					"openapi.convert.xml-node-type-removed",
					"OpenAPI 3.0 cannot represent this XML node type",
				)
			}
			continue
		}
		if member.Name == "attribute" && hasNodeType {
			continue
		}
		result = append(result, member)
	}
	if hasNodeType && validNodeType &&
		(nodeType == "element" || nodeType == "attribute") {
		result = append(result, jsonvalue.Member{
			Name: "attribute", Value: jsonvalue.Boolean(nodeType == "attribute"),
		})
	}
	converted, _ := jsonvalue.Object(result)
	return converted
}

func (converter *oas31SchemaConverter) withoutObjectField(
	value jsonvalue.Value,
	pointer string,
	name string,
	message string,
) jsonvalue.Value {
	if value.Kind() != jsonvalue.ObjectKind {
		return value
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		if member.Name == name {
			converter.loss(
				pointer+"/"+escapePointer(name),
				"openapi.convert.object-field-removed",
				message,
			)
			continue
		}
		result = append(result, member)
	}
	converted, _ := jsonvalue.Object(result)
	return converted
}

func downgradeExclusiveBound(
	value jsonvalue.Value,
	boundName string,
	exclusiveName string,
	lower bool,
) (jsonvalue.Value, bool, bool) {
	exclusive, exists := value.Lookup(exclusiveName)
	if !exists || exclusive.Kind() != jsonvalue.NumberKind {
		return jsonvalue.Value{}, false, false
	}
	bound, hasBound := value.Lookup(boundName)
	if !hasBound || bound.Kind() != jsonvalue.NumberKind {
		return exclusive, true, true
	}
	exclusiveNumber, exclusiveValid := exactNumber(exclusive)
	boundNumber, boundValid := exactNumber(bound)
	if !exclusiveValid || !boundValid {
		return exclusive, true, true
	}
	comparison := exclusiveNumber.Cmp(boundNumber)
	if lower {
		if comparison >= 0 {
			return exclusive, true, true
		}
	} else {
		if comparison <= 0 {
			return exclusive, true, true
		}
	}
	return bound, false, true
}

func exactNumber(value jsonvalue.Value) (*big.Rat, bool) {
	text, valid := value.NumberText()
	if !valid {
		return nil, false
	}
	return new(big.Rat).SetString(text)
}

func isSecurityType(value jsonvalue.Value, expected string) bool {
	typeValue, exists := value.Lookup("type")
	typeName, valid := typeValue.Text()
	return exists && valid && typeName == expected
}

func schemaTypeContainsNull(value jsonvalue.Value) bool {
	typeValue, exists := value.Lookup("type")
	if !exists {
		return false
	}
	elements, valid := typeValue.Elements()
	if !valid {
		return false
	}
	for _, element := range elements {
		if name, valid := element.Text(); valid && name == "null" {
			return true
		}
	}
	return false
}

func schemaTypeNeedsAllOf(value jsonvalue.Value) bool {
	_, hasAnyOf := value.Lookup("anyOf")
	typeValue, exists := value.Lookup("type")
	if !exists {
		return false
	}
	elements, valid := typeValue.Elements()
	if !valid {
		return false
	}
	nonNull := 0
	nullable := false
	for _, element := range elements {
		if name, valid := element.Text(); valid {
			if name == "null" {
				nullable = true
			} else {
				nonNull++
			}
		}
	}
	if nonNull < 1 {
		return nullable
	}
	return nonNull > 1 && hasAnyOf
}

func constEnumConflict(value jsonvalue.Value) bool {
	constant, hasConst := value.Lookup("const")
	enumeration, hasEnum := value.Lookup("enum")
	if !hasConst || !hasEnum {
		return false
	}
	elements, valid := enumeration.Elements()
	if !valid {
		return false
	}
	for _, element := range elements {
		if conversionValueEqual(constant, element) {
			return false
		}
	}
	return true
}

func conversionValueEqual(left jsonvalue.Value, right jsonvalue.Value) bool {
	if left.Kind() != right.Kind() {
		return false
	}
	switch left.Kind() {
	case jsonvalue.NullKind:
		return true
	case jsonvalue.BooleanKind:
		leftValue, _ := left.Bool()
		rightValue, _ := right.Bool()
		return leftValue == rightValue
	case jsonvalue.NumberKind:
		leftValue, leftValid := exactNumber(left)
		rightValue, rightValid := exactNumber(right)
		return leftValid && rightValid && leftValue.Cmp(rightValue) == 0
	case jsonvalue.StringKind:
		leftValue, _ := left.Text()
		rightValue, _ := right.Text()
		return leftValue == rightValue
	case jsonvalue.ArrayKind:
		leftValues, _ := left.Elements()
		rightValues, _ := right.Elements()
		if len(leftValues) != len(rightValues) {
			return false
		}
		for index := range leftValues {
			if !conversionValueEqual(leftValues[index], rightValues[index]) {
				return false
			}
		}
		return true
	case jsonvalue.ObjectKind:
		leftMembers, _ := left.Members()
		rightMembers, _ := right.Members()
		if len(leftMembers) != len(rightMembers) {
			return false
		}
		for _, member := range leftMembers {
			rightValue, exists := right.Lookup(member.Name)
			if !exists || !conversionValueEqual(member.Value, rightValue) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (converter *oas31SchemaConverter) schemaType(
	schema jsonvalue.Value,
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, []jsonvalue.Member, error) {
	elements, array := value.Elements()
	if !array {
		return value, nil, nil
	}
	nonNull := make([]jsonvalue.Value, 0, len(elements))
	nullable := false
	for _, element := range elements {
		name, valid := element.Text()
		if !valid {
			converter.loss(
				pointer,
				"openapi.convert.invalid-schema-type",
				"the non-string schema type was removed",
			)
			continue
		}
		if name == "null" {
			nullable = true
			continue
		}
		nonNull = append(nonNull, element)
	}
	additions := make([]jsonvalue.Member, 0, 2)
	switch len(nonNull) {
	case 0:
		typeValue, _ := jsonvalue.String("string")
		notSchema, _ := jsonvalue.Object([]jsonvalue.Member{{
			Name: "type", Value: typeValue,
		}})
		constraint, _ := jsonvalue.Object([]jsonvalue.Member{
			{Name: "type", Value: typeValue},
			{Name: "nullable", Value: jsonvalue.Boolean(true)},
			{Name: "not", Value: notSchema},
		})
		allOf, err := converter.typeConstraintAllOf(schema, constraint, pointer)
		if err != nil {
			return jsonvalue.Value{}, nil, err
		}
		return jsonvalue.Value{}, []jsonvalue.Member{{
			Name: "allOf", Value: allOf,
		}}, nil
	case 1:
		if nullable {
			additions = append(additions, jsonvalue.Member{
				Name: "nullable", Value: jsonvalue.Boolean(true),
			})
		}
		return nonNull[0], additions, nil
	default:
		alternatives := make([]jsonvalue.Value, 0, len(nonNull))
		for index, typeValue := range nonNull {
			members := []jsonvalue.Member{{Name: "type", Value: typeValue}}
			if nullable && index == 0 {
				members = append(members, jsonvalue.Member{
					Name: "nullable", Value: jsonvalue.Boolean(true),
				})
			}
			alternative, _ := jsonvalue.Object(members)
			alternatives = append(alternatives, alternative)
		}
		anyOf, _ := jsonvalue.Array(alternatives)
		if _, exists := schema.Lookup("anyOf"); !exists {
			additions = append(additions, jsonvalue.Member{Name: "anyOf", Value: anyOf})
			return jsonvalue.Value{}, additions, nil
		}
		constraint, _ := jsonvalue.Object([]jsonvalue.Member{{
			Name: "anyOf", Value: anyOf,
		}})
		convertedAllOf, err := converter.typeConstraintAllOf(
			schema, constraint, pointer,
		)
		if err != nil {
			return jsonvalue.Value{}, nil, err
		}
		additions = append(additions, jsonvalue.Member{
			Name: "allOf", Value: convertedAllOf,
		})
		return jsonvalue.Value{}, additions, nil
	}
}

func (converter *oas31SchemaConverter) typeConstraintAllOf(
	schema jsonvalue.Value,
	constraint jsonvalue.Value,
	typePointer string,
) (jsonvalue.Value, error) {
	allOfValue, hasAllOf := schema.Lookup("allOf")
	allOf, validAllOf := allOfValue.Elements()
	allOfPointer := strings.TrimSuffix(typePointer, "/type") + "/allOf"
	if hasAllOf && !validAllOf {
		converter.loss(
			allOfPointer,
			"openapi.convert.invalid-schema-keyword",
			"the invalid allOf value was replaced during type conversion",
		)
	}
	allOf = append(allOf, constraint)
	allOfValue, _ = jsonvalue.Array(allOf)
	return converter.schemaArray(allOfValue, allOfPointer)
}

func (converter *oas31SchemaConverter) schemaChild(
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

func (converter *oas31SchemaConverter) schemaMap(
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

func (converter *oas31SchemaConverter) schemaArray(
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

func (converter *oas31SchemaConverter) reference(
	value jsonvalue.Value,
	pointer string,
) (jsonvalue.Value, error) {
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, 1)
	for _, member := range members {
		if member.Name == "$ref" {
			if target, valid := member.Value.Text(); valid &&
				!strings.HasPrefix(target, "#") {
				converter.diagnostics = append(converter.diagnostics, Diagnostic{
					Code: "openapi.convert.external-reference",
					Kind: ManualAction, Pointer: pointer + "/$ref",
					Message: "verify that the external component uses OpenAPI 3.0 semantics",
				})
			}
			result = append(result, member)
			continue
		}
		converter.loss(
			pointer+"/"+escapePointer(member.Name),
			"openapi.convert.reference-sibling-removed",
			"OpenAPI 3.0 ignores Reference Object siblings",
		)
	}
	return jsonvalue.Object(result)
}

func (converter *oas31SchemaConverter) loss(
	pointer string,
	code string,
	message string,
) {
	converter.diagnostics = append(converter.diagnostics, Diagnostic{
		Code: code, Kind: Loss, Pointer: pointer, Message: message,
	})
}
