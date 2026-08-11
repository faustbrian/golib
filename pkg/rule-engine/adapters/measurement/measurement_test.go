package ruleenginemeasurement_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemeasurement "github.com/faustbrian/golib/pkg/rule-engine/adapters/measurement"
)

func TestQuantityUsesCanonicalVersionedEncoding(t *testing.T) {
	t.Parallel()

	value := ruleenginemeasurement.Quantity(
		measurement.MustNew(decimal.MustParse("1.5"), measurement.Kilogram),
	)
	text, ok := value.StringValue()
	if !ok || text != "quantity:v1|1.5|kg" {
		t.Fatalf("Quantity() = %q, %t", text, ok)
	}
}

func TestQuantityOperatorsConvertExactCompatibleUnits(t *testing.T) {
	t.Parallel()

	compiler, err := ruleengine.NewCompilerWithOperators(ruleengine.DefaultLimits(), ruleenginemeasurement.Operators()...)
	if err != nil {
		t.Fatal(err)
	}
	weight := ruleengine.MustPath("shipment", "weight")
	limit := measurement.MustNew(decimal.MustParse("1"), measurement.Kilogram)
	actual := measurement.MustNew(decimal.MustParse("1001"), measurement.Gram)
	set := ruleengine.RuleSet{ID: "weight", Rules: []ruleengine.Rule{{
		ID: "over",
		When: ruleengine.Compare(ruleenginemeasurement.OpQuantityGreaterThan,
			ruleengine.Variable(weight), ruleengine.Literal(ruleenginemeasurement.Quantity(limit))),
	}}}
	plan, _, err := compiler.Compile(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	facts, _ := ruleengine.NewContext(ruleengine.Fact{Path: weight, Value: ruleenginemeasurement.Quantity(actual)})
	if result := plan.Evaluate(context.Background(), facts); result.Decision != ruleengine.Matched {
		t.Fatalf("Evaluate() = %#v", result)
	}
}

func TestQuantityOperatorsRejectInvalidAndNoncanonicalValues(t *testing.T) {
	t.Parallel()

	equal := measurementOperatorByName(t, ruleenginemeasurement.OpQuantityEqual)
	valid := ruleenginemeasurement.Quantity(
		measurement.MustNew(decimal.MustParse("1"), measurement.Kilogram),
	)
	tests := []struct {
		name  string
		value ruleengine.Value
		cause error
	}{
		{name: "wrong kind", value: ruleengine.Int(1)},
		{name: "legacy unversioned", value: ruleengine.String("quantity:1 kg")},
		{name: "unknown version", value: ruleengine.String("quantity:v2|1|kg")},
		{name: "missing amount", value: ruleengine.String("quantity:v1||kg")},
		{name: "missing unit", value: ruleengine.String("quantity:v1|1|")},
		{name: "additional field", value: ruleengine.String("quantity:v1|1|kg|extra")},
		{name: "noncanonical amount", value: ruleengine.String("quantity:v1|01|kg")},
		{name: "noncanonical negative zero", value: ruleengine.String("quantity:v1|-0|kg")},
		{name: "noncanonical scaled negative zero", value: ruleengine.String("quantity:v1|-0.00|kg")},
		{name: "unknown unit", value: ruleengine.String("quantity:v1|1|stone"), cause: measurement.ErrUnknownUnit},
		{name: "oversized", value: ruleengine.String("quantity:v1|" + strings.Repeat("9", ruleenginemeasurement.MaxTaggedValueBytes) + "|kg")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := equal.Evaluate(context.Background(), test.value, valid)
			if !errors.Is(err, ruleenginemeasurement.ErrInvalidQuantity) {
				t.Fatalf("Evaluate() error = %v, want ErrInvalidQuantity", err)
			}
			if test.cause != nil && !errors.Is(err, test.cause) {
				t.Fatalf("Evaluate() error = %v, want cause %v", err, test.cause)
			}
		})
	}
}

func TestQuantityOperatorsClassifyComparisonFailures(t *testing.T) {
	t.Parallel()

	equal := measurementOperatorByName(t, ruleenginemeasurement.OpQuantityEqual)
	tests := []struct {
		name      string
		left      measurement.Quantity
		right     measurement.Quantity
		class     error
		rootCause error
	}{
		{
			name:      "incompatible dimensions",
			left:      measurement.MustNew(decimal.New(1), measurement.Kilogram),
			right:     measurement.MustNew(decimal.New(1), measurement.Metre),
			class:     ruleenginemeasurement.ErrIncompatibleQuantity,
			rootCause: measurement.ErrDimensionMismatch,
		},
		{
			name:      "unrepresentable exact conversion",
			left:      measurement.MustNew(decimal.New(1), measurement.Celsius),
			right:     measurement.MustNew(decimal.New(1), measurement.Fahrenheit),
			class:     ruleenginemeasurement.ErrInvalidQuantity,
			rootCause: gomath.ErrConversion,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := equal.Evaluate(
				context.Background(),
				ruleenginemeasurement.Quantity(test.left),
				ruleenginemeasurement.Quantity(test.right),
			)
			if !errors.Is(err, test.class) || !errors.Is(err, test.rootCause) {
				t.Fatalf("Evaluate() error = %v, want %v and %v", err, test.class, test.rootCause)
			}
		})
	}
}

