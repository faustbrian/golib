package ruleenginetemporal_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginetemporal "github.com/faustbrian/golib/pkg/rule-engine/adapters/temporal"
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

func TestOperatorsRejectInvalidPersistedPeriods(t *testing.T) {
	t.Parallel()

	equal := temporalOperatorByName(t, ruleenginetemporal.OpPeriodEqual)
	valid := ruleengine.String("period:2026-07-19T10:00:00Z|2026-07-19T11:00:00Z|[)")
	maxLength := ruleengine.String(
		"period:0000-01-01T00:00:00.000000000+23:59|" +
			"9999-12-31T23:59:59.999999999+23:59|[]",
	)
	if matched, err := equal.Evaluate(context.Background(), maxLength, maxLength); err != nil || !matched {
		t.Fatalf("maximum-length period = %t, %v", matched, err)
	}
	invalid := []ruleengine.Value{
		ruleengine.Int(1),
		ruleengine.String("2026-07-19T10:00:00Z|2026-07-19T11:00:00Z|[)"),
		ruleengine.String("period:v2:2026-07-19T10:00:00Z|2026-07-19T11:00:00Z|[)"),
		ruleengine.String("period:2026-07-19T10:00:00Z|2026-07-19T11:00:00Z"),
		ruleengine.String("period:2026-07-19T10:00:00Z|2026-07-19T11:00:00Z|[)|tail"),
		ruleengine.String("period:2026-02-30T10:00:00Z|2026-07-19T11:00:00Z|[)"),
		ruleengine.String("period:x026-07-19T10:00:00Z|2026-07-19T11:00:00Z|[)"),
		ruleengine.String("period:/026-07-19T10:00:00Z|2026-07-19T11:00:00Z|[)"),
		ruleengine.String("period:2026-07-19T10:00:60Z|2026-07-19T11:00:00Z|[)"),
		ruleengine.String("period:2026-07-19T10:00:00.1xZ|2026-07-19T11:00:00Z|[)"),
		ruleengine.String("period:2026-07-19T10:00:00.1234567890+23:59|2026-07-19T11:00:00Z|[)"),
		ruleengine.String("period:2026-07-19T10:00:00,1Z|2026-07-19T11:00:00Z|[)"),
		ruleengine.String("period:2026-07-19T10:00:00+24:00|2026-07-19T11:00:00Z|[)"),
		ruleengine.String("period:2026-07-19T10:00:00Z|2026-07-19T11:00:00Z|{}"),
		ruleengine.String("period:2026-07-19T11:00:00Z|2026-07-19T10:00:00Z|[)"),
		ruleengine.String("period:" + strings.Repeat("0", 82)),
	}
	for index, value := range invalid {
		if _, err := equal.Evaluate(context.Background(), value, valid); err == nil {
			t.Fatalf("invalid period %d accepted on left", index)
		}
		if _, err := equal.Evaluate(context.Background(), valid, value); err == nil {
			t.Fatalf("invalid period %d accepted on right", index)
		}
	}
}

