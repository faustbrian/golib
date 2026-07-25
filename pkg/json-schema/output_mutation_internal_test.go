package jsonschema

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestOutputBoundaryHelpersAreExact(t *testing.T) {
	t.Parallel()

	if !annotationWithinLimit([]byte("x"), 1) || annotationWithinLimit([]byte("xx"), 1) {
		t.Fatal("annotation byte limit was not inclusive")
	}
	for _, test := range []struct {
		comparison int
		exclusive  bool
		below      bool
		above      bool
	}{
		{comparison: -1, below: true},
		{comparison: 0},
		{comparison: 0, exclusive: true, below: true, above: true},
		{comparison: 1, above: true},
	} {
		if got := belowMinimum(test.comparison, test.exclusive); got != test.below {
			t.Errorf("below comparison=%d exclusive=%v: got %v", test.comparison, test.exclusive, got)
		}
		if got := aboveMaximum(test.comparison, test.exclusive); got != test.above {
			t.Errorf("above comparison=%d exclusive=%v: got %v", test.comparison, test.exclusive, got)
		}
	}
	if boundKeyword("minimum", true, Draft3) != "minimum" ||
		boundKeyword("maximum", true, Draft4) != "maximum" ||
		boundKeyword("minimum", true, Draft6) != "exclusiveMinimum" ||
		boundKeyword("maximum", false, Draft202012) != "maximum" {
		t.Fatal("unexpected numeric bound keyword")
	}
	one, two := "1", "2"
	if belowConfiguredMinimum("1", nil) || belowConfiguredMinimum("1", &one) ||
		!belowConfiguredMinimum("1", &two) {
		t.Fatal("unexpected configured minimum comparison")
	}
	if aboveConfiguredMaximum("1", nil) || aboveConfiguredMaximum("1", &one) ||
		!aboveConfiguredMaximum("2", &one) {
		t.Fatal("unexpected configured maximum comparison")
	}
	if outsideConfiguredCardinality("1", "1", nil) ||
		outsideConfiguredCardinality("1", "0", &one) ||
		!outsideConfiguredCardinality("0", "1", nil) ||
		!outsideConfiguredCardinality("2", "0", &one) {
		t.Fatal("unexpected configured cardinality comparison")
	}
}

