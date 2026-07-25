// Package jsonschema integrates OpenAPI Schema Object dialects with the
// canonical json-schema evaluator.
package jsonschema

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"

	canonical "github.com/faustbrian/golib/pkg/json-schema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/specification"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

// Dialect identifies one pinned Swagger or OpenAPI Schema Object rule set.
type Dialect string

const (
	// DialectSwagger20 identifies the pinned Swagger 2.0 Schema Object subset.
	// Swagger 2.0 has no dialect declaration field; this identifier is
	// package-owned selection metadata and is never inserted as $schema.
	DialectSwagger20 Dialect = "http://swagger.io/v2/schema.json"
	// DialectOAS30 identifies the pinned OpenAPI 3.0 Schema Object subset.
	// OpenAPI 3.0 predates JSON Schema dialect declarations; this identifier
	// is package-owned selection metadata and is never inserted as $schema.
	DialectOAS30 Dialect = "https://spec.openapis.org/oas/3.0/schema/2024-10-18"
	// DialectOAS31 is the normative base dialect URI used by OpenAPI 3.1
	// documents when jsonSchemaDialect is absent.
	DialectOAS31 Dialect = "https://spec.openapis.org/oas/3.1/dialect/base"
	// DialectOAS31Snapshot is the exact 3.1 dialect revision pinned here.
	DialectOAS31Snapshot Dialect = "https://spec.openapis.org/oas/3.1/dialect/2024-11-10"
	// DialectOAS32 is the pinned OpenAPI 3.2 vocabulary dialect.
	DialectOAS32 Dialect = "https://spec.openapis.org/oas/3.2/dialect/2025-09-17"
)

const (
	vocabularySwagger20 = "https://github.com/faustbrian/golib/pkg/openapi/jsonschema/swagger/2.0"
	vocabularyOAS30     = "https://github.com/faustbrian/golib/pkg/openapi/jsonschema/oas/3.0"
	vocabularyOAS31     = "https://spec.openapis.org/oas/3.1/vocab/base"
	vocabularyOAS32     = "https://spec.openapis.org/oas/3.2/vocab/base"
	defaultMaxNodes     = 1_000_000
	defaultMaxDepth     = 256
)

// ErrUnsupportedDialect reports a schema dialect outside the supported
// OpenAPI Schema Object versions.
var ErrUnsupportedDialect = errors.New("unsupported Schema Object dialect")

// ErrLimitExceeded reports Schema Object traversal beyond compiler policy.
var ErrLimitExceeded = errors.New("Schema Object traversal limit exceeded")

// Schema is an immutable compiled JSON Schema evaluation plan.
type Schema = canonical.Schema

// Result is the minimum JSON Schema flag output.
type Result = canonical.Result

// OutputUnit is one unit of standard JSON Schema output.
type OutputUnit = canonical.OutputUnit

// ResourceLoader retrieves an explicitly authorized schema resource.
type ResourceLoader = canonical.ResourceLoader

// ResourceLoaderFunc adapts a function to ResourceLoader.
type ResourceLoaderFunc = canonical.ResourceLoaderFunc

// KeywordCompiler compiles one custom vocabulary keyword.
type KeywordCompiler = canonical.KeywordCompiler

// Compiler validates Schema Objects against their OpenAPI vocabulary before
// compiling them into concurrent-safe evaluation plans.
type Compiler struct {
	defaultDialect  Dialect
	baseURI         string
	loader          canonical.ResourceLoader
	vocabularies    []vocabulary
	enginesMu       sync.Mutex
	engines         map[Dialect]*dialectEngine
	constructEngine func(...canonical.Option) (*canonical.Compiler, error)
	readResource    func(string) ([]byte, error)
	compileSchema   func(*canonical.Compiler, context.Context, []byte) (*canonical.Schema, error)
	validateSchema  func(*canonical.Schema, context.Context, []byte, canonical.OutputFormat) (canonical.OutputUnit, error)
	maxNodes        int
	maxDepth        int
}

type dialectEngine struct {
	ready      chan struct{}
	compiler   *canonical.Compiler
	metaSchema *canonical.Schema
	err        error
}

type safeWrappedError struct {
	message string
	cause   error
}

func (err safeWrappedError) Error() string { return err.message }

func (err safeWrappedError) Unwrap() error { return err.cause }

func safeWrap(message string, cause error) error {
	return safeWrappedError{message: message, cause: cause}
}

type vocabulary struct {
	identifier string
	keywords   map[string]canonical.KeywordCompiler
}

// Document supplies the root fields needed to choose a Schema Object base
// dialect without coupling this package to the root openapi package.
type Document interface {
	Raw() jsonvalue.Value
	SpecificationVersion() specversion.Version
}

// Option configures a Compiler.
type Option func(*Compiler) error

// WithTraversalLimits bounds semantic nodes and nesting depth inspected while
// validating and compiling a Schema Object.
func WithTraversalLimits(maxNodes int, maxDepth int) Option {
	return func(compiler *Compiler) error {
		if maxNodes < 1 || maxDepth < 1 {
			return fmt.Errorf("%w: limits must be positive", ErrLimitExceeded)
		}
		compiler.maxNodes = maxNodes
		compiler.maxDepth = maxDepth
		return nil
	}
}

