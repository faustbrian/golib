package ruleengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestValuePathContextAndErrorEdges(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	badLimits := limits
	badLimits.MaxRules = 0
	if _, err := NewPath(badLimits, "x"); !IsCode(err, CodeInvalidLimit) {
		t.Fatalf("NewPath() error = %v", err)
	}
	if _, err := NewPath(limits); !IsCode(err, CodeInvalidPath) {
		t.Fatalf("NewPath() empty error = %v", err)
	}
	tooMany := make([]string, limits.MaxPathSegments+1)
	for index := range tooMany {
		tooMany[index] = "x"
	}
	if _, err := NewPath(limits, tooMany...); !IsCode(err, CodeInvalidPath) {
		t.Fatalf("NewPath() segment error = %v", err)
	}
	if _, err := NewPath(limits, string([]byte{0xff})); !IsCode(err, CodeInvalidPath) {
		t.Fatalf("NewPath() UTF-8 error = %v", err)
	}
	tinyPath := limits
	tinyPath.MaxPathBytes = 1
	if _, err := NewPath(tinyPath, "xx"); !IsCode(err, CodeInvalidPath) {
		t.Fatalf("NewPath() byte limit error = %v", err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("MustPath() did not panic")
			}
		}()
		MustPath("bad.segment")
	}()

	path := MustPath("fact", "value")
	ctx, err := NewContext(Fact{Path: path, Value: Bool(false), Owner: OwnerSubject})
	if err != nil {
		t.Fatal(err)
	}
	if owner, ok := ctx.Owner(path); !ok || owner != OwnerSubject {
		t.Fatalf("Owner() = %v, %v", owner, ok)
	}
	if _, ok := ctx.Owner(MustPath("absent")); ok {
		t.Fatal("Owner() found absent fact")
	}
	if got := ctx.Lookup(path).Interface(); got != false {
		t.Fatalf("Interface() = %#v", got)
	}
	if value, ok := ctx.Lookup(path).BoolValue(); !ok || value {
		t.Fatalf("BoolValue() = %v, %v", value, ok)
	}
	if _, ok := Int(1).BoolValue(); ok {
		t.Fatal("BoolValue() accepted int")
	}
	if list := List(Int(1)); reflect.TypeOf(list.Interface()).Kind() != reflect.Slice {
		t.Fatalf("list Interface() = %#v", list.Interface())
	}

	badLimits = limits
	badLimits.MaxFacts = 0
	if _, err := NewContextWithLimits(badLimits); !IsCode(err, CodeInvalidLimit) {
		t.Fatalf("NewContextWithLimits() error = %v", err)
	}
	invalidValues := make([]Value, 0, 4)
	invalidValues = append(invalidValues,
		Value{kind: Kind(255)},
		String(strings.Repeat("x", limits.MaxStringBytes+1)),
		List(make([]Value, limits.MaxCollection+1)...),
	)
	deep := Int(1)
	for range limits.MaxASTDepth + 2 {
		deep = List(deep)
	}
	invalidValues = append(invalidValues, deep)
	for _, value := range invalidValues {
		if _, err := NewContext(Fact{Path: path, Value: value}); err == nil {
			t.Fatalf("NewContext() accepted %#v", value)
		}
	}

	typed := newError(CodeInvalidFact, "safe")
	if typed.Code() != CodeInvalidFact || typed.Error() != "invalid_fact: safe" {
		t.Fatalf("typed error = %v, %s", typed.Code(), typed.Error())
	}
	if code := errorCode(errors.New("plain"), CodeEvaluation); code != CodeEvaluation {
		t.Fatalf("errorCode() = %s", code)
	}
}

