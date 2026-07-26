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

func containsJSONValue(values []*jsonValue, wanted *jsonValue) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
