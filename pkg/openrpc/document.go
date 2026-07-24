package openrpc

import "github.com/faustbrian/golib/pkg/openrpc/jsonschema"

const defaultSchemaURI = "https://meta.open-rpc.org/"

// ComponentsInput supplies reusable Components maps. Nil means absent; an
// allocated empty map means explicitly present and empty.
type ComponentsInput struct {
	Schemas            map[string]jsonschema.Schema
	Links              map[string]Link
	Errors             map[string]Error
	Examples           map[string]Example
	ExamplePairings    map[string]ExamplePairing
	ContentDescriptors map[string]ContentDescriptor
	Tags               map[string]Tag
	UnknownFields      Fields
}

// Components is an immutable OpenRPC Components Object.
type Components struct {
	schemas            map[string]jsonschema.Schema
	hasSchemas         bool
	links              map[string]Link
	hasLinks           bool
	errors             map[string]Error
	hasErrors          bool
	examples           map[string]Example
	hasExamples        bool
	examplePairings    map[string]ExamplePairing
	hasExamplePairings bool
	contentDescriptors map[string]ContentDescriptor
	hasDescriptors     bool
	tags               map[string]Tag
	hasTags            bool
	unknown            Fields
}

// NewComponents constructs an owned Components Object.
func NewComponents(input ComponentsInput) (Components, error) {
	return Components{
		schemas:            cloneMap(input.Schemas),
		hasSchemas:         input.Schemas != nil,
		links:              cloneMap(input.Links),
		hasLinks:           input.Links != nil,
		errors:             cloneMap(input.Errors),
		hasErrors:          input.Errors != nil,
		examples:           cloneMap(input.Examples),
		hasExamples:        input.Examples != nil,
		examplePairings:    cloneMap(input.ExamplePairings),
		hasExamplePairings: input.ExamplePairings != nil,
		contentDescriptors: cloneMap(input.ContentDescriptors),
		hasDescriptors:     input.ContentDescriptors != nil,
		tags:               cloneMap(input.Tags),
		hasTags:            input.Tags != nil,
		unknown:            input.UnknownFields,
	}, nil
}

// Schemas returns an owned optional schema component map.
func (components Components) Schemas() (map[string]jsonschema.Schema, bool) {
	return cloneMap(components.schemas), components.hasSchemas
}

// Links returns an owned optional link component map.
func (components Components) Links() (map[string]Link, bool) {
	return cloneMap(components.links), components.hasLinks
}

// Errors returns an owned optional error component map.
func (components Components) Errors() (map[string]Error, bool) {
	return cloneMap(components.errors), components.hasErrors
}

// Examples returns an owned optional example component map.
func (components Components) Examples() (map[string]Example, bool) {
	return cloneMap(components.examples), components.hasExamples
}

// ExamplePairings returns an owned optional example-pairing component map.
func (components Components) ExamplePairings() (map[string]ExamplePairing, bool) {
	return cloneMap(components.examplePairings), components.hasExamplePairings
}

// ContentDescriptors returns an owned optional descriptor component map.
func (components Components) ContentDescriptors() (map[string]ContentDescriptor, bool) {
	return cloneMap(components.contentDescriptors), components.hasDescriptors
}

// Tags returns an owned optional tag component map.
func (components Components) Tags() (map[string]Tag, bool) {
	return cloneMap(components.tags), components.hasTags
}

// UnknownFields returns fields retained by preserving parse mode.
func (components Components) UnknownFields() Fields { return components.unknown }

// DocumentInput supplies root OpenRPC document fields. Info must be non-nil,
// and Methods must be non-nil even when the visible list is empty.
type DocumentInput struct {
	Version       Version
	SchemaURI     *string
	Info          *Info
	ExternalDocs  *ExternalDocumentation
	Servers       []Server
	HasServers    bool
	Methods       []MethodOrReference
	Components    *Components
	Extensions    Fields
	UnknownFields Fields
}

// Document is an immutable OpenRPC root document.
type Document struct {
	objectFields
	version       Version
	schemaURI     optionalString
	info          Info
	externalDocs  ExternalDocumentation
	hasDocs       bool
	servers       []Server
	hasServers    bool
	methods       []MethodOrReference
	components    Components
	hasComponents bool
}

// NewDocument constructs an owned OpenRPC document.
func NewDocument(input DocumentInput) (Document, error) {
	if input.Version.String() == "" {
		return Document{}, missingField("openrpc")
	}
	if input.Info == nil {
		return Document{}, missingField("info")
	}
	if input.Methods == nil {
		return Document{}, missingField("methods")
	}
	document := Document{
		objectFields: newObjectFields(input.Extensions, input.UnknownFields),
		version:      input.Version,
		schemaURI:    stringOption(input.SchemaURI),
		info:         *input.Info,
		servers:      append([]Server(nil), input.Servers...),
		hasServers:   input.HasServers || input.Servers != nil,
		methods:      append([]MethodOrReference(nil), input.Methods...),
	}
	if input.ExternalDocs != nil {
		document.externalDocs = *input.ExternalDocs
		document.hasDocs = true
	}
	if input.Components != nil {
		document.components = *input.Components
		document.hasComponents = true
	}
	return document, nil
}

// Version returns the required OpenRPC specification version.
func (document Document) Version() Version { return document.version }

// SchemaURI returns the effective schema URI and whether it was explicit.
func (document Document) SchemaURI() (string, bool) {
	if !document.schemaURI.present {
		return defaultSchemaURI, false
	}
	return document.schemaURI.value, true
}

// Info returns the required Info Object.
func (document Document) Info() Info { return document.info }

// ExternalDocs returns optional external documentation.
func (document Document) ExternalDocs() (ExternalDocumentation, bool) {
	return document.externalDocs, document.hasDocs
}

// Servers returns an owned explicit server slice and its presence.
func (document Document) Servers() ([]Server, bool) {
	return append([]Server(nil), document.servers...), document.hasServers
}

// EffectiveServers returns explicit non-empty servers or the specification's
// localhost default when servers are absent or empty.
func (document Document) EffectiveServers() []Server {
	if len(document.servers) != 0 {
		return append([]Server(nil), document.servers...)
	}
	server, _ := NewServer(ServerInput{URL: "localhost"})
	return []Server{server}
}

// Methods returns an owned required method slice. It may be empty after
// context-aware security filtering.
func (document Document) Methods() []MethodOrReference {
	return append([]MethodOrReference(nil), document.methods...)
}

// MethodCount returns the required method collection size without allocating
// an owned snapshot. It supports resource-policy checks before traversal.
func (document Document) MethodCount() int { return len(document.methods) }

// Components returns the optional reusable Components Object.
func (document Document) Components() (Components, bool) {
	return document.components, document.hasComponents
}

func cloneMap[T any](input map[string]T) map[string]T {
	if input == nil {
		return nil
	}
	output := make(map[string]T, len(input))
	for name, value := range input {
		output[name] = value
	}
	return output
}
