package jsonschema

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestOutputTraversalPropagatesNestedEvaluatorFailures(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("nested evaluator failed")
	failing := &schemaPlan{custom: []compiledKeyword{{
		name: "failure",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			return KeywordResult{}, sentinel
		}),
	}}}
	truth := true
	condition := &schemaPlan{boolean: &truth}
	pattern, err := compilePattern(".*")
	if err != nil {
		t.Fatal(err)
	}
	object := &jsonValue{
		kind: kindObject,
		object: map[string]*jsonValue{
			"x": {kind: kindNumber, number: "1"},
		},
	}
	array := &jsonValue{
		kind:  kindArray,
		array: []*jsonValue{{kind: kindNumber, number: "1"}},
	}
	scalar := &jsonValue{kind: kindNumber, number: "1"}

	for _, test := range []struct {
		name     string
		plan     *schemaPlan
		instance *jsonValue
	}{
		{name: "reference", plan: &schemaPlan{reference: failing}, instance: scalar},
		{name: "allOf", plan: &schemaPlan{allOf: []*schemaPlan{failing}}, instance: scalar},
		{name: "not", plan: &schemaPlan{not: failing}, instance: scalar},
		{name: "condition", plan: &schemaPlan{condition: failing}, instance: scalar},
		{name: "then", plan: &schemaPlan{condition: condition, then: failing}, instance: scalar},
		{name: "type schema", plan: &schemaPlan{types: []typePlan{{schema: failing}}}, instance: scalar},
		{name: "disallow schema", plan: &schemaPlan{disallowedTypes: []typePlan{{schema: failing}}}, instance: scalar},
		{name: "property", plan: &schemaPlan{properties: map[string]*schemaPlan{"x": failing}}, instance: object},
		{
			name: "pattern property",
			plan: &schemaPlan{patternProperties: []patternPropertyPlan{{
				name: ".*", pattern: pattern, schema: failing,
			}}},
			instance: object,
		},
		{name: "additional property", plan: &schemaPlan{additionalProperties: failing}, instance: object},
		{name: "property name", plan: &schemaPlan{propertyNames: failing}, instance: object},
		{name: "dependent schema", plan: &schemaPlan{dependentSchemas: map[string]*schemaPlan{"x": failing}}, instance: object},
		{name: "unevaluated property", plan: &schemaPlan{unevaluatedProperties: failing}, instance: object},
		{name: "prefix item", plan: &schemaPlan{prefixItems: []*schemaPlan{failing}}, instance: array},
		{name: "items", plan: &schemaPlan{items: failing}, instance: array},
		{name: "unevaluated item", plan: &schemaPlan{unevaluatedItems: failing}, instance: array},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
			_, _, err := test.plan.collectOutput(
				test.instance, Draft202012, "", "", false, true, &state,
			)
			if !errors.Is(err, sentinel) {
				t.Fatalf("got %v, want nested evaluator failure", err)
			}
		})
	}
}

func TestAnnotationTraversalPropagatesNestedEvaluatorFailures(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("annotation evaluator failed")
	failing := &schemaPlan{custom: []compiledKeyword{{
		name: "failure",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			return KeywordResult{}, sentinel
		}),
	}}}
	truth := true
	condition := &schemaPlan{boolean: &truth}
	pattern, err := compilePattern(".*")
	if err != nil {
		t.Fatal(err)
	}
	object := &jsonValue{
		kind: kindObject,
		object: map[string]*jsonValue{
			"x": {kind: kindNumber, number: "1"},
		},
	}
	array := &jsonValue{
		kind:  kindArray,
		array: []*jsonValue{{kind: kindNumber, number: "1"}},
	}
	scalar := &jsonValue{kind: kindNumber, number: "1"}

	for _, test := range []struct {
		name     string
		plan     *schemaPlan
		instance *jsonValue
	}{
		{name: "reference", plan: &schemaPlan{reference: failing}, instance: scalar},
		{name: "allOf", plan: &schemaPlan{allOf: []*schemaPlan{failing}}, instance: scalar},
		{name: "anyOf", plan: &schemaPlan{anyOf: []*schemaPlan{failing}}, instance: scalar},
		{name: "condition", plan: &schemaPlan{condition: failing}, instance: scalar},
		{name: "then", plan: &schemaPlan{condition: condition, then: failing}, instance: scalar},
		{name: "property", plan: &schemaPlan{properties: map[string]*schemaPlan{"x": failing}}, instance: object},
		{
			name: "pattern property",
			plan: &schemaPlan{patternProperties: []patternPropertyPlan{{
				name: ".*", pattern: pattern, schema: failing,
			}}},
			instance: object,
		},
		{name: "additional property", plan: &schemaPlan{additionalProperties: failing}, instance: object},
		{name: "dependent schema", plan: &schemaPlan{dependentSchemas: map[string]*schemaPlan{"x": failing}}, instance: object},
		{name: "unevaluated property", plan: &schemaPlan{unevaluatedProperties: failing}, instance: object},
		{name: "prefix item", plan: &schemaPlan{prefixItems: []*schemaPlan{failing}}, instance: array},
		{name: "items", plan: &schemaPlan{items: failing}, instance: array},
		{name: "contains", plan: &schemaPlan{contains: failing}, instance: array},
		{name: "unevaluated item", plan: &schemaPlan{unevaluatedItems: failing}, instance: array},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
			_, err := test.plan.collectAnnotations(
				test.instance, Draft202012, "", &state,
			)
			if !errors.Is(err, sentinel) {
				t.Fatalf("got %v, want nested evaluator failure", err)
			}
		})
	}
}

