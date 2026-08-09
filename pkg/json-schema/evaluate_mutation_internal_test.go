package jsonschema

import (
	"context"
	"errors"
	"testing"
)

func TestEvaluationBudgetsIncludeTheirExactLimit(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxEvaluationOps = 10
	limits.MaxOutputUnits = 1
	limits.MaxUniqueComparisons = 1
	limits.MaxFormatChecks = 1
	limits.MaxCustomKeywordCalls = 1
	for name, consume := range map[string]func(*evaluationState) error{
		"output": func(state *evaluationState) error { return state.consumeOutputUnits(1) },
		"unique": func(state *evaluationState) error { return state.consumeUniqueComparison() },
		"format": func(state *evaluationState) error { return state.consumeFormatCheck() },
		"custom": func(state *evaluationState) error { return state.consumeCustomKeywordCall() },
	} {
		state := evaluationState{ctx: context.Background(), limits: limits}
		if err := consume(&state); err != nil {
			t.Fatalf("%s: exact limit rejected: %v", name, err)
		}
		if err := consume(&state); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("%s: got %v, want second call limit", name, err)
		}
	}

	limits.MaxEvaluationOps = 1
	state := evaluationState{ctx: context.Background(), limits: limits}
	if err := state.consumeOperation(); err != nil {
		t.Fatalf("operation at limit: %v", err)
	}
	if err := state.consumeOperation(); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want operation limit", err)
	}
}

func TestDialectFeaturePoliciesAreExact(t *testing.T) {
	t.Parallel()

	policy := applyVocabularyDefaults(vocabularyPolicy{applicator: true}, Draft201909)
	if !policy.unevaluated {
		t.Fatal("Draft 2019-09 did not inherit applicator vocabulary behavior")
	}
	policy = applyVocabularyDefaults(vocabularyPolicy{applicator: true}, Draft202012)
	if policy.unevaluated {
		t.Fatal("Draft 2020-12 unexpectedly inherited undeclared vocabulary behavior")
	}
	for _, dialect := range []Dialect{Draft7, Draft201909, Draft202012} {
		if !contentKeywordsSupported(dialect) {
			t.Fatalf("%s content keywords disabled", dialect)
		}
	}
	if contentKeywordsSupported(Draft6) {
		t.Fatal("Draft 6 content keywords enabled")
	}
	if !unevaluatedKeywordsSupported(Draft201909) ||
		!unevaluatedKeywordsSupported(Draft202012) ||
		unevaluatedKeywordsSupported(Draft7) {
		t.Fatal("unexpected unevaluated keyword dialect policy")
	}
	root := &jsonValue{}
	other := &jsonValue{}
	for _, test := range []struct {
		current Dialect
		root    *jsonValue
		value   *jsonValue
		want    bool
	}{
		{current: "", root: root, value: other, want: true},
		{current: Draft7, root: root, value: root, want: true},
		{current: Draft7, root: root, value: other, want: false},
	} {
		if got := resourceDialectShouldUpdate(test.current, test.root, test.value); got != test.want {
			t.Fatalf("current=%q root=%v: got %v, want %v", test.current, test.root, got, test.want)
		}
	}
}

func TestSchemaChildrenDiscoverOnlyApplicableDependencySchemas(t *testing.T) {
	t.Parallel()

	boolean := &jsonValue{kind: kindBoolean}
	objectSchema := &jsonValue{kind: kindObject, object: map[string]*jsonValue{}}
	array := &jsonValue{kind: kindArray}
	dependent := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"dependent": boolean,
	}}
	legacy := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"boolean": boolean,
		"object":  objectSchema,
		"array":   array,
	}}
	root := map[string]*jsonValue{
		"dependentSchemas": dependent,
		"unevaluatedItems": boolean,
		"dependencies":     legacy,
	}
	compiler := &schemaCompiler{}
	children := compiler.schemaChildren(root, Draft202012)
	for _, wanted := range []*jsonValue{boolean, objectSchema} {
		if !containsJSONValue(children, wanted) {
			t.Fatalf("missing child %p from %#v", wanted, children)
		}
	}
	if containsJSONValue(children, array) {
		t.Fatal("dependency name array was indexed as a schema")
	}
	legacyOnly := compiler.schemaChildren(root, Draft7)
	if containsJSONValue(legacyOnly, dependent) {
		t.Fatal("Draft 7 indexed dependentSchemas")
	}
	if children := compiler.schemaChildren(
		map[string]*jsonValue{"dependencies": array}, Draft7,
	); len(children) != 0 {
		t.Fatalf("non-object dependencies yielded %#v", children)
	}
}

