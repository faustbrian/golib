package ruleenginetemporal_test

import (
	"context"
	"strings"
	"testing"
	"time"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginetemporal "github.com/faustbrian/golib/pkg/rule-engine/adapters/gotemporal"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func TestTemporalValuesEncodeCanonicalUTCAndRejectOutOfRange(t *testing.T) {
	t.Parallel()

	offset := time.FixedZone("example", 2*60*60+30*60)
	point := time.Date(2026, time.July, 19, 12, 0, 0, 123456789, offset)
	encodedInstant, err := ruleenginetemporal.Instant(point)
	if err != nil {
		t.Fatal(err)
	}
	instantText, ok := encodedInstant.StringValue()
	if !ok || instantText != "instant:2026-07-19T09:30:00.123456789Z" {
		t.Fatalf("Instant() = %q, %t", instantText, ok)
	}

	period := mustExternalPeriod(t, point, point.Add(time.Nanosecond), temporal.OpenClosed)
	encodedPeriod, err := ruleenginetemporal.Period(period)
	if err != nil {
		t.Fatal(err)
	}
	periodText, ok := encodedPeriod.StringValue()
	if !ok || periodText != "period:2026-07-19T09:30:00.123456789Z|2026-07-19T09:30:00.12345679Z|(]" {
		t.Fatalf("Period() = %q, %t", periodText, ok)
	}

	for _, year := range []int{-1, 10_000} {
		if _, encodeErr := ruleenginetemporal.Instant(time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)); encodeErr == nil {
			t.Fatalf("Instant(year %d) error = nil", year)
		}

		outOfRange := mustExternalPeriod(
			t,
			time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(year, 1, 1, 0, 0, 0, 1, time.UTC),
			temporal.Closed,
		)
		if _, encodeErr := ruleenginetemporal.Period(outOfRange); encodeErr == nil {
			t.Fatalf("Period(year %d) error = nil", year)
		}
	}
	endOutOfRange := mustExternalPeriod(
		t,
		time.Date(9999, 12, 31, 23, 59, 59, 999999998, time.UTC),
		time.Date(10_000, 1, 1, 0, 0, 0, 0, time.UTC),
		temporal.Closed,
	)
	if _, encodeErr := ruleenginetemporal.Period(endOutOfRange); encodeErr == nil {
		t.Fatal("Period() accepted an out-of-range end")
	}

	if strings.Contains(instantText, "+02:30") || strings.Contains(periodText, "+02:30") {
		t.Fatal("encoded values retained a source offset")
	}
}

func TestPeriodContainsInstant(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 19, 10, 0, 0, 0, time.UTC)
	period, err := instant.New(start, start.Add(time.Hour), temporal.ClosedOpen)
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := ruleengine.NewCompilerWithOperators(ruleengine.DefaultLimits(), ruleenginetemporal.Operators()...)
	if err != nil {
		t.Fatal(err)
	}
	window := ruleengine.MustPath("delivery", "window")
	set := ruleengine.RuleSet{ID: "window", Rules: []ruleengine.Rule{{
		ID: "inside",
		When: ruleengine.Compare(ruleenginetemporal.OpPeriodContains,
			ruleengine.Variable(window), ruleengine.Literal(mustEncodedInstant(t, start.Add(time.Minute)))),
	}}}
	plan, _, err := compiler.Compile(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	facts, _ := ruleengine.NewContext(ruleengine.Fact{Path: window, Value: mustEncodedPeriod(t, period)})
	if result := plan.Evaluate(context.Background(), facts); result.Decision != ruleengine.Matched {
		t.Fatalf("Evaluate() = %#v", result)
	}
}

func TestPeriodSetOperatorsPreserveEqualEndpointBounds(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.July, 19, 10, 0, 0, 123, time.UTC)
	closed := mustExternalPeriod(t, base, base.Add(time.Hour), temporal.Closed)
	open := mustExternalPeriod(t, base, base.Add(time.Hour), temporal.Open)
	adjacentClosed := mustExternalPeriod(
		t,
		base.Add(time.Hour),
		base.Add(2*time.Hour),
		temporal.Closed,
	)

	tests := []struct {
		name     ruleengine.OperatorName
		left     instant.Period
		right    instant.Period
		expected bool
	}{
		{name: ruleenginetemporal.OpPeriodEqual, left: closed, right: open, expected: false},
		{name: ruleenginetemporal.OpPeriodOverlaps, left: closed, right: adjacentClosed, expected: true},
		{name: ruleenginetemporal.OpPeriodContainsPeriod, left: closed, right: open, expected: true},
		{name: ruleenginetemporal.OpPeriodContainsPeriod, left: open, right: closed, expected: false},
	}

	for _, test := range tests {
		t.Run(string(test.name), func(t *testing.T) {
			t.Parallel()

			operator := temporalOperatorByName(t, test.name)
			matched, err := operator.Evaluate(
				context.Background(),
				mustEncodedPeriod(t, test.left),
				mustEncodedPeriod(t, test.right),
			)
			if err != nil {
				t.Fatal(err)
			}
			if matched != test.expected {
				t.Fatalf("Evaluate() = %t, want %t", matched, test.expected)
			}
		})
	}
}

func TestOperatorsRejectSubNanosecondPersistedPrecision(t *testing.T) {
	t.Parallel()

	equal := temporalOperatorByName(t, ruleenginetemporal.OpPeriodEqual)
	period := ruleengine.String(
		"period:2026-07-19T10:00:00.1234567891Z|" +
			"2026-07-19T11:00:00Z|[)",
	)
	if _, err := equal.Evaluate(context.Background(), period, period); err == nil {
		t.Fatal("period with subnanosecond precision was accepted")
	}

	contains := temporalOperatorByName(t, ruleenginetemporal.OpPeriodContains)
	validPeriod := mustEncodedPeriod(
		t,
		mustExternalPeriod(
			t,
			time.Date(2026, time.July, 19, 10, 0, 0, 0, time.UTC),
			time.Date(2026, time.July, 19, 11, 0, 0, 0, time.UTC),
			temporal.ClosedOpen,
		),
	)
	instant := ruleengine.String("instant:2026-07-19T10:30:00.1234567891Z")
	if _, err := contains.Evaluate(context.Background(), validPeriod, instant); err == nil {
		t.Fatal("instant with subnanosecond precision was accepted")
	}
}

func mustExternalPeriod(
	t testing.TB,
	start time.Time,
	end time.Time,
	bounds temporal.Bounds,
) instant.Period {
	t.Helper()
	period, err := instant.New(start, end, bounds)
	if err != nil {
		t.Fatal(err)
	}

	return period
}

func mustEncodedPeriod(t testing.TB, period instant.Period) ruleengine.Value {
	t.Helper()
	value, err := ruleenginetemporal.Period(period)
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func mustEncodedInstant(t testing.TB, point time.Time) ruleengine.Value {
	t.Helper()
	value, err := ruleenginetemporal.Instant(point)
	if err != nil {
		t.Fatal(err)
	}

	return value
}