func TestAnnotationTraversalPropagatesFailuresAfterFlagEvaluation(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("annotation collection failed")
	truth := true
	condition := &schemaPlan{boolean: &truth}
	pattern, err := compilePattern(".*")
	if err != nil {
		t.Fatal(err)
	}
	object := &jsonValue{
		kind: kindObject,
		object: map[string]*jsonValue{
			"x": {kind: kindNumber, number: "1"},
		},
	}
	array := &jsonValue{
		kind:  kindArray,
		array: []*jsonValue{{kind: kindNumber, number: "1"}},
	}
	scalar := &jsonValue{kind: kindNumber, number: "1"}

	for _, test := range []struct {
		name     string
		build    func(*schemaPlan) *schemaPlan
		instance *jsonValue
	}{
		{name: "reference", build: func(child *schemaPlan) *schemaPlan {
			return &schemaPlan{reference: child}
		}, instance: scalar},
		{name: "allOf", build: func(child *schemaPlan) *schemaPlan {
			return &schemaPlan{allOf: []*schemaPlan{child}}
		}, instance: scalar},
		{name: "anyOf", build: func(child *schemaPlan) *schemaPlan {
			return &schemaPlan{anyOf: []*schemaPlan{child}}
		}, instance: scalar},
		{name: "condition", build: func(child *schemaPlan) *schemaPlan {
			return &schemaPlan{condition: child}
		}, instance: scalar},
		{name: "then", build: func(child *schemaPlan) *schemaPlan {
			return &schemaPlan{condition: condition, then: child}
		}, instance: scalar},
		{name: "property", build: func(child *schemaPlan) *schemaPlan {
			return &schemaPlan{properties: map[string]*schemaPlan{"x": child}}
		}, instance: object},
		{name: "pattern property", build: func(child *schemaPlan) *schemaPlan {
			return &schemaPlan{patternProperties: []patternPropertyPlan{{
				name: ".*", pattern: pattern, schema: child,
			}}}
		}, instance: object},
		{name: "additional property", build: func(child *schemaPlan) *schemaPlan {
			return &schemaPlan{additionalProperties: child}
		}, instance: object},
		{name: "dependent schema", build: func(child *schemaPlan) *schemaPlan {
			return &schemaPlan{dependentSchemas: map[string]*schemaPlan{"x": child}}
		}, instance: object},
		{name: "unevaluated property", build: func(child *schemaPlan) *schemaPlan {
			return &schemaPlan{unevaluatedProperties: child}
		}, instance: object},
		{name: "prefix item", build: func(child *schemaPlan) *schemaPlan {
			return &schemaPlan{prefixItems: []*schemaPlan{child}}
		}, instance: array},
		{name: "items", build: func(child *schemaPlan) *schemaPlan {
			return &schemaPlan{items: child}
		}, instance: array},
		{name: "contains", build: func(child *schemaPlan) *schemaPlan {
			return &schemaPlan{contains: child}
		}, instance: array},
		{name: "unevaluated item", build: func(child *schemaPlan) *schemaPlan {
			return &schemaPlan{unevaluatedItems: child}
		}, instance: array},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			child := &schemaPlan{custom: []compiledKeyword{{
				name: "delayed",
				evaluator: KeywordEvaluatorFunc(func(
					context.Context, Value,
				) (KeywordResult, error) {
					calls++
					if calls == 1 {
						return KeywordResult{Valid: true}, nil
					}
					return KeywordResult{}, sentinel
				}),
			}}}
			state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
			_, err := test.build(child).collectAnnotations(
				test.instance, Draft202012, "", &state,
			)
			if !errors.Is(err, sentinel) {
				t.Fatalf("got %v after %d calls, want collection failure", err, calls)
			}
		})
	}
}

func TestCustomKeywordOutputRejectsCallbackAndAnnotationFailures(t *testing.T) {
	t.Parallel()

	instance := &jsonValue{kind: kindNull}
	for _, test := range []struct {
		name      string
		evaluator KeywordEvaluator
		limits    Limits
	}{
		{
			name: "callback error",
			evaluator: KeywordEvaluatorFunc(func(
				context.Context, Value,
			) (KeywordResult, error) {
				return KeywordResult{}, errors.New("callback failed")
			}),
			limits: DefaultLimits(),
		},
		{
			name: "annotation bytes",
			evaluator: KeywordEvaluatorFunc(func(
				context.Context, Value,
			) (KeywordResult, error) {
				return KeywordResult{Valid: true, Annotation: json.RawMessage(`"large"`)}, nil
			}),
			limits: func() Limits {
				limits := DefaultLimits()
				limits.MaxAnnotationBytes = 1
				return limits
			}(),
		},
		{
			name: "invalid annotation",
			evaluator: KeywordEvaluatorFunc(func(
				context.Context, Value,
			) (KeywordResult, error) {
				return KeywordResult{Valid: true, Annotation: json.RawMessage(`{`)}, nil
			}),
			limits: DefaultLimits(),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := &schemaPlan{custom: []compiledKeyword{{
				name: "custom", evaluator: test.evaluator,
			}}}
			state := evaluationState{ctx: context.Background(), limits: test.limits}
			if _, _, err := plan.collectOutput(
				instance, Draft202012, "", "", false, true, &state,
			); err == nil {
				t.Fatal("expected custom output error")
			}
		})
	}

	invalid := &schemaPlan{custom: []compiledKeyword{{
		name: "custom",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			return KeywordResult{Valid: false}, nil
		}),
	}}}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	errors, _, err := invalid.collectOutput(
		instance, Draft202012, "", "", false, true, &state,
	)
	if err != nil || len(errors) != 2 || errors[1].KeywordLocation != "/custom" {
		t.Fatalf("unexpected custom failure output %#v, err=%v", errors, err)
	}
}

