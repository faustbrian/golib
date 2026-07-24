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
		{0, Period(period), Period(equal)},
		{1, Period(period), Period(after)},
		{2, Period(period), Period(before)},
		{3, Period(period), Period(overlap)},
		{3, Period(overlap), Period(period)},
		{4, Period(period), Period(during)},
		{5, Period(period), Instant(base.Add(time.Hour))},
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
	if matched, err := operators[0].Evaluate(context.Background(), Period(period), Period(after)); err != nil || matched {
		t.Fatalf("non-match = %v, %v", matched, err)
	}
	if matched, err := operators[5].Evaluate(context.Background(), Period(period), Instant(base.Add(10*time.Hour))); err != nil || matched {
		t.Fatalf("outside = %v, %v", matched, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := operators[0].Evaluate(canceled, Period(period), Period(equal)); !errors.Is(err, context.Canceled) {
		t.Fatalf("relation canceled error = %v", err)
	}
	if _, err := operators[5].Evaluate(canceled, Period(period), Instant(base)); !errors.Is(err, context.Canceled) {
		t.Fatalf("contains canceled error = %v", err)
	}

	invalidPeriods := []ruleengine.Value{
		ruleengine.Int(1),
		ruleengine.String(periodPrefix + "invalid"),
		ruleengine.String(periodPrefix + "invalid|2026-07-19T10:00:00Z|[)"),
		ruleengine.String(periodPrefix + "2026-07-19T10:00:00Z|invalid|[)"),
		ruleengine.String(periodPrefix + "2026-07-19T10:00:00Z|2026-07-19T11:00:00Z|bad"),
		ruleengine.String(periodPrefix + "2026-07-19T11:00:00Z|2026-07-19T10:00:00Z|[)"),
	}
	for _, invalid := range invalidPeriods {
		if _, err := operators[0].Evaluate(context.Background(), invalid, Period(equal)); err == nil {
			t.Fatalf("invalid left %#v error = nil", invalid)
		}
		if _, err := operators[0].Evaluate(context.Background(), Period(equal), invalid); err == nil {
			t.Fatalf("invalid right %#v error = nil", invalid)
		}
	}
	empty := mustPeriod(t, base, base)
	if _, err := operators[0].Evaluate(context.Background(), Period(empty), Period(empty)); err == nil {
		t.Fatal("empty relation error = nil")
	}
	if _, err := operators[5].Evaluate(context.Background(), ruleengine.Int(1), Instant(base)); err == nil {
		t.Fatal("invalid contains period error = nil")
	}
	for _, invalid := range []ruleengine.Value{
		ruleengine.Int(1),
		ruleengine.String(instantPrefix + "invalid"),
	} {
		if _, err := operators[5].Evaluate(context.Background(), Period(period), invalid); err == nil {
			t.Fatalf("invalid instant %#v error = nil", invalid)
		}
	}
}

func mustPeriod(t *testing.T, start, end time.Time) instant.Period {
	t.Helper()
	period, err := instant.New(start, end, temporal.ClosedOpen)
	if err != nil {
		t.Fatal(err)
	}
	return period
}
