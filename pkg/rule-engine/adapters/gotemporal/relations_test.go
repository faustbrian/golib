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

func TestPeriodOperatorsMatchTemporalSetPredicates(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.July, 19, 10, 0, 0, 1, time.UTC)
	allBounds := temporal.AllBounds()
	periods := make([]instant.Period, 0, 5*len(allBounds))
	for _, bounds := range allBounds {
		for _, endpoints := range [][2]time.Time{
			{base, base},
			{base, base.Add(time.Nanosecond)},
			{base, base.Add(time.Hour)},
			{base.Add(time.Hour), base.Add(2 * time.Hour)},
			{base.Add(30 * time.Minute), base.Add(90 * time.Minute)},
		} {
			periods = append(periods, mustExternalPeriod(t, endpoints[0], endpoints[1], bounds))
		}
	}

	operators := map[ruleengine.OperatorName]ruleengine.Operator{}
	for _, operator := range ruleenginetemporal.Operators() {
		operators[operator.Name()] = operator
	}
	relations := []struct {
		name  ruleengine.OperatorName
		match func(instant.Period, instant.Period) bool
	}{
		{name: ruleenginetemporal.OpPeriodEqual, match: instant.Period.SetEqual},
		{name: ruleenginetemporal.OpPeriodBefore, match: instant.Period.IsBefore},
		{name: ruleenginetemporal.OpPeriodAfter, match: instant.Period.IsAfter},
		{name: ruleenginetemporal.OpPeriodOverlaps, match: instant.Period.Overlaps},
		{name: ruleenginetemporal.OpPeriodContainsPeriod, match: instant.Period.Contains},
	}

	for leftIndex, left := range periods {
		for rightIndex, right := range periods {
			for _, relation := range relations {
				operator, ok := operators[relation.name]
				if !ok {
					t.Fatalf("operator %q is missing", relation.name)
				}
				matched, err := operator.Evaluate(
					context.Background(),
					mustEncodedPeriod(t, left),
					mustEncodedPeriod(t, right),
				)
				if err != nil {
					t.Fatalf("%s periods[%d], periods[%d]: %v", relation.name, leftIndex, rightIndex, err)
				}
				if expected := relation.match(left, right); matched != expected {
					t.Fatalf(
						"%s periods[%d], periods[%d] = %t, want %t",
						relation.name,
						leftIndex,
						rightIndex,
						matched,
						expected,
					)
				}
			}
		}
	}
}

func TestContainsInstantMatchesTemporalMembershipAtNanosecondBoundaries(t *testing.T) {
	t.Parallel()

	operator := temporalOperatorByName(t, ruleenginetemporal.OpPeriodContains)
	base := time.Date(2026, time.July, 19, 10, 0, 0, 123456789, time.UTC)
	for _, bounds := range temporal.AllBounds() {
		period := mustExternalPeriod(t, base, base.Add(time.Nanosecond), bounds)
		for _, point := range []time.Time{
			base.Add(-time.Nanosecond),
			base,
			base.Add(time.Nanosecond),
			base.Add(2 * time.Nanosecond),
		} {
			matched, err := operator.Evaluate(
				context.Background(),
				mustEncodedPeriod(t, period),
				mustEncodedInstant(t, point),
			)
			if err != nil {
				t.Fatal(err)
			}
			if expected := period.Includes(point); matched != expected {
				t.Fatalf("bounds %s point %s = %t, want %t", bounds, point, matched, expected)
			}
		}
	}
}

func TestEquivalentOffsetTagsRepresentTheSameUTCInstant(t *testing.T) {
	t.Parallel()

	equal := temporalOperatorByName(t, ruleenginetemporal.OpPeriodEqual)
	left := ruleengine.String(
		"period:2026-07-19T12:30:00.000000001+03:00|" +
			"2026-07-19T13:30:00.000000001+03:00|[]",
	)
	right := ruleengine.String(
		"period:2026-07-19T09:30:00.000000001Z|" +
			"2026-07-19T10:30:00.000000001Z|[]",
	)
	matched, err := equal.Evaluate(context.Background(), left, right)
	if err != nil || !matched {
		t.Fatalf("Evaluate() = %t, %v", matched, err)
	}

	contains := temporalOperatorByName(t, ruleenginetemporal.OpPeriodContains)
	matched, err = contains.Evaluate(
		context.Background(),
		left,
		ruleengine.String("instant:2026-07-19T09:30:00.000000001Z"),
	)
	if err != nil || !matched {
		t.Fatalf("contains Evaluate() = %t, %v", matched, err)
	}
}

func TestOperatorSetsAndSignaturesAreMutationIsolated(t *testing.T) {
	t.Parallel()

	first := ruleenginetemporal.Operators()
	first[0] = nil
	signatures := first[1].Signatures()
	signatures[0] = ruleengine.Signature{Left: ruleengine.KindInt, Right: ruleengine.KindInt}

	second := ruleenginetemporal.Operators()
	if second[0] == nil || second[0].Name() != ruleenginetemporal.OpPeriodEqual {
		t.Fatalf("Operators() reused caller-mutated storage: %#v", second[0])
	}
	if got := second[1].Signatures(); len(got) != 1 ||
		got[0] != (ruleengine.Signature{Left: ruleengine.KindString, Right: ruleengine.KindString}) {
		t.Fatalf("Signatures() reused caller-mutated storage: %#v", got)
	}
}