// WithResourceLoader authorizes an additional explicit schema resource
// loader. Pinned OpenAPI resources always take precedence.
func WithResourceLoader(loader canonical.ResourceLoader) Option {
	return func(compiler *Compiler) error {
		if resourceLoaderIsNil(loader) {
			return fmt.Errorf("%w: nil resource loader", canonical.ErrResourceUnavailable)
		}
		compiler.loader = loader
		return nil
	}
}

// WithBaseURI supplies the absolute retrieval URI used to resolve relative
// references when a compiled Schema Object has no enclosing $id.
func WithBaseURI(identifier string) Option {
	return func(compiler *Compiler) error {
		parsed, err := url.Parse(identifier)
		if err != nil || !parsed.IsAbs() || parsed.Fragment != "" {
			return fmt.Errorf("%w: invalid schema base URI", canonical.ErrInvalidSchema)
		}
		compiler.baseURI = identifier
		return nil
	}
}

func resourceLoaderIsNil(loader canonical.ResourceLoader) bool {
	return interfaceIsNil(loader)
}

func interfaceIsNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

// WithVocabulary registers a custom JSON Schema vocabulary with this compiler.
// Registration remains instance-owned and is applied to explicit loaded
// dialects as well as the pinned OpenAPI dialects.
func WithVocabulary(
	identifier string,
	keywords map[string]canonical.KeywordCompiler,
) Option {
	return func(compiler *Compiler) error {
		if !validDialectIdentifier(Dialect(identifier)) {
			return fmt.Errorf("%w: invalid vocabulary identifier", canonical.ErrInvalidSchema)
		}
		for _, registered := range compiler.vocabularies {
			if registered.identifier == identifier {
				return fmt.Errorf("%w: duplicate vocabulary", canonical.ErrInvalidSchema)
			}
		}
		owned := make(map[string]canonical.KeywordCompiler, len(keywords))
		for name, keyword := range keywords {
			if name == "" || interfaceIsNil(keyword) {
				return fmt.Errorf("%w: invalid custom keyword", canonical.ErrInvalidSchema)
			}
			for _, registered := range compiler.vocabularies {
				if _, duplicate := registered.keywords[name]; duplicate {
					return fmt.Errorf("%w: duplicate custom keyword", canonical.ErrInvalidSchema)
				}
			}
			owned[name] = keyword
		}
		compiler.vocabularies = append(compiler.vocabularies, vocabulary{
			identifier: identifier,
			keywords:   owned,
		})
		return nil
	}
}

// NewCompiler constructs an isolated compiler with an explicit OpenAPI base
// dialect. It never performs implicit network retrieval.
func NewCompiler(dialect Dialect, options ...Option) (*Compiler, error) {
	if !validDialectIdentifier(dialect) {
		return nil, fmt.Errorf("%w: invalid identifier", ErrUnsupportedDialect)
	}
	compiler := &Compiler{
		defaultDialect: dialect,
		engines:        make(map[Dialect]*dialectEngine, 5),
		maxNodes:       defaultMaxNodes,
		maxDepth:       defaultMaxDepth,
	}
	for index, option := range options {
		if option == nil {
			return nil, fmt.Errorf("compiler option %d is nil", index)
		}
		if err := option(compiler); err != nil {
			return nil, safeWrap(fmt.Sprintf("compiler option %d failed", index), err)
		}
	}
	return compiler, nil
}

// NewCompilerForDocument selects the Schema Object rules for an OpenAPI
// document. Swagger 2.0 and OpenAPI 3.0 use fixed Draft 4 subsets. OpenAPI 3.1
// and 3.2 honor jsonSchemaDialect.
func NewCompilerForDocument(document Document, options ...Option) (*Compiler, error) {
	if document == nil {
		return nil, fmt.Errorf("%w: nil document", ErrUnsupportedDialect)
	}
	version := document.SpecificationVersion()
	switch version.Dialect() {
	case specversion.DialectSwagger20:
		return NewCompiler(DialectSwagger20, options...)
	case specversion.DialectOAS30:
		return NewCompiler(DialectOAS30, options...)
	case specversion.DialectOAS31, specversion.DialectOAS32:
	default:
		return nil, fmt.Errorf(
			"%w: OpenAPI %s does not use a JSON Schema dialect",
			ErrUnsupportedDialect,
			version.String(),
		)
	}
	dialect := DialectOAS31
	if version.Dialect() == specversion.DialectOAS32 {
		dialect = DialectOAS32
	}
	if declared, exists := document.Raw().Lookup("jsonSchemaDialect"); exists {
		identifier, ok := declared.Text()
		if !ok {
			return nil, fmt.Errorf(
				"%w: jsonSchemaDialect is not a string",
				canonical.ErrInvalidSchema,
			)
		}
		dialect = Dialect(identifier)
	}
	compiler, err := NewCompiler(dialect, options...)
	if err != nil {
		return nil, err
	}
	if version.Dialect() == specversion.DialectOAS32 {
		compiler.applyDocumentSelf(document.Raw())
	}
	return compiler, nil
}

