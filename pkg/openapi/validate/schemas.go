package validate

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	openapischema "github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

type schemaLocation struct {
	value   jsonvalue.Value
	pointer string
}

type schemaCollector struct {
	dialect   specversion.Dialect
	locations []schemaLocation
}

var swaggerPropertySchemaPattern = regexp.MustCompile(`(?:^|/)properties/[^/]+$`)

func validateSchemas(
	ctx context.Context,
	document openapi.Document,
	options Options,
) ([]Diagnostic, error) {
	dialect := document.SpecificationVersion().Dialect()
	proseDiagnostics := validateSchemaProse(document)
	switch dialect {
	case specversion.DialectSwagger20,
		specversion.DialectOAS30,
		specversion.DialectOAS31,
		specversion.DialectOAS32:
	default:
		return proseDiagnostics, nil
	}
	collector := schemaCollector{dialect: dialect}
	collector.document(document.Raw())
	if len(collector.locations) == 0 {
		return nil, nil
	}
	compilerOptions := schemaCompilerOptions(options)
	compilerFactory := options.schemaCompilerFactory
	if compilerFactory == nil {
		compilerFactory = func(
			document openapi.Document,
			options ...openapischema.Option,
		) (*openapischema.Compiler, error) {
			return openapischema.NewCompilerForDocument(document, options...)
		}
	}
	compiler, err := compilerFactory(document, compilerOptions...)
	if err != nil {
		return nil, err
	}
	diagnostics := proseDiagnostics
	validated := make(map[string]openapischema.OutputUnit)
	for _, location := range collector.locations {
		switch dialect {
		case specversion.DialectSwagger20:
			if swaggerFileResponseSchema(location) {
				continue
			}
		}
		marshaller := options.schemaMarshaller
		if marshaller == nil {
			marshaller = func(value jsonvalue.Value) ([]byte, error) {
				return value.MarshalJSON()
			}
		}
		raw, err := marshaller(location.value)
		if err != nil {
			return nil, fmt.Errorf("marshal Schema Object: %w", err)
		}
		key := string(raw)
		output, exists := validated[key]
		if !exists {
			validator := options.schemaValidator
			if validator == nil {
				validator = func(
					compiler *openapischema.Compiler,
					ctx context.Context,
					value jsonvalue.Value,
				) (openapischema.OutputUnit, error) {
					return compiler.ValidateSchema(ctx, value)
				}
			}
			output, err = validator(compiler, ctx, location.value)
			if err != nil {
				return nil, err
			}
			validated[key] = output
		}
		diagnostics = append(
			diagnostics,
			schemaObjectDiagnostics(
				output,
				location.pointer,
				document.SpecificationVersion().String(),
			)...,
		)
	}
	return diagnostics, nil
}

func schemaCompilerOptions(options Options) []openapischema.Option {
	var result []openapischema.Option
	if options.SchemaResourceLoader != nil {
		result = append(
			result,
			openapischema.WithResourceLoader(options.SchemaResourceLoader),
		)
	}
	if validSchemaBaseURI(options.ReferenceResourceURI) {
		result = append(
			result,
			openapischema.WithBaseURI(options.ReferenceResourceURI),
		)
	}
	return result
}

func validSchemaBaseURI(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if !parsed.IsAbs() {
		return false
	}
	if parsed.Fragment != "" {
		return false
	}
	return true
}

func swaggerFileResponseSchema(location schemaLocation) bool {
	return strings.Contains(location.pointer, "/responses/") &&
		strings.HasSuffix(location.pointer, "/schema") &&
		schemaHasType(location.value, "file")
}

func validateSchemaProse(document openapi.Document) []Diagnostic {
	dialect := document.SpecificationVersion().Dialect()
	collector := schemaCollector{dialect: dialect}
	collector.document(document.Raw())
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	for _, root := range collector.locations {
		walkSchemaTree(root, func(schema schemaLocation) {
			if dialect == specversion.DialectSwagger20 {
				diagnostics = append(
					diagnostics,
					swaggerRequiredReadOnlyDiagnostics(schema, version)...,
				)
			}
			if dialect == specversion.DialectOAS30 &&
				trueMember(schema.value, "readOnly") &&
				trueMember(schema.value, "writeOnly") {
				diagnostics = append(diagnostics, Diagnostic{
					Code:                 "openapi.schema.read-write-only",
					Message:              "a schema must not be both read-only and write-only",
					Severity:             SeverityError,
					Source:               SourceSchema,
					InstanceLocation:     schema.pointer,
					SpecificationVersion: version,
					SpecificationSection: "schema-object",
				})
			}
			diagnostics = append(
				diagnostics,
				validateSchemaDiscriminator(
					document.Raw(), schema, version, dialect,
				)...,
			)
			diagnostics = append(
				diagnostics,
				validateSchemaXML(schema, version, dialect)...,
			)
		})
	}
	return diagnostics
}

