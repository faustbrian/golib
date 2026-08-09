package ruleengine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type boundaryOperator struct {
	name       OperatorName
	signatures []Signature
}

type boundaryResolver struct {
	values map[string]Value
	calls  []string
}

func (resolver *boundaryResolver) Resolve(_ context.Context, path Path) (Value, Owner, bool, error) {
	resolver.calls = append(resolver.calls, path.String())
	value, found := resolver.values[path.String()]
	return value, OwnerResource, found, nil
}

func (operator boundaryOperator) Name() OperatorName { return operator.name }
func (operator boundaryOperator) Signatures() []Signature {
	return append([]Signature(nil), operator.signatures...)
}
func (boundaryOperator) Evaluate(context.Context, Value, Value) (bool, error) {
	return false, nil
}

func TestCompilerOperatorRegistryBoundaries(t *testing.T) {
	t.Parallel()

	valid := []Signature{{Left: KindList, Right: KindList}}
	tests := []struct {
		name     string
		operator Operator
		wantCode Code
	}{
		{name: "nil operator", operator: nil, wantCode: CodeInvalidRule},
		{name: "empty name", operator: boundaryOperator{signatures: valid}, wantCode: CodeInvalidRule},
		{name: "built-in name", operator: boundaryOperator{name: OpEqual, signatures: valid}, wantCode: CodeInvalidRule},
		{name: "left kind above list", operator: boundaryOperator{name: "left_above", signatures: []Signature{{Left: KindList + 1, Right: KindList}}}, wantCode: CodeInvalidRule},
		{name: "right kind above list", operator: boundaryOperator{name: "right_above", signatures: []Signature{{Left: KindList, Right: KindList + 1}}}, wantCode: CodeInvalidRule},
		{name: "missing left kind", operator: boundaryOperator{name: "missing_left", signatures: []Signature{{Left: KindMissing, Right: KindList}}}, wantCode: CodeInvalidRule},
		{name: "missing right kind", operator: boundaryOperator{name: "missing_right", signatures: []Signature{{Left: KindList, Right: KindMissing}}}, wantCode: CodeInvalidRule},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewCompilerWithOperators(DefaultLimits(), test.operator); !IsCode(err, test.wantCode) {
				t.Fatalf("NewCompilerWithOperators() error = %v, want %s", err, test.wantCode)
			}
		})
	}
	if _, err := NewCompilerWithOperators(DefaultLimits(), boundaryOperator{name: "lists", signatures: valid}); err != nil {
		t.Fatalf("NewCompilerWithOperators(list signature) error = %v", err)
	}
}

func TestCompilerAcceptsExactLimitsAndRejectsNextValues(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxRules = 1
	limits.MaxDerivedFacts = 2
	limits.MaxIdentifierBytes = 3
	pathA := MustPath("derived", "a")
	pathB := MustPath("derived", "b")
	exact := RuleSet{ID: "set", Rules: []Rule{{
		ID: "one", When: True(), Derive: []Fact{{Path: pathA, Value: Int(1)}, {Path: pathB, Value: Int(2)}},
	}}}
	if _, _, err := NewCompiler(limits).Compile(context.Background(), exact); err != nil {
		t.Fatalf("Compile(exact limits) error = %v", err)
	}

	twoRules := RuleSet{ID: "set", Rules: []Rule{{ID: "a", When: True()}, {ID: "b", When: True()}}}
	if _, _, err := NewCompiler(limits).Compile(context.Background(), twoRules); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("Compile(over rule limit) error = %v", err)
	}

	threeDerived := exact
	threeDerived.Rules[0].Derive = append(threeDerived.Rules[0].Derive, Fact{Path: MustPath("derived", "c"), Value: Int(3)})
	if _, _, err := NewCompiler(limits).Compile(context.Background(), threeDerived); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("Compile(over per-rule derived limit) error = %v", err)
	}

	global := RuleSet{ID: "set", Strategy: CollectAll, Rules: []Rule{
		{ID: "a", When: True(), Derive: []Fact{{Path: pathA, Value: Int(1)}}},
		{ID: "b", When: True(), Derive: []Fact{{Path: pathB, Value: Int(2)}}},
	}}
	globalLimits := limits
	globalLimits.MaxRules = 2
	if _, _, err := NewCompiler(globalLimits).Compile(context.Background(), global); err != nil {
		t.Fatalf("Compile(exact global derived limit) error = %v", err)
	}
}