func (compiler *Compiler) applyDocumentSelf(root jsonvalue.Value) {
	self, exists := root.Lookup("$self")
	if !exists {
		return
	}
	raw, valid := self.Text()
	if !valid || raw == "" {
		return
	}
	referenceURI, err := url.Parse(raw)
	if err != nil {
		return
	}
	baseURI, err := url.Parse(compiler.baseURI)
	if err != nil {
		return
	}
	resolved := baseURI.ResolveReference(referenceURI)
	if !resolved.IsAbs() || resolved.Fragment != "" {
		return
	}
	compiler.baseURI = resolved.String()
}

// Compile validates and compiles one immutable Schema Object. A recognized
// per-schema $schema declaration overrides the compiler's base dialect.
func (compiler *Compiler) Compile(
	ctx context.Context,
	value jsonvalue.Value,
) (*Schema, error) {
	engine, dialect, output, err := compiler.prepare(ctx, value)
	if err != nil {
		return nil, err
	}
	if !output.Valid {
		return nil, fmt.Errorf("%w: Schema Object does not satisfy its dialect", canonical.ErrInvalidSchema)
	}
	compiledRaw, err := schemaForCompilation(value, dialect, compiler.baseURI)
	if err != nil {
		return nil, err
	}
	compiled, err := engine.Compile(ctx, compiledRaw)
	if err != nil {
		return nil, safeWrap("compile Schema Object failed", err)
	}
	return compiled, nil
}

// ValidateSchema validates one Schema Object against its selected OpenAPI
// dialect and returns standard JSON Schema basic output without compiling an
// instance evaluation plan.
func (compiler *Compiler) ValidateSchema(
	ctx context.Context,
	value jsonvalue.Value,
) (OutputUnit, error) {
	_, _, output, err := compiler.prepare(ctx, value)
	return output, err
}

func (compiler *Compiler) prepare(
	ctx context.Context,
	value jsonvalue.Value,
) (*canonical.Compiler, Dialect, canonical.OutputUnit, error) {
	if compiler == nil {
		return nil, "", canonical.OutputUnit{},
			fmt.Errorf("%w: nil compiler", canonical.ErrInvalidSchema)
	}
	if ctx == nil {
		return nil, "", canonical.OutputUnit{},
			fmt.Errorf("%w: nil context", canonical.ErrInvalidSchema)
	}
	if err := boundSchemaValue(
		ctx, value, compiler.maxNodes, compiler.maxDepth,
	); err != nil {
		return nil, "", canonical.OutputUnit{}, err
	}
	dialect, err := selectedDialect(value, compiler.defaultDialect)
	if err != nil {
		return nil, "", canonical.OutputUnit{}, err
	}
	engine, metaSchema, err := compiler.engine(ctx, dialect)
	if err != nil {
		return nil, "", canonical.OutputUnit{}, err
	}
	raw, err := value.MarshalJSON()
	if err != nil {
		return nil, "", canonical.OutputUnit{},
			fmt.Errorf("marshal Schema Object: %w", err)
	}
	output, err := compiler.validate(metaSchema, ctx, raw, canonical.OutputBasic)
	if err != nil {
		return nil, "", canonical.OutputUnit{},
			safeWrap("validate OpenAPI Schema Object failed", err)
	}
	if dialect == DialectOAS30 && output.Valid {
		semanticErrors := openAPI30SchemaErrors(value, "")
		if len(semanticErrors) > 0 {
			output.Valid = false
			output.Errors = append(output.Errors, semanticErrors...)
		}
	}
	if dialect == DialectSwagger20 && output.Valid {
		semanticErrors := swagger20SchemaErrors(value, "")
		if len(semanticErrors) > 0 {
			output.Valid = false
			output.Errors = append(output.Errors, semanticErrors...)
		}
	}
	return engine, dialect, output, nil
}

type boundedSchemaValue struct {
	value jsonvalue.Value
	depth int
}

func boundSchemaValue(
	ctx context.Context,
	root jsonvalue.Value,
	maxNodes int,
	maxDepth int,
) error {
	pending := []boundedSchemaValue{{value: root}}
	visited := 0
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		visited++
		childCount, _ := node.value.Length()
		if !schemaChildrenFit(
			childCount, visited, len(pending), node.depth,
			maxNodes, maxDepth,
		) {
			return ErrLimitExceeded
		}
		switch node.value.Kind() {
		case jsonvalue.ArrayKind:
			elements, _ := node.value.Elements()
			for index := len(elements) - 1; index >= 0; index-- {
				pending = append(pending, boundedSchemaValue{
					value: elements[index], depth: node.depth + 1,
				})
			}
		case jsonvalue.ObjectKind:
			members, _ := node.value.Members()
			for index := len(members) - 1; index >= 0; index-- {
				pending = append(pending, boundedSchemaValue{
					value: members[index].Value, depth: node.depth + 1,
				})
			}
		}
	}
	return nil
}