func TestVerboseAnnotationHelpersAreExact(t *testing.T) {
	t.Parallel()

	plan := &schemaPlan{annotations: map[string]*jsonValue{
		"title": {kind: kindString, text: "value"},
	}}
	annotation := OutputUnit{
		Valid:            true,
		KeywordLocation:  "/target/title",
		InstanceLocation: "/x",
		Annotation:       "value",
	}
	if plan.ownsDirectAnnotation(annotation, "") ||
		plan.ownsDirectAnnotation(OutputUnit{KeywordLocation: "title"}, "") ||
		!plan.ownsDirectAnnotation(OutputUnit{
			KeywordLocation: "/title", InstanceLocation: "/x",
		}, "/x") ||
		!plan.ownsDirectAnnotation(annotation, "/x") {
		t.Fatal("direct annotation ownership was not exact")
	}
	if unitKeywordSuffix("title") != "title" ||
		unitKeywordSuffix("/target/title") != "/title" {
		t.Fatal("unexpected keyword suffix")
	}
	if outputContainsAnnotationAt(nil, annotation) {
		t.Fatal("empty output unexpectedly contained an annotation")
	}
	matchingAbsolute := annotation
	matchingAbsolute.AbsoluteKeywordLocation = "https://example.test/schema#/title"
	if !outputContainsAnnotationAt([]OutputUnit{matchingAbsolute}, matchingAbsolute) {
		t.Fatal("absolute annotation location was not recognized")
	}
	nested := []OutputUnit{{Errors: []OutputUnit{{Annotations: []OutputUnit{{
		Valid:            true,
		KeywordLocation:  "/other/title",
		InstanceLocation: "/x",
		Annotation:       "value",
	}}}}}}
	if !outputContainsAnnotationAt(nested, annotation) {
		t.Fatal("equivalent nested annotation was not recognized")
	}
	annotationCases := []struct {
		name       string
		unit       OutputUnit
		annotation OutputUnit
		want       bool
	}{
		{
			name: "same absolute location with different values",
			unit: OutputUnit{
				AbsoluteKeywordLocation: "https://example.test/schema#/title",
				KeywordLocation:         "/one/title", InstanceLocation: "/x",
				Annotation: "other",
			},
			annotation: OutputUnit{
				AbsoluteKeywordLocation: "https://example.test/schema#/title",
				KeywordLocation:         "/two/title", InstanceLocation: "/x",
				Annotation: "value",
			},
			want: true,
		},
		{
			name: "unit absolute only with different value",
			unit: OutputUnit{
				AbsoluteKeywordLocation: "https://example.test/schema#/title",
				KeywordLocation:         "/title", InstanceLocation: "/x",
				Annotation: "other",
			},
			annotation: OutputUnit{
				KeywordLocation: "/title", InstanceLocation: "/x",
				Annotation: "value",
			},
			want: false,
		},
		{
			name: "annotation absolute only with different value",
			unit: OutputUnit{
				KeywordLocation: "/title", InstanceLocation: "/x",
				Annotation: "other",
			},
			annotation: OutputUnit{
				AbsoluteKeywordLocation: "https://example.test/schema#/title",
				KeywordLocation:         "/title", InstanceLocation: "/x",
				Annotation: "value",
			},
			want: false,
		},
		{
			name: "different absolute locations and values",
			unit: OutputUnit{
				AbsoluteKeywordLocation: "https://example.test/one#/title",
				KeywordLocation:         "/one/title", InstanceLocation: "/x",
				Annotation: "other",
			},
			annotation: OutputUnit{
				AbsoluteKeywordLocation: "https://example.test/two#/title",
				KeywordLocation:         "/two/title", InstanceLocation: "/x",
				Annotation: "value",
			},
			want: false,
		},
		{
			name: "same relative location with different values",
			unit: OutputUnit{
				KeywordLocation: "/title", InstanceLocation: "/x",
				Annotation: "other",
			},
			annotation: OutputUnit{
				KeywordLocation: "/title", InstanceLocation: "/x",
				Annotation: "value",
			},
			want: true,
		},
	}
	for _, test := range annotationCases {
		if got := outputContainsAnnotationAt(
			[]OutputUnit{test.unit}, test.annotation,
		); got != test.want {
			t.Fatalf("%s: got %t, want %t", test.name, got, test.want)
		}
	}
	if outputContainsAnnotationAt(nested, OutputUnit{
		KeywordLocation: "/other/title", InstanceLocation: "/y", Annotation: "value",
	}) {
		t.Fatal("annotation at another instance was treated as equivalent")
	}
	if outputUnitCoveredByKeywords(annotation, "", []string{"$defs"}) ||
		!outputUnitCoveredByKeywords(OutputUnit{
			KeywordLocation: "/target",
		}, "", []string{"target"}) ||
		!outputUnitCoveredByKeywords(annotation, "", []string{"target"}) {
		t.Fatal("passive keyword coverage was not distinguished")
	}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	if err := consumeVerboseOutputUnits(
		&state, nil, []OutputUnit{{Valid: false}}, nil,
	); err != nil || state.outputUnits != 0 {
		t.Fatalf("negative verbose delta was not clamped: %v, %#v", err, state)
	}
	state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	if err := consumeVerboseOutputUnits(
		&state,
		[]OutputUnit{{Valid: true}, {Valid: true}, {Valid: true}},
		[]OutputUnit{{Valid: false}},
		[]OutputUnit{{Valid: true}},
	); err != nil || state.outputUnits != 1 {
		t.Fatalf("positive verbose delta was not consumed: %v, %#v", err, state)
	}
	directError := OutputUnit{
		Valid: false, KeywordLocation: "/type", Error: "direct",
	}
	if got := (&schemaPlan{}).verboseKeywordError(
		"type", "/type", "", false, []OutputUnit{directError},
	); !reflect.DeepEqual(got, directError) {
		t.Fatalf("direct keyword error was wrapped: %#v", got)
	}
	referencedError := (&schemaPlan{
		location: "/target", absoluteBase: "https://example.test/schema",
	}).verboseKeywordError(
		"type", "/path/type", "", true,
		[]OutputUnit{{KeywordLocation: "/path/type/child"}},
	)
	if referencedError.AbsoluteKeywordLocation !=
		"https://example.test/schema#/target/type" {
		t.Fatalf("unexpected referenced error %#v", referencedError)
	}
	if !reflect.DeepEqual(outputUnitsWithin(nil, "/x"), []OutputUnit{}) {
		t.Fatal("empty filtering must return a detached empty slice")
	}
}