func TestVerboseAnnotationCollectorRejectsLateCustomFailures(t *testing.T) {
	t.Parallel()

	instance := &jsonValue{kind: kindNull}
	for _, test := range []struct {
		name   string
		limits Limits
		late   KeywordResult
		err    error
	}{
		{
			name: "custom call budget",
			limits: func() Limits {
				limits := DefaultLimits()
				limits.MaxCustomKeywordCalls = 1
				return limits
			}(),
		},
		{name: "callback error", limits: DefaultLimits(), err: errors.New("late failure")},
		{name: "no annotation", limits: DefaultLimits(), late: KeywordResult{Valid: true}},
		{
			name: "annotation bytes",
			limits: func() Limits {
				limits := DefaultLimits()
				limits.MaxAnnotationBytes = 1
				return limits
			}(),
			late: KeywordResult{Valid: true, Annotation: json.RawMessage(`"large"`)},
		},
		{
			name:   "invalid annotation",
			limits: DefaultLimits(),
			late:   KeywordResult{Valid: true, Annotation: json.RawMessage(`{`)},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			plan := &schemaPlan{custom: []compiledKeyword{{
				name: "custom",
				evaluator: KeywordEvaluatorFunc(func(
					context.Context, Value,
				) (KeywordResult, error) {
					calls++
					if calls == 1 {
						return KeywordResult{Valid: true}, nil
					}
					return test.late, test.err
				}),
			}}}
			state := evaluationState{ctx: context.Background(), limits: test.limits}
			annotations, err := plan.collectAnnotations(
				instance, Draft202012, "", &state,
			)
			if test.name == "no annotation" {
				if err != nil || len(annotations) != 0 {
					t.Fatalf("got %#v, err=%v", annotations, err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected late annotation error")
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := evaluationState{ctx: ctx, limits: DefaultLimits()}
	if _, err := (&schemaPlan{}).collectAnnotations(
		instance, Draft202012, "", &state,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
}

func TestAnnotationCollectorCoversConditionalAndEmptyCollectionEdges(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("condition annotation failed")
	calls := 0
	condition := &schemaPlan{custom: []compiledKeyword{{
		name: "condition",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			calls++
			if calls < 3 {
				return KeywordResult{Valid: true}, nil
			}
			return KeywordResult{}, sentinel
		}),
	}}}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	if _, err := (&schemaPlan{condition: condition}).collectAnnotations(
		&jsonValue{kind: kindNull}, Draft202012, "", &state,
	); !errors.Is(err, sentinel) {
		t.Fatalf("got %v after %d calls, want condition failure", err, calls)
	}

	object := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"present": {kind: kindNull},
	}}
	array := &jsonValue{kind: kindArray}
	truth := true
	plan := &schemaPlan{
		dependentSchemas: map[string]*schemaPlan{"missing": {boolean: &truth}},
		prefixItems:      []*schemaPlan{{boolean: &truth}},
	}
	state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	if _, err := plan.collectObjectAnnotations(
		object, Draft202012, "", &state,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.collectArrayAnnotations(
		array, Draft202012, "", &state,
	); err != nil {
		t.Fatal(err)
	}
}

func TestValidateOutputPropagatesEachPhaseFailure(t *testing.T) {
	t.Parallel()

	baseSchema := func(plan *schemaPlan) *Schema {
		return &Schema{dialect: Draft202012, limits: DefaultLimits(), plan: plan}
	}
	var nilContext context.Context
	if _, err := baseSchema(&schemaPlan{}).ValidateOutput(
		nilContext, []byte(`null`), OutputBasic,
	); !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("got %v, want invalid JSON context", err)
	}

	evaluationFailure := errors.New("evaluation failed")
	failing := &schemaPlan{custom: []compiledKeyword{{
		name: "failure",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			return KeywordResult{}, evaluationFailure
		}),
	}}}
	if _, err := baseSchema(failing).ValidateOutput(
		context.Background(), []byte(`null`), OutputBasic,
	); !errors.Is(err, evaluationFailure) {
		t.Fatalf("got %v, want evaluation failure", err)
	}

	lateFailure := errors.New("verbose collection failed")
	calls := 0
	late := &schemaPlan{custom: []compiledKeyword{{
		name: "late",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			calls++
			if calls == 5 {
				return KeywordResult{}, lateFailure
			}
			return KeywordResult{Valid: true}, nil
		}),
	}}}
	if _, err := baseSchema(late).ValidateOutput(
		context.Background(), []byte(`null`), OutputVerbose,
	); !errors.Is(err, lateFailure) {
		t.Fatalf("got %v after %d calls, want verbose failure", err, calls)
	}

	calls = 0
	annotating := &schemaPlan{custom: []compiledKeyword{{
		name: "late",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			calls++
			result := KeywordResult{Valid: true}
			if calls == 5 {
				result.Annotation = json.RawMessage(`true`)
			}
			return result, nil
		}),
	}}}
	limited := baseSchema(annotating)
	limited.limits.MaxOutputUnits = 0
	if _, err := limited.ValidateOutput(
		context.Background(), []byte(`null`), OutputVerbose,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v after %d calls, want output limit", err, calls)
	}

	calls = 0
	changing := &schemaPlan{custom: []compiledKeyword{{
		name: "changing",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			calls++
			return KeywordResult{Valid: calls != 1}, nil
		}),
	}}}
	output, err := baseSchema(changing).ValidateOutput(
		context.Background(), []byte(`null`), OutputBasic,
	)
	if err != nil || len(output.Errors) != 1 || output.Errors[0].Error == "" {
		t.Fatalf("unexpected fallback output %#v, err=%v", output, err)
	}
	limitedFallback := baseSchema(changing)
	limitedFallback.limits.MaxOutputUnits = 0
	calls = 0
	if _, err := limitedFallback.ValidateOutput(
		context.Background(), []byte(`null`), OutputBasic,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want fallback output limit", err)
	}
}

func TestOutputCollectorEnforcesLateResourceLimits(t *testing.T) {
	t.Parallel()

	instance := &jsonValue{kind: kindNull}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := evaluationState{ctx: ctx, limits: DefaultLimits()}
	if _, _, err := (&schemaPlan{}).collectOutput(
		instance, Draft202012, "", "", false, true, &state,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}

	falsity := false
	limits := DefaultLimits()
	limits.MaxOutputUnits = 0
	state = evaluationState{ctx: context.Background(), limits: limits}
	if _, _, err := (&schemaPlan{boolean: &falsity}).collectOutput(
		instance, Draft202012, "", "", false, true, &state,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want output limit", err)
	}

	plan := &schemaPlan{custom: []compiledKeyword{{
		name: "custom",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			return KeywordResult{Valid: true}, nil
		}),
	}}}
	limits = DefaultLimits()
	limits.MaxCustomKeywordCalls = 0
	state = evaluationState{ctx: context.Background(), limits: limits}
	if _, _, err := plan.collectOutput(
		instance, Draft202012, "", "", false, true, &state,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want custom call limit", err)
	}
}

func TestCollectAnnotationsPropagatesFailures(t *testing.T) {
	t.Parallel()

	var nilSchema *Schema
	if _, err := nilSchema.CollectAnnotations(context.Background(), []byte(`null`)); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("got %v, want invalid schema", err)
	}
	schema := &Schema{
		dialect: Draft202012,
		limits:  DefaultLimits(),
		plan:    &schemaPlan{},
	}
	if _, err := schema.CollectAnnotations(context.Background(), []byte(`{`)); !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("got %v, want invalid JSON", err)
	}

	evaluationFailure := errors.New("annotation evaluation failed")
	schema.plan.custom = []compiledKeyword{{
		name: "failure",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			return KeywordResult{}, evaluationFailure
		}),
	}}
	if _, err := schema.CollectAnnotations(context.Background(), []byte(`null`)); !errors.Is(err, evaluationFailure) {
		t.Fatalf("got %v, want evaluation failure", err)
	}

	schema.plan = &schemaPlan{annotations: map[string]*jsonValue{
		"title": {kind: kindString, text: "bounded"},
	}}
	schema.limits.MaxOutputUnits = 0
	if _, err := schema.CollectAnnotations(context.Background(), []byte(`null`)); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want output limit", err)
	}
}

