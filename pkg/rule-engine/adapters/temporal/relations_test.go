package ruleenginetemporal_test

import (
	"context"
	"testing"
	"time"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginetemporal "github.com/faustbrian/golib/pkg/rule-engine/adapters/temporal"
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

func TestEveryAllenEndpointRelationAcrossAllBounds(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.July, 19, 10, 0, 0, 0, time.UTC)
	at := func(value int) time.Time { return base.Add(time.Duration(value) * time.Nanosecond) }
	layouts := []struct {
		name                 string
		leftStart, leftEnd   int
		rightStart, rightEnd int
	}{
		{"before", 0, 1, 2, 3},
		{"meets", 0, 1, 1, 2},
		{"overlaps", 0, 2, 1, 3},
		{"starts", 0, 1, 0, 2},
		{"during", 1, 2, 0, 3},
		{"finishes", 1, 3, 0, 3},
		{"equal", 0, 3, 0, 3},
		{"finished_by", 0, 3, 1, 3},
		{"contains", 0, 3, 1, 2},
		{"started_by", 0, 2, 0, 1},
		{"overlapped_by", 1, 3, 0, 2},
		{"met_by", 1, 2, 0, 1},
		{"after", 2, 3, 0, 1},
	}
	relations := []struct {
		name  ruleengine.OperatorName
		match func(instant.Period, instant.Period) bool
	}{
		{ruleenginetemporal.OpPeriodEqual, instant.Period.SetEqual},
		{ruleenginetemporal.OpPeriodBefore, instant.Period.IsBefore},
		{ruleenginetemporal.OpPeriodAfter, instant.Period.IsAfter},
		{ruleenginetemporal.OpPeriodOverlaps, instant.Period.Overlaps},
		{ruleenginetemporal.OpPeriodContainsPeriod, instant.Period.Contains},
	}
	for _, layout := range layouts {
		for _, leftBounds := range temporal.AllBounds() {
			for _, rightBounds := range temporal.AllBounds() {
				leftPeriod := mustExternalPeriod(t, at(layout.leftStart), at(layout.leftEnd), leftBounds)
				rightPeriod := mustExternalPeriod(t, at(layout.rightStart), at(layout.rightEnd), rightBounds)
				left := mustEncodedPeriod(t, leftPeriod)
				right := mustEncodedPeriod(t, rightPeriod)
				for _, relation := range relations {
					got, err := temporalOperatorByName(t, relation.name).Evaluate(context.Background(), left, right)
					want := relation.match(leftPeriod, rightPeriod)
					if err != nil || got != want {
						t.Fatalf("%s %s/%s %s = %t, %v; want %t", layout.name, leftBounds, rightBounds, relation.name, got, err, want)
					}
				}
			}
		}
	}
}

func TestZeroDurationPeriodsHaveExactSetSemantics(t *testing.T) {
	t.Parallel()

	point := time.Date(2026, time.July, 19, 10, 0, 0, 1, time.UTC)
	containsInstant := temporalOperatorByName(t, ruleenginetemporal.OpPeriodContains)
	overlaps := temporalOperatorByName(t, ruleenginetemporal.OpPeriodOverlaps)
	containsPeriod := temporalOperatorByName(t, ruleenginetemporal.OpPeriodContainsPeriod)
	for _, bounds := range temporal.AllBounds() {
		period := mustExternalPeriod(t, point, point, bounds)
		encoded := mustEncodedPeriod(t, period)
		member, err := containsInstant.Evaluate(context.Background(), encoded, mustEncodedInstant(t, point))
		if err != nil || member != (bounds == temporal.Closed) {
			t.Fatalf("%s singleton membership = %t, %v", bounds, member, err)
		}
		shared, err := overlaps.Evaluate(context.Background(), encoded, encoded)
		if err != nil || shared != (bounds == temporal.Closed) {
			t.Fatalf("%s self overlap = %t, %v", bounds, shared, err)
		}
		contained, err := containsPeriod.Evaluate(context.Background(), encoded, encoded)
		if err != nil || !contained {
			t.Fatalf("%s self containment = %t, %v", bounds, contained, err)
		}
	}
}