func TestVerboseApplicatorHelpersPropagateChildFailures(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("verbose child failure")
	failing := &schemaPlan{custom: []compiledKeyword{{
		name: "failure",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			return KeywordResult{}, sentinel
		}),
	}}}
	pattern, err := compilePattern("^x")
	if err != nil {
		t.Fatal(err)
	}
	object := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"x": {kind: kindNull},
	}}
	array := &jsonValue{kind: kindArray, array: []*jsonValue{{kind: kindNull}}}
	arrayWithExtra := &jsonValue{kind: kindArray, array: []*jsonValue{
		{kind: kindNull}, {kind: kindNull},
	}}
	truth := true
	for _, test := range []struct {
		name    string
		keyword string
		plan    *schemaPlan
		value   *jsonValue
		dialect Dialect
	}{
		{name: "logical", keyword: "allOf", plan: &schemaPlan{allOf: []*schemaPlan{failing}}, value: &jsonValue{kind: kindNull}},
		{name: "single", keyword: "not", plan: &schemaPlan{not: failing}, value: &jsonValue{kind: kindNull}},
		{name: "property", keyword: "properties", plan: &schemaPlan{properties: map[string]*schemaPlan{"x": failing}}, value: object},
		{name: "pattern", keyword: "patternProperties", plan: &schemaPlan{patternProperties: []patternPropertyPlan{{name: "^x", pattern: pattern, schema: failing}}}, value: object},
		{name: "additional", keyword: "additionalProperties", plan: &schemaPlan{additionalProperties: failing}, value: object},
		{name: "property name", keyword: "propertyNames", plan: &schemaPlan{propertyNames: failing}, value: object},
		{name: "dependent", keyword: "dependentSchemas", plan: &schemaPlan{dependentSchemas: map[string]*schemaPlan{"x": failing}}, value: object},
		{name: "unevaluated property", keyword: "unevaluatedProperties", plan: &schemaPlan{unevaluatedProperties: failing}, value: object},
		{name: "prefix", keyword: "prefixItems", plan: &schemaPlan{prefixItems: []*schemaPlan{failing}}, value: array},
		{name: "items", keyword: "items", plan: &schemaPlan{items: failing}, value: array},
		{name: "tuple", keyword: "items", plan: &schemaPlan{prefixItems: []*schemaPlan{failing}}, value: array, dialect: Draft7},
		{name: "additional item", keyword: "additionalItems", plan: &schemaPlan{prefixItems: []*schemaPlan{{boolean: &truth}}, items: failing}, value: arrayWithExtra, dialect: Draft7},
		{name: "contains", keyword: "contains", plan: &schemaPlan{contains: failing}, value: array},
		{name: "unevaluated item", keyword: "unevaluatedItems", plan: &schemaPlan{unevaluatedItems: failing}, value: array},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dialect := test.dialect
			if dialect == "" {
				dialect = Draft202012
			}
			state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
			_, err := test.plan.verboseKeywordChildren(
				test.keyword, test.value, dialect, "/"+test.keyword,
				"", false, &state,
			)
			if !errors.Is(err, sentinel) {
				t.Fatalf("got %v, want child failure", err)
			}
		})
	}
}