func TestOutputCollectorCoversDialectAndAssertionBranches(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		plan     *schemaPlan
		instance *jsonValue
		dialect  Dialect
	}{
		{
			name: "schema type matches",
			plan: &schemaPlan{types: []typePlan{{
				schema: &schemaPlan{boolean: boolPointer(true)},
			}}},
			instance: &jsonValue{kind: kindNull},
			dialect:  Draft3,
		},
		{
			name:     "modern exclusive minimum",
			plan:     &schemaPlan{minimums: []numberBound{{number: "1", exclusive: true}}},
			instance: &jsonValue{kind: kindNumber, number: "1"},
			dialect:  Draft202012,
		},
		{
			name:     "modern exclusive maximum",
			plan:     &schemaPlan{maximums: []numberBound{{number: "1", exclusive: true}}},
			instance: &jsonValue{kind: kindNumber, number: "1"},
			dialect:  Draft202012,
		},
		{
			name:     "draft 3 divisible by",
			plan:     &schemaPlan{multipleOf: "2"},
			instance: &jsonValue{kind: kindNumber, number: "3"},
			dialect:  Draft3,
		},
		{
			name:     "media type failure",
			plan:     &schemaPlan{contentMediaType: "application/json"},
			instance: &jsonValue{kind: kindString, text: "{"},
			dialect:  Draft202012,
		},
		{
			name:     "encoding failure",
			plan:     &schemaPlan{contentEncoding: "base64"},
			instance: &jsonValue{kind: kindString, text: "!"},
			dialect:  Draft202012,
		},
		{
			name: "explicit minimum contains",
			plan: &schemaPlan{
				contains:    &schemaPlan{boolean: boolPointer(false)},
				minContains: stringPointer("2"),
			},
			instance: &jsonValue{kind: kindArray, array: []*jsonValue{{kind: kindNull}}},
			dialect:  Draft202012,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
			_, _, err := test.plan.collectOutput(
				test.instance, test.dialect, "", "", false, true, &state,
			)
			if err != nil {
				t.Fatal(err)
			}
		})
	}

	patternLimits := DefaultLimits()
	patternLimits.MaxRegexBacktracking = 32
	pattern, err := compilePatternWithLimits("(?:^){100}", patternLimits)
	if err != nil {
		t.Fatal(err)
	}
	state := evaluationState{ctx: context.Background(), limits: patternLimits}
	if _, _, err := (&schemaPlan{pattern: pattern}).collectOutput(
		&jsonValue{kind: kindString}, Draft202012, "", "", false, true, &state,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want regular expression limit", err)
	}

	formatFailure := errors.New("format failed")
	for _, format := range []struct {
		name    string
		limits  Limits
		checker FormatChecker
	}{
		{
			name: "format budget",
			limits: func() Limits {
				limits := DefaultLimits()
				limits.MaxFormatChecks = 0
				return limits
			}(),
			checker: simpleFormatFunc(func(string) bool { return true }),
		},
		{
			name:   "format callback",
			limits: DefaultLimits(),
			checker: FormatFunc(func(context.Context, string) (bool, error) {
				return false, formatFailure
			}),
		},
	} {
		t.Run(format.name, func(t *testing.T) {
			state := evaluationState{ctx: context.Background(), limits: format.limits}
			_, _, err := (&schemaPlan{format: format.checker}).collectOutput(
				&jsonValue{kind: kindString}, Draft202012,
				"", "", false, true, &state,
			)
			if err == nil {
				t.Fatal("expected format output error")
			}
		})
	}

	contentLimits := DefaultLimits()
	contentLimits.MaxInputBytes = 1
	state = evaluationState{ctx: context.Background(), limits: contentLimits}
	if _, _, err := (&schemaPlan{contentMediaType: "application/json"}).collectOutput(
		&jsonValue{kind: kindString, text: "null"}, Draft202012,
		"", "", false, true, &state,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want content decode limit", err)
	}
}

func TestOutputCollectionCoversSkippedAndEvaluationTrackingEdges(t *testing.T) {
	t.Parallel()

	truth := true
	object := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"present": {kind: kindNull},
	}}
	array := &jsonValue{kind: kindArray}
	plan := &schemaPlan{
		dependentRequired: map[string][]string{"missing": {"dependency"}},
		dependentSchemas:  map[string]*schemaPlan{"missing": {boolean: &truth}},
		prefixItems:       []*schemaPlan{{boolean: &truth}},
	}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	if _, _, err := plan.collectOutput(
		object, Draft202012, "", "", false, true, &state,
	); err != nil {
		t.Fatal(err)
	}
	state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	if _, _, err := plan.collectOutput(
		array, Draft202012, "", "", false, true, &state,
	); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("evaluated locations failed")
	delayedChild := func() *schemaPlan {
		calls := 0
		return &schemaPlan{custom: []compiledKeyword{{
			name: "delayed",
			evaluator: KeywordEvaluatorFunc(func(
				context.Context, Value,
			) (KeywordResult, error) {
				calls++
				if calls == 1 {
					return KeywordResult{Valid: true}, nil
				}
				return KeywordResult{}, sentinel
			}),
		}}}
	}
	for _, test := range []struct {
		name     string
		plan     *schemaPlan
		instance *jsonValue
	}{
		{
			name: "object",
			plan: &schemaPlan{
				allOf:                 []*schemaPlan{delayedChild()},
				unevaluatedProperties: &schemaPlan{boolean: &truth},
			},
			instance: object,
		},
		{
			name: "array",
			plan: &schemaPlan{
				allOf:            []*schemaPlan{delayedChild()},
				unevaluatedItems: &schemaPlan{boolean: &truth},
			},
			instance: &jsonValue{kind: kindArray, array: []*jsonValue{{kind: kindNull}}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
			_, _, err := test.plan.collectOutput(
				test.instance, Draft202012, "", "", false, true, &state,
			)
			if !errors.Is(err, sentinel) {
				t.Fatalf("got %v, want evaluated-location failure", err)
			}
		})
	}
}