func swaggerRequiredReadOnlyDiagnostics(
	schema schemaLocation,
	version string,
) []Diagnostic {
	properties, hasProperties := objectMember(schema.value, "properties")
	required, hasRequired := schema.value.Lookup("required")
	if !hasProperties || !hasRequired || required.Kind() != jsonvalue.ArrayKind {
		return nil
	}
	requiredNames := make(map[string]struct{})
	elements, _ := required.Elements()
	for _, element := range elements {
		name, valid := element.Text()
		if valid {
			requiredNames[name] = struct{}{}
		}
	}
	members, _ := properties.Members()
	var diagnostics []Diagnostic
	for _, property := range members {
		if _, isRequired := requiredNames[property.Name]; !isRequired ||
			!trueMember(property.Value, "readOnly") {
			continue
		}
		diagnostics = append(diagnostics, schemaProseDiagnostic(
			version,
			"openapi.schema.read-only.required",
			schema.pointer+"/properties/"+escapePointer(property.Name)+"/readOnly",
			SeverityWarning,
			"read-only properties should not be required",
		))
	}
	return diagnostics
}

func validateSchemaXML(
	schema schemaLocation,
	version string,
	dialect specversion.Dialect,
) []Diagnostic {
	xml, exists := objectMember(schema.value, "xml")
	if !exists {
		return nil
	}
	pointer := schema.pointer + "/xml"
	var diagnostics []Diagnostic
	if dialect == specversion.DialectSwagger20 &&
		!swaggerPropertySchemaPointer(schema.pointer) {
		diagnostics = append(diagnostics, xmlDiagnostic(
			version,
			"openapi.swagger.xml.non-property",
			pointer,
			"Swagger XML metadata applies only to property schemas",
		))
	}
	if (dialect == specversion.DialectOAS30 ||
		dialect == specversion.DialectOAS31) &&
		!swaggerPropertySchemaPointer(schema.pointer) {
		diagnostics = append(diagnostics, xmlDiagnosticSeverity(
			version,
			"openapi.xml.non-property",
			pointer,
			SeverityWarning,
			"XML metadata outside property schemas has no effect",
		))
	}
	if namespace, exists := stringMember(xml, "namespace"); exists &&
		!validAbsoluteURI(namespace) {
		diagnostics = append(diagnostics, xmlDiagnostic(
			version,
			"openapi.xml.namespace.invalid",
			pointer+"/namespace",
			"XML namespace must be a non-relative URI",
		))
	}
	_, hasWrapped := xml.Lookup("wrapped")
	if hasWrapped && !schemaHasType(schema.value, "array") {
		diagnostics = append(diagnostics, xmlDiagnostic(
			version,
			"openapi.xml.wrapped.non-array",
			pointer+"/wrapped",
			"XML wrapped applies only to array schemas",
		))
	}
	_, hasName := xml.Lookup("name")
	if (dialect == specversion.DialectOAS30 ||
		dialect == specversion.DialectOAS31) &&
		schemaHasType(schema.value, "array") && !hasName {
		diagnostics = append(diagnostics, xmlDiagnosticSeverity(
			version,
			"openapi.xml.array-name.missing",
			pointer,
			SeverityWarning,
			"XML array element names should be declared explicitly",
		))
	}
	if dialect != specversion.DialectOAS32 {
		return diagnostics
	}
	nodeType, hasNodeType := stringMember(xml, "nodeType")
	if hasNodeType {
		for _, field := range []string{"attribute", "wrapped"} {
			if _, exists := xml.Lookup(field); !exists {
				continue
			}
			diagnostics = append(diagnostics, xmlDiagnostic(
				version,
				"openapi.xml.node-type.conflict",
				pointer+"/"+field,
				field+" must not be present with nodeType",
			))
		}
	} else {
		switch {
		case trueMember(xml, "attribute"):
			nodeType = "attribute"
		case trueMember(xml, "wrapped"):
			nodeType = "element"
		case schemaHasType(schema.value, "array") ||
			hasMember(schema.value, "$ref") ||
			hasMember(schema.value, "$dynamicRef"):
			nodeType = "none"
		default:
			nodeType = "element"
		}
	}
	switch nodeType {
	case "text", "cdata", "none":
		if hasName {
			diagnostics = append(diagnostics, xmlDiagnosticSeverity(
				version,
				"openapi.xml.name.ignored",
				pointer+"/name",
				SeverityWarning,
				"XML name is ignored for "+nodeType+" nodes",
			))
		}
	case "element", "attribute":
		if !hasName && !xmlNodeNameInferred(schema.pointer) {
			diagnostics = append(diagnostics, xmlDiagnostic(
				version,
				"openapi.xml.name.missing",
				pointer,
				"XML element and attribute nodes require an explicit or inferred name",
			))
		}
	}
	return diagnostics
}

