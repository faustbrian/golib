package openinghours_test

import (
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func TestMetadataDoesNotChangeAvailabilityButChangesSourceEquality(t *testing.T) {
	monday, _ := openinghours.OpenRanges([]openinghours.Range{mustRange(t, 9, 0, 12, 0)}, openinghours.RejectOverlap)
	base := openinghours.Config{
		Timezone: "UTC", Weekly: map[time.Weekday]openinghours.DayRule{time.Monday: monday},
	}
	leftConfig := base
	leftConfig.Metadata = openinghours.Metadata{Label: "Store", Source: "track", Revision: "1"}
	rightConfig := base
	rightConfig.Metadata = openinghours.Metadata{Label: "Renamed", Source: "track", Revision: "2"}
	left, _ := openinghours.NewSchedule(leftConfig)
	right, _ := openinghours.NewSchedule(rightConfig)

	if left.Equal(right) {
		t.Fatal("Equal() ignored provenance metadata")
	}
	if !left.SemanticallyEqual(right) {
		t.Fatal("SemanticallyEqual() changed because of metadata")
	}
	if left.Revision() != "1" || left.Metadata().Label != "Store" {
		t.Fatalf("metadata accessors = %#v, revision=%q", left.Metadata(), left.Revision())
	}
}

func TestEffectiveRangeDefaultsClosedOutsideInclusiveDates(t *testing.T) {
	rule := openinghours.OpenAllDay()
	start := openinghours.MustDate(2026, time.January, 5)
	end := openinghours.MustDate(2026, time.January, 6)
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC",
		Weekly: map[time.Weekday]openinghours.DayRule{
			time.Monday: rule, time.Tuesday: rule, time.Wednesday: rule,
		},
		EffectiveStart: &start,
		EffectiveEnd:   &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		date openinghours.Date
		open bool
	}{
		{openinghours.MustDate(2026, time.January, 4), false},
		{start, true}, {end, true},
		{openinghours.MustDate(2026, time.January, 7), false},
	} {
		result, queryErr := schedule.IsOpenLocal(test.date, mustTime(t, 12, 0), openinghours.RejectDST)
		if queryErr != nil || result.Open != test.open {
			t.Fatalf("IsOpenLocal(%v) = %#v, error=%v, want %t", test.date, result, queryErr, test.open)
		}
	}
}

func TestEffectiveRangeCanReturnTypedOutsideError(t *testing.T) {
	start := openinghours.MustDate(2026, time.January, 5)
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", EffectiveStart: &start,
		OutsideEffective: openinghours.OutsideError,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = schedule.IsOpenLocal(
		openinghours.MustDate(2026, time.January, 4), mustTime(t, 12, 0), openinghours.RejectDST,
	)
	if !openinghours.IsCode(err, openinghours.CodeOutsideEffectiveRange) {
		t.Fatalf("IsOpenLocal() error = %v, want outside effective range", err)
	}
}
