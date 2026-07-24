package validate

import (
	"bytes"
	"context"
	"math/big"
	"net/url"
	"reflect"
	"strings"
	"unicode"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	openapischema "github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parameter"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func validateExamples(
	ctx context.Context,
	document openapi.Document,
	options Options,
) []Diagnostic {
	if document.SpecificationVersion().Dialect() == specversion.DialectSwagger20 {
		return validateSwaggerExampleMediaTypes(ctx, document, options)
	}
	version := document.SpecificationVersion().String()
	dialect := document.SpecificationVersion().Dialect()
	var diagnostics []Diagnostic
	for _, example := range exampleObjects(document) {
		_, hasValue := example.value.Lookup("value")
		_, hasExternal := example.value.Lookup("externalValue")
		_, hasData := example.value.Lookup("dataValue")
		_, hasSerialized := example.value.Lookup("serializedValue")
		if dialect == specversion.DialectOAS32 && hasExternal {
			externalValue, _ := example.value.Lookup("externalValue")
			raw, valid := externalValue.Text()
			_, parseErr := url.Parse(raw)
			if !valid || parseErr != nil ||
				strings.IndexFunc(raw, unicode.IsControl) >= 0 {
				diagnostics = append(diagnostics, Diagnostic{
					Code:                 "openapi.example.external-value.invalid",
					Message:              "externalValue must be a valid URI-reference",
					Severity:             SeverityError,
					Source:               SourceDocument,
					InstanceLocation:     example.pointer + "/externalValue",
					SpecificationVersion: version,
					SpecificationSection: "example-object",
				})
			}
		}
		conflict := hasValue && hasExternal
		if dialect == specversion.DialectOAS32 {
			conflict = conflict || hasValue && (hasData || hasSerialized) ||
				hasSerialized && hasExternal
		}
		if !conflict {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Code:                 "openapi.example.value.conflict",
			Message:              "example value source fields are mutually exclusive",
			Severity:             SeverityError,
			Source:               SourceDocument,
			InstanceLocation:     example.pointer,
			SpecificationVersion: version,
			SpecificationSection: "example-object",
		})
	}
	diagnostics = append(
		diagnostics,
		validateSerializedExampleFraming(ctx, document)...,
	)
	diagnostics = append(
		diagnostics,
		validateParameterExampleSerializations(ctx, document, options)...,
	)
	diagnostics = append(
		diagnostics,
		validateOpenAPIExampleSchemas(ctx, document, options)...,
	)
	if dialect == specversion.DialectOAS32 {
		diagnostics = append(
			diagnostics,
			validateAmbiguousLegacyExamples(ctx, document)...,
		)
	}
	return diagnostics
}

func validateAmbiguousLegacyExamples(
	ctx context.Context,
	document openapi.Document,
) []Diagnostic {
	root := document.Raw()
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	owners := parameterObjects(document)
	owners = append(owners, headerObjects(document)...)
	for _, owner := range owners {
		diagnostics = appendAmbiguousLegacyExampleWarnings(
			ctx, diagnostics, root, owner.value, owner.pointer, version,
		)
	}
	for _, mediaType := range mediaTypeObjects(document) {
		name := baseMediaType(mediaType.name)
		if name == "application/json" || strings.HasSuffix(name, "+json") ||
			plainStringMediaType(ctx, root, mediaType) {
			continue
		}
		diagnostics = appendAmbiguousLegacyExampleWarnings(
			ctx, diagnostics, root, mediaType.value, mediaType.pointer, version,
		)
	}
	return diagnostics
}

func plainStringMediaType(
	ctx context.Context,
	root jsonvalue.Value,
	mediaType mediaTypeLocation,
) bool {
	if baseMediaType(mediaType.name) != "text/plain" {
		return false
	}
	schema, exists := mediaType.value.Lookup("schema")
	if !exists {
		return false
	}
	resolved, ok := resolveReferencedSchema(ctx, root, schema)
	return ok && schemaHasType(resolved, "string")
}

func appendAmbiguousLegacyExampleWarnings(
	ctx context.Context,
	diagnostics []Diagnostic,
	root jsonvalue.Value,
	owner jsonvalue.Value,
	pointer string,
	version string,
) []Diagnostic {
	if _, exists := owner.Lookup("example"); exists {
		diagnostics = append(diagnostics, ambiguousLegacyExampleDiagnostic(
			pointer+"/example", version,
		))
	}
	examples, exists := objectMember(owner, "examples")
	if !exists {
		return diagnostics
	}
	members, _ := examples.Members()
	for _, member := range members {
		example, resolved := resolveReferencedObject(ctx, root, member.Value)
		if !resolved {
			continue
		}
		if _, hasValue := example.Lookup("value"); !hasValue {
			continue
		}
		examplePointer := pointer + "/examples/" +
			escapePointer(member.Name) + "/value"
		if isReference(member.Value) {
			examplePointer = pointer + "/examples/" +
				escapePointer(member.Name) + "/$ref"
		}
		diagnostics = append(diagnostics, ambiguousLegacyExampleDiagnostic(
			examplePointer, version,
		))
	}
	return diagnostics
}

func ambiguousLegacyExampleDiagnostic(pointer string, version string) Diagnostic {
	return Diagnostic{
		Code:                 "openapi.example.value.nonportable",
		Message:              "value is ambiguous for a non-JSON serialization target",
		Severity:             SeverityWarning,
		Source:               SourceDocument,
		InstanceLocation:     pointer,
		SpecificationVersion: version,
		SpecificationSection: "example-object",
	}
}

