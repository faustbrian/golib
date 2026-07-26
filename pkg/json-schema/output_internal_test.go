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

func boolPointer(value bool) *bool { return &value }

func stringPointer(value string) *string { return &value }
