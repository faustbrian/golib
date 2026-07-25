// Package builder provides ownership-safe, deterministic OpenRPC construction
// without making design-first documents depend on reflection or registration.
package builder

import (
	"errors"
	"sort"
	"strings"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
)

var (
	// ErrInvalidBuilder reports missing required builder state.
	ErrInvalidBuilder = errors.New("builder: invalid document builder")
	// ErrDuplicateMethod reports a repeated case-sensitive method name.
	ErrDuplicateMethod = errors.New("builder: duplicate method name")
	// ErrInvalidRegistry reports a nil registry or invalid zero method.
	ErrInvalidRegistry = errors.New("builder: invalid method registry")
)

// Document is an immutable OpenRPC document builder. NewDocument establishes
// every required root field and an explicitly present, possibly empty method
// collection.
type Document struct {
	version       openrpc.Version
	info          openrpc.Info
	methods       map[string]openrpc.Method
	references    []openrpc.Reference
	schemaURI     *string
	externalDocs  *openrpc.ExternalDocumentation
	servers       []openrpc.Server
	hasServers    bool
	components    *openrpc.Components
	extensions    openrpc.Fields
	unknownFields openrpc.Fields
	valid         bool
}

// NewDocument constructs a builder with valid required root state.
func NewDocument(version openrpc.Version, info openrpc.Info) (Document, error) {
	if _, err := openrpc.NewDocument(openrpc.DocumentInput{
		Version: version,
		Info:    &info,
		Methods: []openrpc.MethodOrReference{},
	}); err != nil {
		return Document{}, ErrInvalidBuilder
	}
	return Document{
		version: version,
		info:    info,
		methods: make(map[string]openrpc.Method),
		valid:   true,
	}, nil
}

// WithMethod returns a copy containing method.
func (builder Document) WithMethod(method openrpc.Method) (Document, error) {
	if !builder.valid || method.Name() == "" {
		return Document{}, ErrInvalidBuilder
	}
	if _, duplicate := builder.methods[method.Name()]; duplicate {
		return Document{}, ErrDuplicateMethod
	}
	result := builder.clone()
	result.methods[method.Name()] = method
	return result, nil
}

// WithMethodReference returns a copy containing an unresolved root method
// reference. References are sorted lexically during Build.
func (builder Document) WithMethodReference(reference openrpc.Reference) (Document, error) {
	if !builder.valid || reference.Ref() == "" {
		return Document{}, ErrInvalidBuilder
	}
	result := builder.clone()
	result.references = append(result.references, reference)
	return result, nil
}

// WithServers returns a copy with explicit server presence. An empty slice
// remains explicitly present.
func (builder Document) WithServers(servers []openrpc.Server) (Document, error) {
	if !builder.valid {
		return Document{}, ErrInvalidBuilder
	}
	result := builder.clone()
	result.servers = append([]openrpc.Server(nil), servers...)
	result.hasServers = true
	return result, nil
}

// WithComponents returns a copy with reusable components.
func (builder Document) WithComponents(components openrpc.Components) (Document, error) {
	if !builder.valid {
		return Document{}, ErrInvalidBuilder
	}
	result := builder.clone()
	result.components = &components
	return result, nil
}

// WithSchemaURI returns a copy with an explicit meta-schema URI.
func (builder Document) WithSchemaURI(uri string) (Document, error) {
	if !builder.valid || uri == "" {
		return Document{}, ErrInvalidBuilder
	}
	result := builder.clone()
	result.schemaURI = &uri
	return result, nil
}

// WithExternalDocs returns a copy with external documentation.
func (builder Document) WithExternalDocs(documentation openrpc.ExternalDocumentation) (Document, error) {
	if !builder.valid || documentation.URL() == "" {
		return Document{}, ErrInvalidBuilder
	}
	result := builder.clone()
	result.externalDocs = &documentation
	return result, nil
}

// WithFields returns a copy with root extension and preserved unknown fields.
func (builder Document) WithFields(extensions openrpc.Fields, unknown openrpc.Fields) (Document, error) {
	if !builder.valid {
		return Document{}, ErrInvalidBuilder
	}
	result := builder.clone()
	result.extensions = extensions
	result.unknownFields = unknown
	return result, nil
}

// Build creates one immutable document with lexically deterministic methods
// and method references.
func (builder Document) Build() (openrpc.Document, error) {
	if !builder.valid {
		return openrpc.Document{}, ErrInvalidBuilder
	}
	names := make([]string, 0, len(builder.methods))
	for name := range builder.methods {
		names = append(names, name)
	}
	sort.Strings(names)
	methods := make([]openrpc.MethodOrReference, 0, len(names)+len(builder.references))
	for _, name := range names {
		methods = append(methods, openrpc.MethodValue(builder.methods[name]))
	}
	references := append([]openrpc.Reference(nil), builder.references...)
	sort.Slice(references, func(left int, right int) bool {
		return strings.Compare(references[left].Ref(), references[right].Ref()) == -1
	})
	for _, reference := range references {
		methods = append(methods, openrpc.MethodReference(reference))
	}
	return openrpc.NewDocument(openrpc.DocumentInput{
		Version:       builder.version,
		SchemaURI:     builder.schemaURI,
		Info:          &builder.info,
		ExternalDocs:  builder.externalDocs,
		Servers:       append([]openrpc.Server(nil), builder.servers...),
		HasServers:    builder.hasServers,
		Methods:       methods,
		Components:    builder.components,
		Extensions:    builder.extensions,
		UnknownFields: builder.unknownFields,
	})
}

func (builder Document) clone() Document {
	result := builder
	result.methods = make(map[string]openrpc.Method, len(builder.methods))
	for name, method := range builder.methods {
		result.methods[name] = method
	}
	result.references = append([]openrpc.Reference(nil), builder.references...)
	result.servers = append([]openrpc.Server(nil), builder.servers...)
	if builder.schemaURI != nil {
		value := *builder.schemaURI
		result.schemaURI = &value
	}
	if builder.externalDocs != nil {
		value := *builder.externalDocs
		result.externalDocs = &value
	}
	if builder.components != nil {
		value := *builder.components
		result.components = &value
	}
	return result
}