func validateParameterExampleSerializations(
	ctx context.Context,
	document openapi.Document,
	options Options,
) []Diagnostic {
	version := document.SpecificationVersion()
	resource := validationResource(document, options.ReferenceResourceURI)
	var diagnostics []Diagnostic
	for _, owner := range parameterObjects(document) {
		schema, exists := owner.value.Lookup("schema")
		if !exists {
			continue
		}
		resolved, _, ok := resolveReferencedSchemaResourceWithPolicy(
			ctx,
			resource,
			schema,
			options.ReferenceResolver,
			options.ReferenceLimits,
		)
		if !ok {
			continue
		}
		shape, ok := parameterShape(resolved)
		if !ok {
			continue
		}
		parameterOptions, err := parameter.OptionsForResolvedSchema(
			version,
			owner.value,
			resolved,
		)
		if err != nil {
			continue
		}
		name, exists := stringMember(owner.value, "name")
		if !exists {
			continue
		}
		for _, example := range parameterExamples(
			ctx,
			document.Raw(),
			owner.value,
			owner.pointer,
			version.String(),
		) {
			valid := false
			if !example.serialized {
				_, err = parameter.Encode(name, example.value, parameterOptions)
				valid = err == nil
			}
			if !valid {
				if raw, text := example.value.Text(); text {
					_, err = parameter.Decode(name, raw, shape, parameterOptions)
					valid = err == nil
				}
			}
			if valid {
				continue
			}
			diagnostics = append(diagnostics, serializedExampleDiagnostic(
				"openapi.example.parameter-serialization",
				"example does not follow the parameter serialization strategy",
				example.pointer,
				version.String(),
			))
		}
		if version.Dialect() == specversion.DialectOAS32 {
			diagnostics = append(
				diagnostics,
				validateParameterSerializedPairs(
					ctx,
					resource,
					owner,
					name,
					shape,
					parameterOptions,
					version.String(),
					options,
				)...,
			)
		}
	}
	return diagnostics
}

func validateParameterSerializedPairs(
	ctx context.Context,
	resource reference.Resource,
	owner locatedParameter,
	name string,
	shape parameter.Shape,
	parameterOptions parameter.Options,
	version string,
	options Options,
) []Diagnostic {
	examples, exists := objectMember(owner.value, "examples")
	if !exists {
		return nil
	}
	members, _ := examples.Members()
	var diagnostics []Diagnostic
	for _, member := range members {
		pointer := owner.pointer + "/examples/" + escapePointer(member.Name)
		example, ok := resolveReferencedObjectWithPolicy(
			ctx,
			resource,
			member.Value,
			options.ReferenceResolver,
			options.ReferenceLimits,
		)
		if !ok {
			continue
		}
		data, hasData := example.Lookup("dataValue")
		normalizedData := jsonvalue.Value{}
		hasNormalizedData := false
		if hasData {
			encoded, encodeErr := parameter.Encode(
				name,
				data,
				parameterOptions,
			)
			if encodeErr == nil {
				normalizedData, encodeErr = parameter.Decode(
					name,
					encoded,
					shape,
					parameterOptions,
				)
				hasNormalizedData = encodeErr == nil
			}
		}
		if serialized, hasSerialized := example.Lookup("serializedValue"); hasSerialized {
			raw, valid := serialized.Text()
			if valid {
				diagnosticPointer := pointer + "/serializedValue"
				if isReference(member.Value) {
					diagnosticPointer = pointer + "/$ref"
				}
				diagnostics = appendParameterSerializedPairDiagnostics(
					diagnostics,
					name,
					raw,
					shape,
					parameterOptions,
					normalizedData,
					hasNormalizedData,
					diagnosticPointer,
					version,
				)
			}
		}
		external, hasExternal := example.Lookup("externalValue")
		if !hasExternal || options.ExternalExampleResolver == nil {
			continue
		}
		diagnosticPointer := pointer + "/externalValue"
		if isReference(member.Value) {
			diagnosticPointer = pointer + "/$ref"
		}
		rawIdentifier, valid := external.Text()
		if !valid {
			continue
		}
		identifier, valid := externalExampleIdentifier(
			options.ReferenceResourceURI,
			rawIdentifier,
		)
		if !valid {
			continue
		}
		externalResource, resolveErr :=
			options.ExternalExampleResolver.ResolveExternalExample(ctx, identifier)
		if resolveErr != nil {
			diagnostics = append(diagnostics, serializedJSONDiagnostic(
				"openapi.example.external-value.unresolved",
				"externalValue could not be resolved by the configured resolver",
				diagnosticPointer,
				version,
			))
			continue
		}
		if len(externalResource.Data) > options.MaxExternalExampleBytes {
			diagnostics = append(diagnostics, serializedJSONDiagnostic(
				"openapi.example.external-value.limit",
				"externalValue exceeds the configured byte limit",
				diagnosticPointer,
				version,
			))
			continue
		}
		diagnostics = appendParameterSerializedPairDiagnostics(
			diagnostics,
			name,
			string(externalResource.Data),
			shape,
			parameterOptions,
			normalizedData,
			hasNormalizedData,
			diagnosticPointer,
			version,
		)
	}
	return diagnostics
}

