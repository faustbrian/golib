package jsonschema

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"mime"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type schemaPlan struct {
	boolean               *bool
	types                 []typePlan
	disallowedTypes       []typePlan
	properties            map[string]*schemaPlan
	patternProperties     []patternPropertyPlan
	additionalProperties  *schemaPlan
	propertyNames         *schemaPlan
	dependentRequired     map[string][]string
	dependentSchemas      map[string]*schemaPlan
	required              []string
	requiredByParent      bool
	hasEnum               bool
	enum                  []*jsonValue
	hasConst              bool
	constant              *jsonValue
	minimums              []numberBound
	maximums              []numberBound
	multipleOf            string
	minLength             *string
	maxLength             *string
	pattern               *ecmaPattern
	format                FormatChecker
	contentEncoding       string
	contentMediaType      string
	minItems              *string
	maxItems              *string
	minProperties         *string
	maxProperties         *string
	uniqueItems           bool
	prefixItems           []*schemaPlan
	items                 *schemaPlan
	contains              *schemaPlan
	minContains           *string
	maxContains           *string
	unevaluatedItems      *schemaPlan
	allOf                 []*schemaPlan
	anyOf                 []*schemaPlan
	oneOf                 []*schemaPlan
	not                   *schemaPlan
	condition             *schemaPlan
	then                  *schemaPlan
	otherwise             *schemaPlan
	unevaluatedProperties *schemaPlan
	reference             *schemaPlan
	referenceKeyword      string
	dynamicReference      string
	recursiveReference    bool
	resource              *schemaResource
	dialect               Dialect
	location              string
	absoluteBase          string
	annotations           map[string]*jsonValue
	custom                []compiledKeyword
	outputKeywords        []string
}

type schemaResource struct {
	root              *schemaPlan
	dynamicAnchors    map[string]*schemaPlan
	recursiveAnchored bool
	dialect           Dialect
}

type numberBound struct {
	number    string
	exclusive bool
}

type patternPropertyPlan struct {
	name    string
	pattern *ecmaPattern
	schema  *schemaPlan
}

type compiledKeyword struct {
	name      string
	evaluator KeywordEvaluator
}

type typePlan struct {
	name   string
	schema *schemaPlan
}

// Result is the minimum flag output from an evaluation.
type Result struct {
	Valid bool `json:"valid"`
}

type schemaCompiler struct {
	ctx                context.Context
	dialect            Dialect
	limits             Limits
	loader             ResourceLoader
	root               *jsonValue
	plans              map[*jsonValue]*schemaPlan
	bases              map[*jsonValue]*url.URL
	resources          map[string]*jsonValue
	anchors            map[string]*jsonValue
	dynamicAnchors     map[string]*jsonValue
	resourceFor        map[*jsonValue]string
	schemaResources    map[string]*schemaResource
	loadedResources    int
	loadedBytes        int
	assertFormats      bool
	assertContent      bool
	formats            map[string]FormatChecker
	locations          map[*jsonValue]string
	regexCount         int
	combinatorBranches int
	vocabularies       map[string]registeredVocabulary
	vocabularyPolicies map[string]vocabularyPolicy
	customCompiles     int
	indexError         error
}

type vocabularyPolicy struct {
	validation  bool
	applicator  bool
	unevaluated bool
	formats     bool
	keywords    map[string]KeywordCompiler
}

func newSchemaCompiler(
	ctx context.Context,
	root *jsonValue,
	dialect Dialect,
	limits Limits,
	loader ResourceLoader,
	rootBytes int,
	assertFormats bool,
	assertContent bool,
	formats map[string]FormatChecker,
	vocabularies map[string]registeredVocabulary,
) *schemaCompiler {
	compiler := &schemaCompiler{
		ctx:                ctx,
		dialect:            dialect,
		limits:             limits,
		loader:             loader,
		root:               root,
		plans:              make(map[*jsonValue]*schemaPlan),
		bases:              make(map[*jsonValue]*url.URL),
		resources:          make(map[string]*jsonValue),
		anchors:            make(map[string]*jsonValue),
		dynamicAnchors:     make(map[string]*jsonValue),
		resourceFor:        make(map[*jsonValue]string),
		schemaResources:    make(map[string]*schemaResource),
		loadedResources:    1,
		loadedBytes:        rootBytes,
		assertFormats:      assertFormats,
		assertContent:      assertContent,
		formats:            formats,
		locations:          make(map[*jsonValue]string),
		vocabularies:       cloneVocabularies(vocabularies),
		vocabularyPolicies: make(map[string]vocabularyPolicy),
	}
	compiler.indexLocations(root, "")
	compiler.indexResources(root, &url.URL{}, "", dialect)

	return compiler
}

func (compiler *schemaCompiler) configureVocabularies(root *jsonValue) error {
	if compiler.indexError != nil {
		return compiler.indexError
	}
	_, err := compiler.vocabularyPolicy(root)
	return err
}

func (compiler *schemaCompiler) vocabularyPolicy(value *jsonValue) (vocabularyPolicy, error) {
	resourceIdentifier := compiler.resourceFor[value]
	if cached, exists := compiler.vocabularyPolicies[resourceIdentifier]; exists {
		return cached, nil
	}
	dialect := compiler.dialectFor(value)
	policy := vocabularyPolicy{
		validation:  true,
		applicator:  true,
		unevaluated: true,
		formats:     compiler.assertFormats,
		keywords:    make(map[string]KeywordCompiler),
	}
	if dialect != Draft201909 && dialect != Draft202012 {
		for _, vocabulary := range compiler.vocabularies {
			for name, keywordCompiler := range vocabulary.keywords {
				policy.keywords[name] = keywordCompiler
			}
		}
		compiler.vocabularyPolicies[resourceIdentifier] = policy
		return policy, nil
	}
	root := compiler.resources[resourceIdentifier]
	if root == nil || root.kind != kindObject {
		compiler.vocabularyPolicies[resourceIdentifier] = policy
		return policy, nil
	}
	declared, exists := root.object["$schema"]
	if !exists || declared.kind != kindString || declared.text == string(dialect) {
		compiler.vocabularyPolicies[resourceIdentifier] = policy
		return policy, nil
	}
	metaSchema, err := compiler.resolveReference(root, declared.text)
	if err != nil {
		return vocabularyPolicy{}, err
	}
	if metaSchema.kind != kindObject {
		return vocabularyPolicy{}, fmt.Errorf("%w: meta-schema must be an object", ErrInvalidSchema)
	}
	declaredVocabularies, exists := metaSchema.object["$vocabulary"]
	if !exists {
		compiler.vocabularyPolicies[resourceIdentifier] = policy
		return policy, nil
	}
	if declaredVocabularies.kind != kindObject {
		return vocabularyPolicy{}, fmt.Errorf("%w: $vocabulary must be an object", ErrInvalidSchema)
	}
	policy.validation = false
	policy.applicator = false
	policy.unevaluated = false
	for identifier, required := range declaredVocabularies.object {
		if required.kind != kindBoolean {
			return vocabularyPolicy{}, fmt.Errorf(
				"%w: vocabulary requirement %q must be boolean",
				ErrInvalidSchema,
				identifier,
			)
		}
		kind := compiler.knownVocabulary(identifier, dialect)
		if kind == "" && required.boolean {
			return vocabularyPolicy{}, fmt.Errorf("%w: %q", ErrUnsupportedVocabulary, identifier)
		}
		switch kind {
		case "validation":
			policy.validation = true
		case "applicator":
			policy.applicator = true
		case "unevaluated":
			policy.unevaluated = true
		case "format-assertion":
			policy.formats = true
		case "custom":
			for name, keywordCompiler := range compiler.vocabularies[identifier].keywords {
				policy.keywords[name] = keywordCompiler
			}
		}
	}
	policy = applyVocabularyDefaults(policy, dialect)
	compiler.vocabularyPolicies[resourceIdentifier] = policy
	return policy, nil
}

func (compiler *schemaCompiler) knownVocabulary(identifier string, dialect Dialect) string {
	if _, exists := compiler.vocabularies[identifier]; exists {
		return "custom"
	}
	prefix := "https://json-schema.org/draft/2019-09/vocab/"
	if dialect == Draft202012 {
		prefix = "https://json-schema.org/draft/2020-12/vocab/"
	}
	if !strings.HasPrefix(identifier, prefix) {
		return ""
	}
	switch strings.TrimPrefix(identifier, prefix) {
	case "core", "meta-data", "format", "format-annotation", "content":
		return "known"
	case "format-assertion":
		return "format-assertion"
	case "validation":
		return "validation"
	case "applicator":
		return "applicator"
	case "unevaluated":
		return "unevaluated"
	default:
		return ""
	}
}