func TestDirectPredicateContracts(t *testing.T) {
	t.Parallel()

	facts, _ := NewContext()
	ctx := context.Background()
	failed := PredicateFunc(func(context.Context, Context) (bool, error) {
		return false, errors.New("failed")
	})
	if _, err := failed.Evaluate(ctx, facts); err == nil {
		t.Fatal("PredicateFunc.Evaluate() error = nil")
	}
	if matched, err := True().Evaluate(ctx, facts); err != nil || !matched {
		t.Fatalf("True().Evaluate() = %v, %v", matched, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := True().Evaluate(canceled, facts); !errors.Is(err, context.Canceled) {
		t.Fatalf("constant Evaluate() error = %v", err)
	}
	if matched, err := All(True(), False()).Evaluate(ctx, facts); err != nil || matched {
		t.Fatalf("All().Evaluate() = %v, %v", matched, err)
	}
	if _, err := All(True(), failed).Evaluate(ctx, facts); err == nil {
		t.Fatal("All().Evaluate() error = nil")
	}
	if matched, err := All(True(), True()).Evaluate(ctx, facts); err != nil || !matched {
		t.Fatalf("All true = %v, %v", matched, err)
	}
	if matched, err := Any(False(), True()).Evaluate(ctx, facts); err != nil || !matched {
		t.Fatalf("Any().Evaluate() = %v, %v", matched, err)
	}
	if _, err := Any(False(), failed).Evaluate(ctx, facts); err == nil {
		t.Fatal("Any().Evaluate() error = nil")
	}
	if matched, err := Any(False(), False()).Evaluate(ctx, facts); err != nil || matched {
		t.Fatalf("Any false = %v, %v", matched, err)
	}
	if matched, err := Not(False()).Evaluate(ctx, facts); err != nil || !matched {
		t.Fatalf("Not().Evaluate() = %v, %v", matched, err)
	}
	path := MustPath("fact")
	if _, err := Exists(path).Evaluate(canceled, facts); !errors.Is(err, context.Canceled) {
		t.Fatalf("Exists().Evaluate() error = %v", err)
	}
	comparison := Compare(OpEqual, Literal(Int(1)), Literal(Int(1)))
	if matched, err := comparison.Evaluate(ctx, facts); err != nil || !matched {
		t.Fatalf("Compare().Evaluate() = %v, %v", matched, err)
	}
	if _, err := comparison.Evaluate(canceled, facts); !errors.Is(err, context.Canceled) {
		t.Fatalf("Compare canceled error = %v", err)
	}
}

func TestOperatorEdgeMatrix(t *testing.T) {
	t.Parallel()

	if supportsSignature([]Signature{{Left: KindString, Right: KindString}}, KindInt, KindInt) {
		t.Fatal("supportsSignature() accepted mismatch")
	}
	if err := validateOperatorKinds(OpEqual, KindInt, KindString); !IsCode(err, CodeTypeMismatch) {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  OperatorName
		left  Kind
		right Kind
	}{
		{OpLessThan, KindBool, KindBool},
		{OpIn, KindInt, KindInt},
		{OpContains, KindInt, KindInt},
		{OpContains, KindString, KindInt},
		{OpStartsWith, KindString, KindInt},
	} {
		if err := validateOperatorKinds(test.name, test.left, test.right); err == nil {
			t.Fatalf("validateOperatorKinds(%s) error = nil", test.name)
		}
	}
	if err := validateOperatorKinds(OpEqual, KindMissing, KindInt); err != nil {
		t.Fatal(err)
	}
	if got, err := evaluateBuiltin(OpEqual, Int(1), Int(2)); err != nil || got {
		t.Fatalf("unequal = %v, %v", got, err)
	}
	if got, err := evaluateBuiltin(OpNotEqual, Int(1), Int(2)); err != nil || !got {
		t.Fatalf("not equal = %v, %v", got, err)
	}
	if got, _ := evaluateBuiltin(OpLessOrEqual, Int(2), Int(1)); got {
		t.Fatal("less or equal matched")
	}
	if got, _ := evaluateBuiltin(OpGreaterOrEqual, Int(1), Int(2)); got {
		t.Fatal("greater or equal matched")
	}
	if got, _ := evaluateBuiltin(OpContains, String("abc"), String("z")); got {
		t.Fatal("contains matched")
	}
	if got, _ := evaluateBuiltin(OpStartsWith, String("abc"), String("z")); got {
		t.Fatal("starts matched")
	}
	if got, _ := evaluateBuiltin(OpEndsWith, String("abc"), String("z")); got {
		t.Fatal("ends matched")
	}
	if got, _ := evaluateBuiltin(OpMatches, String("abc"), String("[")); got {
		t.Fatal("invalid regex matched")
	}
	if valuesEqual(List(Int(1)), List(Int(1), Int(2))) {
		t.Fatal("different list lengths equal")
	}
	if valuesEqual(List(Int(1)), List(Int(2))) {
		t.Fatal("different list values equal")
	}
	if !valuesEqual(List(Int(1)), List(Int(1))) {
		t.Fatal("equal lists differ")
	}
	if valuesEqual(Int(1), String("1")) {
		t.Fatal("different kinds equal")
	}
	for _, value := range []Value{Missing(), Null(), Bool(false), List()} {
		if compareOrdered(value, value) != 0 {
			t.Fatalf("compareOrdered(%v) != 0", value.kind)
		}
	}
	if compareOrdered(Value{kind: Kind(255)}, Value{kind: Kind(255)}) != 0 {
		t.Fatal("unknown ordering was nonzero")
	}
	if compareOrdered(Int(2), Int(1)) != 1 || compareOrdered(Int(1), Int(2)) != -1 ||
		compareOrdered(Int(1), Int(1)) != 0 {
		t.Fatal("integer ordering invalid")
	}
}

type stepContext struct{ calls int }

func (*stepContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*stepContext) Done() <-chan struct{}       { return nil }
func (ctx *stepContext) Err() error {
	ctx.calls++
	if ctx.calls > 1 {
		return context.Canceled
	}
	return nil
}
func (*stepContext) Value(any) any { return nil }

type nilOperator struct{}

func (nilOperator) Name() OperatorName                                   { return "nil" }
func (nilOperator) Signatures() []Signature                              { return []Signature{{Left: Kind(255), Right: KindInt}} }
func (nilOperator) Evaluate(context.Context, Value, Value) (bool, error) { return false, nil }

type opaquePredicate struct{}

func (opaquePredicate) Evaluate(context.Context, Context) (bool, error) { return true, nil }

func TestCompilerFailureBranches(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	bad := limits
	bad.MaxRules = 0
	if _, err := NewCompilerWithOperators(bad); !IsCode(err, CodeInvalidLimit) {
		t.Fatal(err)
	}
	if _, err := NewCompilerWithOperators(limits, nilOperator{}); !IsCode(err, CodeInvalidRule) {
		t.Fatal(err)
	}
	customCompiler, err := NewCompilerWithOperators(limits, stringOnlyOperator{})
	if err != nil {
		t.Fatal(err)
	}
	staticMismatch := RuleSet{ID: "mismatch", Rules: []Rule{{ID: "mismatch", When: Compare("string_only", Literal(Int(1)), Literal(Int(1)))}}}
	if _, _, err := customCompiler.Compile(context.Background(), staticMismatch); !IsCode(err, CodeTypeMismatch) {
		t.Fatalf("custom static mismatch error = %v", err)
	}
	rightNaN := RuleSet{ID: "nan", Rules: []Rule{{ID: "nan", When: Compare(OpEqual, Literal(Int(1)), Literal(Float(math.NaN())))}}}
	if _, _, err := NewCompiler(limits).Compile(context.Background(), rightNaN); !IsCode(err, CodeInvalidFact) {
		t.Fatalf("right NaN error = %v", err)
	}
	set := RuleSet{ID: "set", Rules: []Rule{{ID: "a", When: True()}, {ID: "b", When: True()}}}
	if _, _, err := NewCompiler(limits).Compile(&stepContext{}, set); !errors.Is(err, context.Canceled) {
		t.Fatalf("Compile(step) error = %v", err)
	}
	badCompiler := NewCompiler(bad)
	if _, _, err := badCompiler.Compile(context.Background(), set); !IsCode(err, CodeInvalidLimit) {
		t.Fatal(err)
	}
	invalidPredicates := []Predicate{
		All(), Any(), All(nil), Any(nil), Not(nil),
		Exists(Path{}),
	}
	if _, _, err := NewCompiler(limits).Compile(context.Background(), RuleSet{ID: "bad-namespace", Rules: []Rule{{ID: "rule", Namespace: "bad\n", When: True()}}}); err == nil {
		t.Fatal("invalid rule namespace error = nil")
	}
	if _, _, err := NewCompiler(limits).Compile(context.Background(), RuleSet{ID: "nil", Rules: []Rule{{ID: "nil"}}}); err == nil {
		t.Fatal("nil predicate error = nil")
	}
	for index, predicate := range invalidPredicates {
		set := RuleSet{ID: "invalid", Rules: []Rule{{ID: RuleID(string(rune('a' + index))), When: predicate}}}
		if _, _, err := NewCompiler(limits).Compile(context.Background(), set); err == nil {
			t.Fatalf("Compile(%T) error = nil", predicate)
		}
	}
	custom := PredicateFunc(func(context.Context, Context) (bool, error) { return true, nil })
	if _, _, err := NewCompiler(limits).Compile(context.Background(), RuleSet{ID: "custom", Rules: []Rule{{ID: "custom", When: custom}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewCompiler(limits).Compile(context.Background(), RuleSet{ID: "opaque", Rules: []Rule{{ID: "opaque", When: opaquePredicate{}}}}); err != nil {
		t.Fatal(err)
	}
	tooManyOperands := limits
	tooManyOperands.MaxOperands = 1
	if _, _, err := NewCompiler(tooManyOperands).Compile(context.Background(), RuleSet{ID: "wide", Rules: []Rule{{ID: "wide", When: All(True(), True())}}}); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("wide error = %v", err)
	}
	invalidDerived := RuleSet{ID: "derive", Rules: []Rule{{ID: "derive", When: True(), Derive: []Fact{{Path: Path{}, Value: Int(1)}}}}}
	if _, _, err := NewCompiler(limits).Compile(context.Background(), invalidDerived); !IsCode(err, CodeInvalidFact) {
		t.Fatalf("derived error = %v", err)
	}
	oneDerived := limits
	oneDerived.MaxDerivedFacts = 1
	pathA := MustPath("derived", "a")
	pathB := MustPath("derived", "b")
	tooManyDerived := RuleSet{ID: "derive", Rules: []Rule{{ID: "derive", When: True(), Derive: []Fact{{Path: pathA, Value: Int(1)}, {Path: pathB, Value: Int(2)}}}}}
	if _, _, err := NewCompiler(oneDerived).Compile(context.Background(), tooManyDerived); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("per-rule derived error = %v", err)
	}
	duplicateDerived := RuleSet{ID: "duplicate", Strategy: CollectAll, Rules: []Rule{
		{ID: "a", When: True(), Derive: []Fact{{Path: pathA, Value: Int(1)}}},
		{ID: "b", When: True(), Derive: []Fact{{Path: pathA, Value: Int(1)}}},
	}}
	if _, _, err := NewCompiler(limits).Compile(context.Background(), duplicateDerived); !IsCode(err, CodeDuplicateFact) {
		t.Fatalf("duplicate derived error = %v", err)
	}
	if _, _, err := NewCompiler(limits).Compile(context.Background(), RuleSet{ID: "nested", Rules: []Rule{{ID: "nested", When: Any(Not(nil))}}}); err == nil {
		t.Fatal("nested invalid predicate error = nil")
	}

	x := MustPath("graph", "x")
	y := MustPath("graph", "y")
	z := MustPath("graph", "z")
	_ = findDerivationCycle([]Rule{
		{ID: "xy", When: Exists(x), Derive: []Fact{{Path: y, Value: Int(1)}}},
		{ID: "xz", When: Exists(x), Derive: []Fact{{Path: z, Value: Int(1)}}},
		{ID: "yz", When: Exists(y), Derive: []Fact{{Path: z, Value: Int(1)}}},
	})
	compound := All(
		Compare(OpEqual, Literal(Int(1)), Variable(x)),
		Any(Not(Exists(y))),
		opaquePredicate{},
	)
	if got := predicatePaths(compound); len(got) < 2 {
		t.Fatalf("predicatePaths() = %#v", got)
	}
	paths := map[string]Path{}
	collectPredicatePaths(compound, paths)
	if len(paths) != 2 {
		t.Fatalf("collectPredicatePaths() = %#v", paths)
	}
}

type cacheStub struct {
	get func(context.Context, string) (Plan, bool, error)
	put func(context.Context, string, Plan) error
}

func (cache cacheStub) Get(ctx context.Context, key string) (Plan, bool, error) {
	return cache.get(ctx, key)
}
func (cache cacheStub) Put(ctx context.Context, key string, plan Plan) error {
	return cache.put(ctx, key, plan)
}

func TestCacheFailureAndReplacementBranches(t *testing.T) {
	t.Parallel()

	compiler := NewCompiler(DefaultLimits())
	set := RuleSet{ID: "cache", Rules: []Rule{{ID: "rule", When: True()}}}
	if _, _, err := compiler.CompileCached(context.Background(), set, nil); !IsCode(err, CodeCache) {
		t.Fatal(err)
	}
	if _, _, err := compiler.CompileCached(context.Background(), RuleSet{}, cacheStub{}); err == nil {
		t.Fatal("CompileCached(invalid) error = nil")
	}
	getError := cacheStub{
		get: func(context.Context, string) (Plan, bool, error) { return Plan{}, false, errors.New("read") },
		put: func(context.Context, string, Plan) error { return nil },
	}
	if _, _, err := compiler.CompileCached(context.Background(), set, getError); !IsCode(err, CodeCache) {
		t.Fatal(err)
	}
	putError := cacheStub{
		get: func(context.Context, string) (Plan, bool, error) { return Plan{hash: "wrong"}, true, nil },
		put: func(context.Context, string, Plan) error { return errors.New("write") },
	}
	if _, _, err := compiler.CompileCached(context.Background(), set, putError); !IsCode(err, CodeCache) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	miss := cacheStub{
		get: func(context.Context, string) (Plan, bool, error) { return Plan{}, false, nil },
		put: func(context.Context, string, Plan) error { return nil },
	}
	if _, _, err := compiler.CompileCached(canceled, set, miss); !errors.Is(err, context.Canceled) {
		t.Fatalf("CompileCached(canceled) error = %v", err)
	}

	cache, _ := NewMemoryPlanCache(1)
	if _, _, err := cache.Get(context.Background(), ""); !IsCode(err, CodeCache) {
		t.Fatal(err)
	}
	if err := cache.Put(context.Background(), "", Plan{}); !IsCode(err, CodeCache) {
		t.Fatal(err)
	}
	if _, ok, err := cache.Get(context.Background(), "missing"); err != nil || ok {
		t.Fatalf("Get(missing) = %v, %v", ok, err)
	}
	if err := cache.Put(context.Background(), "same", Plan{hash: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(context.Background(), "same", Plan{hash: "second"}); err != nil {
		t.Fatal(err)
	}
	plan, ok, err := cache.Get(context.Background(), "same")
	if err != nil || !ok || plan.hash != "second" {
		t.Fatalf("updated cache = %#v, %v, %v", plan, ok, err)
	}
}

type resolverStub struct {
	resolve func(context.Context, Path) (Value, Owner, bool, error)
}

func (resolver resolverStub) Resolve(ctx context.Context, path Path) (Value, Owner, bool, error) {
	return resolver.resolve(ctx, path)
}

type stringOnlyOperator struct{ fail bool }

func (stringOnlyOperator) Name() OperatorName { return "string_only" }
func (stringOnlyOperator) Signatures() []Signature {
	return []Signature{{Left: KindString, Right: KindString}}
}
func (operator stringOnlyOperator) Evaluate(context.Context, Value, Value) (bool, error) {
	if operator.fail {
		return false, errors.New("operator failure")
	}
	return true, nil
}

func TestPlanResolverConflictAndCustomOperatorBranches(t *testing.T) {
	t.Parallel()

	path := MustPath("fact", "value")
	set := RuleSet{ID: "resolve", Rules: []Rule{{ID: "rule", When: Exists(path)}}}
	plan, _, err := NewCompiler(DefaultLimits()).Compile(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := NewContext()
	if result := plan.EvaluateResolved(context.Background(), base, nil); result.Decision != Indeterminate {
		t.Fatalf("nil resolver result = %#v", result)
	}
	errorResolver := resolverStub{resolve: func(context.Context, Path) (Value, Owner, bool, error) {
		return Value{}, OwnerUnspecified, false, errors.New("resolve")
	}}
	if result := plan.EvaluateResolved(context.Background(), base, errorResolver); result.Decision != Indeterminate {
		t.Fatalf("error resolver result = %#v", result)
	}
	missingResolver := resolverStub{resolve: func(context.Context, Path) (Value, Owner, bool, error) {
		return Value{}, OwnerUnspecified, false, nil
	}}
	if result := plan.EvaluateResolved(context.Background(), base, missingResolver); result.Decision != Unmatched {
		t.Fatalf("missing resolver result = %#v", result)
	}
	invalidResolver := resolverStub{resolve: func(context.Context, Path) (Value, Owner, bool, error) {
		return Missing(), OwnerUnspecified, true, nil
	}}
	if result := plan.EvaluateResolved(context.Background(), base, invalidResolver); result.Decision != Indeterminate {
		t.Fatalf("invalid resolver result = %#v", result)
	}

	conflictSet := RuleSet{ID: "conflict", Strategy: CollectAll, Rules: []Rule{{
		ID: "derive", When: True(), Derive: []Fact{{Path: path, Value: Int(2)}},
	}}}
	conflictPlan, _, _ := NewCompiler(DefaultLimits()).Compile(context.Background(), conflictSet)
	existing, _ := NewContext(Fact{Path: path, Value: Int(1)})
	if result := conflictPlan.Evaluate(context.Background(), existing); result.Decision != Indeterminate ||
		!IsCode(result.Errors[0], CodeConflict) {
		t.Fatalf("conflict result = %#v", result)
	}

	multiple := RuleSet{ID: "multiple", Strategy: ErrorOnMultiple, Rules: []Rule{
		{ID: "a", When: True()}, {ID: "b", When: True()},
	}}
	multiplePlan, _, _ := NewCompiler(DefaultLimits()).Compile(context.Background(), multiple)
	if result := multiplePlan.Evaluate(context.Background(), base); result.Decision != Indeterminate ||
		!IsCode(result.Errors[0], CodeConflict) {
		t.Fatalf("multiple result = %#v", result)
	}

	if _, err := NewCompilerWithOperators(DefaultLimits(), stringOnlyOperator{}, stringOnlyOperator{fail: true}); err == nil {
		t.Fatal("duplicate custom operators error = nil")
	}
	goodCompiler, _ := NewCompilerWithOperators(DefaultLimits(), stringOnlyOperator{})
	customSet := RuleSet{ID: "custom", Rules: []Rule{{ID: "custom", When: Compare("string_only", Variable(path), Literal(String("x")))}}}
	customPlan, _, _ := goodCompiler.Compile(context.Background(), customSet)
	wrongFacts, _ := NewContext(Fact{Path: path, Value: Int(1)})
	if result := customPlan.Evaluate(context.Background(), wrongFacts); result.Decision != Indeterminate {
		t.Fatalf("custom mismatch result = %#v", result)
	}
	missingFacts, _ := NewContext()
	if result := customPlan.Evaluate(context.Background(), missingFacts); result.Decision != Unmatched {
		t.Fatalf("custom missing result = %#v", result)
	}

	failingCompiler, _ := NewCompilerWithOperators(DefaultLimits(), namedStringOperator{name: "failing", fail: true})
	failingSet := RuleSet{ID: "failing", Rules: []Rule{{ID: "failing", When: Compare("failing", Literal(String("x")), Literal(String("x")))}}}
	failingPlan, _, _ := failingCompiler.Compile(context.Background(), failingSet)
	if result := failingPlan.Evaluate(context.Background(), base); result.Decision != Indeterminate {
		t.Fatalf("custom failure result = %#v", result)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := failingPlan.evaluatePredicate(canceled, True(), base); !errors.Is(err, context.Canceled) {
		t.Fatalf("evaluatePredicate(canceled) error = %v", err)
	}
	if matched, err := failingPlan.evaluatePredicate(context.Background(), All(True(), True()), base); err != nil || !matched {
		t.Fatalf("compiled all = %v, %v", matched, err)
	}
	if matched, err := failingPlan.evaluatePredicate(context.Background(), Any(False(), False()), base); err != nil || matched {
		t.Fatalf("compiled any = %v, %v", matched, err)
	}
	if matched, err := failingPlan.evaluatePredicate(context.Background(), Not(False()), base); err != nil || !matched {
		t.Fatalf("compiled not = %v, %v", matched, err)
	}
}

type namedStringOperator struct {
	name OperatorName
	fail bool
}

func (operator namedStringOperator) Name() OperatorName { return operator.name }
func (namedStringOperator) Signatures() []Signature {
	return []Signature{{Left: KindString, Right: KindString}}
}
func (operator namedStringOperator) Evaluate(context.Context, Value, Value) (bool, error) {
	if operator.fail {
		return false, errors.New("failed")
	}
	return true, nil
}

type opaqueOperand struct{}

func (opaqueOperand) resolve(Context) Value    { return Int(1) }
func (opaqueOperand) staticKind() (Kind, bool) { return KindInt, true }

func TestCanonicalEncodingCoversEveryBuiltinVariant(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 19, 10, 11, 12, 13, time.FixedZone("EEST", 3*60*60))
	path := MustPath("fact", "value")
	values := []Value{
		Missing(), Null(), Bool(false), Int(-1), Float(1.5), String("text"),
		Time(now), Duration(time.Second), List(Int(1), String("x")),
	}
	for _, value := range values {
		encoded := encodeValue(value)
		decoded, err := decodeValue(encoded, DefaultLimits(), 0)
		if err != nil || !valuesEqual(value, decoded) {
			t.Fatalf("value round trip %v = %#v, %v", value.kind, decoded, err)
		}
	}

	predicates := []Predicate{
		True(), False(), Exists(path),
		Compare(OpEqual, Variable(path), Literal(Int(1))),
		All(True(), False()), Any(False(), True()), Not(False()),
	}
	for _, predicate := range predicates {
		encoded, err := encodePredicate(predicate)
		if err != nil {
			t.Fatalf("encodePredicate(%T) error = %v", predicate, err)
		}
		if _, err := decodePredicate(encoded, DefaultLimits(), 1); err != nil {
			t.Fatalf("decodePredicate(%s) error = %v", encoded.Kind, err)
		}
	}
	if _, err := encodePredicate(PredicateFunc(func(context.Context, Context) (bool, error) { return true, nil })); !IsCode(err, CodeNotSerializable) {
		t.Fatalf("encode custom predicate error = %v", err)
	}
	if _, err := encodeChildren("all", []Predicate{PredicateFunc(func(context.Context, Context) (bool, error) { return true, nil })}); err == nil {
		t.Fatal("encodeChildren(custom) error = nil")
	}
	if _, err := encodeOperand(opaqueOperand{}); !IsCode(err, CodeNotSerializable) {
		t.Fatalf("encodeOperand() error = %v", err)
	}
	if _, err := encodePredicate(Compare(OpEqual, opaqueOperand{}, Literal(Int(1)))); !IsCode(err, CodeNotSerializable) {
		t.Fatalf("encode opaque left error = %v", err)
	}
	if _, err := encodePredicate(Compare(OpEqual, Literal(Int(1)), opaqueOperand{})); !IsCode(err, CodeNotSerializable) {
		t.Fatalf("encode opaque right error = %v", err)
	}

	owners := []Owner{OwnerUnspecified, OwnerSubject, OwnerResource, OwnerEnvironment}
	derive := make([]Fact, len(owners))
	for index, owner := range owners {
		derive[index] = Fact{Path: MustPath("derived", string(rune('a'+index))), Owner: owner, Value: values[index+1]}
	}
	rule := Rule{ID: "encoded", Tags: []string{"z", "a", "a"}, When: True(), Derive: derive}
	encodedRule, err := encodeRule(rule)
	if err != nil || !reflect.DeepEqual(encodedRule.Tags, []string{"a", "z"}) {
		t.Fatalf("encodeRule() = %#v, %v", encodedRule, err)
	}
	if _, err := decodeRule(encodedRule, DefaultLimits()); err != nil {
		t.Fatalf("decodeRule() error = %v", err)
	}
	if got := joinPath([]string{"a", "b"}); got == "" {
		t.Fatal("joinPath() empty")
	}
	for _, strategy := range []ConflictStrategy{FirstMatch, CollectAll, ErrorOnMultiple, ConflictStrategy(255)} {
		name := strategyName(strategy)
		if strategy <= ErrorOnMultiple {
			if _, err := parseStrategy(name); err != nil {
				t.Fatalf("parseStrategy(%s) error = %v", name, err)
			}
		}
	}
	if _, err := parseStrategy("invalid"); err == nil {
		t.Fatal("parseStrategy(invalid) error = nil")
	}
	for _, owner := range append(owners, Owner(255)) {
		name := ownerName(owner)
		if owner <= OwnerEnvironment {
			if _, err := parseOwner(name); err != nil {
				t.Fatalf("parseOwner(%s) error = %v", name, err)
			}
		}
	}
	if _, err := parseOwner("invalid"); err == nil {
		t.Fatal("parseOwner(invalid) error = nil")
	}
	if kindName(Kind(255)) != "invalid" {
		t.Fatal("kindName(invalid) did not report invalid")
	}
}

func TestJSONDecoderRejectsEveryMalformedVariant(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	path := []string{"fact", "value"}
	validInt := jsonValue{Type: "int", Int: pointer(int64(1))}
	invalidValues := []jsonValue{
		{Type: "missing", Bool: pointer(true)},
		{Type: "null", List: []jsonValue{{Type: "null"}}},
		{Type: "bool"}, {Type: "int"}, {Type: "float"}, {Type: "string"},
		{Type: "time"}, {Type: "duration"}, {Type: "invalid"},
		{Type: "bool", Bool: pointer(true), Int: pointer(int64(1))},
		{Type: "int", Int: pointer(int64(1)), List: []jsonValue{}},
	}
	for _, value := range invalidValues {
		if _, err := decodeValue(value, limits, 0); !IsCode(err, CodeInvalidJSON) {
			t.Fatalf("decodeValue(%#v) error = %v", value, err)
		}
	}
	if _, err := decodeValue(jsonValue{Type: "time", Time: pointer("invalid")}, limits, 0); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("invalid time error = %v", err)
	}
	if _, err := decodeValue(validInt, limits, limits.MaxASTDepth+1); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("deep value error = %v", err)
	}
	tooLong := strings.Repeat("x", limits.MaxStringBytes+1)
	if _, err := decodeValue(jsonValue{Type: "string", String: &tooLong}, limits, 0); !IsCode(err, CodeInvalidFact) {
		t.Fatalf("long string error = %v", err)
	}
	badList := jsonValue{Type: "list", List: []jsonValue{{Type: "invalid"}}}
	if _, err := decodeValue(badList, limits, 0); err == nil {
		t.Fatal("nested invalid value error = nil")
	}

	invalidOperands := []jsonOperand{
		{Kind: "variable"},
		{Kind: "variable", Path: path, Value: &validInt},
		{Kind: "literal"},
		{Kind: "literal", Path: path, Value: &validInt},
		{Kind: "invalid"},
	}
	for _, operand := range invalidOperands {
		if _, err := decodeOperand(operand, limits); !IsCode(err, CodeInvalidJSON) {
			t.Fatalf("decodeOperand(%#v) error = %v", operand, err)
		}
	}
	if _, err := decodeOperand(jsonOperand{Kind: "variable", Path: []string{"bad.path"}}, limits); !IsCode(err, CodeInvalidPath) {
		t.Fatalf("invalid variable path error = %v", err)
	}
	invalidValue := jsonValue{Type: "invalid"}
	if _, err := decodeOperand(jsonOperand{Kind: "literal", Value: &invalidValue}, limits); err == nil {
		t.Fatal("invalid literal error = nil")
	}

	validLiteral := jsonOperand{Kind: "literal", Value: &validInt}
	validVariable := jsonOperand{Kind: "variable", Path: path}
	invalidNodes := []jsonNode{
		{Kind: "true", Path: path},
		{Kind: "exists"},
		{Kind: "compare", Operator: OpEqual, Left: &validLiteral},
		{Kind: "all"},
		{Kind: "not"},
		{Kind: "invalid"},
	}
	for _, node := range invalidNodes {
		if _, err := decodePredicate(node, limits, 1); !IsCode(err, CodeInvalidJSON) {
			t.Fatalf("decodePredicate(%#v) error = %v", node, err)
		}
	}
	if _, err := decodePredicate(jsonNode{Kind: "true"}, limits, limits.MaxASTDepth+1); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("deep predicate error = %v", err)
	}
	badExists := jsonNode{Kind: "exists", Path: []string{"bad.path"}}
	if _, err := decodePredicate(badExists, limits, 1); !IsCode(err, CodeInvalidPath) {
		t.Fatalf("bad exists error = %v", err)
	}
	badCompareLeft := jsonNode{Kind: "compare", Operator: OpEqual, Left: &jsonOperand{Kind: "invalid"}, Right: &validLiteral}
	if _, err := decodePredicate(badCompareLeft, limits, 1); err == nil {
		t.Fatal("bad left operand error = nil")
	}
	badCompareRight := jsonNode{Kind: "compare", Operator: OpEqual, Left: &validVariable, Right: &jsonOperand{Kind: "invalid"}}
	if _, err := decodePredicate(badCompareRight, limits, 1); err == nil {
		t.Fatal("bad right operand error = nil")
	}
	badChild := jsonNode{Kind: "all", Children: []jsonNode{{Kind: "invalid"}}}
	if _, err := decodePredicate(badChild, limits, 1); err == nil {
		t.Fatal("bad all child error = nil")
	}
	badAnyChild := jsonNode{Kind: "any", Children: []jsonNode{{Kind: "invalid"}}}
	if _, err := decodePredicate(badAnyChild, limits, 1); err == nil {
		t.Fatal("bad any child error = nil")
	}
	badNotChild := jsonNode{Kind: "not", Child: &jsonNode{Kind: "invalid"}}
	if _, err := decodePredicate(badNotChild, limits, 1); err == nil {
		t.Fatal("bad not child error = nil")
	}
}

func TestJSONRootAndRuleFailureBranches(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	badLimits := limits
	badLimits.MaxRules = 0
	if _, _, err := ParseJSON([]byte("{}"), badLimits); !IsCode(err, CodeInvalidLimit) {
		t.Fatal(err)
	}
	for _, input := range [][]byte{
		nil,
		[]byte("{"),
		[]byte(`{"version":"1","id":"x","strategy":"first_match","rules":[]} {}`),
		[]byte(`{"version":"2","id":"x","strategy":"first_match","rules":[]}`),
		[]byte(`{"version":"1","id":"x","strategy":"invalid","rules":[]}`),
	} {
		if _, _, err := ParseJSON(input, limits); err == nil {
			t.Fatalf("ParseJSON(%q) error = nil", input)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte("{}")))
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		t.Fatal(err)
	}
	decoder = json.NewDecoder(bytes.NewReader([]byte("{} {}")))
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	if err := ensureJSONEnd(decoder); err == nil {
		t.Fatal("ensureJSONEnd() trailing error = nil")
	}

	badRules := []jsonRule{
		{ID: "bad-path", When: jsonNode{Kind: "true"}, Derive: []jsonFact{{Path: []string{"bad.path"}, Owner: "subject", Value: jsonValue{Type: "int", Int: pointer(int64(1))}}}},
		{ID: "bad-value", When: jsonNode{Kind: "true"}, Derive: []jsonFact{{Path: []string{"x"}, Owner: "subject", Value: jsonValue{Type: "invalid"}}}},
		{ID: "bad-owner", When: jsonNode{Kind: "true"}, Derive: []jsonFact{{Path: []string{"x"}, Owner: "invalid", Value: jsonValue{Type: "int", Int: pointer(int64(1))}}}},
		{ID: "bad-predicate", When: jsonNode{Kind: "invalid"}},
	}
	for _, rule := range badRules {
		if _, err := decodeRule(rule, limits); err == nil {
			t.Fatalf("decodeRule(%s) error = nil", rule.ID)
		}
	}

	invalidCompiled := jsonDefinition{Version: "1", ID: "set", Strategy: "first_match", Rules: []jsonRule{{ID: "rule", When: jsonNode{Kind: "compare", Operator: "unknown", Left: &jsonOperand{Kind: "literal", Value: &jsonValue{Type: "int", Int: pointer(int64(1))}}, Right: &jsonOperand{Kind: "literal", Value: &jsonValue{Type: "int", Int: pointer(int64(1))}}}}}}
	data, _ := json.Marshal(invalidCompiled)
	if _, diagnostics, err := ParseJSON(data, limits); err == nil || len(diagnostics) == 0 {
		t.Fatalf("ParseJSON(compile failure) = %#v, %v", diagnostics, err)
	}

	if _, err := MarshalCanonical(RuleSet{}); err == nil {
		t.Fatal("MarshalCanonical(invalid) error = nil")
	}
	if _, err := CanonicalHash(RuleSet{}); err == nil {
		t.Fatal("CanonicalHash(invalid) error = nil")
	}
}

func pointer[T any](value T) *T { return &value }
