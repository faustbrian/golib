package validate

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	"github.com/faustbrian/golib/pkg/openrpc/reference"
)

type schemaReferenceLocation struct {
	pointer string
	ref     string
}

func validateResolvedSchemas(
	ctx context.Context,
	document openrpc.Document,
	root jsonvalue.Value,
	base string,
	resolver *reference.Resolver,
	options Options,
) Report {
	locations := externalSchemaReferences(document, base)
	if len(locations) == 0 {
		return Report{}
	}
	inputs := make([]string, len(locations))
	for index, location := range locations {
		inputs[index] = location.ref
	}
	loaded, err := resolver.Resources(ctx, root, base, inputs)
	if err != nil {
		return resolutionFailure(locations[0].pointer)
	}
	resources := make(map[string]jsonschema.Schema, len(loaded))
	for uri, value := range loaded {
		schema, err := jsonschema.FromValue(value)
		if err != nil {
			return Report{diagnostics: []Diagnostic{{
				Code: CodeInvalidSchema, Pointer: locations[0].pointer,
				Severity:      SeverityError,
				Specification: "https://json-schema.org/draft-07/schema",
				Message:       "external schema resource must be an object or boolean schema",
			}}}
		}
		resources[uri] = schema
	}
	engine := validator{ctx: ctx, options: options}
	engine.validateSchemasUsing(document, base, resources, true)
	return engine.report()
}

func externalSchemaReferences(document openrpc.Document, base string) []schemaReferenceLocation {
	locations := make([]schemaReferenceLocation, 0)
	components, hasComponents := document.Components()
	if hasComponents {
		if schemas, present := components.Schemas(); present {
			for _, name := range sortedNames(schemas) {
				appendSchemaReferences(
					"#/components/schemas/"+escape(name), schemas[name], base, &locations,
				)
			}
		}
		if descriptors, present := components.ContentDescriptors(); present {
			for _, name := range sortedNames(descriptors) {
				appendSchemaReferences(
					"#/components/contentDescriptors/"+escape(name)+"/schema",
					descriptors[name].Schema(), base, &locations,
				)
			}
		}
	}
	for methodIndex, union := range document.Methods() {
		method, inline := union.Method()
		if !inline {
			continue
		}
		for parameterIndex, union := range method.Params() {
			if descriptor, inline := union.Descriptor(); inline {
				appendSchemaReferences(
					pointer("methods", methodIndex, "params", parameterIndex, "schema"),
					descriptor.Schema(), base, &locations,
				)
			}
		}
		if union, present := method.Result(); present {
			if descriptor, inline := union.Descriptor(); inline {
				appendSchemaReferences(
					pointer("methods", methodIndex, "result", "schema"),
					descriptor.Schema(), base, &locations,
				)
			}
		}
	}
	slices.SortFunc(locations, func(left schemaReferenceLocation, right schemaReferenceLocation) int {
		if byPointer := strings.Compare(left.pointer, right.pointer); byPointer != 0 {
			return byPointer
		}
		return strings.Compare(left.ref, right.ref)
	})
	return locations
}

func appendSchemaReferences(
	pointer string,
	schema jsonschema.Schema,
	base string,
	locations *[]schemaReferenceLocation,
) {
	decoder := json.NewDecoder(bytes.NewReader(schema.Bytes()))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return
	}
	walkSchemaReferenceValues(value, pointer, base, locations)
}

func walkSchemaReferenceValues(
	value any,
	pointer string,
	base string,
	locations *[]schemaReferenceLocation,
) {
	typed, object := value.(map[string]any)
	if !object {
		return
	}
	currentBase := base
	if identifier, ok := typed["$id"].(string); ok && identifier != "" {
		if resolved, err := resolveSchemaURI(base, identifier); err == nil {
			currentBase = resolved
		}
	}
	if ref, ok := typed["$ref"].(string); ok && ref != "" && !strings.HasPrefix(ref, "#") {
		if resolved, err := resolveSchemaURI(currentBase, ref); err == nil {
			ref = resolved
		}
		*locations = append(*locations, schemaReferenceLocation{pointer: pointer, ref: ref})
	}
	for _, keyword := range []string{
		"additionalItems", "additionalProperties", "contains", "else", "if",
		"not", "propertyNames", "then",
	} {
		walkSchemaReferenceValues(typed[keyword], pointer, currentBase, locations)
	}
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		walkSchemaArrayReferences(typed[keyword], pointer, currentBase, locations)
	}
	if items, array := typed["items"].([]any); array {
		for _, item := range items {
			walkSchemaReferenceValues(item, pointer, currentBase, locations)
		}
	} else {
		walkSchemaReferenceValues(typed["items"], pointer, currentBase, locations)
	}
	for _, keyword := range []string{"definitions", "patternProperties", "properties"} {
		if schemas, ok := typed[keyword].(map[string]any); ok {
			for _, schema := range schemas {
				walkSchemaReferenceValues(schema, pointer, currentBase, locations)
			}
		}
	}
	if dependencies, ok := typed["dependencies"].(map[string]any); ok {
		for _, dependency := range dependencies {
			walkSchemaReferenceValues(dependency, pointer, currentBase, locations)
		}
	}
}

func walkSchemaArrayReferences(value any, pointer string, base string, locations *[]schemaReferenceLocation) {
	values, ok := value.([]any)
	if !ok {
		return
	}
	for _, child := range values {
		walkSchemaReferenceValues(child, pointer, base, locations)
	}
}

func resolveSchemaURI(base string, input string) (string, error) {
	baseURI, err := url.Parse(base)
	if err != nil || !baseURI.IsAbs() {
		return "", reference.ErrInvalidBase
	}
	referenceURI, err := url.Parse(input)
	if err != nil {
		return "", err
	}
	return baseURI.ResolveReference(referenceURI).String(), nil
}

func mergeValidationReports(base Report, additional Report, options Options) Report {
	if options.Mode == FailFast && len(base.diagnostics) != 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base.diagnostics))
	for _, diagnostic := range base.diagnostics {
		seen[string(diagnostic.Code)+"\x00"+diagnostic.Pointer] = struct{}{}
	}
	merged := append([]Diagnostic(nil), base.diagnostics...)
	truncated := base.truncated || additional.truncated
	for _, diagnostic := range additional.diagnostics {
		key := string(diagnostic.Code) + "\x00" + diagnostic.Pointer
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		if len(merged) >= options.MaxDiagnostics {
			truncated = true
			break
		}
		seen[key] = struct{}{}
		merged = append(merged, diagnostic)
		if options.Mode == FailFast {
			break
		}
	}
	return Report{diagnostics: merged, truncated: truncated}
}
