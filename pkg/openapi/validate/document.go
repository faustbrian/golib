package validate

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	canonical "github.com/faustbrian/golib/pkg/json-schema"
	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specification"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

// Document validates one selected OpenAPI description against the pinned
// official data-model schema for its specification line. Schema Objects are
// intentionally reported by the separate Schema source in later passes.
func Document(ctx context.Context, document openapi.Document) (Report, error) {
	return NewValidator().DocumentWithOptions(ctx, document, DefaultOptions())
}

// DocumentWithOptions validates with explicit bounded diagnostic policy.
func DocumentWithOptions(
	ctx context.Context,
	document openapi.Document,
	options Options,
) (Report, error) {
	return NewValidator().DocumentWithOptions(ctx, document, options)
}

// Validator owns compiled official data-model schemas for explicit reuse.
// A Validator is safe for concurrent use and starts no background work.
type Validator struct {
	mutex          sync.Mutex
	entries        map[specversion.Dialect]*documentSchemaEntry
	documentLimits canonical.Limits
	validateOutput func(
		*canonical.Schema, context.Context, []byte, canonical.OutputFormat,
	) (canonical.OutputUnit, error)
}

// NewValidator constructs an isolated validation cache.
func NewValidator() *Validator {
	return &Validator{
		entries:        make(map[specversion.Dialect]*documentSchemaEntry, 4),
		documentLimits: canonical.DefaultLimits(),
	}
}

// NewValidatorWithDocumentSchemaLimits constructs an isolated validation
// cache whose official OpenAPI data-model schemas use the supplied canonical
// JSON Schema compile and evaluation limits. Callers should start from
// canonical.DefaultLimits and change only justified bounds.
func NewValidatorWithDocumentSchemaLimits(
	limits canonical.Limits,
) (*Validator, error) {
	if _, err := canonical.NewCompiler(canonical.WithLimits(limits)); err != nil {
		return nil, fmt.Errorf("construct OpenAPI document validator: %w", err)
	}
	return &Validator{
		entries:        make(map[specversion.Dialect]*documentSchemaEntry, 4),
		documentLimits: limits,
	}, nil
}

// Document validates one description with default bounded options.
func (validator *Validator) Document(
	ctx context.Context,
	document openapi.Document,
) (Report, error) {
	return validator.DocumentWithOptions(ctx, document, DefaultOptions())
}