func TestCompilerPredicateDepthAndOperandBoundaries(t *testing.T) {
	t.Parallel()

	depthLimits := DefaultLimits()
	depthLimits.MaxASTDepth = 2
	if _, _, err := NewCompiler(depthLimits).Compile(context.Background(), RuleSet{ID: "depth", Rules: []Rule{{ID: "exact", When: Not(True())}}}); err != nil {
		t.Fatalf("Compile(exact depth) error = %v", err)
	}
	for name, predicate := range map[string]Predicate{
		"all": All(All(True())),
		"any": Any(Any(True())),
		"not": Not(Not(True())),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			set := RuleSet{ID: "depth", Rules: []Rule{{ID: RuleID(name), When: predicate}}}
			if _, _, err := NewCompiler(depthLimits).Compile(context.Background(), set); !IsCode(err, CodeLimitExceeded) {
				t.Fatalf("Compile(over depth) error = %v", err)
			}
		})
	}

	operandLimits := DefaultLimits()
	operandLimits.MaxOperands = 2
	if _, _, err := NewCompiler(operandLimits).Compile(context.Background(), RuleSet{ID: "operands", Rules: []Rule{{ID: "exact", When: All(True())}}}); err != nil {
		t.Fatalf("Compile(exact operands) error = %v", err)
	}
	if _, _, err := NewCompiler(operandLimits).Compile(context.Background(), RuleSet{ID: "operands", Rules: []Rule{{ID: "over", When: All(True(), True())}}}); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("Compile(over operands) error = %v", err)
	}
}

func TestCompilerSortsAtPriorityAndIdentifierBoundaries(t *testing.T) {
	t.Parallel()

	set := RuleSet{ID: "order", Rules: []Rule{
		{ID: "z", Priority: 1, When: True()},
		{ID: "b", Priority: 2, When: True()},
		{ID: "a", Priority: 2, When: True()},
	}}
	plan, _, err := NewCompiler(DefaultLimits()).Compile(context.Background(), set)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	for index, want := range []RuleID{"a", "b", "z"} {
		if plan.rules[index].ID != want {
			t.Fatalf("rules[%d].ID = %q, want %q", index, plan.rules[index].ID, want)
		}
	}
}

func TestContextAcceptsExactBoundsAndRejectsNextValues(t *testing.T) {
	t.Parallel()

	pathA := MustPath("facts", "a")
	pathB := MustPath("facts", "b")
	limits := DefaultLimits()
	limits.MaxFacts = 1
	limits.MaxASTDepth = 1
	limits.MaxStringBytes = 2
	limits.MaxCollection = 1
	if _, err := NewContextWithLimits(limits, Fact{Path: pathA, Value: List(String("ab"))}); err != nil {
		t.Fatalf("NewContextWithLimits(exact bounds) error = %v", err)
	}
	for name, facts := range map[string][]Fact{
		"facts":      {{Path: pathA, Value: Int(1)}, {Path: pathB, Value: Int(2)}},
		"depth":      {{Path: pathA, Value: List(List(Int(1)))}},
		"string":     {{Path: pathA, Value: String("abc")}},
		"collection": {{Path: pathA, Value: List(Int(1), Int(2))}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewContextWithLimits(limits, facts...); err == nil {
				t.Fatal("NewContextWithLimits(over bound) error = nil")
			}
		})
	}
}