func (compiler *schemaCompiler) compile(value *jsonValue) (*schemaPlan, error) {
	if compiler.indexError != nil {
		return nil, compiler.indexError
	}
	if existing, exists := compiler.plans[value]; exists {
		return existing, nil
	}
	if len(compiler.plans) >= compiler.limits.MaxSchemaNodes {
		return nil, &LimitError{
			Resource: "schema nodes",
			Limit:    compiler.limits.MaxSchemaNodes,
		}
	}
	dialect := compiler.dialectFor(value)

	if value.kind == kindBoolean {
		if !dialect.supportsBooleanSchemas() {
			return nil, fmt.Errorf(
				"%w: boolean schemas are not supported by %s",
				ErrInvalidSchema,
				dialect,
			)
		}

		boolean := value.boolean
		plan := &schemaPlan{
			boolean:      &boolean,
			resource:     compiler.schemaResource(compiler.resourceFor[value]),
			dialect:      dialect,
			location:     compiler.locations[value],
			absoluteBase: compiler.resourceFor[value],
		}
		compiler.plans[value] = plan
		if compiler.resources[compiler.resourceFor[value]] == value {
			plan.resource.root = plan
		}

		return plan, nil
	}
	if value.kind != kindObject {
		return nil, fmt.Errorf("%w: schema must be an object", ErrInvalidSchema)
	}
	policy, err := compiler.vocabularyPolicy(value)
	if err != nil {
		return nil, err
	}

	plan := &schemaPlan{
		resource:       compiler.schemaResource(compiler.resourceFor[value]),
		dialect:        dialect,
		location:       compiler.locations[value],
		absoluteBase:   compiler.resourceFor[value],
		annotations:    make(map[string]*jsonValue),
		outputKeywords: standardOutputKeywords(value.object, dialect),
	}
	for _, keyword := range sortedStringKeys(value.object) {
		annotation := value.object[keyword]
		_, custom := policy.keywords[keyword]
		if isAnnotationKeyword(keyword) || !isKnownKeyword(keyword) && !custom {
			plan.annotations[keyword] = annotation
		}
	}
	compiler.plans[value] = plan
	if compiler.resources[compiler.resourceFor[value]] == value {
		plan.resource.root = plan
	}
	if anchor, exists := value.object["$dynamicAnchor"]; dialect == Draft202012 && exists && anchor.kind == kindString {
		plan.resource.dynamicAnchors[anchor.text] = plan
	}
	if referenceValue, exists := value.object["$ref"]; exists {
		if referenceValue.kind != kindString {
			return nil, fmt.Errorf("%w: $ref must be a string", ErrInvalidSchema)
		}
		target, err := compiler.resolveReference(value, referenceValue.text)
		if err != nil {
			return nil, err
		}
		plan.reference, err = compiler.compile(target)
		if err != nil {
			return nil, err
		}
		plan.referenceKeyword = "$ref"
		if dialect.referenceReplacesSiblings() {
			return plan, nil
		}
	}
	if referenceValue, exists := value.object["$recursiveRef"]; exists && dialect == Draft201909 {
		if referenceValue.kind != kindString {
			return nil, fmt.Errorf("%w: $recursiveRef must be a string", ErrInvalidSchema)
		}
		target, dynamic, err := compiler.resolveRecursiveReference(value, referenceValue.text)
		if err != nil {
			return nil, err
		}
		plan.reference, err = compiler.compile(target)
		if err != nil {
			return nil, err
		}
		plan.recursiveReference = dynamic
		plan.referenceKeyword = "$recursiveRef"
	}
	if referenceValue, exists := value.object["$dynamicRef"]; exists && dialect == Draft202012 {
		if referenceValue.kind != kindString {
			return nil, fmt.Errorf("%w: $dynamicRef must be a string", ErrInvalidSchema)
		}
		target, dynamic, err := compiler.resolveDynamicReference(value, referenceValue.text)
		if err != nil {
			return nil, err
		}
		plan.reference, err = compiler.compile(target)
		if err != nil {
			return nil, err
		}
		plan.dynamicReference = dynamic
		plan.referenceKeyword = "$dynamicRef"
		if dynamic != "" {
			if err := compiler.compileDynamicAnchors(dynamic); err != nil {
				return nil, err
			}
		}
	}

	if policy.validation {
		typeValue, exists := value.object["type"]
		if exists {
			types, err := compileTypes(typeValue, compiler, dialect)
			if err != nil {
				return nil, err
			}
			plan.types = types
		}
		if disallow, exists := value.object["disallow"]; exists && dialect == Draft3 {
			types, err := compileTypes(disallow, compiler, dialect)
			if err != nil {
				return nil, fmt.Errorf("%w: disallow: %w", ErrInvalidSchema, err)
			}
			plan.disallowedTypes = types
		}

		if enumValue, exists := value.object["enum"]; exists {
			if enumValue.kind != kindArray {
				return nil, fmt.Errorf("%w: enum must be an array", ErrInvalidSchema)
			}
			plan.hasEnum = true
			plan.enum = append([]*jsonValue(nil), enumValue.array...)
		}

		if constValue, exists := value.object["const"]; exists && dialect != Draft3 && dialect != Draft4 {
			plan.hasConst = true
			plan.constant = constValue
		}

		if requiredValue, exists := value.object["required"]; exists {
			if dialect == Draft3 {
				if requiredValue.kind != kindBoolean {
					return nil, fmt.Errorf("%w: Draft 3 required must be a boolean", ErrInvalidSchema)
				}
				plan.requiredByParent = requiredValue.boolean
			} else {
				required, err := compileRequired(requiredValue, dialect)
				if err != nil {
					return nil, err
				}
				plan.required = required
			}
		}

		if err := compileNumberKeywords(plan, value.object, dialect); err != nil {
			return nil, err
		}
		if err := compileCardinalityKeywords(plan, value.object, dialect); err != nil {
			return nil, err
		}
		if pattern, exists := value.object["pattern"]; exists {
			if pattern.kind != kindString {
				return nil, fmt.Errorf("%w: pattern must be a string", ErrInvalidSchema)
			}
			compiled, err := compiler.compilePattern(pattern.text)
			if err != nil {
				return nil, fmt.Errorf("%w: pattern: %w", ErrInvalidSchema, err)
			}
			plan.pattern = compiled
		}
	}
	if format, exists := value.object["format"]; exists {
		if format.kind != kindString {
			return nil, fmt.Errorf("%w: format must be a string", ErrInvalidSchema)
		}
		if policy.formats {
			plan.format = compiler.formats[format.text]
			_, custom := plan.format.(customFormatChecker)
			if !custom && !standardFormatSupported(plan.dialect, format.text) {
				plan.format = nil
			}
			if !custom && format.text == "time" && plan.dialect == Draft3 {
				plan.format = simpleFormatFunc(validLegacyTime)
			}
		}
	}
	if contentKeywordsSupported(dialect) {
		if encoding, exists := value.object["contentEncoding"]; exists {
			if encoding.kind != kindString {
				return nil, fmt.Errorf(
					"%w: contentEncoding must be a string",
					ErrInvalidSchema,
				)
			}
			if compiler.assertContent {
				plan.contentEncoding = strings.ToLower(encoding.text)
			}
		}
		if mediaType, exists := value.object["contentMediaType"]; exists {
			if mediaType.kind != kindString {
				return nil, fmt.Errorf(
					"%w: contentMediaType must be a string",
					ErrInvalidSchema,
				)
			}
			if compiler.assertContent {
				plan.contentMediaType = mediaType.text
			}
		}
	}
	if policy.applicator || policy.validation {
		if err := compileArrayKeywords(plan, value.object, compiler, policy); err != nil {
			return nil, err
		}
	}
	if policy.applicator {
		if err := compileApplicatorKeywords(plan, value.object, compiler); err != nil {
			return nil, err
		}
	}
	if (dialect == Draft201909 || dialect == Draft202012) &&
		policy.unevaluated {
		if unevaluated, exists := value.object["unevaluatedItems"]; exists {
			compiled, err := compileKeywordSchema(unevaluated, compiler)
			if err != nil {
				return nil, fmt.Errorf(
					"%w: unevaluatedItems: %w",
					ErrInvalidSchema,
					err,
				)
			}
			plan.unevaluatedItems = compiled
		}
		if unevaluated, exists := value.object["unevaluatedProperties"]; exists {
			compiled, err := compileKeywordSchema(unevaluated, compiler)
			if err != nil {
				return nil, fmt.Errorf(
					"%w: unevaluatedProperties: %w",
					ErrInvalidSchema,
					err,
				)
			}
			plan.unevaluatedProperties = compiled
		}
	}
	if (dialect == Draft201909 || dialect == Draft202012) &&
		policy.validation {
		if dependencies, exists := value.object["dependentRequired"]; exists {
			required, err := compileDependentRequired(dependencies, false)
			if err != nil {
				return nil, err
			}
			plan.dependentRequired = required
		}
	}
	if !policy.applicator {
		return plan, nil
	}

	propertiesValue, exists := value.object["properties"]
	if exists {
		if propertiesValue.kind != kindObject {
			return nil, fmt.Errorf("%w: properties must be an object", ErrInvalidSchema)
		}

		plan.properties = make(map[string]*schemaPlan, len(propertiesValue.object))
		for name, propertyValue := range propertiesValue.object {
			property, err := compiler.compile(propertyValue)
			if err != nil {
				return nil, fmt.Errorf("%w: property %q: %w", ErrInvalidSchema, name, err)
			}
			plan.properties[name] = property
		}
	}
	if patterns, exists := value.object["patternProperties"]; exists {
		if patterns.kind != kindObject {
			return nil, fmt.Errorf("%w: patternProperties must be an object", ErrInvalidSchema)
		}
		names := make([]string, 0, len(patterns.object))
		for name := range patterns.object {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			pattern, err := compiler.compilePattern(name)
			if err != nil {
				return nil, fmt.Errorf(
					"%w: patternProperties %q: %w",
					ErrInvalidSchema,
					name,
					err,
				)
			}
			schema, err := compileKeywordSchema(patterns.object[name], compiler)
			if err != nil {
				return nil, fmt.Errorf(
					"%w: patternProperties %q: %w",
					ErrInvalidSchema,
					name,
					err,
				)
			}
			plan.patternProperties = append(
				plan.patternProperties,
				patternPropertyPlan{name: name, pattern: pattern, schema: schema},
			)
		}
	}
	if additional, exists := value.object["additionalProperties"]; exists {
		compiled, err := compileKeywordSchema(additional, compiler)
		if err != nil {
			return nil, fmt.Errorf("%w: additionalProperties: %w", ErrInvalidSchema, err)
		}
		plan.additionalProperties = compiled
	}
	if propertyNames, exists := value.object["propertyNames"]; exists && dialect != Draft3 && dialect != Draft4 {
		compiled, err := compileKeywordSchema(propertyNames, compiler)
		if err != nil {
			return nil, fmt.Errorf("%w: propertyNames: %w", ErrInvalidSchema, err)
		}
		plan.propertyNames = compiled
	}
	if dialect == Draft201909 || dialect == Draft202012 {
		if dependencies, exists := value.object["dependentSchemas"]; exists {
			schemas, err := compileDependentSchemas(dependencies, compiler)
			if err != nil {
				return nil, err
			}
			plan.dependentSchemas = schemas
		}
	}
	if dependencies, exists := value.object["dependencies"]; exists {
		if dependencies.kind != kindObject {
			return nil, fmt.Errorf("%w: dependencies must be an object", ErrInvalidSchema)
		}
		plan.dependentRequired = make(map[string][]string)
		plan.dependentSchemas = make(map[string]*schemaPlan)
		for name, dependency := range dependencies.object {
			if dependency.kind == kindArray || dialect == Draft3 && dependency.kind == kindString {
				required, err := compileDependencyNames(dependency, dialect == Draft3)
				if err != nil {
					return nil, fmt.Errorf("%w: dependency %q: %w", ErrInvalidSchema, name, err)
				}
				plan.dependentRequired[name] = required
				continue
			}
			compiled, err := compileKeywordSchema(dependency, compiler)
			if err != nil {
				return nil, fmt.Errorf("%w: dependency %q: %w", ErrInvalidSchema, name, err)
			}
			plan.dependentSchemas[name] = compiled
		}
	}
	for _, name := range sortedStringKeys(policy.keywords) {
		keywordValue, exists := value.object[name]
		if !exists {
			continue
		}
		compiler.customCompiles++
		if compiler.customCompiles > compiler.limits.MaxCustomKeywordCompiles {
			return nil, &LimitError{
				Resource: "custom keyword compiles",
				Limit:    compiler.limits.MaxCustomKeywordCompiles,
			}
		}
		evaluator, err := callKeywordCompiler(
			compiler.ctx,
			policy.keywords[name],
			dialect,
			Value{value: keywordValue},
		)
		if err != nil {
			return nil, fmt.Errorf("%w: custom keyword %q: %w", ErrInvalidSchema, name, err)
		}
		if interfaceIsNil(evaluator) {
			return nil, fmt.Errorf("%w: custom keyword %q returned nil", ErrInvalidSchema, name)
		}
		plan.custom = append(plan.custom, compiledKeyword{name: name, evaluator: evaluator})
	}

	return plan, nil
}