// DocumentWithOptions validates one description and reuses only schemas owned
// by this Validator.
func (validator *Validator) DocumentWithOptions(
	ctx context.Context,
	document openapi.Document,
	options Options,
) (Report, error) {
	if ctx == nil {
		return Report{}, fmt.Errorf("validate OpenAPI document: nil context")
	}
	if document == nil {
		return Report{}, fmt.Errorf("validate OpenAPI document: nil document")
	}
	if options.MaxDiagnostics < 1 {
		return Report{}, fmt.Errorf("validate OpenAPI document: diagnostic limit must be positive")
	}
	if options.MaxDocumentNodes < 0 || options.MaxDocumentDepth < 0 ||
		options.MaxReferences < 0 || options.MaxExternalExampleBytes < 0 ||
		options.ReferenceLimits.MaxTraversalDepth < 0 ||
		options.ReferenceLimits.MaxTraversalNodes < 0 ||
		options.ReferenceLimits.MaxReferenceDepth < 0 ||
		!validReferenceResourceURI(options.ReferenceResourceURI) {
		return Report{}, fmt.Errorf("validate OpenAPI document: invalid options")
	}
	if options.MaxReferences == 0 {
		options.MaxReferences = DefaultOptions().MaxReferences
	}
	if options.MaxDocumentNodes == 0 {
		options.MaxDocumentNodes = DefaultOptions().MaxDocumentNodes
	}
	if options.MaxDocumentDepth == 0 {
		options.MaxDocumentDepth = DefaultOptions().MaxDocumentDepth
	}
	if options.MaxExternalExampleBytes == 0 {
		options.MaxExternalExampleBytes =
			DefaultOptions().MaxExternalExampleBytes
	}
	options.ReferenceLimits = normalizedReferenceLimits(options.ReferenceLimits)
	options.ReferenceResolver = validationResolver(
		options.ReferenceResolver,
		document.SpecificationVersion().Dialect(),
	)
	if validator == nil {
		return Report{}, fmt.Errorf("validate OpenAPI document: nil validator")
	}
	if err := boundDocument(
		ctx, document.Raw(), options.MaxDocumentNodes, options.MaxDocumentDepth,
	); err != nil {
		return Report{}, err
	}
	schema, err := validator.documentSchema(
		ctx, document.SpecificationVersion().Dialect(),
	)
	if err != nil {
		return Report{}, err
	}
	rawDocument, err := document.Raw().MarshalJSON()
	if err != nil {
		return Report{}, fmt.Errorf("marshal OpenAPI document: %w", err)
	}
	validateOutput := validator.validateOutput
	if validateOutput == nil {
		validateOutput = func(
			schema *canonical.Schema,
			ctx context.Context,
			document []byte,
			format canonical.OutputFormat,
		) (canonical.OutputUnit, error) {
			return schema.ValidateOutput(ctx, document, format)
		}
	}
	output, err := validateOutput(schema, ctx, rawDocument, canonical.OutputBasic)
	if err != nil {
		return Report{}, fmt.Errorf("validate OpenAPI document: %w", err)
	}
	diagnostics := diagnostics(output, document.SpecificationVersion().String())
	referenceDiagnostics, err := validateReferenceTargets(ctx, document, options)
	if err != nil {
		return Report{}, err
	}
	diagnostics = append(diagnostics, validateRoot(document, options)...)
	diagnostics = append(diagnostics, validateMetadata(document)...)
	diagnostics = append(diagnostics, validateSwaggerTransport(document)...)
	diagnostics = append(diagnostics, validateComponentNames(document)...)
	diagnostics = append(diagnostics, validateExternalDocumentation(document)...)
	diagnostics = append(diagnostics, validateAdditionalOperations(document)...)
	diagnostics = append(diagnostics, validateResponses(document)...)
	diagnostics = append(diagnostics, validateRequestBodies(document)...)
	diagnostics = append(diagnostics, validatePaths(ctx, document, options)...)
	diagnostics = append(
		diagnostics,
		validateOperationIDs(ctx, document, options)...,
	)
	diagnostics = append(diagnostics, validateServers(document)...)
	diagnostics = append(diagnostics, validateParameters(ctx, document, options)...)
	diagnostics = append(diagnostics, validateHeaders(document)...)
	diagnostics = append(diagnostics, validateMediaTypes(ctx, document, options)...)
	diagnostics = append(diagnostics, validateExamples(ctx, document, options)...)
	diagnostics = append(diagnostics, validateSecurity(ctx, document, options)...)
	diagnostics = append(diagnostics, validateDeprecatedDeclarations(document)...)
	diagnostics = append(diagnostics, validateTags(document)...)
	diagnostics = append(diagnostics, validateLinks(ctx, document, options)...)
	diagnostics = append(diagnostics, referenceDiagnostics...)
	schemaDiagnostics, err := validateSchemas(ctx, document, options)
	if err != nil {
		return Report{}, err
	}
	diagnostics = append(diagnostics, schemaDiagnostics...)
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	diagnostics = pruneStructuralSummaries(diagnostics)
	if options.FailFast {
		diagnostics = diagnostics[:min(len(diagnostics), 1)]
	}
	diagnostics = diagnostics[:min(len(diagnostics), options.MaxDiagnostics)]
	return Report{diagnostics: diagnostics}, nil
}

type documentNode struct {
	value jsonvalue.Value
	depth int
}

