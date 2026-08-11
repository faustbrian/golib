package ruleenginetemporal_test

import (
	"context"
	"strings"
	"testing"
	"time"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginetemporal "github.com/faustbrian/golib/pkg/rule-engine/adapters/temporal"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func BenchmarkPeriodContainsInstant(b *testing.B) {
	operator := temporalOperatorByName(
		b,
		ruleenginetemporal.OpPeriodContains,
	)
	start := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	period, err := instant.New(
		start,
		start.Add(time.Hour),
		temporal.ClosedOpen,
	)
	if err != nil {
		b.Fatal(err)
	}
	left := mustEncodedPeriod(b, period)
	right := mustEncodedInstant(b, start.Add(time.Minute))
	leftText, _ := left.StringValue()
	rightText, _ := right.StringValue()
	periodParts := strings.Split(strings.TrimPrefix(leftText, "period:"), "|")
	pointText := strings.TrimPrefix(rightText, "instant:")
	ctx := context.Background()

	b.Run("adapter_parse_and_evaluate", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			matched, evaluateErr := operator.Evaluate(ctx, left, right)
			if evaluateErr != nil || !matched {
				b.Fatalf("Evaluate() = %t, %v", matched, evaluateErr)
			}
		}
	})
	b.Run("direct_parse_construct_and_membership", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			parsedStart, parseErr := time.ParseInLocation(time.RFC3339Nano, periodParts[0], time.UTC)
			if parseErr != nil {
				b.Fatal(parseErr)
			}
			parsedEnd, parseErr := time.ParseInLocation(time.RFC3339Nano, periodParts[1], time.UTC)
			if parseErr != nil {
				b.Fatal(parseErr)
			}
			var bounds temporal.Bounds
			if parseErr = bounds.UnmarshalText([]byte(periodParts[2])); parseErr != nil {
				b.Fatal(parseErr)
			}
			parsedPeriod, parseErr := instant.New(parsedStart.UTC(), parsedEnd.UTC(), bounds)
			if parseErr != nil {
				b.Fatal(parseErr)
			}
			point, parseErr := time.ParseInLocation(time.RFC3339Nano, pointText, time.UTC)
			if parseErr != nil || !parsedPeriod.Includes(point.UTC()) {
				b.Fatalf("direct parse and membership = %v", parseErr)
			}
		}
	})
	b.Run("direct_temporal_membership", func(b *testing.B) {
		point := start.Add(time.Minute)
		b.ReportAllocs()
		for b.Loop() {
			if !period.Includes(point) {
				b.Fatal("Includes() = false")
			}
		}
	})
}

func temporalOperatorByName(
	tb testing.TB,
	name ruleengine.OperatorName,
) ruleengine.Operator {
	tb.Helper()
	for _, operator := range ruleenginetemporal.Operators() {
		if operator.Name() == name {
			return operator
		}
	}
	tb.Fatalf("operator %q is missing", name)
	panic("unreachable")
}