func compileDependentRequired(value *jsonValue, allowString bool) (map[string][]string, error) {
	if value.kind != kindObject {
		return nil, fmt.Errorf("%w: dependentRequired must be an object", ErrInvalidSchema)
	}
	result := make(map[string][]string, len(value.object))
	for name, dependency := range value.object {
		required, err := compileDependencyNames(dependency, allowString)
		if err != nil {
			return nil, fmt.Errorf("%w: dependentRequired %q: %w", ErrInvalidSchema, name, err)
		}
		result[name] = required
	}

	return result, nil
}

func (compiler *schemaCompiler) compilePattern(pattern string) (*ecmaPattern, error) {
	if len(pattern) > compiler.limits.MaxRegexBytes {
		return nil, &LimitError{
			Resource: "regular expression bytes",
			Limit:    compiler.limits.MaxRegexBytes,
		}
	}
	compiler.regexCount++
	if compiler.regexCount > compiler.limits.MaxRegexCount {
		return nil, &LimitError{
			Resource: "regular expression count",
			Limit:    compiler.limits.MaxRegexCount,
		}
	}
	return compilePatternWithLimits(pattern, compiler.limits)
}

func isASCIIAlpha(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func compileDependencyNames(value *jsonValue, allowString bool) ([]string, error) {
	if allowString && value.kind == kindString {
		return []string{value.text}, nil
	}
	if value.kind != kindArray {
		return nil, fmt.Errorf("dependency must be an array of strings")
	}
	result := make([]string, 0, len(value.array))
	seen := make(map[string]struct{}, len(value.array))
	for _, item := range value.array {
		if item.kind != kindString {
			return nil, fmt.Errorf("dependency entries must be strings")
		}
		if _, duplicate := seen[item.text]; duplicate {
			return nil, fmt.Errorf("duplicate dependency %q", item.text)
		}
		seen[item.text] = struct{}{}
		result = append(result, item.text)
	}

	return result, nil
}

func compileDependentSchemas(value *jsonValue, compiler *schemaCompiler) (map[string]*schemaPlan, error) {
	if value.kind != kindObject {
		return nil, fmt.Errorf("%w: dependentSchemas must be an object", ErrInvalidSchema)
	}
	result := make(map[string]*schemaPlan, len(value.object))
	for name, dependency := range value.object {
		compiled, err := compileKeywordSchema(dependency, compiler)
		if err != nil {
			return nil, fmt.Errorf("%w: dependentSchemas %q: %w", ErrInvalidSchema, name, err)
		}
		result[name] = compiled
	}

	return result, nil
}

func (
	compiler *schemaCompiler,
) indexResources(
	value *jsonValue,
	inheritedBase *url.URL,
	resource string,
	inheritedDialect Dialect,
) {
	if compiler.indexError != nil {
		return
	}
	base := inheritedBase
	dialect := inheritedDialect
	if value.kind == kindObject {
		if declared, exists := value.object["$schema"]; exists && declared.kind == kindString {
			if selected, recognized := dialectFromIdentifier(declared.text); recognized {
				dialect = selected
			}
		}
		identifierName := "$id"
		if dialect == Draft3 || dialect == Draft4 {
			identifierName = "id"
		}
		_, hasReplacingReference := value.object["$ref"]
		if identifier, exists := value.object[identifierName]; exists && identifier.kind == kindString &&
			(!hasReplacingReference || !dialect.referenceReplacesSiblings()) {
			parsed, err := url.Parse(identifier.text)
			if err != nil {
				compiler.indexError = fmt.Errorf(
					"%w: invalid %s resource identifier",
					ErrInvalidSchema,
					identifierName,
				)
				return
			}
			resolved, err := normalizeURL(inheritedBase.ResolveReference(parsed))
			if err != nil {
				compiler.indexError = fmt.Errorf(
					"%w: invalid normalized resource identifier",
					ErrInvalidSchema,
				)
				return
			}
			base = resolved
			resourceURL := *resolved
			fragment := resourceURL.EscapedFragment()
			resourceURL.Fragment = ""
			resourceURL.RawFragment = ""
			if fragment != "" {
				compiler.registerAnchor(resourceURL.String()+"#"+fragment, value)
			} else {
				resource = resourceURL.String()
				compiler.registerResource(resource, value)
			}
		}
	}
	if resource == "" {
		compiler.registerResource("", compiler.root)
	}
	if compiler.indexError != nil {
		return
	}
	compiler.bases[value] = base
	compiler.resourceFor[value] = resource
	indexedResource := compiler.schemaResource(resource)
	if resourceDialectShouldUpdate(
		indexedResource.dialect,
		compiler.resources[resource],
		value,
	) {
		indexedResource.dialect = dialect
	}
	if value.kind == kindObject {
		staticAnchor, hasStaticAnchor := value.object["$anchor"]
		dynamicAnchor, hasDynamicAnchor := value.object["$dynamicAnchor"]
		if dialect == Draft202012 && hasStaticAnchor && hasDynamicAnchor &&
			staticAnchor.kind == kindString && dynamicAnchor.kind == kindString &&
			staticAnchor.text == dynamicAnchor.text {
			compiler.indexError = fmt.Errorf(
				"%w: duplicate anchor identifier",
				ErrInvalidSchema,
			)
			return
		}
		if anchor, exists := value.object["$anchor"]; (dialect == Draft201909 || dialect == Draft202012) &&
			exists && anchor.kind == kindString {
			compiler.registerAnchor(resource+"#"+anchor.text, value)
		}
		if anchor, exists := value.object["$dynamicAnchor"]; dialect == Draft202012 && exists && anchor.kind == kindString {
			compiler.registerAnchor(resource+"#"+anchor.text, value)
			if compiler.indexError != nil {
				return
			}
			compiler.dynamicAnchors[resource+"#"+anchor.text] = value
		}
		if anchor, exists := value.object["$recursiveAnchor"]; dialect == Draft201909 && exists &&
			anchor.kind == kindBoolean && anchor.boolean {
			compiler.schemaResource(resource).recursiveAnchored = true
		}
		for _, child := range compiler.schemaChildren(value.object, dialect) {
			compiler.indexResources(child, base, resource, dialect)
		}
	}
}

func (compiler *schemaCompiler) registerResource(identifier string, value *jsonValue) {
	if existing, exists := compiler.resources[identifier]; exists && existing != value {
		compiler.indexError = fmt.Errorf(
			"%w: duplicate resource identifier",
			ErrInvalidSchema,
		)
		return
	}
	compiler.resources[identifier] = value
}

func (compiler *schemaCompiler) registerAnchor(identifier string, value *jsonValue) {
	if existing, exists := compiler.anchors[identifier]; exists && existing != value {
		compiler.indexError = fmt.Errorf(
			"%w: duplicate anchor identifier",
			ErrInvalidSchema,
		)
		return
	}
	compiler.anchors[identifier] = value
}

func (compiler *schemaCompiler) indexLocations(value *jsonValue, location string) {
	compiler.locations[value] = location
	switch value.kind {
	case kindArray:
		for index, child := range value.array {
			compiler.indexLocations(
				child,
				location+"/"+strconv.Itoa(index),
			)
		}
	case kindObject:
		for _, name := range sortedStringKeys(value.object) {
			compiler.indexLocations(
				value.object[name],
				location+"/"+escapePointerToken(name),
			)
		}
	}
}

func (
	compiler *schemaCompiler,
) schemaChildren(object map[string]*jsonValue, dialect Dialect) []*jsonValue {
	children := make([]*jsonValue, 0)
	appendMap := func(name string) {
		value, exists := object[name]
		if !exists || value.kind != kindObject {
			return
		}
		for _, childName := range sortedStringKeys(value.object) {
			children = append(children, value.object[childName])
		}
	}
	appendSchema := func(name string) {
		if value, exists := object[name]; exists {
			children = append(children, value)
		}
	}
	appendArray := func(name string) {
		value, exists := object[name]
		if !exists || value.kind != kindArray {
			return
		}
		children = append(children, value.array...)
	}

	appendMap("definitions")
	if unevaluatedKeywordsSupported(dialect) {
		appendMap("$defs")
	}
	for _, name := range []string{"properties", "patternProperties"} {
		appendMap(name)
	}
	for _, name := range []string{
		"additionalProperties",
		"additionalItems",
		"contains",
		"not",
	} {
		appendSchema(name)
	}
	if items, exists := object["items"]; exists {
		if items.kind == kindArray {
			children = append(children, items.array...)
		} else {
			children = append(children, items)
		}
	}
	for _, name := range []string{"allOf", "anyOf", "oneOf"} {
		appendArray(name)
	}
	if dialect == Draft3 {
		if extends, exists := object["extends"]; exists {
			if extends.kind == kindArray {
				children = append(children, extends.array...)
			} else {
				children = append(children, extends)
			}
		}
		for _, name := range []string{"type", "disallow"} {
			if types, exists := object[name]; exists && types.kind == kindArray {
				for _, child := range types.array {
					if child.kind == kindObject {
						children = append(children, child)
					}
				}
			}
		}
	}
	if dialect == Draft7 || dialect == Draft201909 || dialect == Draft202012 {
		for _, name := range []string{"if", "then", "else"} {
			appendSchema(name)
		}
	}
	if dialect != Draft3 && dialect != Draft4 {
		appendSchema("propertyNames")
	}
	if dialect == Draft202012 {
		appendArray("prefixItems")
	}
	if unevaluatedKeywordsSupported(dialect) {
		appendMap("dependentSchemas")
		for _, name := range []string{
			"unevaluatedItems",
			"unevaluatedProperties",
			"contentSchema",
		} {
			appendSchema(name)
		}
	}
	if dependencies, exists := object["dependencies"]; exists && dependencies.kind == kindObject {
		for _, name := range sortedStringKeys(dependencies.object) {
			dependency := dependencies.object[name]
			if dependency.kind == kindObject || dependency.kind == kindBoolean {
				children = append(children, dependency)
			}
		}
	}

	return children
}

func applyVocabularyDefaults(policy vocabularyPolicy, dialect Dialect) vocabularyPolicy {
	if dialect == Draft201909 {
		policy.unevaluated = policy.applicator
	}
	return policy
}

func contentKeywordsSupported(dialect Dialect) bool {
	switch dialect {
	case Draft7, Draft201909, Draft202012:
		return true
	default:
		return false
	}
}

func unevaluatedKeywordsSupported(dialect Dialect) bool {
	switch dialect {
	case Draft201909, Draft202012:
		return true
	default:
		return false
	}
}

func resourceDialectShouldUpdate(
	current Dialect,
	resourceRoot *jsonValue,
	value *jsonValue,
) bool {
	return current == "" || resourceRoot == value
}

func dialectFromIdentifier(identifier string) (Dialect, bool) {
	for _, dialect := range []Dialect{
		Draft3,
		Draft4,
		Draft6,
		Draft7,
		Draft201909,
		Draft202012,
	} {
		if identifier == string(dialect) {
			return dialect, true
		}
	}

	return "", false
}

func (compiler *schemaCompiler) schemaResource(identifier string) *schemaResource {
	resource, exists := compiler.schemaResources[identifier]
	if !exists {
		resource = &schemaResource{dynamicAnchors: make(map[string]*schemaPlan)}
		compiler.schemaResources[identifier] = resource
	}

	return resource
}

func (compiler *schemaCompiler) dialectFor(value *jsonValue) Dialect {
	resource := compiler.schemaResource(compiler.resourceFor[value])
	if resource.dialect != "" {
		return resource.dialect
	}

	return compiler.dialect
}

func (
	compiler *schemaCompiler,
) resolveReference(source *jsonValue, reference string) (*jsonValue, error) {
	parsed, err := url.Parse(reference)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid reference %q", ErrInvalidSchema, reference)
	}
	sourceBase := compiler.bases[source]
	if sourceBase == nil {
		return nil, fmt.Errorf("%w: reference source has no base URI", ErrInvalidSchema)
	}
	resolved, err := normalizeURL(sourceBase.ResolveReference(parsed))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid normalized reference", ErrInvalidSchema)
	}
	resourceURL := *resolved
	fragment := resourceURL.EscapedFragment()
	resourceURL.Fragment = ""
	resourceURL.RawFragment = ""
	resource := resourceURL.String()
	root, exists := compiler.resources[resource]
	if !exists {
		loaded, err := compiler.loadResource(resource)
		if err != nil {
			return nil, err
		}
		root = loaded
	}
	if fragment == "" {
		return root, nil
	}
	if !strings.HasPrefix(fragment, "/") {
		target, exists := compiler.anchors[resource+"#"+fragment]
		if !exists {
			return nil, fmt.Errorf("%w: unresolved anchor %q", ErrInvalidSchema, reference)
		}
		return target, nil
	}

	// EscapedFragment guarantees valid percent encoding.
	pointer, _ := url.PathUnescape(fragment)
	current := root
	for _, encodedToken := range strings.Split(pointer[1:], "/") {
		token := strings.ReplaceAll(encodedToken, "~1", "/")
		token = strings.ReplaceAll(token, "~0", "~")
		switch current.kind {
		case kindObject:
			child, exists := current.object[token]
			if !exists {
				return nil, fmt.Errorf(
					"%w: unresolved local $ref %q",
					ErrInvalidSchema,
					reference,
				)
			}
			current = child
		case kindArray:
			if token == "" || token != "0" && strings.HasPrefix(token, "0") {
				return nil, fmt.Errorf("%w: invalid array index in $ref %q", ErrInvalidSchema, reference)
			}
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(current.array) {
				return nil, fmt.Errorf("%w: unresolved local $ref %q", ErrInvalidSchema, reference)
			}
			current = current.array[index]
		default:
			return nil, fmt.Errorf("%w: unresolved local $ref %q", ErrInvalidSchema, reference)
		}
	}

	if _, indexed := compiler.bases[current]; !indexed {
		rootBase := compiler.bases[root]
		if rootBase == nil {
			rootBase = &url.URL{}
		}
		compiler.indexResources(
			current,
			rootBase,
			resource,
			compiler.dialectFor(root),
		)
		if compiler.indexError != nil {
			return nil, compiler.indexError
		}
	}

	return current, nil
}

