package jsonschema

import (
	"context"
	"errors"
	"testing"
)

func TestValidationAndEvaluationPropagateNestedFailures(t *testing.T) {
	t.Parallel()

	var nilSchema *Schema
	if _, err := nilSchema.Validate(context.Background(), []byte(`null`)); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("got %v, want ErrInvalidSchema", err)
	}
	schema := &Schema{dialect: Draft202012, limits: DefaultLimits(), plan: &schemaPlan{}}
	if _, err := schema.Validate(context.Background(), []byte(`{`)); !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("got %v, want ErrInvalidJSON", err)
	}

	sentinel := errors.New("nested evaluation failed")
	failing := evaluatorFailurePlan(sentinel)
	schema.plan = failing
	if _, err := schema.Validate(context.Background(), []byte(`null`)); !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want evaluator failure", err)
	}

	object := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"x": {kind: kindNull},
	}}
	array := &jsonValue{kind: kindArray, array: []*jsonValue{{kind: kindNull}}}
	scalar := &jsonValue{kind: kindNull}
	for _, test := range []struct {
		name     string
		plan     *schemaPlan
		instance *jsonValue
	}{
		{name: "schema type", plan: &schemaPlan{types: []typePlan{{schema: failing}}}, instance: scalar},
		{name: "schema disallow", plan: &schemaPlan{disallowedTypes: []typePlan{{schema: failing}}}, instance: scalar},
		{name: "oneOf", plan: &schemaPlan{oneOf: []*schemaPlan{failing}}, instance: scalar},
		{name: "not", plan: &schemaPlan{not: failing}, instance: scalar},
		{name: "contains", plan: &schemaPlan{contains: failing}, instance: array},
		{name: "property names", plan: &schemaPlan{propertyNames: failing}, instance: object},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
			if _, err := test.plan.evaluate(test.instance, Draft202012, &state); !errors.Is(err, sentinel) {
				t.Fatalf("got %v, want nested failure", err)
			}
		})
	}
}

func TestEvaluationCountersPreserveCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, consume := range []func(*evaluationState) error{
		func(state *evaluationState) error { return state.consumeOutputUnits(1) },
		func(state *evaluationState) error { return state.consumeUniqueComparison() },
		func(state *evaluationState) error { return state.consumeFormatCheck() },
		func(state *evaluationState) error { return state.consumeCustomKeywordCall() },
	} {
		state := evaluationState{ctx: ctx, limits: DefaultLimits()}
		if err := consume(&state); !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want cancellation", err)
		}
	}

	truth := true
	limits := DefaultLimits()
	limits.MaxDynamicScopeDepth = 1
	state := evaluationState{
		ctx:          context.Background(),
		limits:       limits,
		dynamicScope: []*schemaResource{{}},
	}
	if _, err := (&schemaPlan{reference: &schemaPlan{boolean: &truth}}).evaluate(
		&jsonValue{kind: kindNull}, Draft202012, &state,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want dynamic scope limit", err)
	}
}

func TestContentValidationCoversPermissiveAndStrictBranches(t *testing.T) {
	t.Parallel()

	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	for _, test := range []struct {
		name  string
		plan  *schemaPlan
		value string
		valid bool
	}{
		{name: "unknown encoding", plan: &schemaPlan{contentEncoding: "other"}, value: "value", valid: true},
		{name: "strict base64 padding bits", plan: &schemaPlan{contentEncoding: "base64"}, value: "AB==", valid: false},
		{name: "invalid media type", plan: &schemaPlan{contentMediaType: ";"}, value: "value", valid: true},
		{name: "non JSON media type", plan: &schemaPlan{contentMediaType: "text/plain"}, value: "value", valid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			valid, err := test.plan.validateContent(test.value, &state)
			if err != nil || valid != test.valid {
				t.Fatalf("got valid=%v err=%v, want %v", valid, err, test.valid)
			}
		})
	}
}

