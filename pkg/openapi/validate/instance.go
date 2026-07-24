package validate

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	openapischema "github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

// InstanceDirection selects direction-dependent Schema Object semantics.
type InstanceDirection string

const (
	// DirectionNeutral applies direction-independent schema semantics only.
	DirectionNeutral InstanceDirection = ""
	// DirectionRequest rejects values for properties marked readOnly.
	DirectionRequest InstanceDirection = "request"
	// DirectionResponse permits readOnly properties in response values.
	DirectionResponse InstanceDirection = "response"
)

// InstanceOptions controls direction-dependent Schema Object evaluation.
type InstanceOptions struct {
	Direction InstanceDirection
	// MaxNodes bounds direction-dependent schema and instance traversal. Zero
	// uses a conservative default.
	MaxNodes int
	// SchemaName identifies a named Swagger definition for discriminator
	// evaluation. An empty name is inferred when the schema exactly matches a
	// definition in the supplied document.
	SchemaName        string
	instanceValidator func(
		*openapischema.Schema, context.Context, []byte,
	) (openapischema.Result, error)
	directionMarshaller func(jsonvalue.Value) ([]byte, error)
}

// NamedBinaryMediaPart identifies one raw binary multipart value by its Schema
// Object property name.
type NamedBinaryMediaPart struct {
	Name   string
	Octets uint64
}

// PositionalBinaryMediaPart identifies one raw binary multipart value by its
// zero-based sequential-media item index.
type PositionalBinaryMediaPart struct {
	Index  int
	Octets uint64
}

type binaryPartSchema struct {
	value    jsonvalue.Value
	resource reference.Resource
}

// BinaryMediaLength validates the octet length of an unencoded binary payload
// against a Schema Object maxLength without loading the payload into memory.
func BinaryMediaLength(
	ctx context.Context,
	document openapi.Document,
	schema jsonvalue.Value,
	octets uint64,
) (Report, error) {
	return BinaryMediaLengthWithOptions(
		ctx, document, schema, octets, DefaultOptions(),
	)
}