func TestVerboseApplicatorHelpersIgnoreInapplicableInstances(t *testing.T) {
	t.Parallel()

	truth := true
	pattern, err := compilePattern("^x")
	if err != nil {
		t.Fatal(err)
	}
	object := &jsonValue{kind: kindObject, object: map[string]*jsonValue{}}
	array := &jsonValue{kind: kindArray}
	for _, test := range []struct {
		name    string
		keyword string
		plan    *schemaPlan
		value   *jsonValue
		dialect Dialect
	}{
		{name: "properties type", keyword: "properties", plan: &schemaPlan{}, value: array},
		{name: "missing property", keyword: "properties", plan: &schemaPlan{properties: map[string]*schemaPlan{"x": {boolean: &truth}}}, value: object},
		{name: "patterns type", keyword: "patternProperties", plan: &schemaPlan{}, value: array},
		{name: "unmatched pattern", keyword: "patternProperties", plan: &schemaPlan{patternProperties: []patternPropertyPlan{{name: "^x", pattern: pattern, schema: &schemaPlan{boolean: &truth}}}}, value: &jsonValue{kind: kindObject, object: map[string]*jsonValue{"y": {kind: kindNull}}}},
		{name: "additional type", keyword: "additionalProperties", plan: &schemaPlan{}, value: array},
		{name: "property names type", keyword: "propertyNames", plan: &schemaPlan{}, value: array},
		{name: "dependencies type", keyword: "dependentSchemas", plan: &schemaPlan{}, value: array},
		{name: "missing dependency", keyword: "dependentSchemas", plan: &schemaPlan{dependentSchemas: map[string]*schemaPlan{"x": {boolean: &truth}}}, value: object},
		{name: "unevaluated properties type", keyword: "unevaluatedProperties", plan: &schemaPlan{}, value: array},
		{name: "prefix type", keyword: "prefixItems", plan: &schemaPlan{}, value: object},
		{name: "prefix exhausted", keyword: "prefixItems", plan: &schemaPlan{prefixItems: []*schemaPlan{{boolean: &truth}}}, value: array},
		{name: "items type", keyword: "items", plan: &schemaPlan{}, value: object},
		{name: "missing items", keyword: "items", plan: &schemaPlan{}, value: array},
		{name: "additional items type", keyword: "additionalItems", plan: &schemaPlan{}, value: object},
		{name: "contains type", keyword: "contains", plan: &schemaPlan{}, value: object},
		{name: "unevaluated items type", keyword: "unevaluatedItems", plan: &schemaPlan{}, value: object},
		{name: "unknown", keyword: "unknown", plan: &schemaPlan{}, value: object},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dialect := test.dialect
			if dialect == "" {
				dialect = Draft202012
			}
			state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
			children, err := test.plan.verboseKeywordChildren(
				test.keyword, test.value, dialect, "/"+test.keyword,
				"", false, &state,
			)
			if err != nil || len(children) != 0 {
				t.Fatalf("got %#v, %v", children, err)
			}
		})
	}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	if children, err := verboseSingleSchema(
		nil, object, Draft202012, "/nil", "", false, &state,
	); err != nil || children != nil {
		t.Fatalf("nil schema returned %#v, %v", children, err)
	}
}

func TestVerboseReferenceUnitRestoresScopeOnFailures(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("reference output failure")
	targetResource := &schemaResource{dynamicAnchors: make(map[string]*schemaPlan)}
	target := &schemaPlan{
		resource: targetResource,
		custom: []compiledKeyword{{
			name: "failure",
			evaluator: KeywordEvaluatorFunc(func(
				context.Context, Value,
			) (KeywordResult, error) {
				return KeywordResult{}, sentinel
			}),
		}},
	}
	targetResource.root = target
	rootResource := &schemaResource{dynamicAnchors: make(map[string]*schemaPlan)}
	plan := &schemaPlan{
		resource: rootResource, reference: target, referenceKeyword: "$ref",
		location: "", absoluteBase: "https://example.test/root",
		outputKeywords: []string{"$ref"},
	}
	rootResource.root = plan
	state := evaluationState{
		ctx: context.Background(), limits: DefaultLimits(),
		dynamicScope: []*schemaResource{rootResource},
	}
	if _, err := plan.verboseReferenceUnit(
		&jsonValue{kind: kindNull}, Draft202012, "/$ref", "", &state,
	); !errors.Is(err, sentinel) || len(state.dynamicScope) != 1 {
		t.Fatalf("got %v with scope %#v", err, state.dynamicScope)
	}
	state = evaluationState{
		ctx: context.Background(), limits: DefaultLimits(),
		dynamicScope: []*schemaResource{rootResource},
	}
	if _, err := plan.verboseOutputUnits(
		&jsonValue{kind: kindNull}, nil, nil, "", "", false,
		Draft202012, &state,
	); !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want reference output failure", err)
	}

	calls := 0
	target.custom[0].evaluator = KeywordEvaluatorFunc(func(
		context.Context, Value,
	) (KeywordResult, error) {
		calls++
		if calls > 1 {
			return KeywordResult{}, sentinel
		}
		return KeywordResult{Valid: true}, nil
	})
	if _, err := plan.verboseReferenceUnit(
		&jsonValue{kind: kindNull}, Draft202012, "/$ref", "", &state,
	); !errors.Is(err, sentinel) || len(state.dynamicScope) != 1 {
		t.Fatalf("got %v with scope %#v", err, state.dynamicScope)
	}
	truth := true
	target.custom = nil
	target.boolean = &truth
	if unit, err := plan.verboseReferenceUnit(
		&jsonValue{kind: kindNull}, Draft202012, "/$ref", "", &state,
	); err != nil || !unit.Valid || len(state.dynamicScope) != 1 {
		t.Fatalf("got %#v, %v with scope %#v", unit, err, state.dynamicScope)
	}
	if _, _, err := plan.collectOutput(
		&jsonValue{kind: kindNull}, Draft202012, "", "", false, true, &state,
	); err != nil || len(state.dynamicScope) != 1 {
		t.Fatalf("collect output: %v with scope %#v", err, state.dynamicScope)
	}
}