func TestEveryLimitRejectsZero(t *testing.T) {
	t.Parallel()

	typeOfLimits := reflect.TypeOf(Limits{})
	for index := range typeOfLimits.NumField() {
		field := typeOfLimits.Field(index)
		t.Run(field.Name, func(t *testing.T) {
			t.Parallel()
			limits := DefaultLimits()
			value := reflect.ValueOf(&limits).Elem().FieldByIndex(field.Index)
			value.Set(reflect.Zero(value.Type()))
			if err := limits.validate(); !IsCode(err, CodeInvalidLimit) {
				t.Fatalf("Limits.validate() error = %v", err)
			}
		})
	}
}

func TestErrorClassificationRequiresMatchingTypedError(t *testing.T) {
	t.Parallel()

	if IsCode(errors.New("invalid_rule"), CodeInvalidRule) {
		t.Fatal("IsCode() matched an untyped error")
	}
	if IsCode(newError(CodeInvalidFact, "safe"), CodeInvalidRule) {
		t.Fatal("IsCode() matched a different code")
	}
}

func TestRegexAndOrderingBoundaries(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxRegexBytes = 2
	valid := RuleSet{ID: "regex", Rules: []Rule{{ID: "exact", When: Compare(OpMatches, Literal(String("ab")), Literal(String("..")))}}}
	if _, _, err := NewCompiler(limits).Compile(context.Background(), valid); err != nil {
		t.Fatalf("Compile(exact regex bound) error = %v", err)
	}
	invalid := []struct {
		set  RuleSet
		code Code
	}{
		{set: RuleSet{ID: "regex", Rules: []Rule{{ID: "variable", When: Compare(OpMatches, Literal(String("ab")), Variable(MustPath("pattern")))}}}, code: CodeInvalidRule},
		{set: RuleSet{ID: "regex", Rules: []Rule{{ID: "kind", When: Compare(OpMatches, Literal(String("ab")), Literal(Int(1)))}}}, code: CodeInvalidRule},
		{set: RuleSet{ID: "regex", Rules: []Rule{{ID: "length", When: Compare(OpMatches, Literal(String("ab")), Literal(String("...")))}}}, code: CodeLimitExceeded},
	}
	for _, test := range invalid {
		if _, _, err := NewCompiler(limits).Compile(context.Background(), test.set); !IsCode(err, test.code) {
			t.Fatalf("Compile(%q) error = %v, want %s", test.set.Rules[0].ID, err, test.code)
		}
	}

	for _, operator := range []OperatorName{OpLessThan, OpGreaterThan} {
		matched, err := evaluateBuiltin(operator, Int(1), Int(1))
		if err != nil || matched {
			t.Fatalf("evaluateBuiltin(%s, equal) = %v, %v", operator, matched, err)
		}
	}
	for _, operator := range []OperatorName{OpLessOrEqual, OpGreaterOrEqual} {
		matched, err := evaluateBuiltin(operator, Int(1), Int(1))
		if err != nil || !matched {
			t.Fatalf("evaluateBuiltin(%s, equal) = %v, %v", operator, matched, err)
		}
	}
	for _, operands := range [][2]Value{{Missing(), Int(1)}, {Int(1), Missing()}} {
		matched, err := evaluateBuiltin(OpEqual, operands[0], operands[1])
		if err != nil || matched {
			t.Fatalf("evaluateBuiltin(equal, missing) = %v, %v", matched, err)
		}
	}
}

func TestIdentifierAcceptsExactByteLimit(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxIdentifierBytes = 3
	set := RuleSet{ID: strings.Repeat("a", limits.MaxIdentifierBytes), Rules: []Rule{{ID: "one", When: True()}}}
	if _, _, err := NewCompiler(limits).Compile(context.Background(), set); err != nil {
		t.Fatalf("Compile(exact identifier bound) error = %v", err)
	}
}