func TestCustomKeywordCompileBudgetIncludesItsExactLimit(t *testing.T) {
	t.Parallel()

	compiler := compilerWithoutMetaSchema(Draft7)
	compiler.limits.MaxCustomKeywordCompiles = 1
	compiler.vocabularies["https://example.test/v"] = registeredVocabulary{
		keywords: map[string]KeywordCompiler{
			"custom": KeywordCompilerFunc(func(
				context.Context, Dialect, Value,
			) (KeywordEvaluator, error) {
				return KeywordEvaluatorFunc(func(
					context.Context, Value,
				) (KeywordResult, error) {
					return KeywordResult{Valid: true}, nil
				}), nil
			}),
		},
	}
	if _, err := compiler.Compile(
		context.Background(), []byte(`{"custom":true}`),
	); err != nil {
		t.Fatalf("custom compile at limit: %v", err)
	}
}

func TestCompilerBudgetsIncludeTheirExactLimit(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxRegexBytes = 1
	limits.MaxRegexCount = 1
	compiler := &schemaCompiler{limits: limits}
	if _, err := compiler.compilePattern("x"); err != nil {
		t.Fatalf("pattern at limits: %v", err)
	}
	if _, err := compiler.compilePattern(""); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want regex count limit", err)
	}
	if _, err := compiler.compilePattern("xx"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want regex byte limit", err)
	}

	child := &jsonValue{kind: kindBoolean}
	branchLimits := DefaultLimits()
	branchLimits.MaxCombinatorBranches = 1
	compiler = newSchemaCompiler(
		context.Background(), child, Draft202012, branchLimits, nil,
		0, false, false, standardFormats(), nil,
	)
	value := &jsonValue{kind: kindArray, array: []*jsonValue{child}}
	if _, err := compileSchemaArray(value, compiler, false); err != nil {
		t.Fatalf("branch at limit: %v", err)
	}
	value.array = append(value.array, &jsonValue{kind: kindBoolean})
	if _, err := compileSchemaArray(value, compiler, false); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want branch limit", err)
	}
}

func TestEvaluationHelpersPreserveExactSemantics(t *testing.T) {
	t.Parallel()

	for _, character := range []byte{'A', 'Z', 'a', 'z'} {
		if !isASCIIAlpha(character) {
			t.Fatalf("letter boundary %q rejected", character)
		}
	}
	for _, character := range []byte{'@', '[', '`', '{'} {
		if isASCIIAlpha(character) {
			t.Fatalf("non-letter boundary %q accepted", character)
		}
	}
	if effectiveDialect(Draft3, Draft202012) != Draft3 ||
		effectiveDialect("", Draft202012) != Draft202012 {
		t.Fatal("configured dialect did not override fallback")
	}
	for _, number := range []string{"0", "1", "1e0", "1.0", "100e-2"} {
		if !isInteger(number, Draft202012) {
			t.Fatalf("%q was not classified as an integer", number)
		}
	}
	for _, number := range []string{
		"1.1", "11e-1", "1e-999999999999999999999999999999999",
	} {
		if isInteger(number, Draft202012) {
			t.Fatalf("%q was classified as an integer", number)
		}
	}
	if numberIsMultiple("1", "") {
		t.Fatal("empty multipleOf divisor was accepted")
	}
	if err := compileLegacyExclusive(
		map[string]*jsonValue{"exclusiveMinimum": {kind: kindBoolean}},
		"exclusiveMinimum",
		nil,
	); err != nil {
		t.Fatalf("false exclusivity without a bound: %v", err)
	}
}