func TestVerboseOutputOperationBudgetsCoverEveryPhase(t *testing.T) {
	t.Parallel()

	child := &schemaPlan{
		outputKeywords: []string{"type"},
		types:          []typePlan{{name: "null"}},
	}
	validPlan := &schemaPlan{
		outputKeywords: []string{"anyOf"},
		anyOf:          []*schemaPlan{child},
	}
	invalidPlan := &schemaPlan{
		outputKeywords: []string{"properties"},
		properties: map[string]*schemaPlan{
			"x": {outputKeywords: []string{"type"}, types: []typePlan{{name: "string"}}},
		},
	}
	instances := []struct {
		plan *schemaPlan
		raw  []byte
	}{
		{plan: validPlan, raw: []byte(`null`)},
		{plan: invalidPlan, raw: []byte(`{"x":1}`)},
	}
	for _, test := range instances {
		for limit := 1; limit <= 20; limit++ {
			limits := DefaultLimits()
			limits.MaxEvaluationOps = limit
			schema := &Schema{dialect: Draft202012, limits: limits, plan: test.plan}
			_, _ = schema.ValidateOutput(
				context.Background(), test.raw, OutputVerbose,
			)
		}
	}
	limits := DefaultLimits()
	limits.MaxOutputUnits = 2
	schema := &Schema{dialect: Draft202012, limits: limits, plan: &schemaPlan{
		outputKeywords: []string{"$defs", "definitions", "type"},
		types:          []typePlan{{name: "string"}},
	}}
	if _, err := schema.ValidateOutput(
		context.Background(), []byte(`1`), OutputVerbose,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want verbose output limit", err)
	}
}