func TestJSONValueOutputCoversAllExactKinds(t *testing.T) {
	t.Parallel()

	for _, value := range []*jsonValue{
		{kind: kindNull},
		{kind: kindNumber, number: "1e100"},
		{kind: 255},
	} {
		_ = jsonValueOutput(value)
	}
}

func TestOutputTraversalPropagatesRegexAndTrackingLimits(t *testing.T) {
	t.Parallel()

	regexLimits := DefaultLimits()
	regexLimits.MaxRegexBacktracking = 32
	pattern, err := compilePatternWithLimits("(?:^){100}", regexLimits)
	if err != nil {
		t.Fatal(err)
	}
	plan := &schemaPlan{patternProperties: []patternPropertyPlan{{
		name: "limited", pattern: pattern, schema: &schemaPlan{},
	}}}
	object := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"x": {kind: kindNull},
	}}
	state := evaluationState{ctx: context.Background(), limits: regexLimits}
	if _, err := plan.collectObjectAnnotations(
		object, Draft202012, "", &state,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want annotation regex limit", err)
	}
	state = evaluationState{ctx: context.Background(), limits: regexLimits}
	if _, _, err := plan.collectOutput(
		object, Draft202012, "", "", false, true, &state,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want output regex limit", err)
	}
	plan.additionalProperties = &schemaPlan{}
	state = evaluationState{ctx: context.Background(), limits: regexLimits}
	if _, err := plan.verboseKeywordChildren(
		"additionalProperties", object, Draft202012, "", "", false, &state,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want verbose additionalProperties regex limit", err)
	}

	zeroOps := DefaultLimits()
	zeroOps.MaxEvaluationOps = 0
	for _, test := range []struct {
		name     string
		plan     *schemaPlan
		instance *jsonValue
		collect  func(*schemaPlan, *jsonValue, *evaluationState) error
	}{
		{
			name:     "object annotations",
			plan:     &schemaPlan{unevaluatedProperties: &schemaPlan{}},
			instance: object,
			collect: func(plan *schemaPlan, instance *jsonValue, state *evaluationState) error {
				_, err := plan.collectObjectAnnotations(instance, Draft202012, "", state)
				return err
			},
		},
		{
			name: "array annotations",
			plan: &schemaPlan{unevaluatedItems: &schemaPlan{}},
			instance: &jsonValue{
				kind: kindArray, array: []*jsonValue{{kind: kindNull}},
			},
			collect: func(plan *schemaPlan, instance *jsonValue, state *evaluationState) error {
				_, err := plan.collectArrayAnnotations(instance, Draft202012, "", state)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := evaluationState{ctx: context.Background(), limits: zeroOps}
			if err := test.collect(test.plan, test.instance, &state); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("got %v, want evaluation limit", err)
			}
		})
	}

	oneOp := DefaultLimits()
	oneOp.MaxEvaluationOps = 1
	for _, test := range []struct {
		name     string
		plan     *schemaPlan
		instance *jsonValue
	}{
		{
			name:     "object output tracking",
			plan:     &schemaPlan{unevaluatedProperties: &schemaPlan{}},
			instance: object,
		},
		{
			name: "array output tracking",
			plan: &schemaPlan{unevaluatedItems: &schemaPlan{}},
			instance: &jsonValue{
				kind: kindArray, array: []*jsonValue{{kind: kindNull}},
			},
		},
		{
			name:     "final flag evaluation",
			plan:     &schemaPlan{},
			instance: &jsonValue{kind: kindNull},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := evaluationState{ctx: context.Background(), limits: oneOp}
			if _, _, err := test.plan.collectOutput(
				test.instance, Draft202012, "", "", false, true, &state,
			); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("got %v, want evaluation limit", err)
			}
		})
	}
}

func TestOutputTraversalPropagatesContainsAndUniqueItemLimits(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("contains evaluator failed")
	failing := &schemaPlan{custom: []compiledKeyword{{
		name: "failure",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			return KeywordResult{}, sentinel
		}),
	}}}
	array := &jsonValue{kind: kindArray, array: []*jsonValue{
		{kind: kindNumber, number: "1"},
		{kind: kindNumber, number: "2"},
	}}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	if _, _, err := (&schemaPlan{contains: failing}).collectOutput(
		array, Draft202012, "", "", false, true, &state,
	); !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want contains failure", err)
	}

	limits := DefaultLimits()
	limits.MaxUniqueComparisons = 0
	state = evaluationState{ctx: context.Background(), limits: limits}
	if _, _, err := (&schemaPlan{uniqueItems: true}).collectOutput(
		array, Draft202012, "", "", false, true, &state,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want unique comparison limit", err)
	}
}