func TestContentValidationSeparatesSyntaxAndResourceFailures(t *testing.T) {
	t.Parallel()

	plan := &schemaPlan{contentMediaType: "application/json"}
	limits := DefaultLimits()
	limits.MaxInputBytes = 1
	state := evaluationState{ctx: context.Background(), limits: limits}
	if valid, err := plan.validateContent("null", &state); valid || !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got valid=%v err=%v, want resource limit", valid, err)
	}
	state = evaluationState{ctx: context.Background(), limits: limits}
	if valid, err := plan.evaluate(
		&jsonValue{kind: kindString, text: "null"}, Draft202012, &state,
	); valid || !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got evaluation valid=%v err=%v, want resource limit", valid, err)
	}
	state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	if valid, err := plan.validateContent("{", &state); valid || err != nil {
		t.Fatalf("got valid=%v err=%v, want ordinary syntax failure", valid, err)
	}
}

func TestReferenceDepthIsRestoredOnSuccessAndFailure(t *testing.T) {
	t.Parallel()

	truth := true
	plan := &schemaPlan{reference: &schemaPlan{boolean: &truth}}
	limits := DefaultLimits()
	limits.MaxReferenceDepth = 1
	state := evaluationState{ctx: context.Background(), limits: limits}
	valid, err := plan.evaluate(&jsonValue{kind: kindNull}, Draft202012, &state)
	if err != nil || !valid || state.referenceDepth != 0 {
		t.Fatalf("success left depth=%d valid=%v err=%v", state.referenceDepth, valid, err)
	}
	state.referenceDepth = 1
	if _, err := plan.evaluate(
		&jsonValue{kind: kindNull}, Draft202012, &state,
	); !errors.Is(err, ErrLimitExceeded) || state.referenceDepth != 1 {
		t.Fatalf("failure left depth=%d err=%v", state.referenceDepth, err)
	}
}

func TestEmptyAndSingletonUniqueArraysNeedNoComparisons(t *testing.T) {
	t.Parallel()

	plan := &schemaPlan{uniqueItems: true}
	for _, items := range [][]*jsonValue{nil, {{kind: kindNull}}} {
		limits := DefaultLimits()
		limits.MaxUniqueComparisons = 0
		state := evaluationState{ctx: context.Background(), limits: limits}
		valid, err := plan.evaluate(
			&jsonValue{kind: kindArray, array: items}, Draft202012, &state,
		)
		if err != nil || !valid || state.uniqueComparisons != 0 {
			t.Fatalf("items=%d valid=%v comparisons=%d err=%v", len(items), valid, state.uniqueComparisons, err)
		}
	}
}

func TestCompilationHonorsIndependentVocabularyCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dialect Dialect
		policy  vocabularyPolicy
		schema  string
		wantErr bool
	}{
		{
			name:    "validation without applicator",
			policy:  vocabularyPolicy{validation: true},
			schema:  `{"uniqueItems":"invalid"}`,
			wantErr: true,
		},
		{
			name:    "applicator without validation",
			policy:  vocabularyPolicy{applicator: true},
			schema:  `{"contains":1}`,
			wantErr: true,
		},
		{
			name:    "unevaluated 2019-09",
			dialect: Draft201909,
			policy:  vocabularyPolicy{unevaluated: true},
			schema:  `{"unevaluatedItems":1}`,
			wantErr: true,
		},
		{
			name:    "unevaluated 2020-12",
			dialect: Draft202012,
			policy:  vocabularyPolicy{unevaluated: true},
			schema:  `{"unevaluatedProperties":1}`,
			wantErr: true,
		},
		{
			name:    "unevaluated ignored by draft 7",
			dialect: Draft7,
			policy:  vocabularyPolicy{unevaluated: true},
			schema:  `{"unevaluatedItems":1}`,
		},
		{
			name:    "unevaluated ignored without vocabulary",
			dialect: Draft202012,
			policy:  vocabularyPolicy{},
			schema:  `{"unevaluatedProperties":1}`,
		},
		{
			name:    "contains ignored by draft 3",
			dialect: Draft3,
			policy:  vocabularyPolicy{applicator: true},
			schema:  `{"contains":1}`,
		},
		{
			name:    "contains ignored by draft 4",
			dialect: Draft4,
			policy:  vocabularyPolicy{applicator: true},
			schema:  `{"contains":1}`,
		},
		{
			name:    "contains ignored without applicator vocabulary",
			dialect: Draft6,
			policy:  vocabularyPolicy{},
			schema:  `{"contains":1}`,
		},
		{
			name:    "contains compiled by draft 6",
			dialect: Draft6,
			policy:  vocabularyPolicy{applicator: true},
			schema:  `{"contains":1}`,
			wantErr: true,
		},
		{
			name:    "minimum contains ignored without validation vocabulary",
			dialect: Draft202012,
			policy:  vocabularyPolicy{},
			schema:  `{"minContains":-1}`,
		},
		{
			name:    "minimum contains ignored by draft 7",
			dialect: Draft7,
			policy:  vocabularyPolicy{validation: true},
			schema:  `{"minContains":-1}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := compileWithVocabularyPolicy(t, test.dialect, test.schema, test.policy)
			if test.wantErr && !errors.Is(err, ErrInvalidSchema) {
				t.Fatalf("got %v, want ErrInvalidSchema", err)
			}
			if !test.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVocabularyPolicyFallsBackWhenResourceRootIsUnavailable(t *testing.T) {
	t.Parallel()

	root := &jsonValue{kind: kindObject, object: map[string]*jsonValue{}}
	compiler := newSchemaCompiler(
		context.Background(), root, Draft202012, DefaultLimits(), nil, 0,
		false, false, standardFormats(), nil,
	)
	resourceIdentifier := compiler.resourceFor[root]
	delete(compiler.resources, resourceIdentifier)
	policy, err := compiler.vocabularyPolicy(root)
	if err != nil {
		t.Fatal(err)
	}
	if !policy.validation || !policy.applicator || !policy.unevaluated {
		t.Fatalf("unexpected fallback policy %#v", policy)
	}
	if _, cached := compiler.vocabularyPolicies[resourceIdentifier]; !cached {
		t.Fatal("fallback policy was not cached")
	}
}

func TestContainsCardinalityRejectsEachInvalidNumberClass(t *testing.T) {
	t.Parallel()

	for _, dialect := range []Dialect{Draft201909, Draft202012} {
		for _, keyword := range []string{"minContains", "maxContains"} {
			for _, value := range []string{`"one"`, `1.5`, `-1`} {
				schema := `{"` + keyword + `":` + value + `}`
				err := compileWithVocabularyPolicy(
					t, dialect, schema, vocabularyPolicy{validation: true},
				)
				if !errors.Is(err, ErrInvalidSchema) {
					t.Fatalf("%s %s %s: got %v", dialect, keyword, value, err)
				}
			}
		}
	}
}

func TestGeneralCardinalityRejectsEachInvalidNumberClass(t *testing.T) {
	t.Parallel()

	for _, keyword := range []string{
		"minLength", "maxLength", "minItems", "maxItems",
		"minProperties", "maxProperties",
	} {
		for _, value := range []string{`"one"`, `1.5`, `-1`} {
			schema := `{"` + keyword + `":` + value + `}`
			err := compileWithVocabularyPolicy(
				t, Draft202012, schema, vocabularyPolicy{validation: true},
			)
			if !errors.Is(err, ErrInvalidSchema) {
				t.Fatalf("%s %s: got %v", keyword, value, err)
			}
		}
	}
}

func TestCustomKeywordCompilationSkipsAbsentRegistrations(t *testing.T) {
	t.Parallel()

	root, err := decodeJSON(context.Background(), []byte(`{"present":true}`), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	absentCalled := false
	presentCalled := false
	compiler := newSchemaCompiler(
		context.Background(), root, Draft202012, DefaultLimits(), nil, 0,
		false, false, standardFormats(), nil,
	)
	compiler.vocabularyPolicies[compiler.resourceFor[root]] = vocabularyPolicy{
		applicator: true,
		keywords: map[string]KeywordCompiler{
			"absent": KeywordCompilerFunc(func(
				context.Context, Dialect, Value,
			) (KeywordEvaluator, error) {
				absentCalled = true
				return nil, errors.New("absent keyword compiled")
			}),
			"present": KeywordCompilerFunc(func(
				context.Context, Dialect, Value,
			) (KeywordEvaluator, error) {
				presentCalled = true
				return KeywordEvaluatorFunc(func(
					context.Context, Value,
				) (KeywordResult, error) {
					return KeywordResult{Valid: true}, nil
				}), nil
			}),
		},
	}
	if _, err := compiler.compile(root); err != nil {
		t.Fatal(err)
	}
	if absentCalled || !presentCalled {
		t.Fatalf("absent=%t present=%t", absentCalled, presentCalled)
	}
}

func TestReferenceResolutionRejectsEachArrayIndexSyntax(t *testing.T) {
	t.Parallel()

	root, err := decodeJSON(
		context.Background(),
		[]byte(`{"$defs":{"list":[true]}}`),
		DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	compiler := newSchemaCompiler(
		context.Background(), root, Draft202012, DefaultLimits(), nil, 0,
		false, false, standardFormats(), nil,
	)
	for _, reference := range []string{"#/$defs/list/", "#/$defs/list/00"} {
		if _, err := compiler.resolveReference(root, reference); !errors.Is(err, ErrInvalidSchema) {
			t.Fatalf("%q: got %v, want ErrInvalidSchema", reference, err)
		}
	}
}

func TestDynamicAnchorCompilationIgnoresOtherAnchorNames(t *testing.T) {
	t.Parallel()

	compiler := compilerWithoutMetaSchema(Draft202012)
	if _, err := compiler.Compile(context.Background(), []byte(`{
		"$dynamicRef":"#z",
		"$defs":{
			"target":{"$dynamicAnchor":"z"},
			"unrelated":{"$dynamicAnchor":"a","$ref":1}
		}
	}`)); err != nil {
		t.Fatal(err)
	}
}

func TestDynamicAnchorCompilationContinuesToMatchingAnchor(t *testing.T) {
	t.Parallel()

	invalid := &jsonValue{kind: kindNumber, number: "1"}
	compiler := newSchemaCompiler(
		context.Background(), invalid, Draft202012, DefaultLimits(), nil, 0,
		false, false, standardFormats(), nil,
	)
	compiler.dynamicAnchors = map[string]*jsonValue{
		"#a": {kind: kindBoolean, boolean: true},
		"#z": invalid,
	}
	if err := compiler.compileDynamicAnchors("z"); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("got %v, want matching-anchor failure", err)
	}
}

func TestCombinatorBudgetAccumulatesAcrossSchemaArrays(t *testing.T) {
	t.Parallel()

	child := &jsonValue{kind: kindBoolean, boolean: true}
	limits := DefaultLimits()
	limits.MaxCombinatorBranches = 2
	compiler := newSchemaCompiler(
		context.Background(), child, Draft202012, limits, nil, 0,
		false, false, standardFormats(), nil,
	)
	if _, err := compileSchemaArray(
		&jsonValue{kind: kindArray, array: []*jsonValue{child}}, compiler, false,
	); err != nil {
		t.Fatal(err)
	}
	_, err := compileSchemaArray(
		&jsonValue{kind: kindArray, array: []*jsonValue{child, child}}, compiler, false,
	)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want cumulative branch limit", err)
	}
}

func TestEvaluationShortCircuitsDecisiveTypeBranches(t *testing.T) {
	t.Parallel()

	failing := &schemaPlan{custom: []compiledKeyword{{
		name: "must not run",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			return KeywordResult{}, errors.New("unexpected evaluation")
		}),
	}}}
	instance := &jsonValue{kind: kindNull}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	plan := &schemaPlan{types: []typePlan{{name: "null"}, {schema: failing}}}
	valid, err := plan.evaluate(instance, Draft3, &state)
	if err != nil || !valid {
		t.Fatalf("allowed type: valid=%t err=%v", valid, err)
	}

	falseValue := false
	state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	plan = &schemaPlan{disallowedTypes: []typePlan{
		{schema: &schemaPlan{boolean: &falseValue}},
		{name: "null"},
	}}
	valid, err = plan.evaluate(instance, Draft3, &state)
	if err != nil || valid {
		t.Fatalf("disallowed type: valid=%t err=%v", valid, err)
	}
}

func TestEvaluationStopsAtAvailablePrefixItems(t *testing.T) {
	t.Parallel()

	truth := true
	plan := &schemaPlan{prefixItems: []*schemaPlan{
		{boolean: &truth},
		{boolean: &truth},
	}}
	instance := &jsonValue{kind: kindArray, array: []*jsonValue{{kind: kindNull}}}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	valid, err := plan.evaluate(instance, Draft202012, &state)
	if err != nil || !valid {
		t.Fatalf("valid=%t err=%v", valid, err)
	}
}

func TestEvaluatedPropertiesDistinguishConfiguredAndPatternMatches(t *testing.T) {
	t.Parallel()

	pattern, err := compilePattern("^pattern$")
	if err != nil {
		t.Fatal(err)
	}
	plan := &schemaPlan{
		properties: map[string]*schemaPlan{"configured": {}},
		patternProperties: []patternPropertyPlan{{
			name: "^pattern$", pattern: pattern, schema: &schemaPlan{},
		}},
		additionalProperties: &schemaPlan{},
	}
	instance := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"configured": {kind: kindNull},
		"pattern":    {kind: kindNull},
		"additional": {kind: kindNull},
	}}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	evaluated, err := plan.collectEvaluatedProperties(instance, Draft202012, &state)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"configured", "pattern", "additional"} {
		if _, exists := evaluated[name]; !exists {
			t.Fatalf("%q was not marked evaluated", name)
		}
	}
	state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	evaluated, err = (&schemaPlan{}).collectEvaluatedProperties(
		&jsonValue{kind: kindObject, object: map[string]*jsonValue{
			"unconfigured": {kind: kindNull},
		}},
		Draft202012,
		&state,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(evaluated) != 0 {
		t.Fatalf("property without an applicator was marked evaluated: %#v", evaluated)
	}
}

func TestReferenceScopeRejectsNilAndDuplicateResourcesIndependently(t *testing.T) {
	t.Parallel()

	existing := &schemaResource{}
	for _, test := range []struct {
		name   string
		target *schemaPlan
		scope  []*schemaResource
	}{
		{
			name:   "nil resource",
			target: &schemaPlan{},
			scope:  []*schemaResource{existing},
		},
		{
			name:   "duplicate resource",
			target: &schemaPlan{resource: existing},
			scope:  []*schemaResource{existing},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := evaluationState{dynamicScope: append([]*schemaResource(nil), test.scope...)}
			if pushReferenceResource(test.target, &state) {
				t.Fatal("resource was pushed")
			}
			if len(state.dynamicScope) != len(test.scope) {
				t.Fatalf("scope length changed to %d", len(state.dynamicScope))
			}
		})
	}
}

func TestMathematicalIntegerUsesFractionDigitsBeforeScaling(t *testing.T) {
	t.Parallel()

	if !isInteger("1.20e1", Draft202012) {
		t.Fatal("1.20e1 was not recognized as the integer 12")
	}
}

func compileWithVocabularyPolicy(
	t *testing.T,
	dialect Dialect,
	schema string,
	policy vocabularyPolicy,
) error {
	t.Helper()
	if dialect == "" {
		dialect = Draft202012
	}
	root, err := decodeJSON(context.Background(), []byte(schema), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	compiler := newSchemaCompiler(
		context.Background(), root, dialect, DefaultLimits(), nil, len(schema),
		false, false, standardFormats(), nil,
	)
	compiler.vocabularyPolicies[compiler.resourceFor[root]] = policy
	_, err = compiler.compile(root)
	return err
}

func containsJSONValue(values []*jsonValue, wanted *jsonValue) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