func schemaChildrenFit(
	childCount int,
	visited int,
	queued int,
	depth int,
	maxNodes int,
	maxDepth int,
) bool {
	if childCount == 0 {
		return true
	}
	if depth >= maxDepth {
		return false
	}
	return childCount <= maxNodes-visited-queued
}

func swagger20SchemaErrors(
	value jsonvalue.Value,
	pointer string,
) []canonical.OutputUnit {
	if value.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	if _, reference := value.Lookup("$ref"); reference {
		return nil
	}
	var errors []canonical.OutputUnit
	if defaultValue, hasDefault := value.Lookup("default"); hasDefault {
		if typeValue, hasType := value.Lookup("type"); hasType &&
			!swagger20DefaultMatches(typeValue, defaultValue) {
			errors = append(errors, legacySchemaError(
				pointer+"/default",
				"/properties/default/type",
				"default must conform to the Schema Object type",
			))
		}
	}
	for _, name := range []string{"items", "additionalProperties"} {
		child, exists := value.Lookup(name)
		if !exists {
			continue
		}
		if child.Kind() == jsonvalue.ObjectKind {
			errors = append(
				errors,
				swagger20SchemaErrors(child, pointer+"/"+name)...,
			)
			continue
		}
		if name != "items" {
			continue
		}
		elements, ok := child.Elements()
		if !ok {
			continue
		}
		for index, item := range elements {
			errors = append(errors, swagger20SchemaErrors(
				item,
				pointer+"/items/"+strconv.Itoa(index),
			)...)
		}
	}
	allOf, exists := value.Lookup("allOf")
	if exists {
		elements, ok := allOf.Elements()
		if ok {
			for index, child := range elements {
				errors = append(errors, swagger20SchemaErrors(
					child,
					pointer+"/allOf/"+strconv.Itoa(index),
				)...)
			}
		}
	}
	properties, exists := value.Lookup("properties")
	if !exists {
		return errors
	}
	members, ok := properties.Members()
	if !ok {
		return errors
	}
	for _, member := range members {
		errors = append(errors, swagger20SchemaErrors(
			member.Value,
			pointer+"/properties/"+escapeSchemaPointerToken(member.Name),
		)...)
	}
	return errors
}

func swagger20DefaultMatches(
	typeValue jsonvalue.Value,
	defaultValue jsonvalue.Value,
) bool {
	if typeName, valid := typeValue.Text(); valid {
		return schemaValueMatchesType(typeName, defaultValue)
	}
	types, valid := typeValue.Elements()
	if !valid {
		return true
	}
	for _, candidate := range types {
		typeName, valid := candidate.Text()
		if valid && schemaValueMatchesType(typeName, defaultValue) {
			return true
		}
	}
	return false
}

func openAPI30SchemaErrors(
	value jsonvalue.Value,
	pointer string,
) []canonical.OutputUnit {
	if value.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	if _, reference := value.Lookup("$ref"); reference {
		return nil
	}
	var errors []canonical.OutputUnit
	if typeName, exists := stringMember(value, "type"); exists {
		if typeName == "array" {
			if _, hasItems := value.Lookup("items"); !hasItems {
				errors = append(errors, legacySchemaError(
					pointer+"/items",
					"/properties/items/required",
					"items is required when type is array",
				))
			}
		}
		if defaultValue, hasDefault := value.Lookup("default"); hasDefault &&
			!openAPI30DefaultMatches(value, typeName, defaultValue) {
			errors = append(errors, legacySchemaError(
				pointer+"/default",
				"/properties/default/type",
				"default must conform to the Schema Object type",
			))
		}
	}
	for _, name := range []string{"not", "items", "additionalProperties"} {
		if child, exists := value.Lookup(name); exists {
			errors = append(
				errors,
				openAPI30SchemaErrors(child, pointer+"/"+name)...,
			)
		}
	}
	for _, name := range []string{"allOf", "oneOf", "anyOf"} {
		children, exists := value.Lookup(name)
		if !exists {
			continue
		}
		elements, ok := children.Elements()
		if !ok {
			continue
		}
		for index, child := range elements {
			errors = append(errors, openAPI30SchemaErrors(
				child,
				pointer+"/"+name+"/"+strconv.Itoa(index),
			)...)
		}
	}
	properties, exists := value.Lookup("properties")
	if !exists {
		return errors
	}
	members, ok := properties.Members()
	if !ok {
		return errors
	}
	for _, member := range members {
		errors = append(errors, openAPI30SchemaErrors(
			member.Value,
			pointer+"/properties/"+escapeSchemaPointerToken(member.Name),
		)...)
	}
	return errors
}

func legacySchemaError(
	pointer string,
	keywordPointer string,
	message string,
) canonical.OutputUnit {
	return canonical.OutputUnit{
		Valid:            false,
		KeywordLocation:  keywordPointer,
		InstanceLocation: pointer,
		Error:            message,
	}
}

func openAPI30DefaultMatches(
	schema jsonvalue.Value,
	typeName string,
	value jsonvalue.Value,
) bool {
	if value.Kind() == jsonvalue.NullKind {
		nullable, _ := schema.Lookup("nullable")
		allowed, _ := nullable.Bool()
		return allowed
	}
	return schemaValueMatchesType(typeName, value)
}

