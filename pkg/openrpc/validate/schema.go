package validate

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
	"unicode/utf8"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

// CodeInvalidSchema reports a schema that cannot compile as Draft 7.
const CodeInvalidSchema Code = "schema.invalid"

func (validator *validator) validateSchemas(document openrpc.Document) {
	validator.validateSchemasUsing(document, "", nil, false)
}

func (validator *validator) validateSchemasUsing(
	document openrpc.Document,
	base string,
	resources map[string]jsonschema.Schema,
	preserveExternal bool,
) {
	components, hasComponents := document.Components()
	componentSchemas := map[string]jsonschema.Schema{}
	if hasComponents {
		if schemas, present := components.Schemas(); present {
			componentSchemas = schemas
			for _, name := range sortedNames(schemas) {
				validator.validateSchema(
					"#/components/schemas/"+escape(name), schemas[name], componentSchemas,
					base, resources, preserveExternal,
				)
			}
		}
		if descriptors, present := components.ContentDescriptors(); present {
			for _, name := range sortedNames(descriptors) {
				validator.validateSchema(
					"#/components/contentDescriptors/"+escape(name)+"/schema",
					descriptors[name].Schema(), componentSchemas,
					base, resources, preserveExternal,
				)
			}
		}
	}

	for methodIndex, union := range document.Methods() {
		method, inline := union.Method()
		if !inline {
			continue
		}
		for parameterIndex, parameterUnion := range method.Params() {
			if descriptor, inline := parameterUnion.Descriptor(); inline {
				validator.validateSchema(
					pointer("methods", methodIndex, "params", parameterIndex, "schema"),
					descriptor.Schema(), componentSchemas,
					base, resources, preserveExternal,
				)
			}
		}
		if resultUnion, present := method.Result(); present {
			if descriptor, inline := resultUnion.Descriptor(); inline {
				validator.validateSchema(
					pointer("methods", methodIndex, "result", "schema"),
					descriptor.Schema(), componentSchemas,
					base, resources, preserveExternal,
				)
			}
		}
	}
}

func (validator *validator) validateSchema(
	pointer string,
	schema jsonschema.Schema,
	components map[string]jsonschema.Schema,
	base string,
	resources map[string]jsonschema.Schema,
	preserveExternal bool,
) {
	if validator.stop || validator.canceled() {
		return
	}
	wrapped, err := schemaCompilationUnit(schema, components, preserveExternal)
	if err == nil {
		options := jsonschema.DefaultValidationOptions()
		if base != "" {
			options.BaseURI = base
		}
		options.Resources = resources
		_, err = jsonschema.Compile(wrapped, options)
	}
	if err != nil {
		validator.add(Diagnostic{
			Code:          CodeInvalidSchema,
			Pointer:       pointer,
			Severity:      SeverityError,
			Specification: "https://json-schema.org/draft-07/schema",
			Message:       "schema must compile as JSON Schema Draft 7",
		})
	}
}

func schemaCompilationUnit(
	target jsonschema.Schema,
	components map[string]jsonschema.Schema,
	preserveExternal bool,
) (jsonschema.Schema, error) {
	definitions := make(map[string]any, len(components))
	for _, name := range sortedNames(components) {
		decoded, err := decodeSchema(components[name])
		if err != nil {
			return jsonschema.Schema{}, err
		}
		if err := rewriteSchemaReferences(decoded, preserveExternal); err != nil {
			return jsonschema.Schema{}, err
		}
		definitions[name] = decoded
	}
	decodedTarget, err := decodeSchema(target)
	if err != nil {
		return jsonschema.Schema{}, err
	}
	if err := rewriteSchemaReferences(decodedTarget, preserveExternal); err != nil {
		return jsonschema.Schema{}, err
	}
	wrapper := map[string]any{
		"$schema":     "http://json-schema.org/draft-07/schema#",
		"allOf":       []any{decodedTarget},
		"definitions": definitions,
	}
	encoded, _ := json.Marshal(wrapper)
	return jsonschema.Parse(encoded, jsonvalue.DefaultPolicy())
}

func decodeSchema(schema jsonschema.Schema) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(schema.Bytes()))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func rewriteSchemaReferences(value any, preserveExternal bool) error {
	switch typed := value.(type) {
	case map[string]any:
		if reference, ok := typed["$ref"].(string); ok {
			if !validSchemaReference(reference) {
				return jsonschema.ErrSchemaCompile
			}
			const prefix = "#/components/schemas"
			if reference == prefix || strings.HasPrefix(reference, prefix+"/") {
				typed["$ref"] = "#/definitions" + strings.TrimPrefix(reference, prefix)
			} else if !preserveExternal && reference != "" && !strings.HasPrefix(reference, "#") {
				delete(typed, "$ref")
			}
		}
		for _, child := range typed {
			if err := rewriteSchemaReferences(child, preserveExternal); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := rewriteSchemaReferences(child, preserveExternal); err != nil {
				return err
			}
		}
	}
	return nil
}

func validSchemaReference(input string) bool {
	if !utf8.ValidString(input) {
		return false
	}
	for _, character := range input {
		if character <= 0x20 || character == 0x7f {
			return false
		}
	}
	_, err := url.Parse(input)
	return err == nil
}
