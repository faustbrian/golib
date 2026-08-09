package ruleenginetemporal_test

import (
	"context"
	"testing"
	"time"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginetemporal "github.com/faustbrian/golib/pkg/rule-engine/adapters/gotemporal"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func FuzzPeriodTaggedValues(f *testing.F) {
	f.Add("2026-01-01T00:00:00Z|2026-01-02T00:00:00Z|[)")
	f.Add("2026-01-01T00:00:00.123456789+02:00|2026-01-02T00:00:00Z|[]")
	f.Add("2026-01-01T00:00:00.1234567891Z|2026-01-02T00:00:00Z|[)")
	f.Add("2026-01-01T00:00:60Z|2026-01-02T00:00:00Z|[)")
	f.Add("not-a-period")

	equal := ruleenginetemporal.Operators()[0]
	f.Fuzz(func(t *testing.T, text string) {
		value := ruleengine.String("period:" + text)
		matched, err := equal.Evaluate(context.Background(), value, value)
		if err == nil && !matched {
			t.Fatalf("Evaluate(%q, %q) = false without an error", text, text)
		}
	})
}

func FuzzInstantTaggedValues(f *testing.F) {
	f.Add("2026-01-01T12:00:00Z")
	f.Add("2026-01-01T12:00:00.123456789+02:30")
	f.Add("2026-01-01T12:00:00.1234567891Z")
	f.Add("2026-01-01T12:00:60Z")
	f.Add("not-an-instant")

	start := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2100, time.January, 1, 0, 0, 0, 0, time.UTC)
	period, err := instant.New(start, end, temporal.ClosedOpen)
	if err != nil {
		f.Fatalf("create reference period: %v", err)
	}
	contains := ruleenginetemporal.Operators()[5]
	left := mustEncodedPeriod(f, period)

	f.Fuzz(func(t *testing.T, text string) {
		point, parseErr := time.Parse(time.RFC3339Nano, text)
		matched, evaluateErr := contains.Evaluate(
			context.Background(),
			left,
			ruleengine.String("instant:"+text),
		)
		if evaluateErr != nil {
			return
		}
		if parseErr != nil {
			t.Fatalf("Evaluate(%q) accepted an invalid instant", text)
		}
		if matched != period.Includes(point) {
			t.Fatalf(
				"Evaluate(%q) = %t, want %t",
				text,
				matched,
				period.Includes(point),
			)
		}
	})
}