func TestPathAcceptsExactBoundsAndRejectsNextValues(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxPathSegments = 2
	limits.MaxPathBytes = 3
	path, err := NewPath(limits, "a", "b")
	if err != nil {
		t.Fatalf("NewPath(exact bounds) error = %v", err)
	}
	if path.String() != "a.b" {
		t.Fatalf("NewPath(exact bounds) = %q", path.String())
	}
	if _, err := NewPath(limits, "a", "b", "c"); !IsCode(err, CodeInvalidPath) {
		t.Fatalf("NewPath(over segment bound) error = %v", err)
	}
	tooFewBytes := limits
	tooFewBytes.MaxPathBytes = 2
	if _, err := NewPath(tooFewBytes, "a", "b"); !IsCode(err, CodeInvalidPath) {
		t.Fatalf("NewPath(over byte bound) error = %v", err)
	}
	if (Path{key: "a"}).valid() || (Path{segments: []string{"a"}}).valid() {
		t.Fatal("Path.valid() accepted an incomplete path")
	}
}

func TestEvaluateResolvedContinuesPastKnownAndMissingFacts(t *testing.T) {
	t.Parallel()

	a := MustPath("facts", "a")
	b := MustPath("facts", "b")
	set := RuleSet{ID: "resolved", Rules: []Rule{{ID: "both", When: All(
		Compare(OpEqual, Variable(a), Literal(Int(1))),
		Compare(OpEqual, Variable(b), Literal(Int(2))),
	)}}}
	plan, _, err := NewCompiler(DefaultLimits()).Compile(context.Background(), set)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	base, _ := NewContext(Fact{Path: a, Value: Int(1)})
	resolver := &boundaryResolver{values: map[string]Value{b.String(): Int(2)}}
	if result := plan.EvaluateResolved(context.Background(), base, resolver); result.Decision != Matched {
		t.Fatalf("EvaluateResolved(known first) = %#v", result)
	}
	if !reflect.DeepEqual(resolver.calls, []string{b.String()}) {
		t.Fatalf("resolver calls = %#v", resolver.calls)
	}

	empty, _ := NewContext()
	resolver = &boundaryResolver{values: map[string]Value{b.String(): Int(2)}}
	result := plan.EvaluateResolved(context.Background(), empty, resolver)
	if !reflect.DeepEqual(resolver.calls, []string{a.String(), b.String()}) {
		t.Fatalf("resolver calls after missing first = %#v", resolver.calls)
	}
	if result.Decision != Unmatched {
		t.Fatalf("EvaluateResolved(missing first) = %#v", result)
	}
}

func TestEvaluationHonorsExactOutputAndIterationLimits(t *testing.T) {
	t.Parallel()

	failing := PredicateFunc(func(context.Context, Context) (bool, error) {
		return false, errors.New("failure")
	})
	limits := DefaultLimits()
	limits.MaxExplanation = 1
	limits.MaxDiagnostics = 1
	set := RuleSet{ID: "bounded", Strategy: CollectAll, Rules: []Rule{
		{ID: "a", When: failing},
		{ID: "b", When: failing},
	}}
	plan, _, err := NewCompiler(limits).Compile(context.Background(), set)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	result := plan.Evaluate(context.Background(), Context{})
	if len(result.Explanation) != 1 || len(result.Errors) != 1 {
		t.Fatalf("bounded result = %#v", result)
	}

	limits = DefaultLimits()
	limits.MaxIterations = 1
	path := MustPath("derived", "value")
	chain := RuleSet{ID: "iterations", Strategy: CollectAll, Rules: []Rule{{
		ID: "derive", When: True(), Derive: []Fact{{Path: path, Value: Int(1)}},
	}}}
	plan, _, err = NewCompiler(limits).Compile(context.Background(), chain)
	if err != nil {
		t.Fatalf("Compile(iterations) error = %v", err)
	}
	result = plan.Evaluate(context.Background(), Context{})
	if result.Decision != Indeterminate || !IsCode(result.Errors[len(result.Errors)-1], CodeLimitExceeded) {
		t.Fatalf("Evaluate(iteration limit) = %#v", result)
	}
}

