package jsonschema

import (
	"context"
	"fmt"
	"reflect"
)

// Option configures a Compiler without mutating shared global state.
type Option func(*compilerConfig) error

type compilerConfig struct {
	dialect       Dialect
	limits        Limits
	loader        ResourceLoader
	assertFormats bool
	assertContent bool
	formats       map[string]FormatChecker
	vocabularies  map[string]registeredVocabulary
}

// ResourceLoader retrieves an explicitly authorized schema resource.
type ResourceLoader interface {
	Load(context.Context, string) ([]byte, error)
}

// ResourceLoaderFunc adapts a function to ResourceLoader.
type ResourceLoaderFunc func(context.Context, string) ([]byte, error)

// Load implements ResourceLoader.
func (loader ResourceLoaderFunc) Load(ctx context.Context, identifier string) ([]byte, error) {
	return loader(ctx, identifier)
}

// WithDialect selects the dialect used to compile schemas.
func WithDialect(dialect Dialect) Option {
	return func(config *compilerConfig) error {
		if err := dialect.validate(); err != nil {
			return err
		}
		config.dialect = dialect

		return nil
	}
}

// WithLimits replaces the compiler's resource limits.
func WithLimits(limits Limits) Option {
	return func(config *compilerConfig) error {
		if err := limits.validate(); err != nil {
			return err
		}
		config.limits = limits

		return nil
	}
}

// WithResourceLoader authorizes explicit schema retrieval during compilation.
func WithResourceLoader(loader ResourceLoader) Option {
	return func(config *compilerConfig) error {
		if resourceLoaderIsNil(loader) {
			return fmt.Errorf("%w: nil resource loader", ErrResourceUnavailable)
		}
		config.loader = loader

		return nil
	}
}

// WithFormatAssertion enables format validation for recognized formats.
func WithFormatAssertion() Option {
	return func(config *compilerConfig) error {
		config.assertFormats = true

		return nil
	}
}

// WithContentAssertion enables validation of recognized content encodings and
// media types. Content keywords are annotations unless this option is used.
func WithContentAssertion() Option {
	return func(config *compilerConfig) error {
		config.assertContent = true

		return nil
	}
}

// WithFormat registers or replaces one compiler-owned format checker.
func WithFormat(name string, checker FormatChecker) Option {
	return func(config *compilerConfig) error {
		if name == "" || formatCheckerIsNil(checker) {
			return fmt.Errorf("%w: invalid format registration %q", ErrInvalidSchema, name)
		}
		config.formats[name] = customFormatChecker{checker: checker}

		return nil
	}
}

func resourceLoaderIsNil(loader ResourceLoader) bool {
	if loader == nil {
		return true
	}
	value := reflect.ValueOf(loader)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func formatCheckerIsNil(checker FormatChecker) bool {
	if checker == nil {
		return true
	}
	value := reflect.ValueOf(checker)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Compiler owns immutable dialect and resource policy used during compilation.
type Compiler struct {
	dialect       Dialect
	limits        Limits
	loader        ResourceLoader
	assertFormats bool
	assertContent bool
	formats       map[string]FormatChecker
	metaSchema    *Schema
	vocabularies  map[string]registeredVocabulary
}

// NewCompiler constructs an isolated compiler. Draft 2020-12 is the explicit
// default when no dialect option is supplied.
func NewCompiler(options ...Option) (*Compiler, error) {
	config := compilerConfig{
		dialect:      Draft202012,
		limits:       DefaultLimits(),
		formats:      standardFormats(),
		vocabularies: make(map[string]registeredVocabulary),
	}

	for index, option := range options {
		if option == nil {
			return nil, fmt.Errorf("compiler option %d is nil", index)
		}
		if err := option(&config); err != nil {
			return nil, fmt.Errorf("compiler option %d: %w", index, err)
		}
	}
	return newCompiler(config, compileOfficialMetaSchema)
}

func newCompiler(
	config compilerConfig,
	prepareMetaSchema func(Dialect) (*Schema, error),
) (*Compiler, error) {
	formats := cloneFormats(config.formats)
	applyStandardFormatLimits(formats, config.limits)
	compiler := &Compiler{
		dialect:       config.dialect,
		limits:        config.limits,
		loader:        config.loader,
		assertFormats: config.assertFormats,
		assertContent: config.assertContent,
		formats:       formats,
		vocabularies:  cloneVocabularies(config.vocabularies),
	}
	metaSchema, err := prepareMetaSchema(config.dialect)
	if err != nil {
		return nil, fmt.Errorf("prepare %s meta-schema: %w", config.dialect, err)
	}
	compiler.metaSchema = metaSchema

	return compiler, nil
}

// Schema is an immutable evaluation plan safe for concurrent validation.
type Schema struct {
	dialect Dialect
	limits  Limits
	plan    *schemaPlan
}

// Compile parses and compiles one schema document using the configured loader.
func (compiler *Compiler) Compile(ctx context.Context, raw []byte) (*Schema, error) {
	if compiler == nil {
		return nil, fmt.Errorf("%w: nil compiler", ErrInvalidSchema)
	}
	if len(raw) > compiler.limits.MaxTotalSchemaBytes {
		return nil, &LimitError{
			Resource: "total schema bytes",
			Limit:    compiler.limits.MaxTotalSchemaBytes,
		}
	}

	value, err := decodeJSON(ctx, raw, compiler.limits)
	if err != nil {
		return nil, fmt.Errorf("compile schema: %w", err)
	}
	if compiler.metaSchema != nil {
		state := schemaEvaluationState(
			ctx,
			compiler.metaSchema.plan,
			DefaultLimits(),
		)
		valid, err := compiler.metaSchema.plan.evaluate(
			value,
			compiler.dialect,
			&state,
		)
		if err != nil {
			return nil, fmt.Errorf("validate schema: %w", err)
		}
		if !valid {
			return nil, fmt.Errorf(
				"%w: schema does not satisfy the %s meta-schema",
				ErrInvalidSchema,
				compiler.dialect,
			)
		}
	}

	schemaCompiler := newSchemaCompiler(
		ctx,
		value,
		compiler.dialect,
		compiler.limits,
		compiler.loader,
		len(raw),
		compiler.assertFormats,
		compiler.assertContent,
		compiler.formats,
		compiler.vocabularies,
	)
	if err := schemaCompiler.configureVocabularies(value); err != nil {
		return nil, err
	}
	plan, err := schemaCompiler.compile(value)
	if err != nil {
		return nil, err
	}

	return &Schema{
		dialect: compiler.dialect,
		limits:  compiler.limits,
		plan:    plan,
	}, nil
}

func schemaEvaluationState(
	ctx context.Context,
	plan *schemaPlan,
	limits Limits,
) evaluationState {
	state := evaluationState{ctx: ctx, limits: limits}
	if plan.resource != nil {
		state.dynamicScope = append(state.dynamicScope, plan.resource)
	}
	return state
}