func TestEvaluatedLocationCollectorsPropagateBudgetFailures(t *testing.T) {
	t.Parallel()

	array := &jsonValue{kind: kindArray, array: []*jsonValue{{kind: kindNull}}}
	object := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"x": {kind: kindNull},
	}}
	truth := true
	resource := &schemaResource{}
	for _, test := range []struct {
		name     string
		plan     *schemaPlan
		instance *jsonValue
		items    bool
		maxOps   int
	}{
		{name: "item contains", plan: &schemaPlan{contains: evaluatorFailurePlan(errors.New("contains"))}, instance: array, items: true, maxOps: 100},
		{name: "item reference merge", plan: &schemaPlan{reference: &schemaPlan{}}, instance: array, items: true, maxOps: 1},
		{name: "item allOf evaluation", plan: &schemaPlan{allOf: []*schemaPlan{{}}}, instance: array, items: true, maxOps: 1},
		{name: "item allOf merge", plan: &schemaPlan{allOf: []*schemaPlan{{boolean: &truth}}}, instance: array, items: true, maxOps: 2},
		{name: "item anyOf evaluation", plan: &schemaPlan{anyOf: []*schemaPlan{{}}}, instance: array, items: true, maxOps: 1},
		{name: "item anyOf merge", plan: &schemaPlan{anyOf: []*schemaPlan{{boolean: &truth}}}, instance: array, items: true, maxOps: 2},
		{name: "item condition evaluation", plan: &schemaPlan{condition: &schemaPlan{}}, instance: array, items: true, maxOps: 1},
		{name: "item condition merge", plan: &schemaPlan{condition: &schemaPlan{boolean: &truth}}, instance: array, items: true, maxOps: 2},
		{name: "property reference merge", plan: &schemaPlan{reference: &schemaPlan{}}, instance: object, maxOps: 1},
		{name: "property dependency merge", plan: &schemaPlan{dependentSchemas: map[string]*schemaPlan{"x": {}}}, instance: object, maxOps: 1},
		{name: "property allOf evaluation", plan: &schemaPlan{allOf: []*schemaPlan{{}}}, instance: object, maxOps: 1},
		{name: "property allOf merge", plan: &schemaPlan{allOf: []*schemaPlan{{boolean: &truth}}}, instance: object, maxOps: 2},
		{name: "property anyOf evaluation", plan: &schemaPlan{anyOf: []*schemaPlan{{}}}, instance: object, maxOps: 1},
		{name: "property anyOf merge", plan: &schemaPlan{anyOf: []*schemaPlan{{boolean: &truth}}}, instance: object, maxOps: 2},
		{name: "property condition evaluation", plan: &schemaPlan{condition: &schemaPlan{}}, instance: object, maxOps: 1},
		{name: "property condition merge", plan: &schemaPlan{condition: &schemaPlan{boolean: &truth}}, instance: object, maxOps: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			limits.MaxEvaluationOps = test.maxOps
			state := evaluationState{ctx: context.Background(), limits: limits}
			var err error
			if test.items {
				_, err = test.plan.collectEvaluatedItems(test.instance, Draft202012, &state)
			} else {
				_, err = test.plan.collectEvaluatedProperties(test.instance, Draft202012, &state)
			}
			if err == nil {
				t.Fatal("expected evaluated-location failure")
			}
		})
	}

	for _, collect := range []func(*evaluationState) error{
		func(state *evaluationState) error {
			plan := &schemaPlan{resource: resource}
			_, err := plan.collectEvaluatedItems(
				&jsonValue{kind: kindNull}, Draft202012, state,
			)
			return err
		},
		func(state *evaluationState) error {
			plan := &schemaPlan{resource: resource}
			_, err := plan.collectEvaluatedProperties(
				&jsonValue{kind: kindNull}, Draft202012, state,
			)
			return err
		},
	} {
		state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
		if err := collect(&state); err != nil || len(state.dynamicScope) != 0 {
			t.Fatalf("resource scope was not restored: %#v, err=%v", state.dynamicScope, err)
		}
	}
}

func TestUnevaluatedAndPatternKeywordsPropagateTrackingFailures(t *testing.T) {
	t.Parallel()

	truth := true
	array := &jsonValue{kind: kindArray, array: []*jsonValue{{kind: kindNull}}}
	object := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"x": {kind: kindNull},
	}}
	for _, test := range []struct {
		name     string
		plan     *schemaPlan
		instance *jsonValue
	}{
		{
			name:     "unevaluated items",
			plan:     &schemaPlan{unevaluatedItems: &schemaPlan{boolean: &truth}},
			instance: array,
		},
		{
			name:     "unevaluated properties",
			plan:     &schemaPlan{unevaluatedProperties: &schemaPlan{boolean: &truth}},
			instance: object,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			limits.MaxEvaluationOps = 1
			state := evaluationState{ctx: context.Background(), limits: limits}
			if _, err := test.plan.evaluate(test.instance, Draft202012, &state); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("got %v, want tracking limit", err)
			}
		})
	}

	regexLimits := DefaultLimits()
	regexLimits.MaxRegexBacktracking = 32
	pattern, err := compilePatternWithLimits("(?:^){100}", regexLimits)
	if err != nil {
		t.Fatal(err)
	}
	patternPlan := &schemaPlan{patternProperties: []patternPropertyPlan{{
		name: "limited", pattern: pattern, schema: &schemaPlan{},
	}}}
	state := evaluationState{ctx: context.Background(), limits: regexLimits}
	if _, err := patternPlan.evaluate(object, Draft202012, &state); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want regex limit", err)
	}
	state = evaluationState{ctx: context.Background(), limits: regexLimits}
	if _, err := patternPlan.collectEvaluatedProperties(object, Draft202012, &state); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want tracking regex limit", err)
	}
}

func TestConditionalEvaluatedLocationMergesPropagateFailures(t *testing.T) {
	t.Parallel()

	truth := true
	array := &jsonValue{kind: kindArray, array: []*jsonValue{{kind: kindNull}}}
	object := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"x": {kind: kindNull},
	}}
	for _, test := range []struct {
		name     string
		instance *jsonValue
		collect  func(*schemaPlan, *evaluationState) error
	}{
		{
			name:     "items then branch",
			instance: array,
			collect: func(plan *schemaPlan, state *evaluationState) error {
				_, err := plan.collectEvaluatedItems(array, Draft202012, state)
				return err
			},
		},
		{
			name:     "properties then branch",
			instance: object,
			collect: func(plan *schemaPlan, state *evaluationState) error {
				_, err := plan.collectEvaluatedProperties(object, Draft202012, state)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := &schemaPlan{
				condition: &schemaPlan{boolean: &truth},
				then:      &schemaPlan{},
			}
			limits := DefaultLimits()
			limits.MaxEvaluationOps = 3
			state := evaluationState{ctx: context.Background(), limits: limits}
			if err := test.collect(plan, &state); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("got %v, want then-branch merge limit", err)
			}
		})
	}
}

func TestInternalTypeHelpersRejectImpossibleInputs(t *testing.T) {
	t.Parallel()

	if matchesType(&jsonValue{kind: kindNull}, "unknown", Draft202012) {
		t.Fatal("unknown type matched")
	}
	if isInteger("1e+", Draft202012) {
		t.Fatal("malformed exponent was treated as an integer")
	}
}

func evaluatorFailurePlan(err error) *schemaPlan {
	return &schemaPlan{custom: []compiledKeyword{{
		name: "failure",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			return KeywordResult{}, err
		}),
	}}}
}