func schemaValueMatchesType(
	typeName string,
	value jsonvalue.Value,
) bool {
	if value.Kind() == jsonvalue.NullKind {
		return typeName == "null"
	}
	switch typeName {
	case "array":
		return value.Kind() == jsonvalue.ArrayKind
	case "boolean":
		return value.Kind() == jsonvalue.BooleanKind
	case "integer":
		if value.Kind() != jsonvalue.NumberKind {
			return false
		}
		number, _ := value.NumberText()
		rational, valid := new(big.Rat).SetString(number)
		return valid && rational.IsInt()
	case "number":
		return value.Kind() == jsonvalue.NumberKind
	case "null":
		return false
	case "object":
		return value.Kind() == jsonvalue.ObjectKind
	case "string":
		return value.Kind() == jsonvalue.StringKind
	default:
		return true
	}
}

func stringMember(value jsonvalue.Value, name string) (string, bool) {
	member, exists := value.Lookup(name)
	if !exists {
		return "", false
	}
	return member.Text()
}

func escapeSchemaPointerToken(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}

func (compiler *Compiler) engine(
	ctx context.Context,
	dialect Dialect,
) (*canonical.Compiler, *canonical.Schema, error) {
	_, pinned := dialectResource(dialect)
	cacheable := pinned || dialect == compiler.defaultDialect
	if !cacheable {
		return compiler.newEngine(ctx, dialect)
	}

	compiler.enginesMu.Lock()
	entry, exists := compiler.engines[dialect]
	if !exists {
		entry = &dialectEngine{ready: make(chan struct{})}
		compiler.engines[dialect] = entry
	}
	compiler.enginesMu.Unlock()
	if exists {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-entry.ready:
			return entry.compiler, entry.metaSchema, entry.err
		}
	}

	entry.compiler, entry.metaSchema, entry.err = compiler.newEngine(ctx, dialect)
	if entry.err != nil {
		compiler.enginesMu.Lock()
		if compiler.engines[dialect] == entry {
			delete(compiler.engines, dialect)
		}
		compiler.enginesMu.Unlock()
	}
	close(entry.ready)
	return entry.compiler, entry.metaSchema, entry.err
}

func (compiler *Compiler) newEngine(
	ctx context.Context,
	dialect Dialect,
) (*canonical.Compiler, *canonical.Schema, error) {
	resource, _ := metaResource(dialect)
	loader := &resourceLoader{fallback: compiler.loader, dialect: dialect}
	baseDialect := canonical.Draft202012
	if dialect == DialectSwagger20 || dialect == DialectOAS30 {
		baseDialect = canonical.Draft4
	}
	options := []canonical.Option{
		canonical.WithDialect(baseDialect),
		canonical.WithResourceLoader(loader),
	}
	if _, known := dialectResource(dialect); known {
		options = append(options, canonical.WithVocabulary(
			vocabularyIdentifier(dialect),
			schemaKeywords(dialect),
		))
	}
	for _, vocabulary := range compiler.vocabularies {
		options = append(options, canonical.WithVocabulary(
			vocabulary.identifier,
			vocabulary.keywords,
		))
	}
	construct := compiler.constructEngine
	if construct == nil {
		construct = canonical.NewCompiler
	}
	engine, err := construct(options...)
	if err != nil {
		return nil, nil, safeWrap("construct JSON Schema compiler failed", err)
	}
	var raw []byte
	if dialect == DialectSwagger20 {
		raw = []byte(`{"$ref":"http://swagger.io/v2/schema.json#/definitions/schema"}`)
	} else if dialect == DialectOAS30 {
		raw = []byte(`{"$ref":"https://spec.openapis.org/oas/3.0/schema/2024-10-18#/definitions/Schema"}`)
	} else if resource != "" {
		read := compiler.readResource
		if read == nil {
			read = specification.Read
		}
		raw, err = read(resource)
	} else {
		raw, err = loader.Load(ctx, string(dialect))
	}
	if err != nil {
		return nil, nil, safeWrap("load Schema Object dialect failed", err)
	}
	compile := compiler.compileSchema
	if compile == nil {
		compile = func(
			engine *canonical.Compiler,
			ctx context.Context,
			raw []byte,
		) (*canonical.Schema, error) {
			return engine.Compile(ctx, raw)
		}
	}
	metaSchema, err := compile(engine, ctx, raw)
	if err != nil {
		return nil, nil, safeWrap("compile Schema Object rules failed", err)
	}
	loader.metaSchema = metaSchema
	return engine, metaSchema, nil
}

func (compiler *Compiler) validate(
	schema *canonical.Schema,
	ctx context.Context,
	raw []byte,
	format canonical.OutputFormat,
) (canonical.OutputUnit, error) {
	if compiler.validateSchema != nil {
		return compiler.validateSchema(schema, ctx, raw, format)
	}
	return schema.ValidateOutput(ctx, raw, format)
}

