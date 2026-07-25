package openrpc

import (
	"bytes"
	"encoding/json"
	"errors"
)

var (
	// ErrFieldCollision reports an extension or preserved unknown field whose
	// name collides with a standard field during serialization.
	ErrFieldCollision = errors.New("openrpc: field collision")
	// ErrInvalidUnion reports a zero or otherwise unselected union value.
	ErrInvalidUnion = errors.New("openrpc: invalid union value")
)

// MarshalCanonical serializes a Document deterministically. Object members
// are sorted lexically, insignificant whitespace is removed, and optional
// field presence is retained without materializing defaults.
func MarshalCanonical(document Document) ([]byte, error) {
	if document.version.String() == "" {
		return nil, missingField("openrpc")
	}
	if document.info.title == "" {
		return nil, missingField("info")
	}
	object, err := documentObject(document)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(object); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte{'\n'}), nil
}

func documentObject(document Document) (map[string]any, error) {
	object, err := extensibleObject(document.objectFields)
	if err != nil {
		return nil, err
	}
	if err := put(object, "openrpc", document.version.String()); err != nil {
		return nil, err
	}
	if document.schemaURI.present {
		if err := put(object, "$schema", document.schemaURI.value); err != nil {
			return nil, err
		}
	}
	info, err := infoObject(document.info)
	if err != nil {
		return nil, err
	}
	if err := put(object, "info", info); err != nil {
		return nil, err
	}
	if document.hasDocs {
		value, err := externalDocumentationObject(document.externalDocs)
		if err != nil {
			return nil, err
		}
		if err := put(object, "externalDocs", value); err != nil {
			return nil, err
		}
	}
	if document.hasServers {
		values, err := serverObjects(document.servers)
		if err != nil {
			return nil, err
		}
		if err := put(object, "servers", values); err != nil {
			return nil, err
		}
	}
	methods, err := methodUnionObjects(document.methods)
	if err != nil {
		return nil, err
	}
	if err := put(object, "methods", methods); err != nil {
		return nil, err
	}
	if document.hasComponents {
		value, err := componentsObject(document.components)
		if err != nil {
			return nil, err
		}
		if err := put(object, "components", value); err != nil {
			return nil, err
		}
	}
	return object, nil
}

func infoObject(info Info) (map[string]any, error) {
	object, err := extensibleObject(info.objectFields)
	if err != nil {
		return nil, err
	}
	if err := put(object, "title", info.title); err != nil {
		return nil, err
	}
	if err := put(object, "version", info.version); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "description", info.description); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "termsOfService", info.termsOfService); err != nil {
		return nil, err
	}
	if info.hasContact {
		value, err := contactObject(info.contact)
		if err != nil {
			return nil, err
		}
		if err := put(object, "contact", value); err != nil {
			return nil, err
		}
	}
	if info.hasLicense {
		value, err := licenseObject(info.license)
		if err != nil {
			return nil, err
		}
		if err := put(object, "license", value); err != nil {
			return nil, err
		}
	}
	return object, nil
}

func contactObject(contact Contact) (map[string]any, error) {
	object, err := extensibleObject(contact.objectFields)
	if err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "name", contact.name); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "email", contact.email); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "url", contact.url); err != nil {
		return nil, err
	}
	return object, nil
}

func licenseObject(license License) (map[string]any, error) {
	object, err := extensibleObject(license.objectFields)
	if err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "name", license.name); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "url", license.url); err != nil {
		return nil, err
	}
	return object, nil
}

func externalDocumentationObject(documentation ExternalDocumentation) (map[string]any, error) {
	object, err := extensibleObject(documentation.objectFields)
	if err != nil {
		return nil, err
	}
	if err := put(object, "url", documentation.url); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "description", documentation.description); err != nil {
		return nil, err
	}
	return object, nil
}

func serverObjects(servers []Server) ([]any, error) {
	values := make([]any, len(servers))
	for index, server := range servers {
		value, err := serverObject(server)
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

func serverObject(server Server) (map[string]any, error) {
	object, err := extensibleObject(server.objectFields)
	if err != nil {
		return nil, err
	}
	if err := put(object, "url", server.url); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "name", server.name); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "description", server.description); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "summary", server.summary); err != nil {
		return nil, err
	}
	if server.hasVariables {
		variables := make(map[string]any, len(server.variables))
		for name, variable := range server.variables {
			value, err := serverVariableObject(variable)
			if err != nil {
				return nil, err
			}
			variables[name] = value
		}
		if err := put(object, "variables", variables); err != nil {
			return nil, err
		}
	}
	return object, nil
}