func TestVerboseHelperLimitAndExhaustionBoundaries(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("condition failed")
	failing := &schemaPlan{custom: []compiledKeyword{{
		name: "failure",
		evaluator: KeywordEvaluatorFunc(func(
			context.Context, Value,
		) (KeywordResult, error) {
			return KeywordResult{}, sentinel
		}),
	}}}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	if _, err := (&schemaPlan{
		outputKeywords: []string{"then"}, condition: failing,
	}).verboseOutputUnits(
		&jsonValue{kind: kindNull}, nil, nil, "", "", false,
		Draft202012, &state,
	); !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want condition failure", err)
	}

	limits := DefaultLimits()
	limits.MaxRegexBacktracking = 32
	pattern, err := compilePatternWithLimits("(?:^){100}", limits)
	if err != nil {
		t.Fatal(err)
	}
	patternPlan := &schemaPlan{patternProperties: []patternPropertyPlan{{
		name: "pattern", pattern: pattern, schema: &schemaPlan{},
	}}}
	object := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		strings.Repeat("x", 2): {kind: kindNull},
	}}
	state = evaluationState{ctx: context.Background(), limits: limits}
	if _, err := patternPlan.verboseKeywordChildren(
		"patternProperties", object, Draft202012, "/patternProperties",
		"", false, &state,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want pattern limit", err)
	}
	if _, err := patternPlan.propertyIsConfigured("x"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want configured-property pattern limit", err)
	}

	truth := true
	array := &jsonValue{kind: kindArray}
	state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	if children, err := verboseTupleItems(
		[]*schemaPlan{{boolean: &truth}}, array, Draft7,
		"/items", "", false, &state,
	); err != nil || len(children) != 0 {
		t.Fatalf("got %#v, %v", children, err)
	}
	if units, err := (&schemaPlan{
		boolean: &truth, location: "/target", absoluteBase: "https://example.test/schema",
	}).verboseAppliedSchemaUnits(
		&jsonValue{kind: kindNull}, Draft202012, "/$ref", "", true, &state,
	); err != nil || units[0].AbsoluteKeywordLocation == "" {
		t.Fatalf("got %#v, %v", units, err)
	}

	zeroLimits := DefaultLimits()
	zeroLimits.MaxEvaluationOps = 0
	state = evaluationState{ctx: context.Background(), limits: zeroLimits}
	unevaluatedObject := &schemaPlan{unevaluatedProperties: &schemaPlan{boolean: &truth}}
	if _, err := unevaluatedObject.verboseKeywordChildren(
		"unevaluatedProperties", object, Draft202012,
		"/unevaluatedProperties", "", false, &state,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want property evaluation limit", err)
	}
	state = evaluationState{ctx: context.Background(), limits: zeroLimits}
	unevaluatedArray := &schemaPlan{unevaluatedItems: &schemaPlan{boolean: &truth}}
	if _, err := unevaluatedArray.verboseKeywordChildren(
		"unevaluatedItems", &jsonValue{kind: kindArray, array: []*jsonValue{{kind: kindNull}}},
		Draft202012, "/unevaluatedItems", "", false, &state,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want item evaluation limit", err)
	}

	state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	evaluatedObject := &schemaPlan{
		properties:            map[string]*schemaPlan{"x": {boolean: &truth}},
		unevaluatedProperties: &schemaPlan{boolean: &truth},
	}
	if children, err := evaluatedObject.verboseKeywordChildren(
		"unevaluatedProperties",
		&jsonValue{kind: kindObject, object: map[string]*jsonValue{"x": {kind: kindNull}}},
		Draft202012, "/unevaluatedProperties", "", false, &state,
	); err != nil || len(children) != 0 {
		t.Fatalf("got %#v, %v", children, err)
	}
	evaluatedArray := &schemaPlan{
		prefixItems:      []*schemaPlan{{boolean: &truth}},
		unevaluatedItems: &schemaPlan{boolean: &truth},
	}
	if children, err := evaluatedArray.verboseKeywordChildren(
		"unevaluatedItems", &jsonValue{kind: kindArray, array: []*jsonValue{{kind: kindNull}}},
		Draft202012, "/unevaluatedItems", "", false, &state,
	); err != nil || len(children) != 0 {
		t.Fatalf("got %#v, %v", children, err)
	}
}

func TestOutputAccountingAndBranchWrappingAreExact(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		valid    bool
		errors   int
		required bool
		want     bool
	}{
		{valid: false, errors: 1, required: true, want: true},
		{valid: true, errors: 1, required: true},
		{valid: false, errors: 0, required: true},
		{valid: false, errors: 1, required: false},
	} {
		if got := shouldWrapBranchError(test.valid, test.errors, test.required); got != test.want {
			t.Errorf("got %v for %#v", got, test)
		}
	}
	for _, test := range []struct {
		errors, annotations int
		previous, current   int
		want                int
	}{
		{errors: 1, want: 1},
		{annotations: 1, want: 1},
		{errors: 2, annotations: 3, previous: 4, current: 6, want: 3},
		{previous: 2, current: 2, want: 0},
	} {
		if got := uncountedOutputUnits(
			test.errors, test.annotations, test.previous, test.current,
		); got != test.want {
			t.Errorf("got %d, want %d for %#v", got, test.want, test)
		}
	}
	if result := detailedErrors(nil); len(result) != 0 {
		t.Fatalf("empty detailed output yielded %#v", result)
	}
	flat := []OutputUnit{
		{Error: "schema evaluation had errors"},
		{KeywordLocation: "/type", Error: "wrong type"},
	}
	result := detailedErrors(flat)
	if len(result) != 1 || result[0].KeywordLocation != "/type" {
		t.Fatalf("root error was not removed: %#v", result)
	}
}

