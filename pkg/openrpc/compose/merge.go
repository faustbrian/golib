package compose

import (
	"context"
	"errors"
	"sort"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
)

var (
	ErrInvalidMerge  = errors.New("compose: invalid merge options")
	ErrMergeLimit    = errors.New("compose: merge limit exceeded")
	ErrMergeConflict = errors.New("compose: merge conflict")
)

// ConflictPolicy controls duplicate method and component names.
type ConflictPolicy uint8

const (
	ConflictError ConflictPolicy = iota
	KeepFirst
	KeepLast
)

// MergeOptions bounds composition and makes collision behavior explicit.
type MergeOptions struct {
	Conflict      ConflictPolicy
	MaxDocuments  int
	MaxMethods    int
	MaxComponents int
}

func DefaultMergeOptions() MergeOptions {
	return MergeOptions{
		Conflict: ConflictError, MaxDocuments: 32,
		MaxMethods: 10_000, MaxComponents: 100_000,
	}
}

// Merge combines methods and every component registry. Root metadata and
// optional non-registry fields come from the first document. All documents
// must declare the same supported version.
func Merge(ctx context.Context, documents []openrpc.Document, options MergeOptions) (openrpc.Document, error) {
	if ctx == nil || len(documents) == 0 || options.MaxDocuments <= 0 ||
		options.MaxMethods <= 0 || options.MaxComponents <= 0 ||
		options.Conflict > KeepLast {
		return openrpc.Document{}, ErrInvalidMerge
	}
	if len(documents) > options.MaxDocuments {
		return openrpc.Document{}, ErrMergeLimit
	}
	if err := ctx.Err(); err != nil {
		return openrpc.Document{}, err
	}
	methods := make(map[string]openrpc.Method)
	references := make(map[string]openrpc.Reference)
	components := componentAccumulator{}
	methodCount := 0
	componentCount := 0
	for _, document := range documents {
		if err := ctx.Err(); err != nil {
			return openrpc.Document{}, err
		}
		if document.Version() != documents[0].Version() {
			return openrpc.Document{}, ErrMergeConflict
		}
		for _, union := range document.Methods() {
			methodCount++
			if methodCount > options.MaxMethods {
				return openrpc.Document{}, ErrMergeLimit
			}
			if method, ok := union.Method(); ok {
				if err := mergeNamed(methods, method.Name(), method, options.Conflict); err != nil {
					return openrpc.Document{}, err
				}
			} else if reference, ok := union.Reference(); ok {
				if err := mergeNamed(references, reference.Ref(), reference, options.Conflict); err != nil {
					return openrpc.Document{}, err
				}
			}
		}
		if value, present := document.Components(); present {
			added, err := components.merge(value, options.Conflict)
			if err != nil {
				return openrpc.Document{}, err
			}
			componentCount += added
			if componentCount > options.MaxComponents {
				return openrpc.Document{}, ErrMergeLimit
			}
		}
	}

	methodNames := sortedMapNames(methods)
	referenceNames := sortedMapNames(references)
	unions := make([]openrpc.MethodOrReference, 0, len(methodNames)+len(referenceNames))
	for _, name := range methodNames {
		unions = append(unions, openrpc.MethodValue(methods[name]))
	}
	for _, name := range referenceNames {
		unions = append(unions, openrpc.MethodReference(references[name]))
	}
	mergedComponents, hasComponents := components.build()
	return copyMergedDocument(documents[0], unions, mergedComponents, hasComponents)
}

func mergeNamed[T any](target map[string]T, name string, value T, policy ConflictPolicy) error {
	if _, duplicate := target[name]; duplicate {
		switch policy {
		case ConflictError:
			return ErrMergeConflict
		case KeepFirst:
			return nil
		}
	}
	target[name] = value
	return nil
}

type componentAccumulator struct {
	schemas                                      map[string]jsonschema.Schema
	links                                        map[string]openrpc.Link
	errors                                       map[string]openrpc.Error
	examples                                     map[string]openrpc.Example
	pairings                                     map[string]openrpc.ExamplePairing
	descriptors                                  map[string]openrpc.ContentDescriptor
	tags                                         map[string]openrpc.Tag
	hasSchemas, hasLinks, hasErrors, hasExamples bool
	hasPairings, hasDescriptors, hasTags         bool
}