func serverVariableObject(variable ServerVariable) (map[string]any, error) {
	object, err := fieldsObject(variable.unknown)
	if err != nil {
		return nil, err
	}
	if err := put(object, "default", variable.defaultValue); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "description", variable.description); err != nil {
		return nil, err
	}
	if variable.hasEnum {
		if err := put(object, "enum", append([]string(nil), variable.enum...)); err != nil {
			return nil, err
		}
	}
	return object, nil
}

func methodUnionObjects(values []MethodOrReference) ([]any, error) {
	objects := make([]any, len(values))
	for index, value := range values {
		object, err := methodUnionObject(value)
		if err != nil {
			return nil, err
		}
		objects[index] = object
	}
	return objects, nil
}

func methodUnionObject(value MethodOrReference) (map[string]any, error) {
	switch value.kind {
	case 1:
		return methodObject(value.method)
	case 2:
		return referenceObject(value.reference), nil
	default:
		return nil, ErrInvalidUnion
	}
}

func methodObject(method Method) (map[string]any, error) {
	object, err := extensibleObject(method.objectFields)
	if err != nil {
		return nil, err
	}
	if err := put(object, "name", method.name); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "description", method.description); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "summary", method.summary); err != nil {
		return nil, err
	}
	if method.hasServers {
		values, err := serverObjects(method.servers)
		if err != nil {
			return nil, err
		}
		if err := put(object, "servers", values); err != nil {
			return nil, err
		}
	}
	if method.hasTags {
		values, err := tagUnionObjects(method.tags)
		if err != nil {
			return nil, err
		}
		if err := put(object, "tags", values); err != nil {
			return nil, err
		}
	}
	params, err := descriptorUnionObjects(method.params)
	if err != nil {
		return nil, err
	}
	if err := put(object, "params", params); err != nil {
		return nil, err
	}
	if method.hasStructure {
		if err := put(object, "paramStructure", method.paramStructure); err != nil {
			return nil, err
		}
	}
	if method.hasResult {
		value, err := descriptorUnionObject(method.result)
		if err != nil {
			return nil, err
		}
		if err := put(object, "result", value); err != nil {
			return nil, err
		}
	}
	if method.hasErrors {
		values, err := errorUnionObjects(method.errors)
		if err != nil {
			return nil, err
		}
		if err := put(object, "errors", values); err != nil {
			return nil, err
		}
	}
	if method.hasLinks {
		values, err := linkUnionObjects(method.links)
		if err != nil {
			return nil, err
		}
		if err := put(object, "links", values); err != nil {
			return nil, err
		}
	}
	if method.hasExamples {
		values, err := pairingUnionObjects(method.examples)
		if err != nil {
			return nil, err
		}
		if err := put(object, "examples", values); err != nil {
			return nil, err
		}
	}
	if method.deprecated.present {
		if err := put(object, "deprecated", method.deprecated.value); err != nil {
			return nil, err
		}
	}
	if method.hasDocs {
		value, err := externalDocumentationObject(method.externalDocs)
		if err != nil {
			return nil, err
		}
		if err := put(object, "externalDocs", value); err != nil {
			return nil, err
		}
	}
	return object, nil
}

func descriptorUnionObjects(values []ContentDescriptorOrReference) ([]any, error) {
	objects := make([]any, len(values))
	for index, value := range values {
		object, err := descriptorUnionObject(value)
		if err != nil {
			return nil, err
		}
		objects[index] = object
	}
	return objects, nil
}

func descriptorUnionObject(value ContentDescriptorOrReference) (map[string]any, error) {
	switch value.kind {
	case 1:
		return contentDescriptorObject(value.descriptor)
	case 2:
		return referenceObject(value.reference), nil
	default:
		return nil, ErrInvalidUnion
	}
}

