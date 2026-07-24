package media

import (
	"context"
	"errors"
	"fmt"
	"math/bits"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

var (
	// ErrInvalidSerializationDataType reports malformed schemas, resources, or
	// options supplied for non-JSON serialization type selection.
	ErrInvalidSerializationDataType = errors.New("invalid serialization data type")
	// ErrAmbiguousSerializationDataType reports multiple schema-derived data
	// types when no validated runtime value is available to disambiguate them.
	ErrAmbiguousSerializationDataType = errors.New("ambiguous serialization data type")
	// ErrSerializationDataTypeLimit reports bounded schema traversal exhaustion.
	ErrSerializationDataTypeLimit = errors.New("serialization data type limit exceeded")
)

// SerializationDataType is the JSON Schema data-model type a non-JSON
// serializer must encode. Any reports that schema inspection did not narrow the
// value from the complete JSON data model.
type SerializationDataType string

const (
	SerializationDataTypeAny     SerializationDataType = "any"
	SerializationDataTypeNull    SerializationDataType = "null"
	SerializationDataTypeBoolean SerializationDataType = "boolean"
	SerializationDataTypeNumber  SerializationDataType = "number"
	SerializationDataTypeString  SerializationDataType = "string"
	SerializationDataTypeArray   SerializationDataType = "array"
	SerializationDataTypeObject  SerializationDataType = "object"
)

// SerializationTypeOptions controls bounded OpenAPI 3.2 serialization type
// selection. A valid Data value takes precedence over schema inspection.
// Resolver is invoked only for references that leave Resource.
type SerializationTypeOptions struct {
	Data            jsonvalue.Value
	Resolver        reference.Resolver
	ReferenceLimits reference.Limits
	MaxSchemas      int
}

// SelectSerializationDataType selects the data-model type for non-JSON
// serialization. It inspects validated runtime data when supplied. Otherwise,
// it follows only $ref and allOf because those schemas apply to every instance;
// ambiguous applicators such as oneOf, anyOf, and $dynamicRef are not inferred.
func SelectSerializationDataType(
	ctx context.Context,
	resource reference.Resource,
	schema jsonvalue.Value,
	options SerializationTypeOptions,
) (SerializationDataType, error) {
	if ctx == nil {
		return "", fmt.Errorf("%w: nil context", ErrInvalidSerializationDataType)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if resource.Root.Kind() == jsonvalue.InvalidKind {
		return "", fmt.Errorf("%w: invalid resource", ErrInvalidSerializationDataType)
	}
	switch schema.Kind() {
	case jsonvalue.ObjectKind, jsonvalue.BooleanKind:
	default:
		return "", fmt.Errorf("%w: schema is not an object or boolean",
			ErrInvalidSerializationDataType)
	}
	if options.MaxSchemas < 0 {
		return "", fmt.Errorf("%w: negative schema limit",
			ErrInvalidSerializationDataType)
	}
	if options.MaxSchemas == 0 {
		options.MaxSchemas = 100_000
	}
	if options.ReferenceLimits == (reference.Limits{}) {
		options.ReferenceLimits = reference.DefaultLimits()
	}
	if options.ReferenceLimits.MaxTraversalDepth < 1 ||
		options.ReferenceLimits.MaxTraversalNodes < 1 ||
		options.ReferenceLimits.MaxReferenceDepth < 1 {
		return "", fmt.Errorf("%w: invalid reference limits",
			ErrInvalidSerializationDataType)
	}
	if dataType, available := serializationDataType(options.Data); available {
		return dataType, nil
	}

	walker := serializationTypeWalker{
		ctx:       ctx,
		resolver:  options.Resolver,
		limits:    options.ReferenceLimits,
		remaining: options.MaxSchemas,
		allowed:   allSerializationTypes,
		visited:   make(map[string]struct{}),
	}
	if err := walker.walk(resource, schema); err != nil {
		return "", err
	}
	return selectedSerializationDataType(walker.allowed, walker.constrained)
}

type serializationTypeMask uint8

const (
	serializationNull serializationTypeMask = 1 << iota
	serializationBoolean
	serializationNumber
	serializationString
	serializationArray
	serializationObject
	allSerializationTypes = serializationNull | serializationBoolean |
		serializationNumber | serializationString | serializationArray |
		serializationObject
)

type serializationTypeWalker struct {
	ctx         context.Context
	resolver    reference.Resolver
	limits      reference.Limits
	remaining   int
	allowed     serializationTypeMask
	constrained bool
	visited     map[string]struct{}
}

func (walker *serializationTypeWalker) walk(
	resource reference.Resource,
	schema jsonvalue.Value,
) error {
	if err := walker.ctx.Err(); err != nil {
		return err
	}
	if walker.remaining < 1 {
		return ErrSerializationDataTypeLimit
	}
	walker.remaining--
	if schema.Kind() == jsonvalue.BooleanKind {
		return nil
	}
	if schema.Kind() != jsonvalue.ObjectKind {
		return fmt.Errorf("%w: reachable schema is not an object or boolean",
			ErrInvalidSerializationDataType)
	}
	if rawReference, exists := schema.Lookup("$ref"); exists {
		raw, valid := rawReference.Text()
		if !valid {
			return fmt.Errorf("%w: $ref is not a string",
				ErrInvalidSerializationDataType)
		}
		target, err := reference.Resolve(
			walker.ctx, resource, raw, walker.resolver, walker.limits,
		)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidSerializationDataType, err)
		}
		identity := serializationTargetIdentity(target)
		if _, visited := walker.visited[identity]; !visited {
			walker.visited[identity] = struct{}{}
			if err := walker.walk(target.Resource, target.Value); err != nil {
				return err
			}
		}
	}
	if rawType, exists := schema.Lookup("type"); exists {
		mask, err := serializationSchemaTypeMask(rawType)
		if err != nil {
			return err
		}
		walker.allowed &= mask
		walker.constrained = true
	}
	allOf, exists := schema.Lookup("allOf")
	if !exists {
		return nil
	}
	schemas, valid := allOf.Elements()
	if !valid {
		return fmt.Errorf("%w: allOf is not an array",
			ErrInvalidSerializationDataType)
	}
	for _, child := range schemas {
		if err := walker.walk(resource, child); err != nil {
			return err
		}
	}
	return nil
}

