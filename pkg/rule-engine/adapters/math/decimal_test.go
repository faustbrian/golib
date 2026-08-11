package ruleenginemath_test

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemath "github.com/faustbrian/golib/pkg/rule-engine/adapters/math"
)

func TestDecimalEncodingIsCanonicalAndVersioned(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"zero":           "0",
		"negative":       "-123.4500",
		"positive scale": "0.00100",
		"integral":       "1200",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			encoded := ruleenginemath.Decimal(decimal.MustParse(input))
			text, ok := encoded.StringValue()
			if !ok || text != ruleenginemath.EncodingV1Prefix+input {
				t.Fatalf("Decimal(%q) = %q, %t", input, text, ok)
			}
		})
	}
}

func TestDecimalEncodingNormalizesNegativeScaleWithoutChangingValue(t *testing.T) {
	t.Parallel()

	value, err := decimal.FromBig(big.NewInt(12), 2, gomath.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	encoded := ruleenginemath.Decimal(value)
	text, ok := encoded.StringValue()
	if !ok || text != ruleenginemath.EncodingV1Prefix+"1200" {
		t.Fatalf("Decimal(12e2) = %q, %t", text, ok)
	}
	canonical := ruleenginemath.Decimal(decimal.MustParse("1200"))
	matched, err := operatorByName(t, ruleenginemath.OpDecimalEqual).Evaluate(context.Background(), encoded, canonical)
	if err != nil || !matched {
		t.Fatalf("normalized equality = %t, %v", matched, err)
	}
}

func TestDecimalEncodingCanonicalizesPositiveExponentZero(t *testing.T) {
	t.Parallel()

	value, err := decimal.FromBig(big.NewInt(0), 2, gomath.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	encoded := ruleenginemath.Decimal(value)
	text, ok := encoded.StringValue()
	if !ok || text != ruleenginemath.EncodingV1Prefix+"0" {
		t.Fatalf("Decimal(0e2) = %q, %t", text, ok)
	}
	matched, err := operatorByName(t, ruleenginemath.OpDecimalEqual).Evaluate(
		context.Background(), encoded, ruleenginemath.Decimal(decimal.New(0)),
	)
	if err != nil || !matched {
		t.Fatalf("positive-exponent zero equality = %t, %v", matched, err)
	}
}

func TestDecimalRuleCanonicalPersistenceRoundTrip(t *testing.T) {
	t.Parallel()

	compiler, err := ruleengine.NewCompilerWithOperators(
		ruleengine.DefaultLimits(), ruleenginemath.Operators()...,
	)
	if err != nil {
		t.Fatal(err)
	}
	amount := ruleengine.MustPath("order", "amount")
	set := ruleengine.RuleSet{ID: "decimal-persistence", Rules: []ruleengine.Rule{{
		ID: "minimum",
		When: ruleengine.Compare(
			ruleenginemath.OpDecimalGreaterOrEqual,
			ruleengine.Variable(amount),
			ruleengine.Literal(ruleenginemath.Decimal(decimal.MustParse("10.00"))),
		),
	}}}
	encoded, err := compiler.MarshalCanonical(set)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":"1","id":"decimal-persistence","strategy":"first_match","rules":[{"id":"minimum","priority":0,"tags":null,"when":{"kind":"compare","operator":"golib_decimal_v1_greater_or_equal","left":{"kind":"variable","path":["order","amount"]},"right":{"kind":"literal","value":{"type":"string","string":"golib.rule-engine.decimal/v1:10.00"}}},"derive":[]}]}`
	if string(encoded) != want {
		t.Fatalf("MarshalCanonical() = %s", encoded)
	}
	decoded, diagnostics, err := compiler.ParseJSON(encoded)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("ParseJSON() diagnostics = %#v, error = %v", diagnostics, err)
	}
	reencoded, err := compiler.MarshalCanonical(decoded)
	if err != nil || string(reencoded) != string(encoded) {
		t.Fatalf("canonical round trip = %s, %v; want %s", reencoded, err, encoded)
	}
	plan, _, err := compiler.Compile(context.Background(), decoded)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := ruleengine.NewContext(ruleengine.Fact{
		Path: amount, Value: ruleenginemath.Decimal(decimal.MustParse("10.0")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result := plan.Evaluate(context.Background(), facts); result.Decision != ruleengine.Matched {
		t.Fatalf("round-tripped Evaluate() = %#v", result)
	}
}

func TestDecimalOperatorsExposeStableNamesAndExactSignatures(t *testing.T) {
	t.Parallel()

	want := []ruleengine.OperatorName{
		"golib_decimal_v1_equal",
		"golib_decimal_v1_less_than",
		"golib_decimal_v1_less_or_equal",
		"golib_decimal_v1_greater_than",
		"golib_decimal_v1_greater_or_equal",
	}
	operators := ruleenginemath.Operators()
	if len(operators) != len(want) {
		t.Fatalf("len(Operators()) = %d, want %d", len(operators), len(want))
	}
	for index, operator := range operators {
		if operator.Name() != want[index] {
			t.Errorf("operator[%d].Name() = %q, want %q", index, operator.Name(), want[index])
		}
		signatures := operator.Signatures()
		if len(signatures) != 1 || signatures[0] != (ruleengine.Signature{Left: ruleengine.KindString, Right: ruleengine.KindString}) {
			t.Errorf("%s Signatures() = %#v", operator.Name(), signatures)
		}
		signatures[0] = ruleengine.Signature{}
		if operator.Signatures()[0].Left != ruleengine.KindString {
			t.Errorf("%s signatures share mutable state", operator.Name())
		}
	}
}

func TestDecimalOperatorPropertiesMatchDirectExactComparison(t *testing.T) {
	t.Parallel()

	values := []string{
		"-999999999999999999999.0001", "-10.00", "-2", "0.00", "0",
		"1.0", "1.00", "1.0000000000000000001", "10", "999999999999999999999.0001",
	}
	operators := ruleenginemath.Operators()
	for _, leftText := range values {
		leftDecimal := decimal.MustParse(leftText)
		left := ruleenginemath.Decimal(leftDecimal)
		for _, rightText := range values {
			rightDecimal := decimal.MustParse(rightText)
			right := ruleenginemath.Decimal(rightDecimal)
			comparison := leftDecimal.Cmp(rightDecimal)
			want := []bool{comparison == 0, comparison < 0, comparison <= 0, comparison > 0, comparison >= 0}
			for index, operator := range operators {
				matched, err := operator.Evaluate(context.Background(), left, right)
				if err != nil || matched != want[index] {
					t.Fatalf("%s(%s, %s) = %t, %v; want %t", operator.Name(), leftText, rightText, matched, err, want[index])
				}
			}
		}
	}
}

func TestDecimalOperatorsComposeWithIsolatedCompilerRegistration(t *testing.T) {
	t.Parallel()

	compiler, err := ruleengine.NewCompilerWithOperators(ruleengine.DefaultLimits(), ruleenginemath.Operators()...)
	if err != nil {
		t.Fatal(err)
	}
	amount := ruleengine.MustPath("shipment", "amount")
	set := ruleengine.RuleSet{ID: "decimal", Rules: []ruleengine.Rule{{
		ID: "larger",
		When: ruleengine.Compare(ruleenginemath.OpDecimalGreaterThan,
			ruleengine.Variable(amount), ruleengine.Literal(ruleenginemath.Decimal(decimal.MustParse("0.3")))),
	}}}
	plan, _, err := compiler.Compile(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := ruleengine.NewContext(ruleengine.Fact{Path: amount, Value: ruleenginemath.Decimal(decimal.MustParse("0.3000000000000000001"))})
	if err != nil {
		t.Fatal(err)
	}
	if result := plan.Evaluate(context.Background(), facts); result.Decision != ruleengine.Matched {
		t.Fatalf("Evaluate() = %#v", result)
	}
}

func TestDecimalOperatorsRejectInvalidInputsWithStableErrorIdentity(t *testing.T) {
	t.Parallel()

	equal := operatorByName(t, ruleenginemath.OpDecimalEqual)
	valid := ruleenginemath.Decimal(decimal.MustParse("1"))
	tests := []struct {
		name  string
		left  ruleengine.Value
		right ruleengine.Value
		want  error
	}{
		{"wrong left kind", ruleengine.Int(1), valid, ruleenginemath.ErrInvalidTaggedValue},
		{"wrong right kind", valid, ruleengine.Int(1), ruleenginemath.ErrInvalidTaggedValue},
		{"missing tag", ruleengine.String("1"), valid, ruleenginemath.ErrInvalidTaggedValue},
		{"legacy tag", ruleengine.String("decimal:1"), valid, ruleenginemath.ErrInvalidTaggedValue},
		{"unsupported version", ruleengine.String("golib.rule-engine.decimal/v2:1"), valid, ruleenginemath.ErrInvalidTaggedValue},
		{"empty payload", ruleengine.String(ruleenginemath.EncodingV1Prefix), valid, decimal.ErrInvalid},
		{"invalid decimal", ruleengine.String(ruleenginemath.EncodingV1Prefix + "one"), valid, decimal.ErrInvalid},
		{"noncanonical negative zero", ruleengine.String(ruleenginemath.EncodingV1Prefix + "-0"), valid, ruleenginemath.ErrNonCanonicalDecimal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := equal.Evaluate(context.Background(), test.left, test.right); !errors.Is(err, test.want) {
				t.Fatalf("Evaluate() error = %v, want identity %v", err, test.want)
			}
		})
	}
}

func TestDecimalOperatorErrorsDoNotDiscloseRejectedValues(t *testing.T) {
	t.Parallel()

	const secret = "customer-secret-value"
	value := ruleengine.String(ruleenginemath.EncodingV1Prefix + secret)
	_, err := operatorByName(t, ruleenginemath.OpDecimalEqual).Evaluate(
		context.Background(), value, value,
	)
	if err == nil {
		t.Fatal("Evaluate() error = nil")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Evaluate() error disclosed rejected value: %v", err)
	}
}

func TestDecimalOperatorsEnforceExplicitLimits(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	limits.MaxInputDigits = 3
	limits.MaxOutputDigits = 3
	limits.MaxExponentMagnitude = 2
	operators, err := ruleenginemath.OperatorsWithLimits(limits)
	if err != nil {
		t.Fatal(err)
	}
	equal := operators[0]
	valid := ruleengine.String(ruleenginemath.EncodingV1Prefix + "1")
	for _, input := range []string{"999", "0.01"} {
		value := ruleengine.String(ruleenginemath.EncodingV1Prefix + input)
		if _, err := equal.Evaluate(context.Background(), value, valid); err != nil {
			t.Fatalf("Evaluate(%q) exact limit error = %v", input, err)
		}
	}
	for _, input := range []string{"1000", "0.001"} {
		value := ruleengine.String(ruleenginemath.EncodingV1Prefix + input)
		if _, err := equal.Evaluate(context.Background(), value, valid); !errors.Is(err, gomath.ErrLimitExceeded) {
			t.Fatalf("Evaluate(%q) error = %v, want ErrLimitExceeded", input, err)
		}
	}

	invalid := limits
	invalid.MaxInputDigits = 0
	if _, err := ruleenginemath.OperatorsWithLimits(invalid); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("OperatorsWithLimits() error = %v, want ErrInvalidArgument", err)
	}
}

func TestDecimalOperatorsPreserveCancellationIdentity(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value := ruleenginemath.Decimal(decimal.MustParse("1"))
	if _, err := operatorByName(t, ruleenginemath.OpDecimalEqual).Evaluate(ctx, value, value); !errors.Is(err, context.Canceled) {
		t.Fatalf("Evaluate() error = %v, want exact context.Canceled", err)
	}
}

func TestDecimalOperatorSetsAreFreshAndSafeAcrossConcurrentEngines(t *testing.T) {
	t.Parallel()

	shared := ruleenginemath.Operators()
	firstCompiler, err := ruleengine.NewCompilerWithOperators(ruleengine.DefaultLimits(), shared...)
	if err != nil {
		t.Fatal(err)
	}
	secondCompiler, err := ruleengine.NewCompilerWithOperators(ruleengine.DefaultLimits(), shared...)
	if err != nil {
		t.Fatal(err)
	}
	path := ruleengine.MustPath("decimal")
	set := ruleengine.RuleSet{ID: "concurrent-decimal", Rules: []ruleengine.Rule{{
		ID: "equal",
		When: ruleengine.Compare(
			ruleenginemath.OpDecimalEqual,
			ruleengine.Variable(path),
			ruleengine.Literal(ruleenginemath.Decimal(decimal.MustParse("1.00"))),
		),
	}}}
	firstPlan, _, err := firstCompiler.Compile(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	secondPlan, _, err := secondCompiler.Compile(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	signatures := shared[0].Signatures()
	signatures[0] = ruleengine.Signature{}
	for index := range shared {
		shared[index] = nil
	}
	if fresh := ruleenginemath.Operators(); len(fresh) != 5 || fresh[0] == nil {
		t.Fatalf("Operators() reused caller-mutated storage: %#v", fresh)
	}
	facts, err := ruleengine.NewContext(ruleengine.Fact{
		Path: path, Value: ruleenginemath.Decimal(decimal.MustParse("1")),
	})
	if err != nil {
		t.Fatal(err)
	}
	plans := []ruleengine.Plan{firstPlan, secondPlan}
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				for _, plan := range plans {
					if result := plan.Evaluate(context.Background(), facts); result.Decision != ruleengine.Matched {
						t.Errorf("concurrent Evaluate() = %#v", result)
					}
				}
			}
		}()
	}
	wait.Wait()
}