func appendParameterSerializedPairDiagnostics(
	diagnostics []Diagnostic,
	name string,
	raw string,
	shape parameter.Shape,
	options parameter.Options,
	normalizedData jsonvalue.Value,
	hasNormalizedData bool,
	pointer string,
	version string,
) []Diagnostic {
	decoded, err := parameter.Decode(name, raw, shape, options)
	if err != nil {
		return append(diagnostics, serializedExampleDiagnostic(
			"openapi.example.parameter-serialization",
			"example does not follow the parameter serialization strategy",
			pointer,
			version,
		))
	}
	if hasNormalizedData && !equalJSONValues(decoded, normalizedData) {
		return append(diagnostics, serializedExampleDiagnostic(
			"openapi.example.parameter-data-mismatch",
			"serialized example does not represent dataValue",
			pointer,
			version,
		))
	}
	return diagnostics
}

func parameterExamples(
	ctx context.Context,
	root jsonvalue.Value,
	owner jsonvalue.Value,
	pointer string,
	version string,
) []parameterExample {
	var result []parameterExample
	if version != "3.2.0" {
		if example, exists := owner.Lookup("example"); exists {
			result = append(result, parameterExample{
				value: example, pointer: pointer + "/example",
			})
		}
	}
	examples, exists := objectMember(owner, "examples")
	if !exists {
		return result
	}
	members, _ := examples.Members()
	for _, member := range members {
		example, resolved := resolveReferencedObject(ctx, root, member.Value)
		if !resolved {
			continue
		}
		field := "value"
		serialized := false
		if version == "3.2.0" {
			field = "dataValue"
			if _, exists := example.Lookup(field); !exists {
				field = "serializedValue"
				serialized = true
			}
		}
		value, exists := example.Lookup(field)
		if !exists {
			continue
		}
		examplePointer := pointer + "/examples/" +
			escapePointer(member.Name) + "/" + field
		if isReference(member.Value) {
			examplePointer = pointer + "/examples/" +
				escapePointer(member.Name) + "/$ref"
		}
		result = append(result, parameterExample{
			value: value, pointer: examplePointer, serialized: serialized,
		})
	}
	return result
}

func parameterShape(schema jsonvalue.Value) (parameter.Shape, bool) {
	typeName, exists := stringMember(schema, "type")
	if !exists {
		return 0, false
	}
	switch typeName {
	case "array":
		return parameter.Array, true
	case "object":
		return parameter.Object, true
	case "boolean", "integer", "number", "string":
		return parameter.Primitive, true
	default:
		return 0, false
	}
}

type serializedExample struct {
	value   string
	pointer string
}

type parameterExample struct {
	value      jsonvalue.Value
	pointer    string
	serialized bool
}

func validateSerializedExampleFraming(
	ctx context.Context,
	document openapi.Document,
) []Diagnostic {
	root := document.Raw()
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	diagnostics = append(
		diagnostics,
		validateSerializedParameterExamples(
			ctx,
			document,
			version == "3.1.2" ||
				document.SpecificationVersion().Dialect() == specversion.DialectOAS32,
		)...,
	)
	for _, mediaType := range mediaTypeObjects(document) {
		if baseMediaType(mediaType.name) != "application/x-www-form-urlencoded" {
			continue
		}
		for _, example := range serializedExamples(
			ctx,
			root,
			mediaType.value,
			mediaType.pointer,
			version,
		) {
			diagnostics = appendLeadingDelimiterDiagnostic(
				diagnostics,
				example,
				version,
			)
		}
	}
	return diagnostics
}

func validateSerializedParameterExamples(
	ctx context.Context,
	document openapi.Document,
	validateQueryDelimiter bool,
) []Diagnostic {
	root := document.Raw()
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	for _, parameter := range parameterObjects(document) {
		location, _ := stringMember(parameter.value, "in")
		name, _ := stringMember(parameter.value, "name")
		for _, example := range serializedExamples(
			ctx,
			root,
			parameter.value,
			parameter.pointer,
			version,
		) {
			if validateQueryDelimiter &&
				(location == "query" || location == "cookie") {
				diagnostics = appendLeadingDelimiterDiagnostic(
					diagnostics,
					example,
					version,
				)
			}
			if location == "header" {
				diagnostics = appendHeaderNameDiagnostic(
					diagnostics,
					example,
					name,
					version,
				)
			}
		}
	}
	for _, header := range headerObjects(document) {
		for _, example := range serializedExamples(
			ctx,
			root,
			header.value,
			header.pointer,
			version,
		) {
			diagnostics = appendHeaderNameDiagnostic(
				diagnostics,
				example,
				header.name,
				version,
			)
		}
	}
	return diagnostics
}

func serializedExamples(
	ctx context.Context,
	root jsonvalue.Value,
	owner jsonvalue.Value,
	pointer string,
	version string,
) []serializedExample {
	var result []serializedExample
	if version != "3.2.0" {
		if example, exists := owner.Lookup("example"); exists {
			if value, ok := example.Text(); ok {
				result = append(result, serializedExample{
					value: value, pointer: pointer + "/example",
				})
			}
		}
	}
	examples, exists := objectMember(owner, "examples")
	if !exists {
		return result
	}
	field := "serializedValue"
	if version != "3.2.0" {
		field = "value"
	}
	members, _ := examples.Members()
	for _, member := range members {
		example, resolved := resolveReferencedObject(ctx, root, member.Value)
		if !resolved {
			continue
		}
		fieldValue, exists := example.Lookup(field)
		if !exists {
			continue
		}
		value, ok := fieldValue.Text()
		if !ok {
			continue
		}
		examplePointer := pointer + "/examples/" +
			escapePointer(member.Name) + "/" + field
		if isReference(member.Value) {
			examplePointer = pointer + "/examples/" +
				escapePointer(member.Name) + "/$ref"
		}
		result = append(result, serializedExample{
			value: value, pointer: examplePointer,
		})
	}
	return result
}