func (compiler *schemaCompiler) loadResource(identifier string) (*jsonValue, error) {
	if compiler.loadedResources >= compiler.limits.MaxSchemaResources {
		return nil, &LimitError{
			Resource: "schema resources",
			Limit:    compiler.limits.MaxSchemaResources,
		}
	}
	raw, bundled, err := loadBundledOfficialMetaSchema(compiler.ctx, identifier)
	if !bundled {
		if compiler.loader == nil {
			return nil, fmt.Errorf(
				"%w: no loader configured for %q",
				ErrResourceUnavailable,
				safeResourceIdentifier(identifier),
			)
		}
		raw, err = callResourceLoader(compiler.ctx, compiler.loader, identifier)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: %q: %w",
				ErrResourceUnavailable,
				safeResourceIdentifier(identifier),
				err,
			)
		}
	} else if err != nil {
		return nil, fmt.Errorf(
			"%w: bundled official meta-schema: %w",
			ErrResourceUnavailable,
			err,
		)
	}
	remainingBytes := compiler.limits.MaxTotalSchemaBytes - compiler.loadedBytes
	if remainingBytes < 0 || len(raw) > remainingBytes {
		return nil, &LimitError{
			Resource: "total schema bytes",
			Limit:    compiler.limits.MaxTotalSchemaBytes,
		}
	}
	value, err := decodeJSON(compiler.ctx, raw, compiler.limits)
	if err != nil {
		return nil, fmt.Errorf("parse schema resource %q: %w", identifier, err)
	}
	compiler.indexLocations(value, "")
	base, err := normalizeResourceURL(identifier)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid resource identifier %q",
			ErrInvalidSchema,
			safeResourceIdentifier(identifier),
		)
	}
	compiler.resources[identifier] = value
	compiler.loadedResources++
	compiler.loadedBytes += len(raw)
	compiler.indexResources(value, base, identifier, compiler.dialect)
	if compiler.indexError != nil {
		return nil, compiler.indexError
	}
	compiler.resources[identifier] = value

	return value, nil
}

func (
	compiler *schemaCompiler,
) resolveRecursiveReference(
	source *jsonValue,
	reference string,
) (*jsonValue, bool, error) {
	target, err := compiler.resolveReference(source, reference)
	if err != nil {
		return nil, false, err
	}
	dynamic := reference == "#" &&
		compiler.schemaResource(compiler.resourceFor[target]).recursiveAnchored

	return target, dynamic, nil
}

func (
	compiler *schemaCompiler,
) resolveDynamicReference(
	source *jsonValue,
	reference string,
) (*jsonValue, string, error) {
	target, err := compiler.resolveReference(source, reference)
	if err != nil {
		return nil, "", err
	}
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Fragment == "" {
		return target, "", nil
	}
	resource := compiler.resourceFor[target]
	if compiler.dynamicAnchors[resource+"#"+parsed.Fragment] != target {
		return target, "", nil
	}

	return target, parsed.Fragment, nil
}