func TestVerboseTraversalSkipsEveryInapplicableChild(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("inapplicable child evaluated")
	failing := &schemaPlan{custom: []compiledKeyword{{
		name: "failure",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			return KeywordResult{}, sentinel
		}),
	}}}
	truth := true
	passing := &schemaPlan{boolean: &truth}
	pattern, err := compilePattern("^other$")
	if err != nil {
		t.Fatal(err)
	}
	object := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"present": {kind: kindNull},
	}}
	objectAB := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"a": {kind: kindNull},
		"b": {kind: kindNull},
	}}
	scalarWithObjectData := &jsonValue{kind: kindNull, object: object.object}
	array := &jsonValue{kind: kindArray, array: []*jsonValue{{kind: kindNull}}}
	arrayAB := &jsonValue{kind: kindArray, array: []*jsonValue{
		{kind: kindNull},
		{kind: kindNull},
	}}
	scalarWithArrayData := &jsonValue{kind: kindNull, array: array.array}

	tests := []struct {
		name         string
		keyword      string
		plan         *schemaPlan
		instance     *jsonValue
		wantChildren int
	}{
		{
			name:     "absent property",
			keyword:  "properties",
			plan:     &schemaPlan{properties: map[string]*schemaPlan{"missing": failing}},
			instance: object,
		},
		{
			name:    "property after absent property",
			keyword: "properties",
			plan: &schemaPlan{properties: map[string]*schemaPlan{
				"0-missing": failing,
				"a":         passing,
			}},
			instance:     objectAB,
			wantChildren: 1,
		},
		{
			name:    "nonmatching pattern property",
			keyword: "patternProperties",
			plan: &schemaPlan{patternProperties: []patternPropertyPlan{{
				name: "^other$", pattern: pattern, schema: failing,
			}}},
			instance: object,
		},
		{
			name:    "matching pattern after nonmatching pattern",
			keyword: "patternProperties",
			plan: &schemaPlan{patternProperties: []patternPropertyPlan{
				{name: "^other$", pattern: pattern, schema: failing},
				{name: "^[ab]$", pattern: mustCompilePattern(t, "^[ab]$"), schema: passing},
			}},
			instance:     objectAB,
			wantChildren: 2,
		},
		{
			name:    "configured additional property",
			keyword: "additionalProperties",
			plan: &schemaPlan{
				properties:           map[string]*schemaPlan{"present": passing},
				additionalProperties: failing,
			},
			instance: object,
		},
		{
			name:    "additional property after configured property",
			keyword: "additionalProperties",
			plan: &schemaPlan{
				properties:           map[string]*schemaPlan{"a": passing},
				additionalProperties: passing,
			},
			instance:     objectAB,
			wantChildren: 1,
		},
		{
			name:     "nil additional property schema",
			keyword:  "additionalProperties",
			plan:     &schemaPlan{},
			instance: object,
		},
		{
			name:    "present dependent schema after absent trigger",
			keyword: "dependentSchemas",
			plan: &schemaPlan{dependentSchemas: map[string]*schemaPlan{
				"0-missing": failing,
				"a":         passing,
			}},
			instance:     objectAB,
			wantChildren: 1,
		},
		{
			name:     "additional property on non-object",
			keyword:  "additionalProperties",
			plan:     &schemaPlan{additionalProperties: failing},
			instance: scalarWithObjectData,
		},
		{
			name:     "nil property names schema",
			keyword:  "propertyNames",
			plan:     &schemaPlan{},
			instance: object,
		},
		{
			name:    "unevaluated property after evaluated property",
			keyword: "unevaluatedProperties",
			plan: &schemaPlan{
				properties:            map[string]*schemaPlan{"a": passing},
				unevaluatedProperties: passing,
			},
			instance:     objectAB,
			wantChildren: 1,
		},
		{
			name:     "property names on non-object",
			keyword:  "propertyNames",
			plan:     &schemaPlan{propertyNames: failing},
			instance: scalarWithObjectData,
		},
		{
			name:     "absent dependent schema trigger",
			keyword:  "dependentSchemas",
			plan:     &schemaPlan{dependentSchemas: map[string]*schemaPlan{"missing": failing}},
			instance: object,
		},
		{
			name:    "evaluated unevaluated property",
			keyword: "unevaluatedProperties",
			plan: &schemaPlan{
				properties:            map[string]*schemaPlan{"present": passing},
				unevaluatedProperties: failing,
			},
			instance: object,
		},
		{
			name:     "nil unevaluated properties schema",
			keyword:  "unevaluatedProperties",
			plan:     &schemaPlan{},
			instance: object,
		},
		{
			name:     "unevaluated properties on non-object",
			keyword:  "unevaluatedProperties",
			plan:     &schemaPlan{unevaluatedProperties: failing},
			instance: scalarWithObjectData,
		},
		{
			name:         "prefix items stop at instance length",
			keyword:      "prefixItems",
			plan:         &schemaPlan{prefixItems: []*schemaPlan{passing, failing}},
			instance:     array,
			wantChildren: 1,
		},
		{
			name:     "nil items schema",
			keyword:  "items",
			plan:     &schemaPlan{},
			instance: array,
		},
		{
			name:     "items on non-array",
			keyword:  "items",
			plan:     &schemaPlan{items: failing},
			instance: scalarWithArrayData,
		},
		{
			name:     "nil additional items schema",
			keyword:  "additionalItems",
			plan:     &schemaPlan{},
			instance: array,
		},
		{
			name:     "additional items on non-array",
			keyword:  "additionalItems",
			plan:     &schemaPlan{items: failing},
			instance: scalarWithArrayData,
		},
		{
			name:     "nil contains schema",
			keyword:  "contains",
			plan:     &schemaPlan{},
			instance: array,
		},
		{
			name:     "contains on non-array",
			keyword:  "contains",
			plan:     &schemaPlan{contains: failing},
			instance: scalarWithArrayData,
		},
		{
			name:    "evaluated unevaluated item",
			keyword: "unevaluatedItems",
			plan: &schemaPlan{
				prefixItems:      []*schemaPlan{passing},
				unevaluatedItems: failing,
			},
			instance: array,
		},
		{
			name:    "unevaluated item after evaluated item",
			keyword: "unevaluatedItems",
			plan: &schemaPlan{
				prefixItems:      []*schemaPlan{passing},
				unevaluatedItems: passing,
			},
			instance:     arrayAB,
			wantChildren: 1,
		},
		{
			name:     "nil unevaluated items schema",
			keyword:  "unevaluatedItems",
			plan:     &schemaPlan{},
			instance: array,
		},
		{
			name:     "unevaluated items on non-array",
			keyword:  "unevaluatedItems",
			plan:     &schemaPlan{unevaluatedItems: failing},
			instance: scalarWithArrayData,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
			children, err := test.plan.verboseKeywordChildren(
				test.keyword, test.instance, Draft202012, "/"+test.keyword,
				"", false, &state,
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(children) != test.wantChildren {
				t.Fatalf("got %d children, want %d: %#v", len(children), test.wantChildren, children)
			}
		})
	}
}

func TestAnnotationAndOutputTraversalSkipUnappliedSchemas(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("unapplied schema evaluated")
	failing := &schemaPlan{custom: []compiledKeyword{{
		name: "failure",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			return KeywordResult{}, sentinel
		}),
	}}}
	truth := true
	passing := &schemaPlan{boolean: &truth}
	pattern, err := compilePattern("^other$")
	if err != nil {
		t.Fatal(err)
	}
	object := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"present": {kind: kindNumber, number: "1"},
	}}
	emptyArray := &jsonValue{kind: kindArray}

	plans := []*schemaPlan{
		{
			properties: map[string]*schemaPlan{"missing": failing},
			patternProperties: []patternPropertyPlan{{
				name: "^other$", pattern: pattern, schema: failing,
			}},
			dependentRequired: map[string][]string{"missing": {"dependency"}},
			dependentSchemas:  map[string]*schemaPlan{"missing": failing},
		},
		{
			properties:            map[string]*schemaPlan{"present": passing},
			unevaluatedProperties: failing,
		},
	}
	for index, plan := range plans {
		state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
		if _, err := plan.collectObjectAnnotations(object, Draft202012, "", &state); err != nil {
			t.Fatalf("annotations plan %d: %v", index, err)
		}
		state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
		if _, _, err := plan.collectOutput(
			object, Draft202012, "", "", false, true, &state,
		); err != nil {
			t.Fatalf("output plan %d: %v", index, err)
		}
	}

	arrayPlan := &schemaPlan{prefixItems: []*schemaPlan{failing}}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	if _, err := arrayPlan.collectArrayAnnotations(
		emptyArray, Draft202012, "", &state,
	); err != nil {
		t.Fatal(err)
	}
	state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	if _, _, err := arrayPlan.collectOutput(
		emptyArray, Draft202012, "", "", false, true, &state,
	); err != nil {
		t.Fatal(err)
	}
}