func appendLeadingDelimiterDiagnostic(
	diagnostics []Diagnostic,
	example serializedExample,
	version string,
) []Diagnostic {
	if !strings.HasPrefix(example.value, "?") &&
		!strings.HasPrefix(example.value, "&") {
		return diagnostics
	}
	return append(diagnostics, serializedExampleDiagnostic(
		"openapi.example.query-leading-delimiter",
		"serialized example must omit its leading query delimiter",
		example.pointer,
		version,
	))
}

func appendHeaderNameDiagnostic(
	diagnostics []Diagnostic,
	example serializedExample,
	headerName string,
	version string,
) []Diagnostic {
	if headerName == "" {
		return diagnostics
	}
	prefix, _, found := strings.Cut(example.value, ":")
	if !found || !strings.EqualFold(strings.TrimSpace(prefix), headerName) {
		return diagnostics
	}
	return append(diagnostics, serializedExampleDiagnostic(
		"openapi.example.header-name",
		"serialized header example must omit the header name",
		example.pointer,
		version,
	))
}

func serializedExampleDiagnostic(
	code string,
	message string,
	pointer string,
	version string,
) Diagnostic {
	return Diagnostic{
		Code: code, Message: message, Severity: SeverityError,
		Source: SourceDocument, InstanceLocation: pointer,
		SpecificationVersion: version,
		SpecificationSection: "example-object",
	}
}

func validateOpenAPIExampleSchemas(
	ctx context.Context,
	document openapi.Document,
	options Options,
) []Diagnostic {
	compiler, err := openapischema.NewCompilerForDocument(
		document,
		schemaCompilerOptions(options)...,
	)
	if err != nil {
		return nil
	}
	owners := parameterObjects(document)
	owners = append(owners, headerObjects(document)...)
	resource := validationResource(document, options.ReferenceResourceURI)
	var diagnostics []Diagnostic
	for _, mediaType := range mediaTypeObjects(document) {
		owners = append(owners, locatedParameter{
			value: mediaType.value, pointer: mediaType.pointer,
		})
		diagnostics = append(
			diagnostics,
			validateSerializedJSONExamples(
				ctx,
				resource,
				compiler,
				mediaType,
				document.SpecificationVersion().String(),
				options,
			)...,
		)
	}
	for _, mediaType := range mediaTypeObjectsWithOptions(ctx, document, options) {
		diagnostics = append(
			diagnostics,
			validateMediaTypeCodecExamples(
				ctx,
				document,
				compiler,
				mediaType,
				options,
			)...,
		)
	}
	for _, owner := range owners {
		diagnostics = append(
			diagnostics,
			validateExampleOwnerSchema(
				ctx,
				document,
				compiler,
				owner,
				options,
			)...,
		)
	}
	return diagnostics
}

func validateMediaTypeCodecExamples(
	ctx context.Context,
	document openapi.Document,
	compiler *openapischema.Compiler,
	mediaType mediaTypeLocation,
	options Options,
) []Diagnostic {
	name := baseMediaType(mediaType.name)
	if name == "application/json" || strings.HasSuffix(name, "+json") ||
		options.MediaTypeExampleCodecResolver == nil {
		return nil
	}
	codec, err := options.MediaTypeExampleCodecResolver.
		ResolveMediaTypeExampleCodec(ctx, mediaType.name, mediaType.value)
	version := document.SpecificationVersion().String()
	if err != nil {
		return []Diagnostic{serializedJSONDiagnostic(
			"openapi.example.media-codec",
			"media type example codec could not be resolved",
			mediaType.pointer,
			version,
		)}
	}
	if mediaTypeExampleCodecIsNil(codec) {
		return nil
	}
	var compiled *openapischema.Schema
	if schema, exists := mediaType.value.Lookup("schema"); exists {
		if resolved, _, ok := resolveReferencedSchemaResourceWithPolicy(
			ctx,
			mediaType.resource,
			schema,
			options.ReferenceResolver,
			options.ReferenceLimits,
		); ok {
			compiled, _ = compiler.Compile(ctx, resolved)
		}
	}
	var diagnostics []Diagnostic
	if document.SpecificationVersion().Dialect() != specversion.DialectOAS32 {
		if example, exists := mediaType.value.Lookup("example"); exists {
			diagnostics = appendCodecDataExample(
				ctx,
				diagnostics,
				codec,
				compiled,
				example,
				mediaType.pointer+"/example",
				version,
				options.MaxExternalExampleBytes,
			)
		}
	}
	examples, exists := objectMember(mediaType.value, "examples")
	if !exists {
		return diagnostics
	}
	members, _ := examples.Members()
	for _, member := range members {
		pointer := mediaType.pointer + "/examples/" + escapePointer(member.Name)
		example, ok := resolveReferencedObjectWithPolicy(
			ctx,
			mediaType.resource,
			member.Value,
			options.ReferenceResolver,
			options.ReferenceLimits,
		)
		if !ok {
			continue
		}
		field := "value"
		if document.SpecificationVersion().Dialect() == specversion.DialectOAS32 {
			field = "dataValue"
		}
		data, hasData := example.Lookup(field)
		dataPointer := pointer + "/" + field
		if isReference(member.Value) {
			dataPointer = pointer + "/$ref"
		}
		if hasData {
			diagnostics = appendCodecDataExample(
				ctx,
				diagnostics,
				codec,
				compiled,
				data,
				dataPointer,
				version,
				options.MaxExternalExampleBytes,
			)
		}
		if serialized, hasSerialized := example.Lookup("serializedValue"); hasSerialized {
			serializedPointer := pointer + "/serializedValue"
			if isReference(member.Value) {
				serializedPointer = pointer + "/$ref"
			}
			diagnostics = appendCodecSerializedExample(
				ctx,
				diagnostics,
				codec,
				compiled,
				serialized,
				data,
				hasData,
				serializedPointer,
				version,
				options.MaxExternalExampleBytes,
			)
		}
		if external, hasExternal := example.Lookup("externalValue"); hasExternal &&
			options.ExternalExampleResolver != nil {
			externalPointer := pointer + "/externalValue"
			if isReference(member.Value) {
				externalPointer = pointer + "/$ref"
			}
			diagnostics = appendCodecExternalExample(
				ctx,
				diagnostics,
				codec,
				compiled,
				external,
				data,
				hasData,
				externalPointer,
				version,
				options,
			)
		}
	}
	return diagnostics
}