func (compiler *schemaCompiler) compileDynamicAnchors(name string) error {
	keys := sortedStringKeys(compiler.dynamicAnchors)
	for _, key := range keys {
		if !strings.HasSuffix(key, "#"+name) {
			continue
		}
		if _, err := compiler.compile(compiler.dynamicAnchors[key]); err != nil {
			return err
		}
	}

	return nil
}

func compileApplicatorKeywords(
	plan *schemaPlan,
	object map[string]*jsonValue,
	compiler *schemaCompiler,
) error {
	if plan.dialect == Draft3 {
		if value, exists := object["extends"]; exists {
			if value.kind == kindArray {
				plans, err := compileSchemaArray(value, compiler, false)
				if err != nil {
					return fmt.Errorf("%w: extends: %w", ErrInvalidSchema, err)
				}
				plan.allOf = append(plan.allOf, plans...)
			} else {
				compiled, err := compiler.compile(value)
				if err != nil {
					return fmt.Errorf("%w: extends: %w", ErrInvalidSchema, err)
				}
				plan.allOf = append(plan.allOf, compiled)
			}
		}
	}
	arrays := []struct {
		name   string
		target *[]*schemaPlan
	}{
		{name: "allOf", target: &plan.allOf},
		{name: "anyOf", target: &plan.anyOf},
		{name: "oneOf", target: &plan.oneOf},
	}
	for _, keyword := range arrays {
		value, exists := object[keyword.name]
		if !exists {
			continue
		}
		plans, err := compileSchemaArray(value, compiler, false)
		if err != nil {
			return fmt.Errorf("%w: %s: %w", ErrInvalidSchema, keyword.name, err)
		}
		*keyword.target = plans
	}

	if value, exists := object["not"]; exists {
		compiled, err := compileKeywordSchema(value, compiler)
		if err != nil {
			return fmt.Errorf("%w: not: %w", ErrInvalidSchema, err)
		}
		plan.not = compiled
	}

	if plan.dialect == Draft7 || plan.dialect == Draft201909 || plan.dialect == Draft202012 {
		if value, exists := object["if"]; exists {
			compiled, err := compileKeywordSchema(value, compiler)
			if err != nil {
				return fmt.Errorf("%w: if: %w", ErrInvalidSchema, err)
			}
			plan.condition = compiled
		}
		if value, exists := object["then"]; exists {
			compiled, err := compileKeywordSchema(value, compiler)
			if err != nil {
				return fmt.Errorf("%w: then: %w", ErrInvalidSchema, err)
			}
			plan.then = compiled
		}
		if value, exists := object["else"]; exists {
			compiled, err := compileKeywordSchema(value, compiler)
			if err != nil {
				return fmt.Errorf("%w: else: %w", ErrInvalidSchema, err)
			}
			plan.otherwise = compiled
		}
	}

	return nil
}

func compileArrayKeywords(
	plan *schemaPlan,
	object map[string]*jsonValue,
	compiler *schemaCompiler,
	policy vocabularyPolicy,
) error {
	if unique, exists := object["uniqueItems"]; exists && policy.validation {
		if unique.kind != kindBoolean {
			return fmt.Errorf("%w: uniqueItems must be a boolean", ErrInvalidSchema)
		}
		plan.uniqueItems = unique.boolean
	}
	if policy.applicator &&
		plan.dialect != Draft3 && plan.dialect != Draft4 {
		if value, exists := object["contains"]; exists {
			compiled, err := compileKeywordSchema(value, compiler)
			if err != nil {
				return fmt.Errorf("%w: contains: %w", ErrInvalidSchema, err)
			}
			plan.contains = compiled
		}
	}
	if policy.validation &&
		(plan.dialect == Draft201909 || plan.dialect == Draft202012) {
		for _, keyword := range []struct {
			name   string
			target **string
		}{
			{name: "minContains", target: &plan.minContains},
			{name: "maxContains", target: &plan.maxContains},
		} {
			value, exists := object[keyword.name]
			if !exists {
				continue
			}
			if value.kind != kindNumber ||
				!isInteger(value.number, plan.dialect) ||
				compareNumber(value.number, "0") < 0 {
				return fmt.Errorf(
					"%w: %s must be a non-negative integer",
					ErrInvalidSchema,
					keyword.name,
				)
			}
			number := value.number
			*keyword.target = &number
		}
	}
	if !policy.applicator {
		return nil
	}

	if plan.dialect == Draft202012 {
		if prefix, exists := object["prefixItems"]; exists {
			plans, err := compileSchemaArray(prefix, compiler, false)
			if err != nil {
				return fmt.Errorf("%w: prefixItems: %w", ErrInvalidSchema, err)
			}
			plan.prefixItems = plans
		}
		if items, exists := object["items"]; exists {
			itemPlan, err := compiler.compile(items)
			if err != nil {
				return fmt.Errorf("%w: items: %w", ErrInvalidSchema, err)
			}
			plan.items = itemPlan
		}

		return nil
	}

	items, exists := object["items"]
	if !exists {
		return nil
	}
	if items.kind == kindArray {
		plans, err := compileSchemaArray(
			items,
			compiler,
			plan.dialect == Draft3,
		)
		if err != nil {
			return fmt.Errorf("%w: items: %w", ErrInvalidSchema, err)
		}
		plan.prefixItems = plans

		if additional, exists := object["additionalItems"]; exists {
			additionalPlan, err := compileKeywordSchema(additional, compiler)
			if err != nil {
				return fmt.Errorf("%w: additionalItems: %w", ErrInvalidSchema, err)
			}
			plan.items = additionalPlan
		}

		return nil
	}

	itemPlan, err := compileKeywordSchema(items, compiler)
	if err != nil {
		return fmt.Errorf("%w: items: %w", ErrInvalidSchema, err)
	}
	plan.items = itemPlan

	return nil
}

func compileSchemaArray(
	value *jsonValue,
	compiler *schemaCompiler,
	allowEmpty bool,
) ([]*schemaPlan, error) {
	if value.kind != kindArray || !allowEmpty && len(value.array) == 0 {
		return nil, fmt.Errorf("schema array must not be empty")
	}
	compiler.combinatorBranches += len(value.array)
	if compiler.combinatorBranches > compiler.limits.MaxCombinatorBranches {
		return nil, &LimitError{
			Resource: "combinator branches",
			Limit:    compiler.limits.MaxCombinatorBranches,
		}
	}

	plans := make([]*schemaPlan, 0, len(value.array))
	for index, item := range value.array {
		plan, err := compileKeywordSchema(item, compiler)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", index, err)
		}
		plans = append(plans, plan)
	}

	return plans, nil
}

func compileKeywordSchema(
	value *jsonValue,
	compiler *schemaCompiler,
) (*schemaPlan, error) {
	if value.kind == kindBoolean && !compiler.dialectFor(value).supportsBooleanSchemas() {
		boolean := value.boolean

		return &schemaPlan{boolean: &boolean}, nil
	}

	return compiler.compile(value)
}

func compileNumberKeywords(
	plan *schemaPlan,
	object map[string]*jsonValue,
	dialect Dialect,
) error {
	minimum, hasMinimum, err := compileNumberKeyword(object, "minimum")
	if err != nil {
		return err
	}
	maximum, hasMaximum, err := compileNumberKeyword(object, "maximum")
	if err != nil {
		return err
	}
	if hasMinimum {
		plan.minimums = append(plan.minimums, numberBound{number: minimum})
	}
	if hasMaximum {
		plan.maximums = append(plan.maximums, numberBound{number: maximum})
	}

	if dialect == Draft3 || dialect == Draft4 {
		if err := compileLegacyExclusive(object, "exclusiveMinimum", plan.minimums); err != nil {
			return err
		}
		if err := compileLegacyExclusive(object, "exclusiveMaximum", plan.maximums); err != nil {
			return err
		}
	} else {
		exclusiveMinimum, exists, err := compileNumberKeyword(object, "exclusiveMinimum")
		if err != nil {
			return err
		}
		if exists {
			plan.minimums = append(
				plan.minimums,
				numberBound{number: exclusiveMinimum, exclusive: true},
			)
		}

		exclusiveMaximum, exists, err := compileNumberKeyword(object, "exclusiveMaximum")
		if err != nil {
			return err
		}
		if exists {
			plan.maximums = append(
				plan.maximums,
				numberBound{number: exclusiveMaximum, exclusive: true},
			)
		}
	}

	multipleKeyword := "multipleOf"
	if dialect == Draft3 {
		multipleKeyword = "divisibleBy"
	}
	multiple, exists, err := compileNumberKeyword(object, multipleKeyword)
	if err != nil {
		return err
	}
	if exists {
		if compareNumber(multiple, "0") <= 0 {
			return fmt.Errorf("%w: %s must be positive", ErrInvalidSchema, multipleKeyword)
		}
		plan.multipleOf = multiple
	}

	return nil
}

func compileNumberKeyword(
	object map[string]*jsonValue,
	name string,
) (string, bool, error) {
	value, exists := object[name]
	if !exists {
		return "", false, nil
	}
	if value.kind != kindNumber {
		return "", false, fmt.Errorf("%w: %s must be a number", ErrInvalidSchema, name)
	}

	return value.number, true, nil
}

func compileLegacyExclusive(
	object map[string]*jsonValue,
	name string,
	bounds []numberBound,
) error {
	value, exists := object[name]
	if !exists {
		return nil
	}
	if value.kind != kindBoolean {
		return fmt.Errorf("%w: %s must be a boolean", ErrInvalidSchema, name)
	}
	if value.boolean && len(bounds) == 0 {
		return fmt.Errorf("%w: %s requires its matching bound", ErrInvalidSchema, name)
	}
	if len(bounds) == 0 {
		return nil
	}
	bounds[0].exclusive = value.boolean

	return nil
}