func contentDescriptorObject(descriptor ContentDescriptor) (map[string]any, error) {
	object, err := extensibleObject(descriptor.objectFields)
	if err != nil {
		return nil, err
	}
	if err := put(object, "name", descriptor.name); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "description", descriptor.description); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "summary", descriptor.summary); err != nil {
		return nil, err
	}
	if err := put(object, "schema", descriptor.schema); err != nil {
		return nil, err
	}
	if descriptor.required.present {
		if err := put(object, "required", descriptor.required.value); err != nil {
			return nil, err
		}
	}
	if descriptor.deprecated.present {
		if err := put(object, "deprecated", descriptor.deprecated.value); err != nil {
			return nil, err
		}
	}
	return object, nil
}

func tagUnionObjects(values []TagOrReference) ([]any, error) {
	objects := make([]any, len(values))
	for index, value := range values {
		var object map[string]any
		var err error
		switch value.kind {
		case 1:
			object, err = tagObject(value.tag)
		case 2:
			object = referenceObject(value.reference)
		default:
			err = ErrInvalidUnion
		}
		if err != nil {
			return nil, err
		}
		objects[index] = object
	}
	return objects, nil
}

func tagObject(tag Tag) (map[string]any, error) {
	object, err := extensibleObject(tag.objectFields)
	if err != nil {
		return nil, err
	}
	if err := put(object, "name", tag.name); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "description", tag.description); err != nil {
		return nil, err
	}
	if tag.hasDocs {
		value, err := externalDocumentationObject(tag.externalDocs)
		if err != nil {
			return nil, err
		}
		if err := put(object, "externalDocs", value); err != nil {
			return nil, err
		}
	}
	return object, nil
}

func errorUnionObjects(values []ErrorOrReference) ([]any, error) {
	objects := make([]any, len(values))
	for index, value := range values {
		var object map[string]any
		var err error
		switch value.kind {
		case 1:
			object, err = errorObject(value.object)
		case 2:
			object = referenceObject(value.reference)
		default:
			err = ErrInvalidUnion
		}
		if err != nil {
			return nil, err
		}
		objects[index] = object
	}
	return objects, nil
}

func errorObject(value Error) (map[string]any, error) {
	object, err := extensibleObject(value.objectFields)
	if err != nil {
		return nil, err
	}
	if err := put(object, "code", json.RawMessage(value.code.String())); err != nil {
		return nil, err
	}
	if err := put(object, "message", value.message); err != nil {
		return nil, err
	}
	if value.data.present {
		if err := put(object, "data", value.data.value); err != nil {
			return nil, err
		}
	}
	return object, nil
}

func linkUnionObjects(values []LinkOrReference) ([]any, error) {
	objects := make([]any, len(values))
	for index, value := range values {
		var object map[string]any
		var err error
		switch value.kind {
		case 1:
			object, err = linkObject(value.link)
		case 2:
			object = referenceObject(value.reference)
		default:
			err = ErrInvalidUnion
		}
		if err != nil {
			return nil, err
		}
		objects[index] = object
	}
	return objects, nil
}

func linkObject(link Link) (map[string]any, error) {
	object, err := extensibleObject(link.objectFields)
	if err != nil {
		return nil, err
	}
	if err := putOptionalValue(object, "name", link.name); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "summary", link.summary); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "description", link.description); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "method", link.method); err != nil {
		return nil, err
	}
	if err := putOptionalValue(object, "params", link.params); err != nil {
		return nil, err
	}
	if link.hasServer {
		value, err := serverObject(link.server)
		if err != nil {
			return nil, err
		}
		if err := put(object, "server", value); err != nil {
			return nil, err
		}
	}
	return object, nil
}

func pairingUnionObjects(values []ExamplePairingOrReference) ([]any, error) {
	objects := make([]any, len(values))
	for index, value := range values {
		var object map[string]any
		var err error
		switch value.kind {
		case 1:
			object, err = examplePairingObject(value.pairing)
		case 2:
			object = referenceObject(value.reference)
		default:
			err = ErrInvalidUnion
		}
		if err != nil {
			return nil, err
		}
		objects[index] = object
	}
	return objects, nil
}

func examplePairingObject(pairing ExamplePairing) (map[string]any, error) {
	object, err := fieldsObject(pairing.unknown)
	if err != nil {
		return nil, err
	}
	if err := put(object, "name", pairing.name); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "description", pairing.description); err != nil {
		return nil, err
	}
	params, err := exampleUnionObjects(pairing.params)
	if err != nil {
		return nil, err
	}
	if err := put(object, "params", params); err != nil {
		return nil, err
	}
	if pairing.hasResult {
		value, err := exampleUnionObject(pairing.result)
		if err != nil {
			return nil, err
		}
		if err := put(object, "result", value); err != nil {
			return nil, err
		}
	}
	return object, nil
}