func selectedDialect(value jsonvalue.Value, fallback Dialect) (Dialect, error) {
	if fallback == DialectSwagger20 || fallback == DialectOAS30 {
		return fallback, nil
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return fallback, nil
	}
	declared, exists := value.Lookup("$schema")
	if !exists {
		return fallback, nil
	}
	identifier, ok := declared.Text()
	if !ok {
		return "", fmt.Errorf("%w: $schema is not a string", canonical.ErrInvalidSchema)
	}
	dialect := Dialect(identifier)
	if !validDialectIdentifier(dialect) {
		return "", fmt.Errorf("%w: invalid identifier", ErrUnsupportedDialect)
	}
	return dialect, nil
}

func validDialectIdentifier(dialect Dialect) bool {
	identifier, err := url.Parse(string(dialect))
	return err == nil && identifier.IsAbs() && identifier.Fragment == ""
}

type valueFactory struct {
	stringValue func(string) (jsonvalue.Value, error)
	arrayValue  func([]jsonvalue.Value) (jsonvalue.Value, error)
	objectValue func([]jsonvalue.Member) (jsonvalue.Value, error)
}

func immutableValueFactory() valueFactory {
	return valueFactory{
		stringValue: jsonvalue.String,
		arrayValue:  jsonvalue.Array,
		objectValue: jsonvalue.Object,
	}
}

func withDialect(value jsonvalue.Value, dialect Dialect) ([]byte, error) {
	return withDialectUsing(value, dialect, immutableValueFactory())
}

func withDialectUsing(
	value jsonvalue.Value,
	dialect Dialect,
	factory valueFactory,
) ([]byte, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value.MarshalJSON()
	}
	if _, exists := value.Lookup("$schema"); exists {
		return value.MarshalJSON()
	}
	identifier, err := factory.stringValue(string(dialect))
	if err != nil {
		return nil, err
	}
	members, _ := value.Members()
	members = append([]jsonvalue.Member{{Name: "$schema", Value: identifier}}, members...)
	withDeclaration, err := factory.objectValue(members)
	if err != nil {
		return nil, err
	}
	return withDeclaration.MarshalJSON()
}

func schemaForCompilation(
	value jsonvalue.Value,
	dialect Dialect,
	baseURI string,
) ([]byte, error) {
	return schemaForCompilationUsing(
		value, dialect, baseURI, applyOpenAPI30Nullable,
	)
}

func schemaForCompilationUsing(
	value jsonvalue.Value,
	dialect Dialect,
	baseURI string,
	translateNullable func(jsonvalue.Value) (jsonvalue.Value, error),
) ([]byte, error) {
	if dialect == DialectSwagger20 {
		return value.MarshalJSON()
	}
	if dialect != DialectOAS30 {
		withBase, err := withSchemaBaseURI(value, baseURI)
		if err != nil {
			return nil, err
		}
		return withDialect(withBase, dialect)
	}
	transformed, err := translateNullable(value)
	if err != nil {
		return nil, fmt.Errorf("translate OpenAPI 3.0 nullable: %w", err)
	}
	return transformed.MarshalJSON()
}

func withSchemaBaseURI(
	value jsonvalue.Value,
	baseURI string,
) (jsonvalue.Value, error) {
	return withSchemaBaseURIUsing(value, baseURI, immutableValueFactory())
}

func withSchemaBaseURIUsing(
	value jsonvalue.Value,
	baseURI string,
	factory valueFactory,
) (jsonvalue.Value, error) {
	if baseURI == "" || value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	members, _ := value.Members()
	if identifierValue, exists := value.Lookup("$id"); exists {
		identifier, valid := identifierValue.Text()
		if !valid {
			return value, nil
		}
		referenceURI, err := url.Parse(identifier)
		if err != nil || referenceURI.IsAbs() {
			return value, nil
		}
		base, err := url.Parse(baseURI)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		absolute, err := factory.stringValue(base.ResolveReference(referenceURI).String())
		if err != nil {
			return jsonvalue.Value{}, err
		}
		for index := range members {
			if members[index].Name == "$id" {
				members[index].Value = absolute
				break
			}
		}
		return factory.objectValue(members)
	}
	identifier, err := factory.stringValue(baseURI)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	members = append([]jsonvalue.Member{{Name: "$id", Value: identifier}}, members...)
	return factory.objectValue(members)
}

func applyOpenAPI30Nullable(value jsonvalue.Value) (jsonvalue.Value, error) {
	return applyOpenAPI30NullableUsing(value, immutableValueFactory())
}