func compileRequired(value *jsonValue, dialect Dialect) ([]string, error) {
	if value.kind != kindArray {
		return nil, fmt.Errorf("%w: required must be an array", ErrInvalidSchema)
	}
	if dialect == Draft4 && len(value.array) == 0 {
		return nil, fmt.Errorf("%w: Draft 4 required must not be empty", ErrInvalidSchema)
	}

	required := make([]string, 0, len(value.array))
	seen := make(map[string]struct{}, len(value.array))
	for _, item := range value.array {
		if item.kind != kindString {
			return nil, fmt.Errorf("%w: required entries must be strings", ErrInvalidSchema)
		}
		if _, duplicate := seen[item.text]; duplicate {
			return nil, fmt.Errorf("%w: duplicate required property %q", ErrInvalidSchema, item.text)
		}
		seen[item.text] = struct{}{}
		required = append(required, item.text)
	}

	return required, nil
}

func compileCardinalityKeywords(
	plan *schemaPlan,
	object map[string]*jsonValue,
	dialect Dialect,
) error {
	keywords := []struct {
		name   string
		target **string
	}{
		{name: "minLength", target: &plan.minLength},
		{name: "maxLength", target: &plan.maxLength},
		{name: "minItems", target: &plan.minItems},
		{name: "maxItems", target: &plan.maxItems},
	}
	if dialect != Draft3 {
		keywords = append(
			keywords,
			struct {
				name   string
				target **string
			}{name: "minProperties", target: &plan.minProperties},
			struct {
				name   string
				target **string
			}{name: "maxProperties", target: &plan.maxProperties},
		)
	}

	for _, keyword := range keywords {
		value, exists := object[keyword.name]
		if !exists {
			continue
		}
		if value.kind != kindNumber ||
			!isInteger(value.number, dialect) ||
			compareNumber(value.number, "0") < 0 {
			return fmt.Errorf(
				"%w: %s must be a non-negative integer",
				ErrInvalidSchema,
				keyword.name,
			)
		}

		number := value.number
		*keyword.target = &number
	}

	return nil
}

func compileTypes(
	value *jsonValue,
	compiler *schemaCompiler,
	dialect Dialect,
) ([]typePlan, error) {
	switch value.kind {
	case kindString:
		item, err := compileTypeName(value.text, dialect)
		if err != nil {
			return nil, err
		}

		return []typePlan{item}, nil
	case kindArray:
		if len(value.array) == 0 {
			return nil, fmt.Errorf("%w: type array must not be empty", ErrInvalidSchema)
		}

		result := make([]typePlan, 0, len(value.array))
		seen := make(map[string]struct{}, len(value.array))
		for _, raw := range value.array {
			if raw.kind == kindString {
				item, err := compileTypeName(raw.text, dialect)
				if err != nil {
					return nil, err
				}
				if _, duplicate := seen[item.name]; duplicate {
					return nil, fmt.Errorf("%w: duplicate type %q", ErrInvalidSchema, item.name)
				}
				seen[item.name] = struct{}{}
				result = append(result, item)

				continue
			}

			if dialect != Draft3 {
				return nil, fmt.Errorf("%w: type array entries must be strings", ErrInvalidSchema)
			}

			schema, err := compiler.compile(raw)
			if err != nil {
				return nil, err
			}
			result = append(result, typePlan{schema: schema})
		}

		return result, nil
	default:
		return nil, fmt.Errorf("%w: type must be a string or array", ErrInvalidSchema)
	}
}

func compileTypeName(name string, dialect Dialect) (typePlan, error) {
	switch name {
	case "null", "boolean", "object", "array", "number", "integer", "string":
		return typePlan{name: name}, nil
	case "any":
		if dialect == Draft3 {
			return typePlan{name: name}, nil
		}
	}

	return typePlan{}, fmt.Errorf("%w: unknown type %q", ErrInvalidSchema, name)
}

// Validate parses and evaluates a raw JSON instance.
func (schema *Schema) Validate(ctx context.Context, raw []byte) (Result, error) {
	if schema == nil || schema.plan == nil {
		return Result{}, fmt.Errorf("%w: nil compiled schema", ErrInvalidSchema)
	}

	instance, err := decodeJSON(ctx, raw, schema.limits)
	if err != nil {
		return Result{}, fmt.Errorf("parse instance: %w", err)
	}

	state := schemaEvaluationState(ctx, schema.plan, schema.limits)
	valid, err := schema.plan.evaluate(instance, schema.dialect, &state)
	if err != nil {
		return Result{}, err
	}

	return Result{Valid: valid}, nil
}

type evaluationState struct {
	ctx                context.Context
	limits             Limits
	operations         int
	uniqueComparisons  int
	formatChecks       int
	referenceDepth     int
	customKeywordCalls int
	outputUnits        int
	dynamicScope       []*schemaResource
}

func (state *evaluationState) consumeOperation() error {
	if err := state.ctx.Err(); err != nil {
		return err
	}
	state.operations++
	if state.operations > state.limits.MaxEvaluationOps {
		return &LimitError{
			Resource: "evaluation operations",
			Limit:    state.limits.MaxEvaluationOps,
		}
	}

	return nil
}

func (state *evaluationState) consumeOutputUnits(count int) error {
	if err := state.ctx.Err(); err != nil {
		return err
	}
	state.outputUnits += count
	if state.outputUnits > state.limits.MaxOutputUnits {
		return &LimitError{
			Resource: "output units",
			Limit:    state.limits.MaxOutputUnits,
		}
	}
	return nil
}

func (state *evaluationState) consumeUniqueComparison() error {
	if err := state.consumeOperation(); err != nil {
		return err
	}
	state.uniqueComparisons++
	if state.uniqueComparisons > state.limits.MaxUniqueComparisons {
		return &LimitError{
			Resource: "unique item comparisons",
			Limit:    state.limits.MaxUniqueComparisons,
		}
	}

	return nil
}

func (state *evaluationState) consumeFormatCheck() error {
	if err := state.consumeOperation(); err != nil {
		return err
	}
	state.formatChecks++
	if state.formatChecks > state.limits.MaxFormatChecks {
		return &LimitError{
			Resource: "format checks",
			Limit:    state.limits.MaxFormatChecks,
		}
	}
	return nil
}

func (state *evaluationState) consumeCustomKeywordCall() error {
	if err := state.consumeOperation(); err != nil {
		return err
	}
	state.customKeywordCalls++
	if state.customKeywordCalls > state.limits.MaxCustomKeywordCalls {
		return &LimitError{
			Resource: "custom keyword calls",
			Limit:    state.limits.MaxCustomKeywordCalls,
		}
	}
	return nil
}