func boundDocument(
	ctx context.Context,
	root jsonvalue.Value,
	maxNodes int,
	maxDepth int,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if maxNodes < 1 || maxDepth < 1 {
		return ErrLimitExceeded
	}
	remaining := maxNodes
	pending := []documentNode{{value: root, depth: 1}}
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		last := len(pending) - 1
		node := pending[last]
		pending = pending[:last]
		remaining--
		childCount, _ := node.value.Length()
		if !documentChildrenFit(
			childCount, len(pending), node.depth, remaining, maxDepth,
		) {
			return ErrLimitExceeded
		}
		switch node.value.Kind() {
		case jsonvalue.ArrayKind:
			children, _ := node.value.Elements()
			for _, child := range slices.Backward(children) {
				pending = append(pending, documentNode{
					value: child, depth: node.depth + 1,
				})
			}
		case jsonvalue.ObjectKind:
			children, _ := node.value.Members()
			for _, child := range slices.Backward(children) {
				pending = append(pending, documentNode{
					value: child.Value, depth: node.depth + 1,
				})
			}
		}
	}
	return nil
}

func documentChildrenFit(
	childCount int,
	queued int,
	depth int,
	remaining int,
	maxDepth int,
) bool {
	if childCount == 0 {
		return true
	}
	if depth >= maxDepth {
		return false
	}
	return childCount <= remaining-queued
}

type documentSchemaEntry struct {
	ready  chan struct{}
	schema *canonical.Schema
	err    error
}

func (validator *Validator) documentSchema(
	ctx context.Context,
	dialect specversion.Dialect,
) (*canonical.Schema, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	validator.mutex.Lock()
	if validator.entries == nil {
		validator.entries = make(map[specversion.Dialect]*documentSchemaEntry, 4)
	}
	entry, exists := validator.entries[dialect]
	if !exists {
		entry = &documentSchemaEntry{ready: make(chan struct{})}
		validator.entries[dialect] = entry
	}
	validator.mutex.Unlock()
	if !exists {
		entry.schema, entry.err = newDocumentSchema(
			ctx, dialect, validator.documentLimits,
		)
		if entry.err != nil {
			validator.mutex.Lock()
			if validator.entries[dialect] == entry {
				delete(validator.entries, dialect)
			}
			validator.mutex.Unlock()
		}
		close(entry.ready)
		return entry.schema, entry.err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-entry.ready:
		return entry.schema, entry.err
	}
}

func newDocumentSchema(
	ctx context.Context,
	dialect specversion.Dialect,
	limits canonical.Limits,
) (*canonical.Schema, error) {
	return newDocumentSchemaUsing(ctx, dialect, documentSchemaDependencies{
		limits: limits,
	})
}

type documentSchemaDependencies struct {
	read      func(string) ([]byte, error)
	construct func(...canonical.Option) (*canonical.Compiler, error)
	compile   func(*canonical.Compiler, context.Context, []byte) (*canonical.Schema, error)
	limits    canonical.Limits
}

func newDocumentSchemaUsing(
	ctx context.Context,
	dialect specversion.Dialect,
	dependencies documentSchemaDependencies,
) (*canonical.Schema, error) {
	resource, schemaDialect, err := schemaResource(dialect)
	if err != nil {
		return nil, err
	}
	read := dependencies.read
	if read == nil {
		read = specification.Read
	}
	rawSchema, err := read(resource)
	if err != nil {
		return nil, err
	}
	construct := dependencies.construct
	if construct == nil {
		construct = canonical.NewCompiler
	}
	limits := dependencies.limits
	if limits == (canonical.Limits{}) {
		limits = canonical.DefaultLimits()
	}
	compiler, err := construct(
		canonical.WithDialect(schemaDialect),
		canonical.WithResourceLoader(pinnedSchemaLoader{}),
		canonical.WithLimits(limits),
	)
	if err != nil {
		return nil, fmt.Errorf("construct document schema compiler: %w", err)
	}
	compile := dependencies.compile
	if compile == nil {
		compile = func(
			compiler *canonical.Compiler,
			ctx context.Context,
			schema []byte,
		) (*canonical.Schema, error) {
			return compiler.Compile(ctx, schema)
		}
	}
	schema, err := compile(compiler, ctx, rawSchema)
	if err != nil {
		return nil, fmt.Errorf("compile document schema: %w", err)
	}
	return schema, nil
}

type pinnedSchemaLoader struct{}

func (pinnedSchemaLoader) Load(_ context.Context, identifier string) ([]byte, error) {
	switch strings.TrimSuffix(identifier, "#") {
	case "http://json-schema.org/draft-04/schema":
		return specification.Read("schemas/json-schema/draft-04.json")
	default:
		return nil, fmt.Errorf(
			"%w: %s",
			canonical.ErrResourceUnavailable,
			identifier,
		)
	}
}