func serializationTargetIdentity(target reference.Target) string {
	fragment := target.Fragment.Pointer().String()
	if target.Fragment.Kind() == reference.FragmentAnchor {
		fragment = target.Fragment.Anchor()
	}
	return target.RequestedURI + "\x00" + fragment
}

func serializationSchemaTypeMask(value jsonvalue.Value) (
	serializationTypeMask,
	error,
) {
	if name, valid := value.Text(); valid {
		return serializationTypeNameMask(name)
	}
	names, valid := value.Elements()
	if !valid || len(names) == 0 {
		return 0, fmt.Errorf("%w: type is not a string or non-empty array",
			ErrInvalidSerializationDataType)
	}
	var result serializationTypeMask
	for _, value := range names {
		name, valid := value.Text()
		if !valid {
			return 0, fmt.Errorf("%w: type array contains a non-string",
				ErrInvalidSerializationDataType)
		}
		mask, err := serializationTypeNameMask(name)
		if err != nil {
			return 0, err
		}
		result |= mask
	}
	return result, nil
}

func serializationTypeNameMask(name string) (serializationTypeMask, error) {
	switch name {
	case "null":
		return serializationNull, nil
	case "boolean":
		return serializationBoolean, nil
	case "integer", "number":
		return serializationNumber, nil
	case "string":
		return serializationString, nil
	case "array":
		return serializationArray, nil
	case "object":
		return serializationObject, nil
	default:
		return 0, fmt.Errorf("%w: unknown schema type %q",
			ErrInvalidSerializationDataType, name)
	}
}

func selectedSerializationDataType(
	allowed serializationTypeMask,
	constrained bool,
) (SerializationDataType, error) {
	if !constrained || allowed == allSerializationTypes {
		return SerializationDataTypeAny, nil
	}
	count := bits.OnesCount8(uint8(allowed))
	if count == 0 {
		return "", fmt.Errorf("%w: incompatible schema types",
			ErrInvalidSerializationDataType)
	}
	if count != 1 {
		return "", ErrAmbiguousSerializationDataType
	}
	types := [...]SerializationDataType{
		serializationNull:    SerializationDataTypeNull,
		serializationBoolean: SerializationDataTypeBoolean,
		serializationNumber:  SerializationDataTypeNumber,
		serializationString:  SerializationDataTypeString,
		serializationArray:   SerializationDataTypeArray,
		serializationObject:  SerializationDataTypeObject,
	}
	return types[allowed], nil
}

func serializationDataType(value jsonvalue.Value) (
	SerializationDataType,
	bool,
) {
	types := [...]SerializationDataType{
		jsonvalue.InvalidKind: "",
		jsonvalue.NullKind:    SerializationDataTypeNull,
		jsonvalue.BooleanKind: SerializationDataTypeBoolean,
		jsonvalue.NumberKind:  SerializationDataTypeNumber,
		jsonvalue.StringKind:  SerializationDataTypeString,
		jsonvalue.ArrayKind:   SerializationDataTypeArray,
		jsonvalue.ObjectKind:  SerializationDataTypeObject,
	}
	dataType := types[value.Kind()]
	return dataType, dataType != ""
}