func (
	plan *schemaPlan,
) evaluate(
	instance *jsonValue,
	dialect Dialect,
	state *evaluationState,
) (bool, error) {
	dialect = effectiveDialect(plan.dialect, dialect)
	pushed := pushReferenceResource(plan, state)
	if pushed {
		defer func() {
			state.dynamicScope = state.dynamicScope[:len(state.dynamicScope)-1]
		}()
	}
	if err := state.consumeOperation(); err != nil {
		return false, err
	}
	if plan.boolean != nil {
		return *plan.boolean, nil
	}
	if plan.reference != nil {
		state.referenceDepth++
		if state.referenceDepth > state.limits.MaxReferenceDepth {
			state.referenceDepth--
			return false, &LimitError{
				Resource: "reference depth",
				Limit:    state.limits.MaxReferenceDepth,
			}
		}
		defer func() { state.referenceDepth-- }()
		if len(state.dynamicScope) >= state.limits.MaxDynamicScopeDepth {
			return false, &LimitError{
				Resource: "dynamic scope depth",
				Limit:    state.limits.MaxDynamicScopeDepth,
			}
		}
		target := plan.referenceTarget(state)
		valid, err := evaluateReference(target, instance, dialect, state)
		if err != nil {
			return false, err
		}
		if !valid {
			return false, nil
		}
	}
	for _, keyword := range plan.custom {
		if err := state.consumeCustomKeywordCall(); err != nil {
			return false, err
		}
		result, err := callKeywordEvaluator(
			state.ctx,
			keyword.evaluator,
			Value{value: instance},
		)
		if err != nil {
			return false, fmt.Errorf("custom keyword %q: %w", keyword.name, err)
		}
		if !result.Valid {
			return false, nil
		}
	}
	if len(plan.types) > 0 {
		matched := false
		for _, expected := range plan.types {
			if expected.schema != nil {
				valid, err := expected.schema.evaluate(instance, dialect, state)
				if err != nil {
					return false, err
				}
				if valid {
					matched = true
					break
				}

				continue
			}
			if matchesType(instance, expected.name, dialect) {
				matched = true
				break
			}
		}
		if !matched {
			return false, nil
		}
	}
	for _, disallowed := range plan.disallowedTypes {
		if disallowed.schema != nil {
			valid, err := disallowed.schema.evaluate(instance, dialect, state)
			if err != nil {
				return false, err
			}
			if valid {
				return false, nil
			}
			continue
		}
		if matchesType(instance, disallowed.name, dialect) {
			return false, nil
		}
	}
	allValid := true
	for _, child := range plan.allOf {
		valid, err := child.evaluate(instance, dialect, state)
		if err != nil {
			return false, err
		}
		allValid = allValid && valid
	}
	if !allValid {
		return false, nil
	}
	if len(plan.anyOf) > 0 {
		anyValid := false
		for _, child := range plan.anyOf {
			valid, err := child.evaluate(instance, dialect, state)
			if err != nil {
				return false, err
			}
			if valid {
				anyValid = true
			}
		}
		if !anyValid {
			return false, nil
		}
	}
	if len(plan.oneOf) > 0 {
		validCount := 0
		for _, child := range plan.oneOf {
			valid, err := child.evaluate(instance, dialect, state)
			if err != nil {
				return false, err
			}
			if valid {
				validCount++
			}
		}
		if validCount != 1 {
			return false, nil
		}
	}
	if plan.not != nil {
		valid, err := plan.not.evaluate(instance, dialect, state)
		if err != nil {
			return false, err
		}
		if valid {
			return false, nil
		}
	}
	if plan.condition != nil {
		matched, err := plan.condition.evaluate(instance, dialect, state)
		if err != nil {
			return false, err
		}
		branch := plan.otherwise
		if matched {
			branch = plan.then
		}
		if branch != nil {
			valid, err := branch.evaluate(instance, dialect, state)
			if err != nil {
				return false, err
			}
			if !valid {
				return false, nil
			}
		}
	}
	if plan.hasEnum {
		matched := false
		for _, candidate := range plan.enum {
			if equalJSON(candidate, instance) {
				matched = true
				break
			}
		}
		if !matched {
			return false, nil
		}
	}
	if plan.hasConst && !equalJSON(plan.constant, instance) {
		return false, nil
	}
	if instance.kind == kindNumber {
		for _, minimum := range plan.minimums {
			comparison := compareNumber(instance.number, minimum.number)
			if comparison < 0 || comparison == 0 && minimum.exclusive {
				return false, nil
			}
		}
		for _, maximum := range plan.maximums {
			comparison := compareNumber(instance.number, maximum.number)
			if comparison > 0 || comparison == 0 && maximum.exclusive {
				return false, nil
			}
		}
		if plan.multipleOf != "" {
			if !numberIsMultiple(instance.number, plan.multipleOf) {
				return false, nil
			}
		}
	}
	if instance.kind == kindString {
		length := utf8.RuneCountInString(instance.text)
		if !cardinalityWithin(length, plan.minLength, plan.maxLength) {
			return false, nil
		}
		if plan.pattern != nil {
			matched, err := plan.pattern.matchString(instance.text)
			if err != nil {
				return false, err
			}
			if !matched {
				return false, nil
			}
		}
		if plan.format != nil {
			if err := state.consumeFormatCheck(); err != nil {
				return false, err
			}
			valid, err := callFormatChecker(state.ctx, plan.format, instance.text)
			if err != nil {
				return false, fmt.Errorf("format validation: %w", err)
			}
			if !valid {
				return false, nil
			}
		}
		if plan.contentEncoding != "" || plan.contentMediaType != "" {
			valid, err := plan.validateContent(instance.text, state)
			if err != nil {
				return false, err
			}
			if !valid {
				return false, nil
			}
		}
	}
	if instance.kind == kindArray {
		if !cardinalityWithin(len(instance.array), plan.minItems, plan.maxItems) {
			return false, nil
		}
		for index, itemPlan := range plan.prefixItems {
			if index >= len(instance.array) {
				break
			}
			valid, err := itemPlan.evaluate(instance.array[index], dialect, state)
			if err != nil {
				return false, err
			}
			if !valid {
				return false, nil
			}
		}
		if plan.items != nil {
			for index := len(plan.prefixItems); index < len(instance.array); index++ {
				valid, err := plan.items.evaluate(instance.array[index], dialect, state)
				if err != nil {
					return false, err
				}
				if !valid {
					return false, nil
				}
			}
		}
		if plan.contains != nil {
			matched, err := plan.matchingContainsItems(instance, dialect, state)
			if err != nil {
				return false, err
			}
			minimum := "1"
			if plan.minContains != nil {
				minimum = *plan.minContains
			}
			actual := strconv.Itoa(len(matched))
			if compareNumber(actual, minimum) < 0 ||
				plan.maxContains != nil && compareNumber(actual, *plan.maxContains) > 0 {
				return false, nil
			}
		}
		if plan.uniqueItems {
			unique, err := uniqueJSON(instance.array, state)
			if err != nil {
				return false, err
			}
			if !unique {
				return false, nil
			}
		}
		if plan.unevaluatedItems != nil {
			evaluated, err := plan.collectEvaluatedItems(instance, dialect, state)
			if err != nil {
				return false, err
			}
			for index, item := range instance.array {
				if _, exists := evaluated[index]; exists {
					continue
				}
				valid, err := plan.unevaluatedItems.evaluate(item, dialect, state)
				if err != nil {
					return false, err
				}
				if !valid {
					return false, nil
				}
			}
		}
	}

	if instance.kind == kindObject {
		if !cardinalityWithin(
			len(instance.object),
			plan.minProperties,
			plan.maxProperties,
		) {
			return false, nil
		}
		for _, name := range plan.required {
			if _, exists := instance.object[name]; !exists {
				return false, nil
			}
		}
		for name, property := range plan.properties {
			if _, exists := instance.object[name]; !exists && property.requiredByParent {
				return false, nil
			}
		}
		names := make([]string, 0, len(instance.object))
		for name := range instance.object {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			property, configured := plan.properties[name]
			value, exists := instance.object[name]
			if configured && exists {
				valid, err := property.evaluate(value, dialect, state)
				if err != nil {
					return false, err
				}
				if !valid {
					return false, nil
				}
			}
			matchedPattern := false
			for _, pattern := range plan.patternProperties {
				matched, err := pattern.pattern.matchString(name)
				if err != nil {
					return false, err
				}
				if !matched {
					continue
				}
				matchedPattern = true
				valid, err := pattern.schema.evaluate(value, dialect, state)
				if err != nil {
					return false, err
				}
				if !valid {
					return false, nil
				}
			}
			if !configured && !matchedPattern && plan.additionalProperties != nil {
				valid, err := plan.additionalProperties.evaluate(value, dialect, state)
				if err != nil {
					return false, err
				}
				if !valid {
					return false, nil
				}
			}
			if plan.propertyNames != nil {
				valid, err := plan.propertyNames.evaluate(
					&jsonValue{kind: kindString, text: name},
					dialect,
					state,
				)
				if err != nil {
					return false, err
				}
				if !valid {
					return false, nil
				}
			}
		}
		dependencyNames := sortedStringKeys(plan.dependentRequired)
		for _, name := range dependencyNames {
			if _, exists := instance.object[name]; !exists {
				continue
			}
			for _, required := range plan.dependentRequired[name] {
				if _, exists := instance.object[required]; !exists {
					return false, nil
				}
			}
		}
		dependencyNames = sortedStringKeys(plan.dependentSchemas)
		for _, name := range dependencyNames {
			if _, exists := instance.object[name]; !exists {
				continue
			}
			valid, err := plan.dependentSchemas[name].evaluate(instance, dialect, state)
			if err != nil {
				return false, err
			}
			if !valid {
				return false, nil
			}
		}
		if plan.unevaluatedProperties != nil {
			evaluated, err := plan.collectEvaluatedProperties(
				instance,
				dialect,
				state,
			)
			if err != nil {
				return false, err
			}
			for _, name := range names {
				if _, exists := evaluated[name]; exists {
					continue
				}
				valid, err := plan.unevaluatedProperties.evaluate(
					instance.object[name],
					dialect,
					state,
				)
				if err != nil {
					return false, err
				}
				if !valid {
					return false, nil
				}
			}
		}
	}

	return true, nil
}