func hasMember(object jsonvalue.Value, name string) bool {
	_, exists := object.Lookup(name)
	return exists
}

func xmlNodeNameInferred(pointer string) bool {
	tokens := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	if len(tokens) == 3 && tokens[0] == "components" && tokens[1] == "schemas" {
		return true
	}
	propertyNamePending := false
	itemsOnly := false
	for _, token := range tokens {
		if propertyNamePending {
			propertyNamePending = false
			itemsOnly = true
			continue
		}
		if itemsOnly {
			if token != "items" {
				return false
			}
			continue
		}
		if token == "properties" {
			propertyNamePending = true
		}
	}
	return itemsOnly
}

func xmlDiagnosticSeverity(
	version string,
	code string,
	pointer string,
	severity Severity,
	message string,
) Diagnostic {
	return Diagnostic{
		Code:                 code,
		Message:              message,
		Severity:             severity,
		Source:               SourceSchema,
		InstanceLocation:     pointer,
		SpecificationVersion: version,
		SpecificationSection: "xml-object",
	}
}

func xmlDiagnostic(
	version string,
	code string,
	pointer string,
	message string,
) Diagnostic {
	return xmlDiagnosticSeverity(
		version,
		code,
		pointer,
		SeverityError,
		message,
	)
}

func swaggerPropertySchemaPointer(pointer string) bool {
	return swaggerPropertySchemaPattern.MatchString(pointer)
}

func schemaHasType(schema jsonvalue.Value, wanted string) bool {
	typeValue, exists := schema.Lookup("type")
	if !exists {
		return false
	}
	if name, valid := typeValue.Text(); valid {
		return name == wanted
	}
	elements, valid := typeValue.Elements()
	if !valid {
		return false
	}
	for _, element := range elements {
		name, valid := element.Text()
		if valid && name == wanted {
			return true
		}
	}
	return false
}

func validateSchemaDiscriminator(
	root jsonvalue.Value,
	schema schemaLocation,
	version string,
	dialect specversion.Dialect,
) []Diagnostic {
	if dialect == specversion.DialectSwagger20 {
		propertyName, exists := stringMember(schema.value, "discriminator")
		if !exists {
			return nil
		}
		var diagnostics []Diagnostic
		properties, hasProperties := objectMember(schema.value, "properties")
		defined := false
		if hasProperties {
			_, defined = properties.Lookup(propertyName)
		}
		if !defined {
			diagnostics = append(diagnostics, schemaProseDiagnostic(
				version,
				"openapi.schema.discriminator.property-missing",
				schema.pointer+"/discriminator",
				SeverityError,
				"discriminator property must be defined by its schema",
			))
		}
		if !schemaRequiresProperty(schema.value, propertyName) {
			diagnostics = append(diagnostics, schemaProseDiagnostic(
				version,
				"openapi.schema.discriminator.not-required",
				schema.pointer+"/discriminator",
				SeverityError,
				"discriminator property must be required by its schema",
			))
		}
		return diagnostics
	}
	discriminator, exists := objectMember(schema.value, "discriminator")
	if !exists {
		return nil
	}
	propertyName, exists := stringMember(discriminator, "propertyName")
	if !exists {
		if dialect != specversion.DialectOAS32 {
			return nil
		}
		return []Diagnostic{schemaProseDiagnostic(
			version,
			"openapi.schema.discriminator.property-name-missing",
			schema.pointer+"/discriminator",
			SeverityError,
			"a discriminator must name its discriminating property",
		)}
	}
	diagnostics := validateDiscriminatorMappings(root, schema, discriminator, version)
	if schemaRequiresProperty(schema.value, propertyName) {
		return diagnostics
	}
	pointer := schema.pointer + "/discriminator"
	if dialect == specversion.DialectOAS32 {
		if _, hasDefault := discriminator.Lookup("defaultMapping"); hasDefault {
			return diagnostics
		}
		return append(diagnostics, schemaProseDiagnostic(
			version,
			"openapi.schema.discriminator.default-mapping-missing",
			pointer,
			SeverityError,
			"an optional discriminator property requires a default mapping",
		))
	}
	return append(diagnostics, schemaProseDiagnostic(
		version,
		"openapi.schema.discriminator.not-required",
		pointer,
		SeverityError,
		"discriminator property must be required by its schema",
	))
}