func applyOpenAPI30NullableUsing(
	value jsonvalue.Value,
	factory valueFactory,
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	if _, reference := value.Lookup("$ref"); reference {
		return value, nil
	}
	members, _ := value.Members()
	for index := range members {
		var err error
		switch members[index].Name {
		case "not", "items", "additionalProperties":
			if members[index].Value.Kind() == jsonvalue.ObjectKind {
				members[index].Value, err = applyOpenAPI30NullableUsing(
					members[index].Value, factory,
				)
			}
		case "allOf", "oneOf", "anyOf":
			members[index].Value, err = transformOpenAPI30SchemaArrayUsing(
				members[index].Value, factory,
			)
		case "properties":
			members[index].Value, err = transformOpenAPI30SchemaMapUsing(
				members[index].Value, factory,
			)
		}
		if err != nil {
			return jsonvalue.Value{}, err
		}
	}
	nullable, hasNullable := value.Lookup("nullable")
	allowsNull, _ := nullable.Bool()
	typeValue, hasType := value.Lookup("type")
	if hasNullable && allowsNull && hasType {
		typeName, stringType := typeValue.Text()
		if stringType {
			nullName, err := factory.stringValue("null")
			if err != nil {
				return jsonvalue.Value{}, err
			}
			namedType, err := factory.stringValue(typeName)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			types, err := factory.arrayValue([]jsonvalue.Value{namedType, nullName})
			if err != nil {
				return jsonvalue.Value{}, err
			}
			for index := range members {
				if members[index].Name == "type" {
					members[index].Value = types
					break
				}
			}
		}
	}
	return factory.objectValue(members)
}

func transformOpenAPI30SchemaArray(value jsonvalue.Value) (jsonvalue.Value, error) {
	return transformOpenAPI30SchemaArrayUsing(value, immutableValueFactory())
}

func transformOpenAPI30SchemaArrayUsing(
	value jsonvalue.Value,
	factory valueFactory,
) (jsonvalue.Value, error) {
	elements, ok := value.Elements()
	if !ok {
		return value, nil
	}
	for index := range elements {
		var err error
		elements[index], err = applyOpenAPI30NullableUsing(elements[index], factory)
		if err != nil {
			return jsonvalue.Value{}, err
		}
	}
	return factory.arrayValue(elements)
}

func transformOpenAPI30SchemaMap(value jsonvalue.Value) (jsonvalue.Value, error) {
	return transformOpenAPI30SchemaMapUsing(value, immutableValueFactory())
}

func transformOpenAPI30SchemaMapUsing(
	value jsonvalue.Value,
	factory valueFactory,
) (jsonvalue.Value, error) {
	members, ok := value.Members()
	if !ok {
		return value, nil
	}
	for index := range members {
		var err error
		members[index].Value, err = applyOpenAPI30NullableUsing(
			members[index].Value, factory,
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
	}
	return factory.objectValue(members)
}

func dialectResource(dialect Dialect) (string, bool) {
	switch dialect {
	case DialectSwagger20:
		return "schemas/2.0/2017-08-27.json", true
	case DialectOAS30:
		return "schemas/3.0/2024-10-18.json", true
	case DialectOAS31, DialectOAS31Snapshot:
		return "schemas/3.1/dialect-2024-11-10.json", true
	case DialectOAS32:
		return "schemas/3.2/dialect-2025-09-17.json", true
	default:
		return "", false
	}
}

func metaResource(dialect Dialect) (string, bool) {
	switch dialect {
	case DialectOAS31, DialectOAS31Snapshot:
		return "schemas/3.1/meta-2024-11-10.json", true
	case DialectOAS32:
		return "schemas/3.2/meta-2025-09-17.json", true
	default:
		return "", false
	}
}

func vocabularyIdentifier(dialect Dialect) string {
	switch dialect {
	case DialectSwagger20:
		return vocabularySwagger20
	case DialectOAS30:
		return vocabularyOAS30
	case DialectOAS31, DialectOAS31Snapshot:
		return vocabularyOAS31
	default:
		return vocabularyOAS32
	}
}

func schemaKeywords(dialect Dialect) map[string]canonical.KeywordCompiler {
	names := []string{"discriminator", "example", "externalDocs", "xml"}
	switch dialect {
	case DialectOAS30:
		names = append(names, "nullable")
	}
	keywords := make(map[string]canonical.KeywordCompiler, len(names))
	for _, name := range names {
		keywords[name] = annotationCompiler{}
	}
	return keywords
}

type annotationCompiler struct {
	marshal func(canonical.Value) (json.RawMessage, error)
}

func (compiler annotationCompiler) Compile(
	_ context.Context,
	_ canonical.Dialect,
	value canonical.Value,
) (canonical.KeywordEvaluator, error) {
	marshal := compiler.marshal
	if marshal == nil {
		marshal = marshalCanonicalValue
	}
	raw, err := marshal(value)
	if err != nil {
		return nil, err
	}
	return canonical.KeywordEvaluatorFunc(func(
		context.Context,
		canonical.Value,
	) (canonical.KeywordResult, error) {
		return canonical.KeywordResult{Valid: true, Annotation: raw}, nil
	}), nil
}

type resourceLoader struct {
	fallback   canonical.ResourceLoader
	dialect    Dialect
	metaSchema *canonical.Schema
	validate   func(*canonical.Schema, context.Context, []byte, canonical.OutputFormat) (canonical.OutputUnit, error)
}

func (loader *resourceLoader) Load(ctx context.Context, identifier string) ([]byte, error) {
	if resource, ok := embeddedResource(identifier); ok {
		return specification.Read(resource)
	}
	if loader.fallback != nil {
		raw, err := loader.fallback.Load(ctx, identifier)
		if err != nil {
			return nil, safeWrap("load Schema Object resource failed", err)
		}
		return loader.prepareLegacyResource(ctx, raw)
	}
	return nil, fmt.Errorf("%w: external resource", canonical.ErrResourceUnavailable)
}

func (loader *resourceLoader) prepareLegacyResource(
	ctx context.Context,
	raw []byte,
) ([]byte, error) {
	if loader.metaSchema == nil ||
		(loader.dialect != DialectSwagger20 && loader.dialect != DialectOAS30) {
		return raw, nil
	}
	value, err := parse.JSON(ctx, bytes.NewReader(raw), parse.DefaultLimits())
	if err != nil {
		return nil, fmt.Errorf("parse loaded Schema Object: %w", err)
	}
	validate := loader.validate
	if validate == nil {
		validate = func(
			schema *canonical.Schema,
			ctx context.Context,
			raw []byte,
			format canonical.OutputFormat,
		) (canonical.OutputUnit, error) {
			return schema.ValidateOutput(ctx, raw, format)
		}
	}
	output, err := validate(loader.metaSchema, ctx, raw, canonical.OutputFlag)
	if err != nil {
		return nil, safeWrap("validate loaded Schema Object failed", err)
	}
	if !output.Valid {
		return nil, fmt.Errorf("%w: loaded Schema Object does not satisfy its dialect", canonical.ErrInvalidSchema)
	}
	var semanticErrors []canonical.OutputUnit
	if loader.dialect == DialectSwagger20 {
		semanticErrors = swagger20SchemaErrors(value, "")
	} else {
		semanticErrors = openAPI30SchemaErrors(value, "")
	}
	if len(semanticErrors) > 0 {
		return nil, fmt.Errorf("%w: loaded Schema Object violates its dialect", canonical.ErrInvalidSchema)
	}
	if loader.dialect == DialectOAS30 {
		return schemaForCompilation(value, loader.dialect, "")
	}
	return raw, nil
}

func embeddedResource(identifier string) (string, bool) {
	switch identifier {
	case string(DialectSwagger20), "http://swagger.io/v2/schema.json#":
		return "schemas/2.0/2017-08-27.json", true
	case "http://json-schema.org/draft-04/schema",
		"http://json-schema.org/draft-04/schema#":
		return "schemas/json-schema/draft-04.json", true
	case string(DialectOAS30):
		return "schemas/3.0/2024-10-18.json", true
	case string(DialectOAS31), string(DialectOAS31Snapshot):
		return "schemas/3.1/dialect-2024-11-10.json", true
	case "https://spec.openapis.org/oas/3.1/meta/2024-11-10":
		return "schemas/3.1/meta-2024-11-10.json", true
	case string(DialectOAS32):
		return "schemas/3.2/dialect-2025-09-17.json", true
	case "https://spec.openapis.org/oas/3.2/meta/2025-09-17":
		return "schemas/3.2/meta-2025-09-17.json", true
	default:
		return "", false
	}
}

func marshalCanonicalValue(value canonical.Value) (json.RawMessage, error) {
	return marshalCanonicalValueUsing(value, immutableCanonicalAccess())
}

func marshalCanonicalValueUsing(
	value canonical.Value,
	access canonicalAccess,
) (json.RawMessage, error) {
	var output bytes.Buffer
	if err := appendCanonicalValueUsing(&output, value, access); err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), output.Bytes()...), nil
}