func (plan *schemaPlan) validateContent(
	value string,
	state *evaluationState,
) (bool, error) {
	content := []byte(value)
	if plan.contentEncoding != "" {
		if plan.contentEncoding != "base64" {
			return true, nil
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(value)
		if err != nil {
			return false, nil
		}
		content = decoded
	}
	if plan.contentMediaType == "" {
		return true, nil
	}
	mediaType, _, err := mime.ParseMediaType(plan.contentMediaType)
	if err != nil {
		return true, nil
	}
	if mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json") {
		return true, nil
	}
	_, err = decodeJSON(state.ctx, content, state.limits)
	if err != nil {
		if errors.Is(err, ErrLimitExceeded) || state.ctx.Err() != nil {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

func (plan *schemaPlan) referenceTarget(state *evaluationState) *schemaPlan {
	target := plan.reference
	if plan.dynamicReference != "" {
		for _, resource := range state.dynamicScope {
			if candidate := resource.dynamicAnchors[plan.dynamicReference]; candidate != nil {
				return candidate
			}
		}
	}
	if plan.recursiveReference {
		for _, resource := range state.dynamicScope {
			if resource.recursiveAnchored && resource.root != nil {
				return resource.root
			}
		}
	}

	return target
}

func evaluateReference(
	target *schemaPlan,
	instance *jsonValue,
	dialect Dialect,
	state *evaluationState,
) (bool, error) {
	pushed := pushReferenceResource(target, state)
	if pushed {
		defer func() {
			state.dynamicScope = state.dynamicScope[:len(state.dynamicScope)-1]
		}()
	}

	return target.evaluate(instance, dialect, state)
}

func pushReferenceResource(target *schemaPlan, state *evaluationState) bool {
	if target.resource == nil ||
		len(state.dynamicScope) > 0 && state.dynamicScope[len(state.dynamicScope)-1] == target.resource {
		return false
	}
	state.dynamicScope = append(state.dynamicScope, target.resource)

	return true
}

func (
	plan *schemaPlan,
) matchingContainsItems(
	instance *jsonValue,
	dialect Dialect,
	state *evaluationState,
) (map[int]struct{}, error) {
	matched := make(map[int]struct{})
	for index, item := range instance.array {
		valid, err := plan.contains.evaluate(item, dialect, state)
		if err != nil {
			return nil, err
		}
		if valid {
			matched[index] = struct{}{}
		}
	}

	return matched, nil
}

func (
	plan *schemaPlan,
) collectEvaluatedItems(
	instance *jsonValue,
	dialect Dialect,
	state *evaluationState,
) (map[int]struct{}, error) {
	dialect = effectiveDialect(plan.dialect, dialect)
	pushed := pushReferenceResource(plan, state)
	if pushed {
		defer func() {
			state.dynamicScope = state.dynamicScope[:len(state.dynamicScope)-1]
		}()
	}
	if err := state.consumeOperation(); err != nil {
		return nil, err
	}
	evaluated := make(map[int]struct{})
	if instance.kind != kindArray {
		return evaluated, nil
	}

	prefixCount := min(len(plan.prefixItems), len(instance.array))
	for index := range prefixCount {
		evaluated[index] = struct{}{}
	}
	if plan.items != nil {
		start := min(len(plan.prefixItems), len(instance.array))
		for index := range instance.array[start:] {
			index += start
			evaluated[index] = struct{}{}
		}
	}
	if dialect == Draft202012 && plan.contains != nil {
		matched, err := plan.matchingContainsItems(instance, dialect, state)
		if err != nil {
			return nil, err
		}
		for index := range matched {
			evaluated[index] = struct{}{}
		}
	}
	if plan.reference != nil {
		if err := mergeReferenceEvaluatedItems(
			evaluated,
			plan.referenceTarget(state),
			instance,
			dialect,
			state,
		); err != nil {
			return nil, err
		}
	}
	for _, child := range plan.allOf {
		valid, err := child.evaluate(instance, dialect, state)
		if err != nil {
			return nil, err
		}
		if valid {
			if err := mergeEvaluatedItems(
				evaluated,
				child,
				instance,
				dialect,
				state,
			); err != nil {
				return nil, err
			}
		}
	}
	for _, children := range [][]*schemaPlan{plan.anyOf, plan.oneOf} {
		for _, child := range children {
			valid, err := child.evaluate(instance, dialect, state)
			if err != nil {
				return nil, err
			}
			if valid {
				if err := mergeEvaluatedItems(
					evaluated,
					child,
					instance,
					dialect,
					state,
				); err != nil {
					return nil, err
				}
			}
		}
	}
	if plan.condition != nil {
		matched, err := plan.condition.evaluate(instance, dialect, state)
		if err != nil {
			return nil, err
		}
		branch := plan.otherwise
		if matched {
			branch = plan.then
			if err := mergeEvaluatedItems(
				evaluated,
				plan.condition,
				instance,
				dialect,
				state,
			); err != nil {
				return nil, err
			}
		}
		if branch != nil {
			if err := mergeEvaluatedItems(
				evaluated,
				branch,
				instance,
				dialect,
				state,
			); err != nil {
				return nil, err
			}
		}
	}

	return evaluated, nil
}

func mergeReferenceEvaluatedItems(
	destination map[int]struct{},
	target *schemaPlan,
	instance *jsonValue,
	dialect Dialect,
	state *evaluationState,
) error {
	pushed := pushReferenceResource(target, state)
	if pushed {
		defer func() {
			state.dynamicScope = state.dynamicScope[:len(state.dynamicScope)-1]
		}()
	}

	return mergeEvaluatedItems(destination, target, instance, dialect, state)
}

func mergeEvaluatedItems(
	destination map[int]struct{},
	plan *schemaPlan,
	instance *jsonValue,
	dialect Dialect,
	state *evaluationState,
) error {
	items, err := plan.collectEvaluatedItems(instance, dialect, state)
	if err != nil {
		return err
	}
	for index := range items {
		destination[index] = struct{}{}
	}
	if plan.unevaluatedItems != nil {
		for index := range instance.array {
			destination[index] = struct{}{}
		}
	}

	return nil
}

func (
	plan *schemaPlan,
) collectEvaluatedProperties(
	instance *jsonValue,
	dialect Dialect,
	state *evaluationState,
) (map[string]struct{}, error) {
	dialect = effectiveDialect(plan.dialect, dialect)
	pushed := pushReferenceResource(plan, state)
	if pushed {
		defer func() {
			state.dynamicScope = state.dynamicScope[:len(state.dynamicScope)-1]
		}()
	}
	if err := state.consumeOperation(); err != nil {
		return nil, err
	}
	evaluated := make(map[string]struct{})
	if instance.kind != kindObject {
		return evaluated, nil
	}

	for name := range plan.properties {
		if _, exists := instance.object[name]; exists {
			evaluated[name] = struct{}{}
		}
	}
	for name := range instance.object {
		matched := false
		for _, pattern := range plan.patternProperties {
			patternMatched, err := pattern.pattern.matchString(name)
			if err != nil {
				return nil, err
			}
			if patternMatched {
				matched = true
				evaluated[name] = struct{}{}
			}
		}
		if _, configured := plan.properties[name]; !configured && !matched && plan.additionalProperties != nil {
			evaluated[name] = struct{}{}
		}
	}
	if plan.reference != nil {
		if err := mergeReferenceEvaluatedProperties(
			evaluated,
			plan.referenceTarget(state),
			instance,
			dialect,
			state,
		); err != nil {
			return nil, err
		}
	}
	for _, name := range sortedStringKeys(plan.dependentSchemas) {
		if _, exists := instance.object[name]; !exists {
			continue
		}
		if err := mergeEvaluatedProperties(
			evaluated,
			plan.dependentSchemas[name],
			instance,
			dialect,
			state,
		); err != nil {
			return nil, err
		}
	}

	for _, child := range plan.allOf {
		valid, err := child.evaluate(instance, dialect, state)
		if err != nil {
			return nil, err
		}
		if valid {
			if err := mergeEvaluatedProperties(
				evaluated,
				child,
				instance,
				dialect,
				state,
			); err != nil {
				return nil, err
			}
		}
	}
	for _, children := range [][]*schemaPlan{plan.anyOf, plan.oneOf} {
		for _, child := range children {
			valid, err := child.evaluate(instance, dialect, state)
			if err != nil {
				return nil, err
			}
			if valid {
				if err := mergeEvaluatedProperties(
					evaluated,
					child,
					instance,
					dialect,
					state,
				); err != nil {
					return nil, err
				}
			}
		}
	}
	if plan.condition != nil {
		matched, err := plan.condition.evaluate(instance, dialect, state)
		if err != nil {
			return nil, err
		}
		branch := plan.otherwise
		if matched {
			branch = plan.then
			if err := mergeEvaluatedProperties(
				evaluated,
				plan.condition,
				instance,
				dialect,
				state,
			); err != nil {
				return nil, err
			}
		}
		if branch != nil {
			if err := mergeEvaluatedProperties(
				evaluated,
				branch,
				instance,
				dialect,
				state,
			); err != nil {
				return nil, err
			}
		}
	}
	return evaluated, nil
}

func mergeReferenceEvaluatedProperties(
	destination map[string]struct{},
	target *schemaPlan,
	instance *jsonValue,
	dialect Dialect,
	state *evaluationState,
) error {
	pushed := pushReferenceResource(target, state)
	if pushed {
		defer func() {
			state.dynamicScope = state.dynamicScope[:len(state.dynamicScope)-1]
		}()
	}

	return mergeEvaluatedProperties(destination, target, instance, dialect, state)
}

func sortedStringKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys
}

func mergeEvaluatedProperties(
	destination map[string]struct{},
	plan *schemaPlan,
	instance *jsonValue,
	dialect Dialect,
	state *evaluationState,
) error {
	properties, err := plan.collectEvaluatedProperties(instance, dialect, state)
	if err != nil {
		return err
	}
	for name := range properties {
		destination[name] = struct{}{}
	}
	if plan.unevaluatedProperties != nil {
		for name := range instance.object {
			destination[name] = struct{}{}
		}
	}

	return nil
}

func cardinalityWithin(length int, minimum *string, maximum *string) bool {
	actual := strconv.Itoa(length)
	if minimum != nil && compareNumber(actual, *minimum) < 0 {
		return false
	}
	if maximum != nil && compareNumber(actual, *maximum) > 0 {
		return false
	}

	return true
}

func matchesType(value *jsonValue, expected string, dialect Dialect) bool {
	switch expected {
	case "any":
		return true
	case "null":
		return value.kind == kindNull
	case "boolean":
		return value.kind == kindBoolean
	case "object":
		return value.kind == kindObject
	case "array":
		return value.kind == kindArray
	case "number":
		return value.kind == kindNumber
	case "integer":
		return value.kind == kindNumber && isInteger(value.number, dialect)
	case "string":
		return value.kind == kindString
	default:
		return false
	}
}

func isInteger(number string, dialect Dialect) bool {
	if !dialect.usesMathematicalIntegers() {
		return !strings.ContainsAny(number, ".eE")
	}

	unsigned := strings.TrimPrefix(number, "-")
	mantissa, exponentText, hasExponent := strings.Cut(unsigned, "e")
	if !hasExponent {
		mantissa, exponentText, hasExponent = strings.Cut(unsigned, "E")
	}

	exponent := new(big.Int)
	if hasExponent {
		if _, ok := exponent.SetString(strings.TrimPrefix(exponentText, "+"), 10); !ok {
			return false
		}
	}

	integer, fraction, hasFraction := strings.Cut(mantissa, ".")
	digits := integer
	if hasFraction {
		digits += fraction
	}

	scale := new(big.Int).Sub(exponent, big.NewInt(int64(len(fraction))))
	if strings.Trim(digits, "0") == "" {
		return true
	}

	requiredZeros := new(big.Int).Neg(scale)
	if !requiredZeros.IsInt64() {
		return false
	}
	trailingZeros := len(digits) - len(strings.TrimRight(digits, "0"))
	return requiredZeros.Int64() <= int64(trailingZeros)
}

func effectiveDialect(configured, fallback Dialect) Dialect {
	if configured != "" {
		return configured
	}
	return fallback
}