func validateDiscriminatorMappings(
	root jsonvalue.Value,
	schema schemaLocation,
	discriminator jsonvalue.Value,
	version string,
) []Diagnostic {
	alternatives, _ := openAPIDiscriminatorAlternatives(root, schema)
	if len(alternatives) == 0 {
		return nil
	}
	direct := directDiscriminatorAlternatives(schema.value)
	adjacent := len(direct) > 0
	var diagnostics []Diagnostic
	if adjacent {
		var inherited []string
		for alternative := range alternatives {
			if _, listed := direct[alternative]; !listed {
				inherited = append(inherited, alternative)
			}
		}
		sort.Strings(inherited)
		for _, alternative := range inherited {
			diagnostics = append(diagnostics, schemaProseDiagnostic(
				version,
				"openapi.schema.discriminator.alternative-unlisted",
				schema.pointer+"/discriminator",
				SeverityError,
				"known discriminator alternative is absent from adjacent oneOf or anyOf: "+
					safeValue(alternative),
			))
		}
	}
	mapping, exists := objectMember(discriminator, "mapping")
	if !exists {
		return diagnostics
	}
	listedAlternatives := alternatives
	if adjacent {
		listedAlternatives = direct
	}
	members, _ := mapping.Members()
	for _, member := range members {
		target, valid := member.Value.Text()
		if !valid {
			continue
		}
		if !strings.ContainsAny(target, "/?#:") {
			target = "#/components/schemas/" + target
		}
		if _, listed := listedAlternatives[target]; listed {
			continue
		}
		diagnostics = append(diagnostics, schemaProseDiagnostic(
			version,
			"openapi.schema.discriminator.mapping-unlisted",
			schema.pointer+"/discriminator/mapping/"+
				escapePointer(member.Name),
			SeverityError,
			"discriminator mapping target is not listed in oneOf or anyOf",
		))
	}
	return diagnostics
}

func openAPIDiscriminatorAlternatives(
	root jsonvalue.Value,
	schema schemaLocation,
) (map[string]struct{}, string) {
	alternatives := directDiscriminatorAlternatives(schema.value)
	schemas, exists := openAPIComponentSchemas(root)
	if !exists {
		return alternatives, ""
	}
	baseName := componentSchemaName(schemas, schema)
	if baseName == "" {
		return alternatives, ""
	}
	baseReference := "#/components/schemas/" + escapePointer(baseName)
	known := map[string]struct{}{baseReference: {}}
	for reference := range alternatives {
		known[reference] = struct{}{}
	}
	members, _ := schemas.Members()
	changed := true
	for changed {
		changed = false
		for _, member := range members {
			reference := "#/components/schemas/" + escapePointer(member.Name)
			if _, exists := known[reference]; exists {
				continue
			}
			if !schemaAllOfReferencesAny(member.Value, known) {
				continue
			}
			known[reference] = struct{}{}
			alternatives[reference] = struct{}{}
			changed = true
		}
	}
	return alternatives, baseReference
}

func openAPIComponentSchemas(root jsonvalue.Value) (jsonvalue.Value, bool) {
	components, exists := objectMember(root, "components")
	if !exists {
		return jsonvalue.Value{}, false
	}
	return objectMember(components, "schemas")
}

func directDiscriminatorAlternatives(schema jsonvalue.Value) map[string]struct{} {
	alternatives := make(map[string]struct{})
	for _, keyword := range []string{"oneOf", "anyOf"} {
		values, exists := schema.Lookup(keyword)
		if !exists || values.Kind() != jsonvalue.ArrayKind {
			continue
		}
		elements, _ := values.Elements()
		for _, element := range elements {
			if target, valid := stringMember(element, "$ref"); valid {
				alternatives[target] = struct{}{}
			}
		}
	}
	return alternatives
}

func componentSchemaName(schemas jsonvalue.Value, schema schemaLocation) string {
	members, _ := schemas.Members()
	for _, member := range members {
		pointer := "/components/schemas/" + escapePointer(member.Name)
		if schema.pointer == pointer ||
			(schema.pointer == "" && equalJSONValues(member.Value, schema.value)) {
			return member.Name
		}
	}
	return ""
}