func TestOutputKeywordFilteringPreservesOnlyEvaluatedContent(t *testing.T) {
	t.Parallel()

	object := map[string]*jsonValue{
		"$anchor": {kind: kindString, text: "node"},
		"$defs":   {kind: kindObject},
		"type":    {kind: kindString, text: "null"},
	}
	keywords := standardOutputKeywords(object, Draft202012)
	if len(keywords) != 2 || keywords[0] != "$defs" || keywords[1] != "type" {
		t.Fatalf("unexpected output keywords %#v", keywords)
	}
	for _, definitionKeyword := range []string{"$defs", "definitions"} {
		if outputUnitCoveredByKeywords(
			OutputUnit{KeywordLocation: "/" + definitionKeyword + "/x"},
			"", []string{definitionKeyword},
		) {
			t.Fatalf("%s incorrectly covered an output unit", definitionKeyword)
		}
		if !outputUnitCoveredByKeywords(
			OutputUnit{KeywordLocation: "/type"},
			"", []string{definitionKeyword, "type"},
		) {
			t.Fatalf("keyword after %s was not considered", definitionKeyword)
		}
	}

	plan := &schemaPlan{
		annotations: map[string]*jsonValue{
			"format": {kind: kindString, text: "email"},
		},
	}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	_, annotations, err := plan.collectOutput(
		&jsonValue{kind: kindNumber, number: "1"},
		Draft202012, "", "", false, true, &state,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 0 {
		t.Fatalf("inapplicable annotation emitted: %#v", annotations)
	}
}

func TestVerboseReferenceSelectionRequiresKeywordAndTarget(t *testing.T) {
	t.Parallel()

	instance := &jsonValue{kind: kindNull}
	for _, plan := range []*schemaPlan{
		{
			outputKeywords:   []string{"$ref"},
			referenceKeyword: "$ref",
		},
		{
			outputKeywords:   []string{"type"},
			referenceKeyword: "$ref",
			reference:        &schemaPlan{boolean: boolPointer(true)},
		},
	} {
		state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
		units, err := plan.verboseOutputUnits(
			instance, nil, nil, "", "", false, Draft202012, &state,
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(units) != 1 || units[0].KeywordLocation != "/"+plan.outputKeywords[0] {
			t.Fatalf("unexpected units %#v", units)
		}
	}
}

func TestDetailedErrorNestingStopsAtSiblingBoundaries(t *testing.T) {
	t.Parallel()

	flat := []OutputUnit{
		{KeywordLocation: "/allOf/0", Error: "branch"},
		{
			KeywordLocation: "/allOf/0/type",
			Error:           "type",
			Annotations: []OutputUnit{{
				KeywordLocation: "/allOf/0/type/title",
				Annotation:      "nested",
			}},
		},
		{KeywordLocation: "/required", Error: "required"},
	}
	result, consumed := nestOutputErrors(flat, "")
	if consumed != len(flat) || len(result) != 2 || len(result[0].Errors) != 1 ||
		result[1].KeywordLocation != "/required" {
		t.Fatalf("consumed=%d result=%#v", consumed, result)
	}
	if countOutputUnits(result) != 4 {
		t.Fatalf("unexpected recursive output unit count %d", countOutputUnits(result))
	}
}

func TestVerboseReferenceTraversalContinuesWithSiblingKeywords(t *testing.T) {
	t.Parallel()

	truth := true
	plan := &schemaPlan{
		outputKeywords:   []string{"$ref", "type"},
		referenceKeyword: "$ref",
		reference:        &schemaPlan{boolean: &truth},
	}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	units, err := plan.verboseOutputUnits(
		&jsonValue{kind: kindNull}, nil, nil, "", "", false, Draft202012, &state,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 || units[0].KeywordLocation != "/$ref" ||
		units[1].KeywordLocation != "/type" {
		t.Fatalf("unexpected units %#v", units)
	}
}

func TestVerboseAnnotationFilteringUsesEachOwnershipRule(t *testing.T) {
	t.Parallel()

	value := &jsonValue{kind: kindString, text: "same"}
	tests := []struct {
		name       string
		plan       *schemaPlan
		annotation OutputUnit
		referenced bool
		wantUnits  int
	}{
		{
			name: "covered by keyword",
			plan: &schemaPlan{outputKeywords: []string{"type"}},
			annotation: OutputUnit{
				Valid: true, KeywordLocation: "/type/detail", Annotation: "same",
			},
			wantUnits: 2,
		},
		{
			name: "duplicate nested annotation",
			plan: &schemaPlan{
				outputKeywords: []string{"title"},
				annotations:    map[string]*jsonValue{"title": value},
			},
			annotation: OutputUnit{
				Valid: true, KeywordLocation: "/else/title", Annotation: "same",
			},
			wantUnits: 2,
		},
		{
			name: "direct referenced annotation",
			plan: &schemaPlan{
				annotations: map[string]*jsonValue{"title": value},
			},
			annotation: OutputUnit{
				Valid: true, KeywordLocation: "/title", Annotation: "same",
			},
			referenced: true,
			wantUnits:  1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
			units, err := test.plan.verboseOutputUnits(
				&jsonValue{kind: kindNull}, nil, []OutputUnit{
					test.annotation,
					{Valid: true, KeywordLocation: "/free", Annotation: "free"},
				},
				"", "", test.referenced, Draft202012, &state,
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(units) != test.wantUnits {
				t.Fatalf("got %d units, want %d: %#v", len(units), test.wantUnits, units)
			}
		})
	}
}

func TestCustomAnnotationTraversalContinuesAfterEmptyResults(t *testing.T) {
	t.Parallel()

	plan := &schemaPlan{custom: []compiledKeyword{
		{
			name: "empty",
			evaluator: KeywordEvaluatorFunc(func(
				context.Context, Value,
			) (KeywordResult, error) {
				return KeywordResult{Valid: true}, nil
			}),
		},
		{
			name: "annotated",
			evaluator: KeywordEvaluatorFunc(func(
				context.Context, Value,
			) (KeywordResult, error) {
				return KeywordResult{Valid: true, Annotation: json.RawMessage(`true`)}, nil
			}),
		},
	}}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	annotations, err := plan.collectAnnotations(
		&jsonValue{kind: kindNull}, Draft202012, "", &state,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 1 || annotations[0].KeywordLocation != "/annotated" {
		t.Fatalf("unexpected annotations %#v", annotations)
	}
}

func TestOutputTraversalContinuesAfterSkippedCollectionEntries(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("later entry evaluated")
	delayedFailure := func() *schemaPlan {
		calls := 0
		return &schemaPlan{custom: []compiledKeyword{{
			name: "failure",
			evaluator: KeywordEvaluatorFunc(func(
				context.Context, Value,
			) (KeywordResult, error) {
				calls++
				if calls == 1 {
					return KeywordResult{Valid: true}, nil
				}
				return KeywordResult{}, sentinel
			}),
		}}}
	}
	patternMissing, err := compilePattern("^a$")
	if err != nil {
		t.Fatal(err)
	}
	patternPresent, err := compilePattern("^b$")
	if err != nil {
		t.Fatal(err)
	}
	object := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"b": {kind: kindNull},
	}}

	tests := []struct {
		name  string
		build func() *schemaPlan
	}{
		{
			name: "properties",
			build: func() *schemaPlan {
				return &schemaPlan{properties: map[string]*schemaPlan{
					"a": {},
					"b": delayedFailure(),
				}}
			},
		},
		{
			name: "pattern properties",
			build: func() *schemaPlan {
				return &schemaPlan{patternProperties: []patternPropertyPlan{
					{name: "^a$", pattern: patternMissing, schema: &schemaPlan{}},
					{name: "^b$", pattern: patternPresent, schema: delayedFailure()},
				}}
			},
		},
		{
			name: "dependent schemas",
			build: func() *schemaPlan {
				return &schemaPlan{dependentSchemas: map[string]*schemaPlan{
					"a": {},
					"b": delayedFailure(),
				}}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
			_, _, err := test.build().collectOutput(
				object, Draft202012, "", "", false, true, &state,
			)
			if !errors.Is(err, sentinel) {
				t.Fatalf("got %v, want later-entry failure", err)
			}
		})
	}

	plan := &schemaPlan{dependentRequired: map[string][]string{
		"a": {"ignored"},
		"b": {"required"},
	}}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	errorsFound, _, err := plan.collectOutput(
		object, Draft202012, "", "", false, true, &state,
	)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, outputError := range errorsFound {
		found = found || outputError.KeywordLocation == "/dependentRequired"
	}
	if !found {
		t.Fatalf("dependent requirement after absent trigger was omitted: %#v", errorsFound)
	}
}

func TestAnnotationTraversalContinuesAfterSkippedCollectionEntries(t *testing.T) {
	t.Parallel()

	annotating := &schemaPlan{annotations: map[string]*jsonValue{
		"title": {kind: kindString, text: "later"},
	}}
	patternMissing, err := compilePattern("^a$")
	if err != nil {
		t.Fatal(err)
	}
	patternPresent, err := compilePattern("^b$")
	if err != nil {
		t.Fatal(err)
	}
	instance := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"b": {kind: kindNull},
	}}
	for _, plan := range []*schemaPlan{
		{
			patternProperties: []patternPropertyPlan{
				{name: "^a$", pattern: patternMissing, schema: &schemaPlan{}},
				{name: "^b$", pattern: patternPresent, schema: annotating},
			},
		},
		{
			dependentSchemas: map[string]*schemaPlan{
				"a": {},
				"b": annotating,
			},
		},
	} {
		state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
		annotations, err := plan.collectObjectAnnotations(
			instance, Draft202012, "", &state,
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(annotations) != 1 || annotations[0].KeywordLocation != "/title" {
			t.Fatalf("unexpected annotations %#v", annotations)
		}
	}
}

func TestUnevaluatedOutputContinuesAfterEvaluatedEntries(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("unevaluated property evaluated")
	calls := 0
	failing := &schemaPlan{custom: []compiledKeyword{{
		name: "failure",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			calls++
			if calls == 1 {
				return KeywordResult{Valid: true}, nil
			}
			return KeywordResult{}, sentinel
		}),
	}}}
	plan := &schemaPlan{
		properties:            map[string]*schemaPlan{"a": {}},
		unevaluatedProperties: failing,
	}
	instance := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"a": {kind: kindNull},
		"b": {kind: kindNull},
	}}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	_, _, err := plan.collectOutput(
		instance, Draft202012, "", "", false, true, &state,
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want unevaluated-property failure", err)
	}
}