func TestEvaluationDerivesEveryFactFromMatchedRule(t *testing.T) {
	t.Parallel()

	a := MustPath("derived", "a")
	b := MustPath("derived", "b")
	set := RuleSet{ID: "derive", Rules: []Rule{{ID: "both", When: True(), Derive: []Fact{
		{Path: a, Value: Int(1)},
		{Path: b, Value: Int(2)},
	}}}}
	plan, _, err := NewCompiler(DefaultLimits()).Compile(context.Background(), set)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	result := plan.Evaluate(context.Background(), Context{})
	if result.DerivedFacts.Lookup(a).Kind() != KindInt || result.DerivedFacts.Lookup(b).Kind() != KindInt {
		t.Fatalf("DerivedFacts = %#v", result.DerivedFacts)
	}
}

func TestErrorOnMultipleRequiresTwoMatches(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	for name, second := range map[string]Predicate{"one": False(), "two": True()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			set := RuleSet{ID: "multiple", Strategy: ErrorOnMultiple, Rules: []Rule{
				{ID: "a", When: True()},
				{ID: "b", When: second},
			}}
			plan, _, err := NewCompiler(limits).Compile(context.Background(), set)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			result := plan.Evaluate(context.Background(), Context{})
			if name == "one" && (result.Decision != Matched || len(result.Errors) != 0) {
				t.Fatalf("Evaluate(one match) = %#v", result)
			}
			if name == "two" && (result.Decision != Indeterminate || !IsCode(result.Errors[0], CodeConflict)) {
				t.Fatalf("Evaluate(two matches) = %#v", result)
			}
		})
	}
}

func TestCompoundPredicatesReturnAtFalseTrueAndErrorBoundaries(t *testing.T) {
	t.Parallel()

	failure := errors.New("predicate failure")
	failing := PredicateFunc(func(context.Context, Context) (bool, error) { return false, failure })
	plan := Plan{}
	tests := []struct {
		name      string
		predicate Predicate
		matched   bool
		wantError bool
	}{
		{name: "all false", predicate: All(True(), False())},
		{name: "all error", predicate: All(True(), failing), wantError: true},
		{name: "any true", predicate: Any(False(), True()), matched: true},
		{name: "any error", predicate: Any(False(), failing), wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			matched, err := plan.evaluatePredicate(context.Background(), test.predicate, Context{})
			if matched != test.matched || (err != nil) != test.wantError {
				t.Fatalf("evaluatePredicate() = %v, %v", matched, err)
			}
		})
	}
}

func TestJSONDecodingHonorsPredicateAndValueDepth(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxASTDepth = 2
	truth := jsonNode{Kind: "true"}
	exactPredicate := jsonNode{Kind: "not", Child: &truth}
	if _, err := decodePredicate(exactPredicate, limits, 1); err != nil {
		t.Fatalf("decodePredicate(exact depth) error = %v", err)
	}
	overNot := jsonNode{Kind: "not", Child: &exactPredicate}
	if _, err := decodePredicate(overNot, limits, 1); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("decodePredicate(over not depth) error = %v", err)
	}
	allChild := jsonNode{Kind: "all", Children: []jsonNode{truth}}
	overAll := jsonNode{Kind: "all", Children: []jsonNode{allChild}}
	if _, err := decodePredicate(overAll, limits, 1); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("decodePredicate(over all depth) error = %v", err)
	}
	for kind, wantType := range map[string]any{"all": allPredicate{}, "any": anyPredicate{}} {
		predicate, err := decodePredicate(jsonNode{Kind: kind, Children: []jsonNode{truth}}, limits, 1)
		if err != nil {
			t.Fatalf("decodePredicate(%s) error = %v", kind, err)
		}
		if reflect.TypeOf(predicate) != reflect.TypeOf(wantType) {
			t.Fatalf("decodePredicate(%s) type = %T", kind, predicate)
		}
	}

	limits.MaxASTDepth = 1
	exactValue := jsonValue{Type: "list", List: []jsonValue{{Type: "int", Int: pointer(int64(1))}}}
	if _, err := decodeValue(exactValue, limits, 0); err != nil {
		t.Fatalf("decodeValue(exact depth) error = %v", err)
	}
	overValue := jsonValue{Type: "list", List: []jsonValue{exactValue}}
	if _, err := decodeValue(overValue, limits, 0); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("decodeValue(over depth) error = %v", err)
	}
}