func schemaResource(dialect specversion.Dialect) (string, canonical.Dialect, error) {
	switch dialect {
	case specversion.DialectSwagger20:
		return "schemas/2.0/2017-08-27.json", canonical.Draft4, nil
	case specversion.DialectOAS30:
		return "schemas/3.0/2024-10-18.json", canonical.Draft4, nil
	case specversion.DialectOAS31:
		return "schemas/3.1/schema-2025-11-23.json", canonical.Draft202012, nil
	case specversion.DialectOAS32:
		return "schemas/3.2/schema-2025-11-23.json", canonical.Draft202012, nil
	default:
		return "", "", fmt.Errorf("unsupported OpenAPI document dialect %q", dialect)
	}
}

func diagnostics(output canonical.OutputUnit, version string) []Diagnostic {
	if output.Valid {
		return nil
	}
	units := output.Errors
	if len(units) == 0 {
		units = []canonical.OutputUnit{output}
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
			Code:                    "openapi.document." + keywordName(unit.KeywordLocation),
			Message:                 unit.Error,
			Severity:                SeverityError,
			Source:                  SourceDocument,
			InstanceLocation:        unit.InstanceLocation,
			KeywordLocation:         unit.KeywordLocation,
			AbsoluteKeywordLocation: unit.AbsoluteKeywordLocation,
			SpecificationVersion:    version,
			SpecificationSection:    "data-model",
		})
	}
	return result
}

func pruneStructuralSummaries(diagnostics []Diagnostic) []Diagnostic {
	result := make([]Diagnostic, 0, len(diagnostics))
	seen := make(map[string]struct{}, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if (diagnostic.Code == "openapi.document.anyOf" ||
			diagnostic.Code == "openapi.document.oneOf") &&
			hasSemanticDiagnosticAt(diagnostics, diagnostic.InstanceLocation) {
			continue
		}
		if (diagnostic.Code == "openapi.document.additionalProperties" ||
			diagnostic.Code == "openapi.document.unevaluatedProperties") &&
			hasDiagnosticCodeAt(
				diagnostics,
				diagnostic.InstanceLocation,
				"openapi.path.key.invalid",
			) {
			continue
		}
		if diagnostic.Code == "openapi.document.const" &&
			hasDiagnosticCodeAt(
				diagnostics,
				diagnostic.InstanceLocation,
				"openapi.path.parameter.not-required",
			) {
			continue
		}
		key := diagnostic.Code + "\x00" + diagnostic.InstanceLocation + "\x00" +
			diagnostic.KeywordLocation + "\x00" + diagnostic.Message
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, diagnostic)
	}
	return result
}

func hasDiagnosticCodeAt(
	diagnostics []Diagnostic,
	pointer string,
	code string,
) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code && diagnostic.InstanceLocation == pointer {
			return true
		}
	}
	return false
}

func hasSemanticDiagnosticAt(diagnostics []Diagnostic, pointer string) bool {
	for _, diagnostic := range diagnostics {
		if strings.HasPrefix(diagnostic.Code, "openapi.document.") {
			continue
		}
		if diagnostic.InstanceLocation == pointer ||
			strings.HasPrefix(diagnostic.InstanceLocation, pointer+"/") {
			return true
		}
	}
	return false
}

func hiddenApplicatorBranch(
	unit canonical.OutputUnit,
	units []canonical.OutputUnit,
) bool {
	for _, parent := range units {
		keyword := keywordName(parent.KeywordLocation)
		if keyword != "anyOf" && keyword != "oneOf" {
			continue
		}
		if strings.HasPrefix(unit.KeywordLocation, parent.KeywordLocation+"/") {
			return true
		}
	}
	return false
}

func keywordName(location string) string {
	index := strings.LastIndexByte(location, '/')
	if index < 0 {
		return "invalid"
	}
	keyword, found := strings.CutPrefix(location[index:], "/")
	if found && keyword != "" {
		return keyword
	}
	return "invalid"
}