func TestApplicableAnnotationsContinueAfterInapplicableKeywords(t *testing.T) {
	t.Parallel()

	plan := &schemaPlan{annotations: map[string]*jsonValue{
		"format": {kind: kindString, text: "email"},
		"title":  {kind: kindString, text: "number"},
	}}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	_, annotations, err := plan.collectOutput(
		&jsonValue{kind: kindNumber, number: "1"},
		Draft202012, "", "", false, true, &state,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 1 || annotations[0].KeywordLocation != "/title" {
		t.Fatalf("unexpected annotations %#v", annotations)
	}
}

func TestVerboseTupleTraversalStopsAtInstanceLength(t *testing.T) {
	t.Parallel()

	truth := true
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	children, err := verboseTupleItems(
		[]*schemaPlan{{boolean: &truth}, {boolean: &truth}},
		&jsonValue{kind: kindArray, array: []*jsonValue{{kind: kindNull}}},
		Draft7, "/items", "", false, &state,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 {
		t.Fatalf("got %d children, want 1", len(children))
	}
}

func boolPointer(value bool) *bool { return &value }

func stringPointer(value string) *string { return &value }

func mustCompilePattern(t *testing.T, expression string) *ecmaPattern {
	t.Helper()
	pattern, err := compilePattern(expression)
	if err != nil {
		t.Fatal(err)
	}
	return pattern
}