func TestOperatorsReturnFreshImmutableMetadata(t *testing.T) {
	t.Parallel()

	first := ruleenginemeasurement.Operators()
	first[0] = nil
	signatures := first[1].Signatures()
	signatures[0] = ruleengine.Signature{Left: ruleengine.KindInt, Right: ruleengine.KindInt}

	second := ruleenginemeasurement.Operators()
	if second[0] == nil {
		t.Fatal("Operators() reused caller-mutated storage")
	}
	want := ruleengine.Signature{Left: ruleengine.KindString, Right: ruleengine.KindString}
	if got := first[1].Signatures(); len(got) != 1 || got[0] != want {
		t.Fatalf("Signatures() = %#v", got)
	}
}

func TestQuantityOperatorsObserveCancellationBetweenBoundedStages(t *testing.T) {
	t.Parallel()

	equal := measurementOperatorByName(t, ruleenginemeasurement.OpQuantityEqual)
	value := ruleenginemeasurement.Quantity(
		measurement.MustNew(decimal.New(1), measurement.Kilogram),
	)
	for _, cancelAt := range []int{2, 3} {
		ctx := &cancelAtContext{cancelAt: cancelAt}
		if _, err := equal.Evaluate(ctx, value, value); !errors.Is(err, context.Canceled) {
			t.Fatalf("Evaluate(cancel at %d) error = %v", cancelAt, err)
		}
	}
}

func TestEquivalentQuantitiesSurviveCanonicalRulePersistence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  measurement.Quantity
		right measurement.Quantity
	}{
		{"dimensionless scale", measurement.MustNew(decimal.MustParse("1.00"), measurement.One), measurement.MustNew(decimal.MustParse("1.00"), measurement.One)},
		{"preserved scale", measurement.MustNew(decimal.MustParse("1.00"), measurement.Kilogram), measurement.MustNew(decimal.MustParse("1000.0"), measurement.Gram)},
		{"length", measurement.MustNew(decimal.New(1), measurement.Metre), measurement.MustNew(decimal.New(100), measurement.Centimetre)},
		{"area", measurement.MustNew(decimal.New(1), measurement.SquareMetre), measurement.MustNew(decimal.New(10_000), measurement.SquareCentimetre)},
		{"volume", measurement.MustNew(decimal.New(1), measurement.Litre), measurement.MustNew(decimal.New(1000), measurement.Millilitre)},
		{"density", measurement.MustNew(decimal.New(1), measurement.GramPerCubicCentimetre), measurement.MustNew(decimal.New(1000), measurement.KilogramPerCubicMetre)},
		{"temperature", measurement.MustNew(decimal.New(0), measurement.Celsius), measurement.MustNew(decimal.MustParse("273.15"), measurement.Kelvin)},
		{"loading metre scale", measurement.MustNew(decimal.MustParse("1.00"), measurement.LoadingMetre), measurement.MustNew(decimal.MustParse("1.00"), measurement.LoadingMetre)},
	}
	compiler, err := ruleengine.NewCompilerWithOperators(ruleengine.DefaultLimits(), ruleenginemeasurement.Operators()...)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := ruleengine.NewContext()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set := ruleengine.RuleSet{ID: "persisted", Rules: []ruleengine.Rule{{
				ID: "equivalent",
				When: ruleengine.Compare(ruleenginemeasurement.OpQuantityEqual,
					ruleengine.Literal(ruleenginemeasurement.Quantity(test.left)),
					ruleengine.Literal(ruleenginemeasurement.Quantity(test.right))),
			}}}
			encoded, marshalErr := compiler.MarshalCanonical(set)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			decoded, diagnostics, parseErr := compiler.ParseJSON(encoded)
			if parseErr != nil || len(diagnostics) != 0 {
				t.Fatalf("ParseJSON() = %#v, %v", diagnostics, parseErr)
			}
			reencoded, marshalErr := compiler.MarshalCanonical(decoded)
			if marshalErr != nil || !bytes.Equal(reencoded, encoded) {
				t.Fatalf("canonical round trip changed: %s, %v", reencoded, marshalErr)
			}
			plan, _, compileErr := compiler.Compile(context.Background(), decoded)
			if compileErr != nil {
				t.Fatal(compileErr)
			}
			if result := plan.Evaluate(context.Background(), facts); result.Decision != ruleengine.Matched {
				t.Fatalf("persisted comparison = %#v", result)
			}
		})
	}
}

func TestOperatorsDoNotMutateTheDefaultRegistry(t *testing.T) {
	t.Parallel()

	set := ruleengine.RuleSet{ID: "isolated", Rules: []ruleengine.Rule{{
		ID: "custom",
		When: ruleengine.Compare(ruleenginemeasurement.OpQuantityEqual,
			ruleengine.Literal(ruleenginemeasurement.Quantity(measurement.MustNew(decimal.New(1), measurement.Kilogram))),
			ruleengine.Literal(ruleenginemeasurement.Quantity(measurement.MustNew(decimal.New(1), measurement.Kilogram)))),
	}}}
	assertUnknown := func() {
		t.Helper()
		if _, _, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(context.Background(), set); !ruleengine.IsCode(err, ruleengine.CodeUnknownOperator) {
			t.Fatalf("default compiler accepted an unregistered operator: %v", err)
		}
	}
	assertUnknown()
	registered, err := ruleengine.NewCompilerWithOperators(ruleengine.DefaultLimits(), ruleenginemeasurement.Operators()...)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := registered.Compile(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	assertUnknown()
}

type cancelAtContext struct {
	calls    int
	cancelAt int
}

func (*cancelAtContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*cancelAtContext) Done() <-chan struct{}       { return nil }
func (ctx *cancelAtContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}
func (*cancelAtContext) Value(any) any { return nil }