func TestOutputTraversalPropagatesSpecificNestedFailures(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("nested output failure")
	truth := true
	pattern, err := compilePattern("x")
	if err != nil {
		t.Fatal(err)
	}
	object := &jsonValue{kind: kindObject, object: map[string]*jsonValue{
		"x": {kind: kindNull},
	}}
	for _, test := range []struct {
		name      string
		plan      func(*schemaPlan) *schemaPlan
		instance  *jsonValue
		failAfter int
	}{
		{
			name: "condition branch",
			plan: func(failing *schemaPlan) *schemaPlan {
				return &schemaPlan{condition: &schemaPlan{boolean: &truth}, then: failing}
			},
			instance:  &jsonValue{kind: kindNull},
			failAfter: 2,
		},
		{
			name: "schema type",
			plan: func(failing *schemaPlan) *schemaPlan {
				return &schemaPlan{types: []typePlan{{schema: failing}}}
			},
			instance:  &jsonValue{kind: kindNull},
			failAfter: 1,
		},
		{
			name: "pattern property",
			plan: func(failing *schemaPlan) *schemaPlan {
				return &schemaPlan{patternProperties: []patternPropertyPlan{{
					name: "x", pattern: pattern, schema: failing,
				}}}
			},
			instance:  object,
			failAfter: 2,
		},
		{
			name: "dependent schema",
			plan: func(failing *schemaPlan) *schemaPlan {
				return &schemaPlan{dependentSchemas: map[string]*schemaPlan{"x": failing}}
			},
			instance:  object,
			failAfter: 2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			failing := &schemaPlan{custom: []compiledKeyword{{
				name: "eventual failure",
				evaluator: KeywordEvaluatorFunc(func(
					context.Context, Value,
				) (KeywordResult, error) {
					calls++
					if calls > test.failAfter {
						return KeywordResult{}, sentinel
					}
					return KeywordResult{Valid: true}, nil
				}),
			}}}
			plan := test.plan(failing)
			state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
			_, _, err := plan.collectOutput(
				test.instance, Draft202012, "", "", false, true, &state,
			)
			if !errors.Is(err, sentinel) {
				t.Fatalf("got %v, want nested failure", err)
			}
		})
	}

	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	if errors, annotations, err := (&schemaPlan{
		condition: &schemaPlan{boolean: &truth},
	}).collectOutput(
		&jsonValue{kind: kindNull}, Draft202012, "", "", false, true, &state,
	); err != nil || len(errors) != 0 || len(annotations) != 0 {
		t.Fatalf("nil conditional branch: errors=%#v annotations=%#v err=%v", errors, annotations, err)
	}
}

func TestOutputContentReportsSyntaxAndResourceFailures(t *testing.T) {
	t.Parallel()

	plan := &schemaPlan{contentMediaType: "application/json"}
	limits := DefaultLimits()
	limits.MaxInputBytes = 1
	state := evaluationState{ctx: context.Background(), limits: limits}
	if _, _, err := plan.collectOutput(
		&jsonValue{kind: kindString, text: "null"},
		Draft202012, "", "", false, true, &state,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want content resource limit", err)
	}
	state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	errors, _, err := plan.collectOutput(
		&jsonValue{kind: kindString, text: "{"},
		Draft202012, "", "", false, true, &state,
	)
	if err != nil || !hasOutputKeyword(errors, "/contentMediaType") {
		t.Fatalf("unexpected content syntax output: %#v, %v", errors, err)
	}
	encodingPlan := &schemaPlan{contentEncoding: "base64"}
	state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	errors, _, err = encodingPlan.collectOutput(
		&jsonValue{kind: kindString, text: "!"},
		Draft202012, "", "", false, true, &state,
	)
	if err != nil || !hasOutputKeyword(errors, "/contentEncoding") {
		t.Fatalf("unexpected content encoding output: %#v, %v", errors, err)
	}
}

func TestUniqueItemOutputDistinguishesSelfAndPeerComparisons(t *testing.T) {
	t.Parallel()

	plan := &schemaPlan{uniqueItems: true}
	for _, test := range []struct {
		items []*jsonValue
		valid bool
	}{
		{items: []*jsonValue{{kind: kindNull}}, valid: true},
		{items: []*jsonValue{{kind: kindNull}, {kind: kindBoolean}}, valid: true},
		{items: []*jsonValue{{kind: kindNull}, {kind: kindNull}}, valid: false},
	} {
		state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
		errors, _, err := plan.collectOutput(
			&jsonValue{kind: kindArray, array: test.items},
			Draft202012, "", "", false, true, &state,
		)
		if err != nil || (len(errors) == 0) != test.valid {
			t.Fatalf("items=%d valid=%v errors=%#v err=%v", len(test.items), test.valid, errors, err)
		}
	}
}

func hasOutputKeyword(units []OutputUnit, keyword string) bool {
	for _, unit := range units {
		if unit.KeywordLocation == keyword {
			return true
		}
	}
	return false
}