// BinaryMediaLengthWithOptions applies BinaryMediaLength with an explicit
// retrieval URI, resolver, and bounded schema traversal policy.
func BinaryMediaLengthWithOptions(
	ctx context.Context,
	document openapi.Document,
	schema jsonvalue.Value,
	octets uint64,
	options Options,
) (Report, error) {
	if ctx == nil {
		return Report{}, fmt.Errorf("validate binary media length: nil context")
	}
	if document == nil {
		return Report{}, fmt.Errorf("validate binary media length: nil document")
	}
	if document.SpecificationVersion().Dialect() == specversion.DialectSwagger20 {
		return Report{}, fmt.Errorf(
			"validate binary media length: OpenAPI 3 document required",
		)
	}
	if schema.Kind() != jsonvalue.ObjectKind &&
		schema.Kind() != jsonvalue.BooleanKind {
		return Report{}, fmt.Errorf(
			"validate binary media length: schema is not an object or boolean",
		)
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	if min(
		options.ReferenceLimits.MaxTraversalDepth,
		options.ReferenceLimits.MaxTraversalNodes,
		options.ReferenceLimits.MaxReferenceDepth,
	) < 0 || !validReferenceResourceURI(options.ReferenceResourceURI) {
		return Report{}, fmt.Errorf(
			"validate binary media length: invalid reference options",
		)
	}
	options.ReferenceLimits = normalizedReferenceLimits(options.ReferenceLimits)
	length := new(big.Int).SetUint64(octets)
	remaining := options.ReferenceLimits.MaxTraversalNodes
	constraints, err := binaryMaxLengthConstraints(
		ctx,
		validationResource(document, options.ReferenceResourceURI),
		schema,
		document.SpecificationVersion().Dialect(),
		options.ReferenceResolver,
		options.ReferenceLimits,
		make(map[string]struct{}),
		&remaining,
		0,
	)
	if err != nil {
		return Report{}, err
	}
	exceeded := false
	for _, limit := range constraints {
		if length.Cmp(limit) > 0 {
			exceeded = true
			break
		}
	}
	if !exceeded {
		return Report{}, nil
	}
	section := "working-with-binary-data"
	switch document.SpecificationVersion().Dialect() {
	case specversion.DialectOAS32:
		section = "binary-streams"
	}
	return Report{diagnostics: []Diagnostic{{
		Code:                 "openapi.schema.binary.max-length",
		Message:              "binary payload exceeds the Schema Object maxLength",
		Severity:             SeverityError,
		Source:               SourceSchema,
		KeywordLocation:      "/maxLength",
		SpecificationVersion: document.SpecificationVersion().String(),
		SpecificationSection: section,
	}}}, nil
}

// MultipartBinaryMediaLengths partitions a Media Type Object's named property,
// prefixItems, items, and itemSchema subschemas before applying raw binary
// maxLength constraints to each supplied part.
func MultipartBinaryMediaLengths(
	ctx context.Context,
	document openapi.Document,
	mediaType jsonvalue.Value,
	named []NamedBinaryMediaPart,
	positional []PositionalBinaryMediaPart,
	options Options,
) (Report, error) {
	if ctx == nil {
		return Report{}, fmt.Errorf("validate multipart binary lengths: nil context")
	}
	if document == nil {
		return Report{}, fmt.Errorf("validate multipart binary lengths: nil document")
	}
	if document.SpecificationVersion().Dialect() != specversion.DialectOAS32 {
		return Report{}, fmt.Errorf(
			"validate multipart binary lengths: OpenAPI 3.2 document required",
		)
	}
	if mediaType.Kind() != jsonvalue.ObjectKind {
		return Report{}, fmt.Errorf(
			"validate multipart binary lengths: media type is not an object",
		)
	}
	if min(
		options.ReferenceLimits.MaxTraversalDepth,
		options.ReferenceLimits.MaxTraversalNodes,
		options.ReferenceLimits.MaxReferenceDepth,
	) < 0 || !validReferenceResourceURI(options.ReferenceResourceURI) {
		return Report{}, fmt.Errorf(
			"validate multipart binary lengths: invalid reference options",
		)
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	options.ReferenceLimits = normalizedReferenceLimits(options.ReferenceLimits)
	resource := validationResource(document, options.ReferenceResourceURI)
	var diagnostics []Diagnostic
	var properties map[string]encodingSchemaProperty
	if schema, exists := mediaType.Lookup("schema"); exists {
		properties, _ = encodingSchemaProperties(ctx, resource, schema, options)
	}
	for _, part := range named {
		if part.Name == "" {
			return Report{}, fmt.Errorf(
				"validate multipart binary lengths: empty part name",
			)
		}
		property, exists := properties[part.Name]
		if !exists {
			continue
		}
		exceeded, err := binaryPartLengthExceeded(
			ctx,
			document,
			[]binaryPartSchema{{value: property.value, resource: property.resource}},
			part.Octets,
			options,
		)
		if err != nil {
			return Report{}, err
		}
		if exceeded {
			diagnostics = append(diagnostics, binaryPartLengthDiagnostic(
				document,
				"/"+escapePointer(part.Name),
			))
		}
	}
	for _, part := range positional {
		if part.Index < 0 {
			return Report{}, fmt.Errorf(
				"validate multipart binary lengths: negative part index",
			)
		}
		schemas, err := positionalBinaryPartSchemas(
			ctx,
			resource,
			mediaType,
			part.Index,
			options,
		)
		if err != nil {
			return Report{}, err
		}
		exceeded, err := binaryPartLengthExceeded(
			ctx,
			document,
			schemas,
			part.Octets,
			options,
		)
		if err != nil {
			return Report{}, err
		}
		if exceeded {
			diagnostics = append(diagnostics, binaryPartLengthDiagnostic(
				document,
				"/"+strconv.Itoa(part.Index),
			))
		}
	}
	return Report{diagnostics: diagnostics}, nil
}

func binaryPartLengthExceeded(
	ctx context.Context,
	document openapi.Document,
	schemas []binaryPartSchema,
	octets uint64,
	options Options,
) (bool, error) {
	length := new(big.Int).SetUint64(octets)
	for _, schema := range schemas {
		remaining := options.ReferenceLimits.MaxTraversalNodes
		constraints, err := binaryMaxLengthConstraints(
			ctx,
			schema.resource,
			schema.value,
			document.SpecificationVersion().Dialect(),
			options.ReferenceResolver,
			options.ReferenceLimits,
			make(map[string]struct{}),
			&remaining,
			0,
		)
		if err != nil {
			return false, err
		}
		for _, limit := range constraints {
			if length.Cmp(limit) > 0 {
				return true, nil
			}
		}
	}
	return false, nil
}

func binaryPartLengthDiagnostic(
	document openapi.Document,
	pointer string,
) Diagnostic {
	return Diagnostic{
		Code:                 "openapi.schema.binary.max-length",
		Message:              "binary multipart part exceeds its Schema Object maxLength",
		Severity:             SeverityError,
		Source:               SourceSchema,
		InstanceLocation:     pointer,
		KeywordLocation:      "/maxLength",
		SpecificationVersion: document.SpecificationVersion().String(),
		SpecificationSection: "binary-streams",
	}
}

func positionalBinaryPartSchemas(
	ctx context.Context,
	resource reference.Resource,
	mediaType jsonvalue.Value,
	index int,
	options Options,
) ([]binaryPartSchema, error) {
	var result []binaryPartSchema
	if itemSchema, exists := mediaType.Lookup("itemSchema"); exists {
		result = append(result, binaryPartSchema{
			value: itemSchema, resource: resource,
		})
	}
	schema, exists := mediaType.Lookup("schema")
	if !exists {
		return result, nil
	}
	remaining := options.ReferenceLimits.MaxTraversalNodes
	err := collectPositionalBinaryPartSchemas(
		ctx,
		resource,
		schema,
		index,
		options.ReferenceResolver,
		options.ReferenceLimits,
		make(map[string]struct{}),
		&remaining,
		0,
		&result,
	)
	return result, err
}

func collectPositionalBinaryPartSchemas(
	ctx context.Context,
	resource reference.Resource,
	schema jsonvalue.Value,
	index int,
	resolver reference.Resolver,
	limits reference.Limits,
	visited map[string]struct{},
	remaining *int,
	depth int,
	result *[]binaryPartSchema,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if *remaining < 1 || depth >= limits.MaxTraversalDepth {
		return ErrLimitExceeded
	}
	if schema.Kind() == jsonvalue.BooleanKind {
		return nil
	}
	if schema.Kind() != jsonvalue.ObjectKind {
		return fmt.Errorf(
			"validate multipart binary lengths: schema is not an object or boolean",
		)
	}
	(*remaining)--
	if rawReference, referenced := stringMember(schema, "$ref"); referenced {
		identity := resource.CanonicalURI
		if identity == "" {
			identity = resource.RetrievalURI
		}
		identity += "\x00" + rawReference
		if _, seen := visited[identity]; !seen {
			visited[identity] = struct{}{}
			target, err := reference.Resolve(
				ctx,
				resource,
				rawReference,
				resolver,
				limits,
			)
			if err != nil {
				return fmt.Errorf(
					"validate multipart binary lengths: unresolved schema reference: %w",
					err,
				)
			}
			if err := collectPositionalBinaryPartSchemas(
				ctx,
				target.Resource,
				target.Value,
				index,
				resolver,
				limits,
				visited,
				remaining,
				depth+1,
				result,
			); err != nil {
				return err
			}
		}
	}
	prefixCount := 0
	if prefix, exists := schema.Lookup("prefixItems"); exists {
		items, valid := prefix.Elements()
		if !valid {
			return fmt.Errorf(
				"validate multipart binary lengths: prefixItems is not an array",
			)
		}
		prefixCount = len(items)
		if index < prefixCount {
			*result = append(*result, binaryPartSchema{
				value: items[index], resource: resource,
			})
		}
	}
	if index >= prefixCount {
		if items, exists := schema.Lookup("items"); exists {
			*result = append(*result, binaryPartSchema{
				value: items, resource: resource,
			})
		}
	}
	if allOf, exists := schema.Lookup("allOf"); exists {
		members, valid := allOf.Elements()
		if !valid {
			return fmt.Errorf(
				"validate multipart binary lengths: allOf is not an array",
			)
		}
		for _, member := range members {
			if err := collectPositionalBinaryPartSchemas(
				ctx,
				resource,
				member,
				index,
				resolver,
				limits,
				visited,
				remaining,
				depth+1,
				result,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func binaryMaxLengthConstraints(
	ctx context.Context,
	resource reference.Resource,
	schema jsonvalue.Value,
	dialect specversion.Dialect,
	resolver reference.Resolver,
	limits reference.Limits,
	visited map[string]struct{},
	remaining *int,
	depth int,
) ([]*big.Int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if *remaining < 1 || depth >= limits.MaxTraversalDepth {
		return nil, ErrLimitExceeded
	}
	(*remaining)--
	if schema.Kind() == jsonvalue.BooleanKind {
		return nil, nil
	}
	if schema.Kind() != jsonvalue.ObjectKind {
		return nil, fmt.Errorf(
			"validate binary media length: schema is not an object or boolean",
		)
	}
	var constraints []*big.Int
	if rawReference, exists := stringMember(schema, "$ref"); exists {
		identity := resource.CanonicalURI
		switch identity {
		case "":
			identity = resource.RetrievalURI
		}
		identity += "\x00" + rawReference
		if _, seen := visited[identity]; !seen {
			visited[identity] = struct{}{}
			target, err := reference.Resolve(
				ctx, resource, rawReference, resolver, limits,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"validate binary media length: unresolved schema reference: %w",
					err,
				)
			}
			resolved, err := binaryMaxLengthConstraints(
				ctx, target.Resource, target.Value, dialect, resolver, limits,
				visited, remaining,
				depth+1,
			)
			if err != nil {
				return nil, err
			}
			constraints = append(constraints, resolved...)
		}
		if dialect == specversion.DialectOAS30 {
			return constraints, nil
		}
	}
	if maximum, exists := schema.Lookup("maxLength"); exists {
		text, number := maximum.NumberText()
		limit, valid := new(big.Rat).SetString(text)
		if !number || !valid {
			return nil, fmt.Errorf(
				"validate binary media length: maxLength is not a non-negative integer",
			)
		}
		if limit.Sign() < 0 || !limit.IsInt() {
			return nil, fmt.Errorf(
				"validate binary media length: maxLength is not a non-negative integer",
			)
		}
		constraints = append(constraints, new(big.Int).Set(limit.Num()))
	}
	if allOf, exists := schema.Lookup("allOf"); exists {
		elements, valid := allOf.Elements()
		if !valid {
			return nil, fmt.Errorf(
				"validate binary media length: allOf is not an array",
			)
		}
		for _, element := range elements {
			nested, err := binaryMaxLengthConstraints(
				ctx, resource, element, dialect, resolver, limits,
				visited, remaining,
				depth+1,
			)
			if err != nil {
				return nil, err
			}
			constraints = append(constraints, nested...)
		}
	}
	return constraints, nil
}

// SequentialMediaInstance validates already-decoded OpenAPI 3.2 sequential
// media items. The Media Type Object schema applies to the complete ordered
// array, while itemSchema applies independently to each item.
func SequentialMediaInstance(
	ctx context.Context,
	document openapi.Document,
	mediaType jsonvalue.Value,
	items []jsonvalue.Value,
	options InstanceOptions,
) (Report, error) {
	if ctx == nil {
		return Report{}, fmt.Errorf("validate sequential media instance: nil context")
	}
	if document == nil {
		return Report{}, fmt.Errorf("validate sequential media instance: nil document")
	}
	if document.SpecificationVersion().Dialect() != specversion.DialectOAS32 {
		return Report{}, fmt.Errorf(
			"validate sequential media instance: OpenAPI 3.2 document required",
		)
	}
	if mediaType.Kind() != jsonvalue.ObjectKind {
		return Report{}, fmt.Errorf(
			"validate sequential media instance: media type is not an object",
		)
	}
	if options.Direction != DirectionNeutral &&
		options.Direction != DirectionRequest &&
		options.Direction != DirectionResponse {
		return Report{}, fmt.Errorf(
			"validate sequential media instance: invalid direction",
		)
	}
	if options.MaxNodes < 0 {
		return Report{}, fmt.Errorf(
			"validate sequential media instance: invalid node limit",
		)
	}
	maxItems := options.MaxNodes
	if maxItems == 0 {
		maxItems = 100_000
	}
	if len(items) > maxItems {
		return Report{}, ErrLimitExceeded
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}

	var diagnostics []Diagnostic
	if schema, exists := mediaType.Lookup("schema"); exists {
		sequence, err := jsonvalue.Array(items)
		if err != nil {
			return Report{}, fmt.Errorf(
				"validate sequential media instance: %w", err,
			)
		}
		report, err := SchemaInstance(
			ctx, document, schema, sequence, options,
		)
		if err != nil {
			return Report{}, fmt.Errorf(
				"validate sequential media instance schema: %w", err,
			)
		}
		diagnostics = appendSequentialMediaDiagnostics(
			diagnostics,
			report.Diagnostics(),
			"openapi.media-type.schema.instance",
			"",
		)
	}
	if itemSchema, exists := mediaType.Lookup("itemSchema"); exists {
		for index, item := range items {
			report, err := SchemaInstance(
				ctx, document, itemSchema, item, options,
			)
			if err != nil {
				return Report{}, fmt.Errorf(
					"validate sequential media item %d: %w", index, err,
				)
			}
			diagnostics = appendSequentialMediaDiagnostics(
				diagnostics,
				report.Diagnostics(),
				"openapi.media-type.item-schema.instance",
				"/"+strconv.Itoa(index),
			)
		}
	}
	return Report{diagnostics: diagnostics}, nil
}

func appendSequentialMediaDiagnostics(
	destination []Diagnostic,
	diagnostics []Diagnostic,
	instanceCode string,
	prefix string,
) []Diagnostic {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "openapi.schema.instance" {
			diagnostic.Code = instanceCode
		}
		diagnostic.InstanceLocation = prefix + diagnostic.InstanceLocation
		diagnostic.SpecificationSection = "sequential-media-types"
		destination = append(destination, diagnostic)
	}
	return destination
}

// SchemaInstance validates a JSON value against an OpenAPI Schema Object and
// applies request or response direction semantics. It performs no I/O.
func SchemaInstance(
	ctx context.Context,
	document openapi.Document,
	schema jsonvalue.Value,
	instance jsonvalue.Value,
	options InstanceOptions,
) (Report, error) {
	if ctx == nil {
		return Report{}, fmt.Errorf("validate schema instance: nil context")
	}
	if document == nil {
		return Report{}, fmt.Errorf("validate schema instance: nil document")
	}
	if options.Direction != DirectionNeutral &&
		options.Direction != DirectionRequest &&
		options.Direction != DirectionResponse {
		return Report{}, fmt.Errorf("validate schema instance: invalid direction")
	}
	if options.MaxNodes < 0 {
		return Report{}, fmt.Errorf("validate schema instance: invalid node limit")
	}
	if options.MaxNodes == 0 {
		options.MaxNodes = 100_000
	}
	switch schema.Kind() {
	case jsonvalue.ObjectKind, jsonvalue.BooleanKind:
	default:
		return Report{}, fmt.Errorf("validate schema instance: schema is not an object or boolean")
	}
	effectiveSchema, resolved := resolveReferencedSchema(
		ctx,
		document.Raw(),
		schema,
	)
	if !resolved {
		return Report{}, fmt.Errorf("validate schema instance: unresolved schema reference")
	}
	compileSchema := effectiveSchema
	if options.Direction != DirectionNeutral {
		visited := 0
		transformed, transformErr := schemaForInstanceDirection(
			effectiveSchema, options.Direction, options.MaxNodes, &visited,
		)
		if transformErr != nil {
			return Report{}, transformErr
		}
		compileSchema = transformed
	}
	rawInstance, err := instance.MarshalJSON()
	if err != nil {
		return Report{}, fmt.Errorf("validate schema instance: %w", err)
	}
	compiler, err := openapischema.NewCompilerForDocument(document)
	if err != nil {
		return Report{}, fmt.Errorf("validate schema instance: %w", err)
	}
	compiled, err := compiler.Compile(ctx, compileSchema)
	if err != nil {
		return Report{}, fmt.Errorf("validate schema instance: %w", err)
	}
	instanceValidator := options.instanceValidator
	if instanceValidator == nil {
		instanceValidator = func(
			schema *openapischema.Schema,
			ctx context.Context,
			instance []byte,
		) (openapischema.Result, error) {
			return schema.Validate(ctx, instance)
		}
	}
	result, err := instanceValidator(compiled, ctx, rawInstance)
	if err != nil {
		return Report{}, fmt.Errorf("validate schema instance: %w", err)
	}
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	if !result.Valid {
		diagnostics = append(diagnostics, instanceDiagnostic(
			version,
			"openapi.schema.instance",
			"",
			"value does not conform to the Schema Object",
		))
	}
	if options.Direction != DirectionNeutral {
		directionDiagnostics, directionErr := schemaDirectionDiagnostics(
			ctx, document.Raw(), effectiveSchema, instance, version,
			document.SpecificationVersion().Dialect(), options.Direction,
			options.MaxNodes, options.directionMarshaller,
		)
		if directionErr != nil {
			return Report{}, directionErr
		}
		diagnostics = append(diagnostics, directionDiagnostics...)
	}
	switch document.SpecificationVersion().Dialect() {
	case specversion.DialectSwagger20:
		diagnostics = append(
			diagnostics,
			swaggerDiscriminatorInstanceDiagnostics(
				document.Raw(), effectiveSchema, instance, options.SchemaName, version,
			)...,
		)
	default:
		diagnostics = append(
			diagnostics,
			openAPIDiscriminatorInstanceDiagnostics(
				document.Raw(), effectiveSchema, instance, version,
			)...,
		)
	}
	return Report{diagnostics: deduplicateInstanceDiagnostics(diagnostics)}, nil
}

func openAPIDiscriminatorInstanceDiagnostics(
	root jsonvalue.Value,
	schema jsonvalue.Value,
	instance jsonvalue.Value,
	version string,
) []Diagnostic {
	discriminator, exists := objectMember(schema, "discriminator")
	if !exists || instance.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	propertyName, exists := stringMember(discriminator, "propertyName")
	if !exists {
		return nil
	}
	value, exists := instance.Lookup(propertyName)
	if !exists {
		return nil
	}
	actual, valid := value.Text()
	if !valid {
		return discriminatorValueDiagnostic(version, propertyName)
	}
	target := actual
	if mapping, exists := objectMember(discriminator, "mapping"); exists {
		if mapped, exists := stringMember(mapping, actual); exists {
			target = mapped
		}
	}
	if !strings.ContainsAny(target, "/?#:") {
		target = "#/components/schemas/" + escapePointer(target)
	}
	name := localComponentSchemaName(target)
	if name == "" {
		return nil
	}
	schemas, exists := openAPIComponentSchemas(root)
	if !exists {
		return discriminatorValueDiagnostic(version, propertyName)
	}
	if _, exists := schemas.Lookup(name); !exists {
		return discriminatorValueDiagnostic(version, propertyName)
	}
	alternatives, baseReference := openAPIDiscriminatorAlternatives(
		root, schemaLocation{value: schema},
	)
	if len(alternatives) == 0 || target == baseReference {
		return nil
	}
	if _, exists := alternatives[target]; exists {
		return nil
	}
	return discriminatorValueDiagnostic(version, propertyName)
}

func localComponentSchemaName(target string) string {
	if !strings.HasPrefix(target, "#") {
		return ""
	}
	fragment, err := reference.ParseFragment(strings.TrimPrefix(target, "#"))
	if err != nil || fragment.Kind() != reference.FragmentPointer {
		return ""
	}
	tokens := fragment.Pointer().Tokens()
	if len(tokens) != 3 || tokens[0] != "components" || tokens[1] != "schemas" {
		return ""
	}
	return tokens[2]
}

func discriminatorValueDiagnostic(version, propertyName string) []Diagnostic {
	return []Diagnostic{instanceDiagnostic(
		version,
		"openapi.schema.discriminator.value",
		"/"+escapePointer(propertyName),
		"discriminator value must identify a listed or inherited schema",
	)}
}

type schemaInstancePair struct {
	schema   jsonvalue.Value
	instance jsonvalue.Value
	pointer  string
}

func schemaDirectionDiagnostics(
	ctx context.Context,
	root jsonvalue.Value,
	schema jsonvalue.Value,
	instance jsonvalue.Value,
	version string,
	dialect specversion.Dialect,
	direction InstanceDirection,
	maxNodes int,
	marshaller func(jsonvalue.Value) ([]byte, error),
) ([]Diagnostic, error) {
	if marshaller == nil {
		marshaller = func(value jsonvalue.Value) ([]byte, error) {
			return value.MarshalJSON()
		}
	}
	pending := []schemaInstancePair{{schema: schema, instance: instance}}
	var diagnostics []Diagnostic
	seen := make(map[string]struct{})
	visited := 0
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		visited++
		if visited > maxNodes {
			return nil, ErrLimitExceeded
		}
		last := len(pending) - 1
		pair := pending[last]
		pending = pending[:last]
		resolved, ok := resolveReferencedObject(ctx, root, pair.schema)
		if !ok {
			continue
		}
		rawSchema, err := marshaller(resolved)
		if err != nil {
			return nil, fmt.Errorf("validate schema instance direction: %w", err)
		}
		key := string(rawSchema) + "\x00" + pair.pointer
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		for _, field := range []string{"allOf", "anyOf", "oneOf"} {
			branches, exists := resolved.Lookup(field)
			if !exists {
				continue
			}
			elements, _ := branches.Elements()
			for _, branch := range elements {
				pending = append(pending, schemaInstancePair{
					schema: branch, instance: pair.instance, pointer: pair.pointer,
				})
			}
		}
		properties, hasProperties := objectMember(resolved, "properties")
		if hasProperties && pair.instance.Kind() == jsonvalue.ObjectKind {
			members, _ := properties.Members()
			for _, property := range members {
				value, present := pair.instance.Lookup(property.Name)
				if !present {
					continue
				}
				pointer := pair.pointer + "/" + escapePointer(property.Name)
				propertySchema, ok := resolveReferencedObject(ctx, root, property.Value)
				if !ok {
					continue
				}
				field := "readOnly"
				code := "openapi.schema.read-only.request"
				message := "read-only property should not be present in a request"
				if direction == DirectionResponse {
					field = "writeOnly"
					code = "openapi.schema.write-only.response"
					message = "write-only property should not be present in a response"
				}
				if trueMember(propertySchema, field) {
					diagnostic := instanceDiagnostic(version, code, pointer, message)
					if dialect != specversion.DialectSwagger20 {
						diagnostic.Severity = SeverityWarning
					}
					diagnostics = append(diagnostics, diagnostic)
				}
				pending = append(pending, schemaInstancePair{
					schema: propertySchema, instance: value, pointer: pointer,
				})
			}
		}
		if items, exists := resolved.Lookup("items"); exists &&
			pair.instance.Kind() == jsonvalue.ArrayKind {
			values, _ := pair.instance.Elements()
			for index, value := range values {
				pending = append(pending, schemaInstancePair{
					schema:   items,
					instance: value,
					pointer:  fmt.Sprintf("%s/%d", pair.pointer, index),
				})
			}
		}
	}
	return diagnostics, nil
}

func schemaForInstanceDirection(
	schema jsonvalue.Value,
	direction InstanceDirection,
	maxNodes int,
	visited *int,
) (jsonvalue.Value, error) {
	*visited++
	if *visited > maxNodes {
		return jsonvalue.Value{}, ErrLimitExceeded
	}
	if schema.Kind() != jsonvalue.ObjectKind || isReference(schema) {
		return schema, nil
	}
	members, _ := schema.Members()
	properties, _ := objectMember(schema, "properties")
	for index := range members {
		var err error
		switch members[index].Name {
		case "properties", "$defs", "definitions", "patternProperties",
			"dependentSchemas":
			members[index].Value, err = transformDirectionalSchemaMap(
				members[index].Value, direction, maxNodes, visited,
			)
		case "allOf", "anyOf", "oneOf", "prefixItems":
			members[index].Value, err = transformDirectionalSchemaArray(
				members[index].Value, direction, maxNodes, visited,
			)
		case "items", "additionalItems", "additionalProperties",
			"unevaluatedProperties", "propertyNames", "contains",
			"unevaluatedItems", "contentSchema", "if", "then", "else", "not":
			members[index].Value, err = schemaForInstanceDirection(
				members[index].Value, direction, maxNodes, visited,
			)
		case "required":
			members[index].Value, err = directionalRequired(
				members[index].Value, properties, direction,
			)
		}
		if err != nil {
			return jsonvalue.Value{}, err
		}
	}
	return jsonvalue.Object(members)
}

func transformDirectionalSchemaMap(
	value jsonvalue.Value,
	direction InstanceDirection,
	maxNodes int,
	visited *int,
) (jsonvalue.Value, error) {
	members, ok := value.Members()
	if !ok {
		return value, nil
	}
	for index := range members {
		transformed, err := schemaForInstanceDirection(
			members[index].Value, direction, maxNodes, visited,
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		members[index].Value = transformed
	}
	return jsonvalue.Object(members)
}

func transformDirectionalSchemaArray(
	value jsonvalue.Value,
	direction InstanceDirection,
	maxNodes int,
	visited *int,
) (jsonvalue.Value, error) {
	elements, ok := value.Elements()
	if !ok {
		return value, nil
	}
	for index := range elements {
		transformed, err := schemaForInstanceDirection(
			elements[index], direction, maxNodes, visited,
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		elements[index] = transformed
	}
	return jsonvalue.Array(elements)
}

func directionalRequired(
	required jsonvalue.Value,
	properties jsonvalue.Value,
	direction InstanceDirection,
) (jsonvalue.Value, error) {
	elements, ok := required.Elements()
	if !ok || properties.Kind() != jsonvalue.ObjectKind {
		return required, nil
	}
	field := "readOnly"
	if direction == DirectionResponse {
		field = "writeOnly"
	}
	filtered := make([]jsonvalue.Value, 0, len(elements))
	for _, element := range elements {
		name, valid := element.Text()
		property, exists := properties.Lookup(name)
		if valid && exists && trueMember(property, field) {
			continue
		}
		filtered = append(filtered, element)
	}
	return jsonvalue.Array(filtered)
}

func swaggerDiscriminatorInstanceDiagnostics(
	root jsonvalue.Value,
	schema jsonvalue.Value,
	instance jsonvalue.Value,
	schemaName string,
	version string,
) []Diagnostic {
	propertyName, exists := stringMember(schema, "discriminator")
	if !exists || instance.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	value, exists := instance.Lookup(propertyName)
	if !exists {
		return nil
	}
	actual, valid := value.Text()
	if schemaName == "" {
		schemaName = swaggerDefinitionName(root, schema)
	}
	allowed := swaggerDiscriminatorNames(root, schemaName)
	if valid {
		if _, exists := allowed[actual]; exists {
			return nil
		}
	}
	return []Diagnostic{instanceDiagnostic(
		version,
		"openapi.schema.discriminator.value",
		"/"+escapePointer(propertyName),
		"discriminator value must name this schema or an inheriting schema",
	)}
}

func swaggerDefinitionName(root jsonvalue.Value, schema jsonvalue.Value) string {
	definitions, exists := objectMember(root, "definitions")
	if !exists {
		return ""
	}
	rawSchema, err := schema.MarshalJSON()
	if err != nil {
		return ""
	}
	members, _ := definitions.Members()
	for _, member := range members {
		rawCandidate, err := member.Value.MarshalJSON()
		if err == nil && string(rawCandidate) == string(rawSchema) {
			return member.Name
		}
	}
	return ""
}

func swaggerDiscriminatorNames(root jsonvalue.Value, schemaName string) map[string]struct{} {
	allowed := make(map[string]struct{})
	if schemaName == "" {
		return allowed
	}
	allowed[schemaName] = struct{}{}
	definitions, exists := objectMember(root, "definitions")
	if !exists {
		return allowed
	}
	parents := make(map[string][]string)
	members, _ := definitions.Members()
	for _, member := range members {
		allOf, exists := member.Value.Lookup("allOf")
		if !exists || allOf.Kind() != jsonvalue.ArrayKind {
			continue
		}
		branches, _ := allOf.Elements()
		for _, branch := range branches {
			if parent := swaggerDefinitionReferenceName(branch); parent != "" {
				parents[member.Name] = append(parents[member.Name], parent)
			}
		}
	}
	changed := true
	for changed {
		changed = false
		for child, directParents := range parents {
			if _, included := allowed[child]; included {
				continue
			}
			for _, parent := range directParents {
				if _, included := allowed[parent]; included {
					allowed[child] = struct{}{}
					changed = true
					break
				}
			}
		}
	}
	return allowed
}

func swaggerDefinitionReferenceName(value jsonvalue.Value) string {
	raw, exists := stringMember(value, "$ref")
	if !exists || !strings.HasPrefix(raw, "#/") {
		return ""
	}
	fragment, err := reference.ParseFragment(strings.TrimPrefix(raw, "#"))
	if err != nil || fragment.Kind() != reference.FragmentPointer {
		return ""
	}
	tokens := fragment.Pointer().Tokens()
	if len(tokens) != 2 || tokens[0] != "definitions" {
		return ""
	}
	return tokens[1]
}

func instanceDiagnostic(
	version string,
	code string,
	pointer string,
	message string,
) Diagnostic {
	return Diagnostic{
		Code: code, Message: message, Severity: SeverityError,
		Source: SourceSchema, InstanceLocation: pointer,
		SpecificationVersion: version,
		SpecificationSection: "schema-instance",
	}
}

func deduplicateInstanceDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	result := make([]Diagnostic, 0, len(diagnostics))
	seen := make(map[string]struct{}, len(diagnostics))
	for _, diagnostic := range diagnostics {
		key := diagnostic.Code + "\x00" + diagnostic.InstanceLocation
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, diagnostic)
	}
	return result
}