func TestJSONNodeShapeRejectsEachAmbiguousField(t *testing.T) {
	t.Parallel()

	operand := &jsonOperand{Kind: "literal", Value: &jsonValue{Type: "null"}}
	child := &jsonNode{Kind: "true"}
	valid := []jsonNode{
		{Kind: "true"},
		{Kind: "false"},
		{Kind: "exists", Path: []string{"a"}},
		{Kind: "compare", Operator: OpEqual, Left: operand, Right: operand},
		{Kind: "all", Children: []jsonNode{}},
		{Kind: "any", Children: []jsonNode{}},
		{Kind: "not", Child: child},
	}
	for _, node := range valid {
		if !validNodeShape(node) {
			t.Fatalf("validNodeShape(%s) = false", node.Kind)
		}
	}
	invalid := []jsonNode{
		{Kind: "true", Operator: OpEqual},
		{Kind: "true", Path: []string{"a"}},
		{Kind: "true", Left: operand},
		{Kind: "true", Right: operand},
		{Kind: "true", Child: child},
		{Kind: "true", Children: []jsonNode{}},
		{Kind: "exists"},
		{Kind: "exists", Path: []string{"a"}, Operator: OpEqual},
		{Kind: "exists", Path: []string{"a"}, Left: operand},
		{Kind: "exists", Path: []string{"a"}, Right: operand},
		{Kind: "exists", Path: []string{"a"}, Child: child},
		{Kind: "exists", Path: []string{"a"}, Children: []jsonNode{}},
		{Kind: "compare", Left: operand, Right: operand},
		{Kind: "compare", Operator: OpEqual, Path: []string{"a"}, Left: operand, Right: operand},
		{Kind: "compare", Operator: OpEqual, Right: operand},
		{Kind: "compare", Operator: OpEqual, Left: operand},
		{Kind: "compare", Operator: OpEqual, Left: operand, Right: operand, Child: child},
		{Kind: "compare", Operator: OpEqual, Left: operand, Right: operand, Children: []jsonNode{}},
		{Kind: "all", Operator: OpEqual, Children: []jsonNode{}},
		{Kind: "all", Path: []string{"a"}, Children: []jsonNode{}},
		{Kind: "all", Left: operand, Children: []jsonNode{}},
		{Kind: "all", Right: operand, Children: []jsonNode{}},
		{Kind: "all", Child: child, Children: []jsonNode{}},
		{Kind: "all"},
		{Kind: "not", Operator: OpEqual, Child: child},
		{Kind: "not", Path: []string{"a"}, Child: child},
		{Kind: "not", Left: operand, Child: child},
		{Kind: "not", Right: operand, Child: child},
		{Kind: "not"},
		{Kind: "not", Child: child, Children: []jsonNode{}},
	}
	for index, node := range invalid {
		if validNodeShape(node) {
			t.Fatalf("validNodeShape(invalid %d, %s) = true", index, node.Kind)
		}
	}
}