func exampleUnionObjects(values []ExampleOrReference) ([]any, error) {
	objects := make([]any, len(values))
	for index, value := range values {
		object, err := exampleUnionObject(value)
		if err != nil {
			return nil, err
		}
		objects[index] = object
	}
	return objects, nil
}

func exampleUnionObject(value ExampleOrReference) (map[string]any, error) {
	switch value.kind {
	case 1:
		return exampleObject(value.example)
	case 2:
		return referenceObject(value.reference), nil
	default:
		return nil, ErrInvalidUnion
	}
}

func exampleObject(example Example) (map[string]any, error) {
	object, err := extensibleObject(example.objectFields)
	if err != nil {
		return nil, err
	}
	if err := put(object, "name", example.name); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "summary", example.summary); err != nil {
		return nil, err
	}
	if err := putOptionalString(object, "description", example.description); err != nil {
		return nil, err
	}
	if err := put(object, "value", example.value); err != nil {
		return nil, err
	}
	return object, nil
}

func componentsObject(components Components) (map[string]any, error) {
	object, err := fieldsObject(components.unknown)
	if err != nil {
		return nil, err
	}
	if components.hasSchemas {
		if err := put(object, "schemas", components.schemas); err != nil {
			return nil, err
		}
	}
	if components.hasLinks {
		values, err := mapObjects(components.links, linkObject)
		if err != nil {
			return nil, err
		}
		if err := put(object, "links", values); err != nil {
			return nil, err
		}
	}
	if components.hasErrors {
		values, err := mapObjects(components.errors, errorObject)
		if err != nil {
			return nil, err
		}
		if err := put(object, "errors", values); err != nil {
			return nil, err
		}
	}
	if components.hasExamples {
		values, err := mapObjects(components.examples, exampleObject)
		if err != nil {
			return nil, err
		}
		if err := put(object, "examples", values); err != nil {
			return nil, err
		}
	}
	if components.hasExamplePairings {
		values, err := mapObjects(components.examplePairings, examplePairingObject)
		if err != nil {
			return nil, err
		}
		if err := put(object, "examplePairings", values); err != nil {
			return nil, err
		}
	}
	if components.hasDescriptors {
		values, err := mapObjects(components.contentDescriptors, contentDescriptorObject)
		if err != nil {
			return nil, err
		}
		if err := put(object, "contentDescriptors", values); err != nil {
			return nil, err
		}
	}
	if components.hasTags {
		values, err := mapObjects(components.tags, tagObject)
		if err != nil {
			return nil, err
		}
		if err := put(object, "tags", values); err != nil {
			return nil, err
		}
	}
	return object, nil
}

func mapObjects[T any](values map[string]T, convert func(T) (map[string]any, error)) (map[string]any, error) {
	objects := make(map[string]any, len(values))
	for name, value := range values {
		object, err := convert(value)
		if err != nil {
			return nil, err
		}
		objects[name] = object
	}
	return objects, nil
}

func referenceObject(reference Reference) map[string]any {
	return map[string]any{"$ref": reference.ref}
}

func extensibleObject(fields objectFields) (map[string]any, error) {
	object, err := fieldsObject(fields.extensions)
	if err != nil {
		return nil, err
	}
	if err := mergeFields(object, fields.unknown); err != nil {
		return nil, err
	}
	return object, nil
}

func fieldsObject(fields Fields) (map[string]any, error) {
	object := make(map[string]any, fields.Len())
	if err := mergeFields(object, fields); err != nil {
		return nil, err
	}
	return object, nil
}

func mergeFields(object map[string]any, fields Fields) error {
	for _, name := range fields.names {
		if err := put(object, name, fields.values[name]); err != nil {
			return err
		}
	}
	return nil
}

func put(object map[string]any, name string, value any) error {
	if _, exists := object[name]; exists {
		return ErrFieldCollision
	}
	object[name] = value
	return nil
}

func putOptionalString(object map[string]any, name string, value optionalString) error {
	if !value.present {
		return nil
	}
	return put(object, name, value.value)
}

func putOptionalValue(object map[string]any, name string, value optionalValue) error {
	if !value.present {
		return nil
	}
	return put(object, name, value.value)
}
