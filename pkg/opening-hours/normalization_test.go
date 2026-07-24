package openinghours_test

import (
	"testing"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func TestOvernightOverlapPoliciesUseOwnerDayCoordinates(t *testing.T) {
	first := mustRange(t, 22, 0, 2, 0)
	second := mustRange(t, 23, 0, 3, 0)

	_, err := openinghours.OpenRanges([]openinghours.Range{first, second}, openinghours.RejectOverlap)
	if !openinghours.IsCode(err, openinghours.CodeOverlap) {
		t.Fatalf("OpenRanges() error = %v, want overlap", err)
	}
	merged, err := openinghours.OpenRanges([]openinghours.Range{second, first}, openinghours.MergeOverlap)
	if err != nil {
		t.Fatal(err)
	}
	ranges := merged.Ranges()
	if len(ranges) != 1 || ranges[0].Start() != mustTime(t, 22, 0) ||
		ranges[0].End() != mustTime(t, 3, 0) || !ranges[0].Overnight() {
		t.Fatalf("merged ranges = %#v", ranges)
	}
}

func TestAdjacencyRequiresNamedPolicy(t *testing.T) {
	first := mustRange(t, 9, 0, 12, 0)
	second := mustRange(t, 12, 0, 15, 0)

	_, err := openinghours.OpenRanges(
		[]openinghours.Range{first, second}, openinghours.RejectOverlapAndAdjacent,
	)
	if !openinghours.IsCode(err, openinghours.CodeAdjacent) {
		t.Fatalf("OpenRanges() error = %v, want adjacent", err)
	}
	separate, err := openinghours.OpenRanges(
		[]openinghours.Range{first, second}, openinghours.RejectOverlap,
	)
	if err != nil || len(separate.Ranges()) != 2 {
		t.Fatalf("RejectOverlap result = %#v, error=%v", separate.Ranges(), err)
	}
	merged, err := openinghours.OpenRanges(
		[]openinghours.Range{first, second}, openinghours.MergeAdjacent,
	)
	if err != nil || len(merged.Ranges()) != 1 {
		t.Fatalf("MergeAdjacent result = %#v, error=%v", merged.Ranges(), err)
	}
}

func TestNormalizationRejectsContinuousOwnerRangeBeyondOneDay(t *testing.T) {
	first := mustRange(t, 0, 0, 23, 0)
	second := mustRange(t, 23, 0, 22, 0)

	_, err := openinghours.OpenRanges([]openinghours.Range{first, second}, openinghours.MergeAdjacent)
	if !openinghours.IsCode(err, openinghours.CodeDayBoundaryOverflow) {
		t.Fatalf("OpenRanges() error = %v, want day-boundary overflow", err)
	}
}

func TestNormalizationCanProduceExplicitAllDay(t *testing.T) {
	first := mustRange(t, 0, 0, 12, 0)
	second := mustRange(t, 12, 0, 0, 0)

	rule, err := openinghours.OpenRanges([]openinghours.Range{first, second}, openinghours.MergeAdjacent)
	if err != nil {
		t.Fatal(err)
	}
	if rule.State() != openinghours.DayOpenAllDay || len(rule.Ranges()) != 0 {
		t.Fatalf("OpenRanges() = state %v ranges %#v", rule.State(), rule.Ranges())
	}
}
