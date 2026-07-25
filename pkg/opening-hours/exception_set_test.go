package openinghours_test

import (
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func TestNamedExceptionSetIsCopiedAndPreserved(t *testing.T) {
	date := openinghours.MustDate(2026, time.December, 24)
	closure, _ := openinghours.NewException(openinghours.ExceptionConfig{
		Date: date, Operation: openinghours.ExceptionClose,
		Priority: 100, Source: "holidays", Revision: "2026",
	})
	input := []openinghours.Exception{closure}
	set, err := openinghours.NewExceptionSet("public-holidays", input)
	if err != nil {
		t.Fatal(err)
	}
	input[0] = openinghours.Exception{}
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", ExceptionSets: []openinghours.ExceptionSet{set},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := schedule.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := openinghours.ParseJSON(encoded)
	if err != nil || !schedule.Equal(decoded) {
		t.Fatalf("set round trip error=%v equal=%t", err, schedule.Equal(decoded))
	}
	if set.Name() != "public-holidays" || len(set.Exceptions()) != 1 ||
		set.Exceptions()[0].Set() != "public-holidays" {
		t.Fatalf("exception set = %#v", set)
	}
}

func TestExceptionRangeExpansionIsInclusiveAndBounded(t *testing.T) {
	start := openinghours.MustDate(2026, time.December, 24)
	end := openinghours.MustDate(2026, time.December, 26)
	set, err := openinghours.ExpandExceptionRange(openinghours.ExceptionRangeConfig{
		Name: "holiday", Start: start, End: end, MaximumDates: 3,
		Operation: openinghours.ExceptionClose, Priority: 10,
		Source: "calendar", Revision: "2026",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Exceptions()) != 3 {
		t.Fatalf("expanded dates = %d, want 3", len(set.Exceptions()))
	}

	_, err = openinghours.ExpandExceptionRange(openinghours.ExceptionRangeConfig{
		Name: "holiday", Start: start, End: end, MaximumDates: 2,
		Operation: openinghours.ExceptionClose, Priority: 10,
		Source: "calendar", Revision: "2026",
	})
	if !openinghours.IsCode(err, openinghours.CodeLimitExceeded) {
		t.Fatalf("ExpandExceptionRange() error = %v, want limit exceeded", err)
	}
}