func TestJSONOperandAndValueShapesRejectAmbiguity(t *testing.T) {
	t.Parallel()

	nullValue := &jsonValue{Type: "null"}
	for _, operand := range []jsonOperand{
		{Kind: "variable", Path: []string{"a"}},
		{Kind: "literal", Value: nullValue},
	} {
		if !validOperandShape(operand) {
			t.Fatalf("validOperandShape(%s) = false", operand.Kind)
		}
	}
	for _, operand := range []jsonOperand{
		{Kind: "variable"},
		{Kind: "variable", Path: []string{"a"}, Value: nullValue},
		{Kind: "literal"},
		{Kind: "literal", Path: []string{"a"}, Value: nullValue},
	} {
		if validOperandShape(operand) {
			t.Fatalf("validOperandShape(invalid %s) = true", operand.Kind)
		}
	}

	boolean := true
	integer := int64(1)
	floating := 1.0
	text := "x"
	duration := int64(1)
	valid := []jsonValue{
		{Type: "missing"}, {Type: "null"},
		{Type: "bool", Bool: &boolean},
		{Type: "int", Int: &integer},
		{Type: "float", Float: &floating},
		{Type: "string", String: &text},
		{Type: "time", Time: &text},
		{Type: "duration", Duration: &duration},
		{Type: "list", List: []jsonValue{}},
	}
	for _, value := range valid {
		if !validValueShape(value) {
			t.Fatalf("validValueShape(%s) = false", value.Type)
		}
	}
	invalid := []jsonValue{
		{Type: "missing", Bool: &boolean},
		{Type: "null", List: []jsonValue{}},
		{Type: "bool"},
		{Type: "bool", Bool: &boolean, Int: &integer},
		{Type: "bool", Bool: &boolean, List: []jsonValue{}},
		{Type: "int"},
		{Type: "int", Int: &integer, Bool: &boolean},
		{Type: "int", Int: &integer, List: []jsonValue{}},
		{Type: "float"},
		{Type: "float", Float: &floating, Bool: &boolean},
		{Type: "float", Float: &floating, List: []jsonValue{}},
		{Type: "string"},
		{Type: "string", String: &text, Bool: &boolean},
		{Type: "string", String: &text, List: []jsonValue{}},
		{Type: "time"},
		{Type: "time", Time: &text, Bool: &boolean},
		{Type: "time", Time: &text, List: []jsonValue{}},
		{Type: "duration"},
		{Type: "duration", Duration: &duration, Bool: &boolean},
		{Type: "duration", Duration: &duration, List: []jsonValue{}},
		{Type: "list", Bool: &boolean},
		{Type: "invalid"},
	}
	for index, value := range invalid {
		if validValueShape(value) {
			t.Fatalf("validValueShape(invalid %d, %s) = true", index, value.Type)
		}
	}
}

func TestKindNameRejectsFirstUnknownKind(t *testing.T) {
	t.Parallel()

	if kindName(KindList) != "list" {
		t.Fatalf("kindName(KindList) = %q", kindName(KindList))
	}
	if kindName(KindList+1) != "invalid" {
		t.Fatalf("kindName(first unknown) = %q", kindName(KindList+1))
	}
}

func TestValueAccessorsRequireMatchingKindAndStorage(t *testing.T) {
	t.Parallel()

	now := time.Unix(1, 0).UTC()
	tests := []struct {
		name      string
		wrongKind Value
		wrongData Value
		read      func(Value) bool
	}{
		{name: "bool", wrongKind: Value{kind: KindInt, data: true}, wrongData: Value{kind: KindBool, data: int64(1)}, read: func(value Value) bool { _, ok := value.BoolValue(); return ok }},
		{name: "int", wrongKind: Value{kind: KindBool, data: int64(1)}, wrongData: Value{kind: KindInt, data: true}, read: func(value Value) bool { _, ok := value.IntValue(); return ok }},
		{name: "float", wrongKind: Value{kind: KindInt, data: 1.0}, wrongData: Value{kind: KindFloat, data: int64(1)}, read: func(value Value) bool { _, ok := value.FloatValue(); return ok }},
		{name: "string", wrongKind: Value{kind: KindInt, data: "x"}, wrongData: Value{kind: KindString, data: int64(1)}, read: func(value Value) bool { _, ok := value.StringValue(); return ok }},
		{name: "time", wrongKind: Value{kind: KindInt, data: now}, wrongData: Value{kind: KindTime, data: int64(1)}, read: func(value Value) bool { _, ok := value.TimeValue(); return ok }},
		{name: "duration", wrongKind: Value{kind: KindInt, data: time.Second}, wrongData: Value{kind: KindDuration, data: int64(1)}, read: func(value Value) bool { _, ok := value.DurationValue(); return ok }},
		{name: "list", wrongKind: Value{kind: KindInt, data: []Value{}}, wrongData: Value{kind: KindList, data: int64(1)}, read: func(value Value) bool { _, ok := value.ListValue(); return ok }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if test.read(test.wrongKind) || test.read(test.wrongData) {
				t.Fatal("accessor accepted mismatched kind or storage")
			}
		})
	}
}