type canonicalAccess struct {
	kind   func(canonical.Value) canonical.ValueKind
	length func(canonical.Value) int
	index  func(canonical.Value, int) (canonical.Value, bool)
	names  func(canonical.Value) []string
	lookup func(canonical.Value, string) (canonical.Value, bool)
}

func immutableCanonicalAccess() canonicalAccess {
	return canonicalAccess{
		kind:   canonical.Value.Kind,
		length: canonical.Value.Len,
		index:  canonical.Value.Index,
		names:  canonical.Value.Names,
		lookup: canonical.Value.Lookup,
	}
}

func appendCanonicalValueUsing(
	output *bytes.Buffer,
	value canonical.Value,
	access canonicalAccess,
) error {
	switch access.kind(value) {
	case canonical.NullKind:
		output.WriteString("null")
	case canonical.BooleanKind:
		boolean, _ := value.Bool()
		output.WriteString(strconv.FormatBool(boolean))
	case canonical.NumberKind:
		number, _ := value.Number()
		output.WriteString(number)
	case canonical.StringKind:
		text, _ := value.String()
		raw, _ := json.Marshal(text)
		output.Write(raw)
	case canonical.ArrayKind:
		output.WriteByte('[')
		for index := range access.length(value) {
			if index > 0 {
				output.WriteByte(',')
			}
			item, _ := access.index(value, index)
			if err := appendCanonicalValueUsing(output, item, access); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case canonical.ObjectKind:
		output.WriteByte('{')
		for index, name := range access.names(value) {
			if index > 0 {
				output.WriteByte(',')
			}
			raw, _ := json.Marshal(name)
			output.Write(raw)
			output.WriteByte(':')
			item, _ := access.lookup(value, name)
			if err := appendCanonicalValueUsing(output, item, access); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	default:
		return fmt.Errorf("unsupported JSON Schema value kind %d", access.kind(value))
	}
	return nil
}