func mediaTypeExampleCodecIsNil(codec MediaTypeExampleCodec) bool {
	if codec == nil {
		return true
	}
	value := reflect.ValueOf(codec)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func appendCodecDataExample(
	ctx context.Context,
	diagnostics []Diagnostic,
	codec MediaTypeExampleCodec,
	compiled *openapischema.Schema,
	data jsonvalue.Value,
	pointer string,
	version string,
	maxBytes int,
) []Diagnostic {
	serialized, err := codec.Encode(ctx, data)
	if err != nil {
		return append(diagnostics, serializedJSONDiagnostic(
			"openapi.example.media-serialization",
			"example cannot be serialized using the configured media type codec",
			pointer,
			version,
		))
	}
	if len(serialized) > maxBytes {
		diagnostics = append(diagnostics, serializedJSONDiagnostic(
			"openapi.example.media-serialization-limit",
			"serialized example exceeds the configured byte limit",
			pointer,
			version,
		))
	}
	return appendInvalidExample(
		ctx,
		diagnostics,
		compiled,
		data,
		pointer,
		version,
	)
}

func appendCodecSerializedExample(
	ctx context.Context,
	diagnostics []Diagnostic,
	codec MediaTypeExampleCodec,
	compiled *openapischema.Schema,
	serialized jsonvalue.Value,
	data jsonvalue.Value,
	hasData bool,
	pointer string,
	version string,
	maxBytes int,
) []Diagnostic {
	raw, valid := serialized.Text()
	if !valid {
		return diagnostics
	}
	if len(raw) > maxBytes {
		return append(diagnostics, serializedJSONDiagnostic(
			"openapi.example.serialized-limit",
			"serializedValue exceeds the configured byte limit",
			pointer,
			version,
		))
	}
	decoded, err := codec.Decode(ctx, []byte(raw))
	if err != nil {
		return append(diagnostics, serializedJSONDiagnostic(
			"openapi.example.serialized-invalid",
			"serializedValue is not valid for its media type",
			pointer,
			version,
		))
	}
	if hasData && !equalJSONValues(decoded, data) {
		diagnostics = append(diagnostics, serializedJSONDiagnostic(
			"openapi.example.serialized-data-mismatch",
			"serializedValue does not represent dataValue",
			pointer,
			version,
		))
	}
	return appendCodecSchemaDiagnostic(
		ctx,
		diagnostics,
		compiled,
		decoded,
		"openapi.example.serialized-schema",
		"serializedValue does not represent schema-valid data",
		pointer,
		version,
	)
}

func appendCodecExternalExample(
	ctx context.Context,
	diagnostics []Diagnostic,
	codec MediaTypeExampleCodec,
	compiled *openapischema.Schema,
	external jsonvalue.Value,
	data jsonvalue.Value,
	hasData bool,
	pointer string,
	version string,
	options Options,
) []Diagnostic {
	rawIdentifier, valid := external.Text()
	if !valid {
		return diagnostics
	}
	identifier, valid := externalExampleIdentifier(
		options.ReferenceResourceURI,
		rawIdentifier,
	)
	if !valid {
		return diagnostics
	}
	resource, err := options.ExternalExampleResolver.ResolveExternalExample(
		ctx,
		identifier,
	)
	if err != nil {
		return append(diagnostics, serializedJSONDiagnostic(
			"openapi.example.external-value.unresolved",
			"externalValue could not be resolved by the configured resolver",
			pointer,
			version,
		))
	}
	if len(resource.Data) > options.MaxExternalExampleBytes {
		return append(diagnostics, serializedJSONDiagnostic(
			"openapi.example.external-value.limit",
			"externalValue exceeds the configured byte limit",
			pointer,
			version,
		))
	}
	decoded, err := codec.Decode(ctx, resource.Data)
	if err != nil {
		return append(diagnostics, serializedJSONDiagnostic(
			"openapi.example.external-value.invalid",
			"externalValue is not valid for its media type",
			pointer,
			version,
		))
	}
	if hasData && !equalJSONValues(decoded, data) {
		diagnostics = append(diagnostics, serializedJSONDiagnostic(
			"openapi.example.external-data-mismatch",
			"externalValue does not represent dataValue",
			pointer,
			version,
		))
	}
	return appendCodecSchemaDiagnostic(
		ctx,
		diagnostics,
		compiled,
		decoded,
		"openapi.example.external-schema",
		"externalValue does not represent schema-valid data",
		pointer,
		version,
	)
}

func appendCodecSchemaDiagnostic(
	ctx context.Context,
	diagnostics []Diagnostic,
	compiled *openapischema.Schema,
	data jsonvalue.Value,
	code string,
	message string,
	pointer string,
	version string,
) []Diagnostic {
	if compiled == nil {
		return diagnostics
	}
	raw, err := data.MarshalJSON()
	if err != nil {
		return diagnostics
	}
	result, err := compiled.Validate(ctx, raw)
	if err != nil || result.Valid {
		return diagnostics
	}
	return append(diagnostics, serializedJSONDiagnostic(
		code,
		message,
		pointer,
		version,
	))
}

func validateSerializedJSONExamples(
	ctx context.Context,
	resource reference.Resource,
	compiler *openapischema.Compiler,
	mediaType mediaTypeLocation,
	version string,
	options Options,
) []Diagnostic {
	name := baseMediaType(mediaType.name)
	if name != "application/json" && !strings.HasSuffix(name, "+json") {
		return nil
	}
	examples, exists := objectMember(mediaType.value, "examples")
	if !exists {
		return nil
	}
	members, _ := examples.Members()
	var diagnostics []Diagnostic
	var compiled *openapischema.Schema
	if schema, exists := mediaType.value.Lookup("schema"); exists {
		if resolved, _, ok := resolveReferencedSchemaResourceWithPolicy(
			ctx,
			resource,
			schema,
			options.ReferenceResolver,
			options.ReferenceLimits,
		); ok {
			compiled, _ = compiler.Compile(ctx, resolved)
		}
	}
	for _, member := range members {
		example, resolved := resolveReferencedObjectWithPolicy(
			ctx,
			resource,
			member.Value,
			options.ReferenceResolver,
			options.ReferenceLimits,
		)
		if !resolved {
			continue
		}
		examplePointer := mediaType.pointer + "/examples/" +
			escapePointer(member.Name)
		if external, exists := example.Lookup("externalValue"); exists &&
			options.ExternalExampleResolver != nil {
			externalPointer := examplePointer + "/externalValue"
			if isReference(member.Value) {
				externalPointer = examplePointer + "/$ref"
			}
			diagnostics = append(diagnostics, validateExternalJSONExample(
				ctx,
				example,
				external,
				externalPointer,
				compiled,
				version,
				options,
			)...)
		}
		serialized, exists := example.Lookup("serializedValue")
		if !exists {
			continue
		}
		pointer := mediaType.pointer + "/examples/" +
			escapePointer(member.Name) + "/serializedValue"
		if isReference(member.Value) {
			pointer = mediaType.pointer + "/examples/" +
				escapePointer(member.Name) + "/$ref"
		}
		diagnostics = append(diagnostics, Diagnostic{
			Code:     "openapi.example.serialized-json",
			Message:  "serializedValue should not be used for JSON examples",
			Severity: SeverityWarning, Source: SourceDocument,
			InstanceLocation: pointer, SpecificationVersion: version,
			SpecificationSection: "example-object",
		})
		raw, valid := serialized.Text()
		if !valid {
			continue
		}
		parsed, err := parse.JSON(
			ctx, strings.NewReader(raw), parse.DefaultLimits(),
		)
		if err != nil {
			diagnostics = append(diagnostics, serializedJSONDiagnostic(
				"openapi.example.serialized-invalid",
				"serializedValue is not valid JSON",
				pointer,
				version,
			))
			continue
		}
		if data, hasData := example.Lookup("dataValue"); hasData &&
			!equalJSONValues(parsed, data) {
			diagnostics = append(diagnostics, serializedJSONDiagnostic(
				"openapi.example.serialized-data-mismatch",
				"serializedValue does not represent dataValue",
				pointer,
				version,
			))
		}
		if compiled != nil {
			result, validationErr := compiled.Validate(ctx, []byte(raw))
			if validationErr == nil && !result.Valid {
				diagnostics = append(diagnostics, serializedJSONDiagnostic(
					"openapi.example.serialized-schema",
					"serializedValue does not represent schema-valid data",
					pointer,
					version,
				))
			}
		}
	}
	return diagnostics
}

func validateExternalJSONExample(
	ctx context.Context,
	example jsonvalue.Value,
	external jsonvalue.Value,
	pointer string,
	compiled *openapischema.Schema,
	version string,
	options Options,
) []Diagnostic {
	rawIdentifier, valid := external.Text()
	if !valid {
		return nil
	}
	identifier, valid := externalExampleIdentifier(
		options.ReferenceResourceURI,
		rawIdentifier,
	)
	if !valid {
		return nil
	}
	resource, err := options.ExternalExampleResolver.ResolveExternalExample(
		ctx,
		identifier,
	)
	if err != nil {
		return []Diagnostic{serializedJSONDiagnostic(
			"openapi.example.external-value.unresolved",
			"externalValue could not be resolved by the configured resolver",
			pointer,
			version,
		)}
	}
	if len(resource.Data) > options.MaxExternalExampleBytes {
		return []Diagnostic{serializedJSONDiagnostic(
			"openapi.example.external-value.limit",
			"externalValue exceeds the configured byte limit",
			pointer,
			version,
		)}
	}
	parsed, err := parse.JSON(
		ctx,
		bytes.NewReader(resource.Data),
		parse.DefaultLimits(),
	)
	if err != nil {
		return []Diagnostic{serializedJSONDiagnostic(
			"openapi.example.external-value.invalid",
			"externalValue does not contain valid JSON",
			pointer,
			version,
		)}
	}
	var diagnostics []Diagnostic
	if data, exists := example.Lookup("dataValue"); exists &&
		!equalJSONValues(parsed, data) {
		diagnostics = append(diagnostics, serializedJSONDiagnostic(
			"openapi.example.external-data-mismatch",
			"externalValue does not represent dataValue",
			pointer,
			version,
		))
	}
	if compiled != nil {
		result, validationErr := compiled.Validate(ctx, resource.Data)
		if validationErr == nil && !result.Valid {
			diagnostics = append(diagnostics, serializedJSONDiagnostic(
				"openapi.example.external-schema",
				"externalValue does not represent schema-valid data",
				pointer,
				version,
			))
		}
	}
	return diagnostics
}

func externalExampleIdentifier(base string, reference string) (string, bool) {
	parsed, err := url.Parse(reference)
	if err != nil {
		return "", false
	}
	if base == "" || parsed.IsAbs() {
		return parsed.String(), true
	}
	baseURI, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	return baseURI.ResolveReference(parsed).String(), true
}

func serializedJSONDiagnostic(
	code string,
	message string,
	pointer string,
	version string,
) Diagnostic {
	return Diagnostic{
		Code: code, Message: message, Severity: SeverityWarning,
		Source: SourceDocument, InstanceLocation: pointer,
		SpecificationVersion: version,
		SpecificationSection: "example-object",
	}
}

func equalJSONValues(left jsonvalue.Value, right jsonvalue.Value) bool {
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
		leftText, _ := left.NumberText()
		rightText, _ := right.NumberText()
		leftNumber, leftValid := new(big.Rat).SetString(leftText)
		rightNumber, rightValid := new(big.Rat).SetString(rightText)
		return leftValid && rightValid && leftNumber.Cmp(rightNumber) == 0
	case jsonvalue.StringKind:
		leftText, _ := left.Text()
		rightText, _ := right.Text()
		return leftText == rightText
	case jsonvalue.ArrayKind:
		leftValues, _ := left.Elements()
		rightValues, _ := right.Elements()
		if len(leftValues) != len(rightValues) {
			return false
		}
		for index := range leftValues {
			if !equalJSONValues(leftValues[index], rightValues[index]) {
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
			value, exists := right.Lookup(member.Name)
			if !exists || !equalJSONValues(member.Value, value) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func validateExampleOwnerSchema(
	ctx context.Context,
	document openapi.Document,
	compiler *openapischema.Compiler,
	owner locatedParameter,
	options Options,
) []Diagnostic {
	schema, exists := owner.value.Lookup("schema")
	if !exists {
		return nil
	}
	schema, _, resolved := resolveReferencedSchemaResourceWithPolicy(
		ctx,
		validationResource(document, options.ReferenceResourceURI),
		schema,
		options.ReferenceResolver,
		options.ReferenceLimits,
	)
	if !resolved {
		return nil
	}
	compiled, err := compiler.Compile(ctx, schema)
	if err != nil {
		return nil
	}
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	if example, exists := owner.value.Lookup("example"); exists {
		diagnostics = appendInvalidExample(
			ctx,
			diagnostics,
			compiled,
			example,
			owner.pointer+"/example",
			version,
		)
	}
	examples, exists := owner.value.Lookup("examples")
	if !exists || examples.Kind() != jsonvalue.ObjectKind {
		return diagnostics
	}
	members, _ := examples.Members()
	for _, member := range members {
		pointer := owner.pointer + "/examples/" + escapePointer(member.Name)
		diagnosticPointer := pointer + "/value"
		if isReference(member.Value) {
			diagnosticPointer = pointer + "/$ref"
		}
		exampleObject, resolved := resolveReferencedObject(
			ctx,
			document.Raw(),
			member.Value,
		)
		if !resolved {
			continue
		}
		value, exists := exampleObject.Lookup("value")
		if document.SpecificationVersion().Dialect() == specversion.DialectOAS32 {
			if dataValue, hasDataValue := exampleObject.Lookup("dataValue"); hasDataValue {
				value = dataValue
				exists = true
				if !isReference(member.Value) {
					diagnosticPointer = pointer + "/dataValue"
				}
			}
		}
		if !exists {
			continue
		}
		diagnostics = appendInvalidExample(
			ctx,
			diagnostics,
			compiled,
			value,
			diagnosticPointer,
			version,
		)
	}
	return diagnostics
}

func appendInvalidExample(
	ctx context.Context,
	diagnostics []Diagnostic,
	schema *openapischema.Schema,
	example jsonvalue.Value,
	pointer string,
	version string,
) []Diagnostic {
	raw, err := example.MarshalJSON()
	if err != nil {
		return diagnostics
	}
	result, err := schema.Validate(ctx, raw)
	if err != nil || result.Valid {
		return diagnostics
	}
	return append(diagnostics, Diagnostic{
		Code:     "openapi.example.schema",
		Message:  "example value should conform to its associated schema",
		Severity: SeverityWarning, Source: SourceDocument,
		InstanceLocation: pointer, SpecificationVersion: version,
		SpecificationSection: "example-object",
	})
}

func validateSwaggerExampleMediaTypes(
	ctx context.Context,
	document openapi.Document,
	options Options,
) []Diagnostic {
	root := document.Raw()
	resource := validationResource(document, options.ReferenceResourceURI)
	rootProduces, _ := swaggerProduces(root)
	version := document.SpecificationVersion().String()
	compiler, _ := openapischema.NewCompilerForDocument(
		document,
		schemaCompilerOptions(options)...,
	)
	var diagnostics []Diagnostic
	operations := append(
		documentOperations(document),
		externalDocumentOperations(ctx, document, options)...,
	)
	for _, operation := range operations {
		operationResource := resource
		if operation.resource.Root.Kind() == jsonvalue.ObjectKind {
			operationResource = operation.resource
		}
		produces, overridden := swaggerProduces(operation.value)
		if !overridden {
			produces = rootProduces
		}
		allowed := make(map[string]struct{}, len(produces))
		for _, mediaType := range produces {
			allowed[mediaType] = struct{}{}
		}
		responses, exists := operation.value.Lookup("responses")
		if !exists || responses.Kind() != jsonvalue.ObjectKind {
			continue
		}
		responseMembers, _ := responses.Members()
		for _, response := range responseMembers {
			responsePointer := operation.pointer + "/responses/" +
				escapePointer(response.Name)
			diagnosticPointer := ""
			if isReference(response.Value) {
				diagnosticPointer = responsePointer + "/$ref"
			}
			resolved, _, ok := resolveReferencedObjectResourceWithPolicy(
				ctx,
				operationResource,
				response.Value,
				options.ReferenceResolver,
				options.ReferenceLimits,
			)
			if !ok {
				continue
			}
			schema, hasSchema := resolved.Lookup("schema")
			if hasSchema && schemaHasType(schema, "file") && len(produces) == 0 {
				pointer := diagnosticPointer
				if pointer == "" {
					pointer = responsePointer + "/schema"
				}
				diagnostics = append(diagnostics, Diagnostic{
					Code:     "openapi.swagger.response.file.produces",
					Message:  "file response schemas should have an effective produces media type",
					Severity: SeverityWarning, Source: SourceDocument,
					InstanceLocation: pointer, SpecificationVersion: version,
					SpecificationSection: "response-object",
				})
			}
			examples, exists := resolved.Lookup("examples")
			if !exists || examples.Kind() != jsonvalue.ObjectKind {
				continue
			}
			exampleMembers, _ := examples.Members()
			for _, example := range exampleMembers {
				if hasSchema && compiler != nil {
					compiled, compileErr := compiler.Compile(ctx, schema)
					if compileErr == nil {
						rawExample, marshalErr := example.Value.MarshalJSON()
						if marshalErr == nil {
							result, validateErr := compiled.Validate(ctx, rawExample)
							if validateErr == nil && !result.Valid {
								diagnostics = append(diagnostics, Diagnostic{
									Code:     "openapi.swagger.example.schema",
									Message:  "response example should conform to the response schema",
									Severity: SeverityWarning, Source: SourceDocument,
									InstanceLocation: responsePointer + "/examples/" +
										escapePointer(example.Name),
									SpecificationVersion: version,
									SpecificationSection: "examples-object",
								})
							}
						}
					}
				}
				if _, valid := allowed[example.Name]; valid {
					continue
				}
				pointer := diagnosticPointer
				if pointer == "" {
					pointer = responsePointer + "/examples/" +
						escapePointer(example.Name)
				}
				diagnostics = append(diagnostics, Diagnostic{
					Code:                 "openapi.swagger.example.media-type",
					Message:              "response example media type must appear in effective produces",
					Severity:             SeverityError,
					Source:               SourceDocument,
					InstanceLocation:     pointer,
					SpecificationVersion: version,
					SpecificationSection: "examples-object",
				})
			}
		}
	}
	return diagnostics
}

func swaggerProduces(owner jsonvalue.Value) ([]string, bool) {
	value, exists := owner.Lookup("produces")
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

func exampleObjects(document openapi.Document) []locatedParameter {
	root := document.Raw()
	var result []locatedParameter
	if components, exists := objectMember(root, "components"); exists {
		result = appendExampleMap(
			result,
			components,
			"/components/examples",
		)
	}
	for _, parameter := range parameterObjects(document) {
		result = appendExampleMap(
			result,
			parameter.value,
			parameter.pointer+"/examples",
		)
	}
	for _, header := range headerObjects(document) {
		result = appendExampleMap(
			result,
			header.value,
			header.pointer+"/examples",
		)
	}
	for _, mediaType := range mediaTypeObjects(document) {
		result = appendExampleMap(
			result,
			mediaType.value,
			mediaType.pointer+"/examples",
		)
	}
	return result
}

func appendExampleMap(
	result []locatedParameter,
	container jsonvalue.Value,
	pointer string,
) []locatedParameter {
	examples, exists := objectMember(container, "examples")
	if !exists {
		return result
	}
	members, _ := examples.Members()
	for _, member := range members {
		if member.Value.Kind() != jsonvalue.ObjectKind || isReference(member.Value) {
			continue
		}
		result = append(result, locatedParameter{
			value:   member.Value,
			pointer: pointer + "/" + escapePointer(member.Name),
		})
	}
	return result
}