func (accumulator *componentAccumulator) merge(value openrpc.Components, policy ConflictPolicy) (int, error) {
	count := 0
	var err error
	if values, present := value.Schemas(); present {
		accumulator.hasSchemas = true
		count += len(values)
		accumulator.schemas, err = mergeComponentMap(accumulator.schemas, values, policy)
		if err != nil {
			return 0, err
		}
	}
	if values, present := value.Links(); present {
		accumulator.hasLinks = true
		count += len(values)
		accumulator.links, err = mergeComponentMap(accumulator.links, values, policy)
		if err != nil {
			return 0, err
		}
	}
	if values, present := value.Errors(); present {
		accumulator.hasErrors = true
		count += len(values)
		accumulator.errors, err = mergeComponentMap(accumulator.errors, values, policy)
		if err != nil {
			return 0, err
		}
	}
	if values, present := value.Examples(); present {
		accumulator.hasExamples = true
		count += len(values)
		accumulator.examples, err = mergeComponentMap(accumulator.examples, values, policy)
		if err != nil {
			return 0, err
		}
	}
	if values, present := value.ExamplePairings(); present {
		accumulator.hasPairings = true
		count += len(values)
		accumulator.pairings, err = mergeComponentMap(accumulator.pairings, values, policy)
		if err != nil {
			return 0, err
		}
	}
	if values, present := value.ContentDescriptors(); present {
		accumulator.hasDescriptors = true
		count += len(values)
		accumulator.descriptors, err = mergeComponentMap(accumulator.descriptors, values, policy)
		if err != nil {
			return 0, err
		}
	}
	if values, present := value.Tags(); present {
		accumulator.hasTags = true
		count += len(values)
		accumulator.tags, err = mergeComponentMap(accumulator.tags, values, policy)
		if err != nil {
			return 0, err
		}
	}
	return count, nil
}

func mergeComponentMap[T any](target map[string]T, source map[string]T, policy ConflictPolicy) (map[string]T, error) {
	if target == nil {
		target = make(map[string]T)
	}
	for _, name := range sortedMapNames(source) {
		if err := mergeNamed(target, name, source[name], policy); err != nil {
			return nil, err
		}
	}
	return target, nil
}

func (accumulator componentAccumulator) build() (openrpc.Components, bool) {
	has := accumulator.hasSchemas || accumulator.hasLinks || accumulator.hasErrors || accumulator.hasExamples || accumulator.hasPairings || accumulator.hasDescriptors || accumulator.hasTags
	if !has {
		return openrpc.Components{}, false
	}
	input := openrpc.ComponentsInput{}
	if accumulator.hasSchemas {
		input.Schemas = accumulator.schemas
	}
	if accumulator.hasLinks {
		input.Links = accumulator.links
	}
	if accumulator.hasErrors {
		input.Errors = accumulator.errors
	}
	if accumulator.hasExamples {
		input.Examples = accumulator.examples
	}
	if accumulator.hasPairings {
		input.ExamplePairings = accumulator.pairings
	}
	if accumulator.hasDescriptors {
		input.ContentDescriptors = accumulator.descriptors
	}
	if accumulator.hasTags {
		input.Tags = accumulator.tags
	}
	components, _ := openrpc.NewComponents(input)
	return components, true
}

func sortedMapNames[T any](values map[string]T) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func copyMergedDocument(base openrpc.Document, methods []openrpc.MethodOrReference, components openrpc.Components, hasComponents bool) (openrpc.Document, error) {
	schemaURI, explicitSchema := base.SchemaURI()
	var schema *string
	if explicitSchema {
		schema = &schemaURI
	}
	servers, hasServers := base.Servers()
	docsValue, hasDocs := base.ExternalDocs()
	var docs *openrpc.ExternalDocumentation
	if hasDocs {
		docs = &docsValue
	}
	var componentInput *openrpc.Components
	if hasComponents {
		componentInput = &components
	}
	info := base.Info()
	return openrpc.NewDocument(openrpc.DocumentInput{
		Version:       base.Version(),
		SchemaURI:     schema,
		Info:          &info,
		ExternalDocs:  docs,
		Servers:       servers,
		HasServers:    hasServers,
		Methods:       methods,
		Components:    componentInput,
		Extensions:    base.Extensions(),
		UnknownFields: base.UnknownFields(),
	})
}
