package ruleenginetemporal

import (
	"context"
	"errors"
	"testing"
	"time"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func TestTemporalOperatorTruthAndFailureTable(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.July, 19, 10, 0, 0, 0, time.UTC)
	period := mustPeriod(t, base, base.Add(4*time.Hour))
	equal := mustPeriod(t, base, base.Add(4*time.Hour))
	after := mustPeriod(t, base.Add(5*time.Hour), base.Add(6*time.Hour))
	before := mustPeriod(t, base.Add(-2*time.Hour), base.Add(-time.Hour))
	overlap := mustPeriod(t, base.Add(3*time.Hour), base.Add(5*time.Hour))
	during := mustPeriod(t, base.Add(time.Hour), base.Add(2*time.Hour))
	values := []struct {
		index int
		left  ruleengine.Value
		right ruleengine.Value
	}{
		{0, mustInternalPeriodValue(t, period), mustInternalPeriodValue(t, equal)},
		{1, mustInternalPeriodValue(t, period), mustInternalPeriodValue(t, after)},
		{2, mustInternalPeriodValue(t, period), mustInternalPeriodValue(t, before)},
		{3, mustInternalPeriodValue(t, period), mustInternalPeriodValue(t, overlap)},
		{3, mustInternalPeriodValue(t, overlap), mustInternalPeriodValue(t, period)},
		{4, mustInternalPeriodValue(t, period), mustInternalPeriodValue(t, during)},
		{5, mustInternalPeriodValue(t, period), mustInternalInstantValue(t, base.Add(time.Hour))},
	}
	operators := Operators()
	for _, test := range values {
		operator := operators[test.index]
		if operator.Name() == "" || len(operator.Signatures()) != 1 {
			t.Fatalf("operator metadata = %q, %#v", operator.Name(), operator.Signatures())
		}
		matched, err := operator.Evaluate(context.Background(), test.left, test.right)
		if err != nil || !matched {
			t.Fatalf("%s Evaluate() = %v, %v", operator.Name(), matched, err)
		}
	}
	if matched, err := operators[0].Evaluate(context.Background(), mustInternalPeriodValue(t, period), mustInternalPeriodValue(t, after)); err != nil || matched {
		t.Fatalf("non-match = %v, %v", matched, err)
	}
	if matched, err := operators[5].Evaluate(context.Background(), mustInternalPeriodValue(t, period), mustInternalInstantValue(t, base.Add(10*time.Hour))); err != nil || matched {
		t.Fatalf("outside = %v, %v", matched, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := operators[0].Evaluate(canceled, mustInternalPeriodValue(t, period), mustInternalPeriodValue(t, equal)); !errors.Is(err, context.Canceled) {
		t.Fatalf("relation canceled error = %v", err)
	}
	if _, err := operators[5].Evaluate(canceled, mustInternalPeriodValue(t, period), mustInternalInstantValue(t, base)); !errors.Is(err, context.Canceled) {
		t.Fatalf("contains canceled error = %v", err)
	}

	invalidPeriods := []ruleengine.Value{
		ruleengine.Int(1),
		ruleengine.String("2026-07-19T10:00:00Z|2026-07-19T11:00:00Z|[)"),
		ruleengine.String(periodPrefix + "invalid"),
		ruleengine.String(periodPrefix + "invalid|2026-07-19T10:00:00Z|[)"),
		ruleengine.String(periodPrefix + "2026-07-19T10:00:00Z|invalid|[)"),
		ruleengine.String(periodPrefix + "2026-07-19T10:00:00Z|2026-07-19T11:00:00Z|bad"),
		ruleengine.String(periodPrefix + "2026-07-19T11:00:00Z|2026-07-19T10:00:00Z|[)"),
	}
	for _, invalid := range invalidPeriods {
		if _, err := operators[0].Evaluate(context.Background(), invalid, mustInternalPeriodValue(t, equal)); err == nil {
			t.Fatalf("invalid left %#v error = nil", invalid)
		}
		if _, err := operators[0].Evaluate(context.Background(), mustInternalPeriodValue(t, equal), invalid); err == nil {
			t.Fatalf("invalid right %#v error = nil", invalid)
		}
	}
	empty := mustPeriod(t, base, base)
	if matched, err := operators[0].Evaluate(context.Background(), mustInternalPeriodValue(t, empty), mustInternalPeriodValue(t, empty)); err != nil || !matched {
		t.Fatalf("empty equality = %t, %v", matched, err)
	}
	if _, err := operators[5].Evaluate(context.Background(), ruleengine.Int(1), mustInternalInstantValue(t, base)); err == nil {
		t.Fatal("invalid contains period error = nil")
	}
	for _, invalid := range []ruleengine.Value{
		ruleengine.Int(1),
		ruleengine.String("2026-07-19T10:00:00Z"),
		ruleengine.String(instantPrefix + "invalid"),
	} {
		if _, err := operators[5].Evaluate(context.Background(), mustInternalPeriodValue(t, period), invalid); err == nil {
			t.Fatalf("invalid instant %#v error = nil", invalid)
		}
	}
}

func mustInternalPeriodValue(t testing.TB, period instant.Period) ruleengine.Value {
	t.Helper()
	value, err := Period(period)
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func mustInternalInstantValue(t testing.TB, point time.Time) ruleengine.Value {
	t.Helper()
	value, err := Instant(point)
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func mustPeriod(t *testing.T, start, end time.Time) instant.Period {
	t.Helper()
	period, err := instant.New(start, end, temporal.ClosedOpen)
	if err != nil {
		t.Fatal(err)
	}
	return period
}

func TestParseTimestampIgnoresAmbientLocalTimezone(t *testing.T) {
	originalLocal := time.Local
	t.Cleanup(func() { time.Local = originalLocal })
	time.Local = time.FixedZone("ambient", 2*60*60)

	parsed, err := parseTimestamp("2026-07-19T12:00:00+02:00")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Location() != time.UTC {
		t.Fatalf("parseTimestamp() location = %q, want UTC", parsed.Location())
	}
	if want := time.Date(2026, time.July, 19, 10, 0, 0, 0, time.UTC); !parsed.Equal(want) {
		t.Fatalf("parseTimestamp() = %s, want %s", parsed, want)
	}
}