func schemaAllOfReferencesAny(
	schema jsonvalue.Value,
	references map[string]struct{},
) bool {
	allOf, exists := schema.Lookup("allOf")
	if !exists || allOf.Kind() != jsonvalue.ArrayKind {
		return false
	}
	branches, _ := allOf.Elements()
	for _, branch := range branches {
		target, exists := stringMember(branch, "$ref")
		if !exists {
			continue
		}
		if _, included := references[target]; included {
			return true
		}
	}
	return false
}

func schemaRequiresProperty(schema jsonvalue.Value, propertyName string) bool {
	required, exists := schema.Lookup("required")
	if !exists || required.Kind() != jsonvalue.ArrayKind {
		return false
	}
	elements, _ := required.Elements()
	for _, element := range elements {
		name, valid := element.Text()
		if valid && name == propertyName {
			return true
		}
	}
	return false
}

func schemaProseDiagnostic(
	version string,
	code string,
	pointer string,
	severity Severity,
	message string,
) Diagnostic {
	return Diagnostic{
		Code:                 code,
		Message:              message,
		Severity:             severity,
		Source:               SourceSchema,
		InstanceLocation:     pointer,
		SpecificationVersion: version,
		SpecificationSection: "schema-object",
	}
}

func trueMember(object jsonvalue.Value, name string) bool {
	member, exists := object.Lookup(name)
	if !exists {
		return false
	}
	value, valid := member.Bool()
	return valid && value
}

func schemaObjectDiagnostics(
	output openapischema.OutputUnit,
	basePointer string,
	version string,
) []Diagnostic {
	if output.Valid {
		return nil
	}
	units := output.Errors
	if len(units) == 0 {
		units = []openapischema.OutputUnit{output}
	}
	result := make([]Diagnostic, 0, len(units))
	for _, unit := range units {
		if unit.KeywordLocation == "" || hiddenApplicatorBranch(unit, units) {
			continue
		}
		keyword := keywordName(unit.KeywordLocation)
		if keyword == "$ref" || keyword == "allOf" {
			continue
		}
		result = append(result, Diagnostic{
			Code:                    "openapi.schema." + keyword,
			Message:                 unit.Error,
			Severity:                SeverityError,
			Source:                  SourceSchema,
			InstanceLocation:        basePointer + unit.InstanceLocation,
			KeywordLocation:         unit.KeywordLocation,
			AbsoluteKeywordLocation: unit.AbsoluteKeywordLocation,
			SpecificationVersion:    version,
			SpecificationSection:    "schema-object",
		})
	}
	if len(result) == 0 {
		result = append(result, Diagnostic{
			Code:                 "openapi.schema.invalid",
			Message:              "Schema Object does not satisfy its selected dialect",
			Severity:             SeverityError,
			Source:               SourceSchema,
			InstanceLocation:     basePointer,
			SpecificationVersion: version,
			SpecificationSection: "schema-object",
		})
	}
	return result
}

func (collector *schemaCollector) document(root jsonvalue.Value) {
	if collector.dialect == specversion.DialectSwagger20 {
		collector.mapObjects(root, "paths", "/paths", collector.pathItem)
		collector.mapValues(root, "definitions", "/definitions", collector.schema)
		collector.mapObjects(root, "parameters", "/parameters", collector.parameter)
		collector.mapObjects(root, "responses", "/responses", collector.response)
		return
	}
	for _, collection := range []string{"paths", "webhooks"} {
		collector.mapObjects(root, collection, "/"+collection, collector.pathItem)
	}
	components, ok := objectMember(root, "components")
	if !ok {
		return
	}
	collector.mapValues(components, "schemas", "/components/schemas", collector.schema)
	collector.mapObjects(components, "parameters", "/components/parameters", collector.parameter)
	collector.mapObjects(components, "headers", "/components/headers", collector.parameter)
	collector.mapObjects(components, "requestBodies", "/components/requestBodies", collector.requestBody)
	collector.mapObjects(components, "responses", "/components/responses", collector.response)
	collector.mapObjects(components, "callbacks", "/components/callbacks", collector.callback)
	collector.mapObjects(components, "pathItems", "/components/pathItems", collector.pathItem)
	switch collector.dialect {
	case specversion.DialectOAS32:
		collector.mapObjects(components, "mediaTypes", "/components/mediaTypes", collector.mediaType)
	}
}