func TestContainsInstantRejectsInvalidPersistedInstants(t *testing.T) {
	t.Parallel()

	contains := temporalOperatorByName(t, ruleenginetemporal.OpPeriodContains)
	period := mustEncodedPeriod(
		t,
		mustExternalPeriod(
			t,
			time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC),
			time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC),
			temporal.ClosedOpen,
		),
	)
	invalid := []ruleengine.Value{
		ruleengine.Int(1),
		ruleengine.String("2026-07-19T10:00:00Z"),
		ruleengine.String("instant:v2:2026-07-19T10:00:00Z"),
		ruleengine.String("instant:2026-07-19T10:00:00Ztail"),
		ruleengine.String("instant:2026/07-19T10:00:00Z"),
		ruleengine.String("instant:2026-07/19T10:00:00Z"),
		ruleengine.String("instant:2026-07-19 10:00:00Z"),
		ruleengine.String("instant:2026-07-19T10.00:00Z"),
		ruleengine.String("instant:2026-07-19T10:00.00Z"),
		ruleengine.String("instant:/026-07-19T10:00:00Z"),
		ruleengine.String("instant:2026-07-19T10:00:60Z"),
		ruleengine.String("instant:2026-07-19T10:00:00,1Z"),
		ruleengine.String("instant:2026-07-19T10:00:00.Z"),
		ruleengine.String("instant:2026-07-19T10:00:00.1xZ"),
		ruleengine.String("instant:2026-07-19T10:00:000"),
		ruleengine.String("instant:2026-07-19T10:00:00*00:00"),
		ruleengine.String("instant:2026-07-19T10:00:00+00000"),
		ruleengine.String("instant:2026-07-19T10:00:00+x0:00"),
		ruleengine.String("instant:2026-07-19T10:00:00+00:x0"),
		ruleengine.String("instant:2026-07-19T10:00:00+24:00"),
		ruleengine.String("instant:2026-07-19T10:00:00+23:60"),
		ruleengine.String("instant:" + strings.Repeat("0", 36)),
	}
	for index, value := range invalid {
		if _, err := contains.Evaluate(context.Background(), period, value); err == nil {
			t.Fatalf("invalid instant %d accepted", index)
		}
	}
}

func TestContainsInstantAcceptsTimestampGrammarBoundaries(t *testing.T) {
	t.Parallel()

	contains := temporalOperatorByName(t, ruleenginetemporal.OpPeriodContains)
	period := ruleengine.String(
		"period:0000-01-01T00:00:00Z|9999-12-31T23:59:59.999999999Z|[]",
	)
	valid := []string{
		"0000-01-01T00:00:00Z",
		"2026-07-19T10:00:00.0Z",
		"2026-07-19T10:00:00-00:00",
		"9999-12-31T23:59:59.999999999+23:59",
	}
	for _, timestamp := range valid {
		if _, err := contains.Evaluate(
			context.Background(),
			period,
			ruleengine.String("instant:"+timestamp),
		); err != nil {
			t.Fatalf("valid timestamp %q error = %v", timestamp, err)
		}
	}
}

func TestOperatorsRejectCancellationBeforeParsing(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	invalid := ruleengine.String("not temporal")
	for _, operator := range ruleenginetemporal.Operators() {
		if _, err := operator.Evaluate(canceled, invalid, invalid); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s error = %v", operator.Name(), err)
		}
	}
}

func TestPersistedInputErrorsDoNotEchoHostileValues(t *testing.T) {
	t.Parallel()

	sentinel := "do-not-echo"
	equal := temporalOperatorByName(t, ruleenginetemporal.OpPeriodEqual)
	valid := ruleengine.String(
		"period:2026-07-19T10:00:00Z|2026-07-19T11:00:00Z|[)",
	)
	for _, hostile := range []ruleengine.Value{
		ruleengine.String("period:" + sentinel + "|2026-07-19T11:00:00Z|[)"),
		ruleengine.String("period:2026-07-19T10:00:00Z|" + sentinel + "|[)"),
		ruleengine.String("period:2026-07-19T10:00:00Z|2026-07-19T11:00:00Z|" + sentinel),
	} {
		_, err := equal.Evaluate(context.Background(), hostile, valid)
		if err == nil {
			t.Fatalf("Evaluate(%#v) error = nil", hostile)
		}
		if strings.Contains(err.Error(), sentinel) {
			t.Fatalf("Evaluate() error echoed hostile input: %q", err)
		}
	}

	contains := temporalOperatorByName(t, ruleenginetemporal.OpPeriodContains)
	_, err := contains.Evaluate(
		context.Background(),
		valid,
		ruleengine.String("instant:"+sentinel),
	)
	if err == nil {
		t.Fatal("Evaluate(hostile instant) error = nil")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("Evaluate() error echoed hostile instant: %q", err)
	}
}