func (collector *schemaCollector) pathItem(value jsonvalue.Value, pointer string) {
	if isReference(value) {
		return
	}
	collector.parameters(value, pointer+"/parameters")
	for _, operation := range operationsAt(value, pointer, collector.dialect) {
		collector.operation(
			operation.value,
			operation.pointer,
			operation.method,
		)
	}
}

func (collector *schemaCollector) operation(
	value jsonvalue.Value,
	pointer string,
	method string,
) {
	collector.parameters(value, pointer+"/parameters")
	if requestBody, ok := objectMember(value, "requestBody"); ok &&
		!consumerIgnoresRequestBody(collector.dialect, method) {
		collector.requestBody(requestBody, pointer+"/requestBody")
	}
	if responses, ok := objectMember(value, "responses"); ok {
		members, _ := responses.Members()
		for _, member := range members {
			collector.response(
				member.Value,
				pointer+"/responses/"+escapePointer(member.Name),
			)
		}
	}
	if callbacks, ok := objectMember(value, "callbacks"); ok {
		members, _ := callbacks.Members()
		for _, member := range members {
			collector.callback(
				member.Value,
				pointer+"/callbacks/"+escapePointer(member.Name),
			)
		}
	}
}

func (collector *schemaCollector) parameters(value jsonvalue.Value, pointer string) {
	parameters, exists := value.Lookup("parameters")
	if !exists {
		return
	}
	if parameters.Kind() != jsonvalue.ArrayKind {
		return
	}
	elements, _ := parameters.Elements()
	for index, parameter := range elements {
		collector.parameter(parameter, pointer+"/"+strconv.Itoa(index))
	}
}

func (collector *schemaCollector) parameter(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind || isReference(value) {
		return
	}
	if ignoredHeaderParameterObject(value) {
		return
	}
	if schema, exists := value.Lookup("schema"); exists {
		collector.schema(schema, pointer+"/schema")
	}
	collector.content(value, pointer+"/content")
}

func (collector *schemaCollector) requestBody(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind || isReference(value) {
		return
	}
	collector.content(value, pointer+"/content")
}

func (collector *schemaCollector) response(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind || isReference(value) {
		return
	}
	if schema, exists := value.Lookup("schema"); exists {
		collector.schema(schema, pointer+"/schema")
	}
	collector.content(value, pointer+"/content")
	collector.mapObjects(value, "headers", pointer+"/headers", collector.parameter)
}

func (collector *schemaCollector) content(value jsonvalue.Value, pointer string) {
	content, ok := objectMember(value, "content")
	if !ok {
		return
	}
	members, _ := content.Members()
	for _, member := range members {
		collector.mediaType(member.Value, pointer+"/"+escapePointer(member.Name))
	}
}

func (collector *schemaCollector) mediaType(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind || isReference(value) {
		return
	}
	if schema, exists := value.Lookup("schema"); exists {
		collector.schema(schema, pointer+"/schema")
	}
	if itemSchema, exists := value.Lookup("itemSchema"); exists {
		collector.schema(itemSchema, pointer+"/itemSchema")
	}
	encodings, ok := objectMember(value, "encoding")
	if !ok {
		return
	}
	members, _ := encodings.Members()
	for _, member := range members {
		collector.mapObjects(
			member.Value,
			"headers",
			pointer+"/encoding/"+escapePointer(member.Name)+"/headers",
			collector.parameter,
		)
	}
}

func (collector *schemaCollector) callback(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind {
		return
	}
	if isReference(value) {
		return
	}
	members, _ := value.Members()
	for _, member := range members {
		collector.pathItem(member.Value, pointer+"/"+escapePointer(member.Name))
	}
}

func (collector *schemaCollector) schema(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind && value.Kind() != jsonvalue.BooleanKind {
		return
	}
	if collector.dialect != specversion.DialectOAS31 &&
		collector.dialect != specversion.DialectOAS32 && isReference(value) {
		return
	}
	collector.locations = append(collector.locations, schemaLocation{
		value: value, pointer: pointer,
	})
}

func (collector *schemaCollector) mapValues(
	container jsonvalue.Value,
	name string,
	pointer string,
	visit func(jsonvalue.Value, string),
) {
	values, ok := objectMember(container, name)
	if !ok {
		return
	}
	members, _ := values.Members()
	for _, member := range members {
		visit(member.Value, pointer+"/"+escapePointer(member.Name))
	}
}

func (collector *schemaCollector) mapObjects(
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
