package openinghours_test

import (
	"strings"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	clock "github.com/faustbrian/golib/pkg/clock"
	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
	openinghoursencoding "github.com/faustbrian/golib/pkg/opening-hours/encoding"
	openinghoursconfig "github.com/faustbrian/golib/pkg/opening-hours/openinghoursconfig"
	openinghoursvalidation "github.com/faustbrian/golib/pkg/opening-hours/openinghoursvalidation"
	openinghourswire "github.com/faustbrian/golib/pkg/opening-hours/openinghourswire"
)

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

var _ clock.Clock = fixedClock{}

func TestCivilDateIsOwnedByGoCalendar(t *testing.T) {
	owned := calendar.MustDate(2026, time.January, 5)
	date := acceptOpeningHoursDate(owned)
	roundTrip := acceptCalendarDate(date)
	if !roundTrip.Equal(owned) {
		t.Fatalf("date alias round trip = %v, want %v", roundTrip, owned)
	}
}

func acceptOpeningHoursDate(date openinghours.Date) openinghours.Date { return date }
func acceptCalendarDate(date calendar.Date) calendar.Date             { return date }

func TestScheduleUsesBoundedCalendarZoneLoader(t *testing.T) {
	_, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: strings.Repeat("x", 256),
	})
	if !openinghours.IsCode(err, openinghours.CodeInvalidTimezone) {
		t.Fatalf("oversized timezone error = %v", err)
	}
}

func TestClockCapabilityIsExplicit(t *testing.T) {
	schedule := scheduleWithMonday(t, mustRange(t, 9, 0, 12, 0))
	result, err := schedule.IsOpenNow(fixedClock{
		now: time.Date(2026, time.January, 5, 10, 0, 0, 0, time.UTC),
	})
	if err != nil || !result.Open {
		t.Fatalf("IsOpenNow() = %#v, error=%v", result, err)
	}
	_, err = schedule.IsOpenNow(nil)
	if !openinghours.IsCode(err, openinghours.CodeInvalidClock) {
		t.Fatalf("IsOpenNow(nil) error = %v, want invalid clock", err)
	}
}

func TestEncodingWireConfigAndValidationAdaptersAgree(t *testing.T) {
	want := scheduleWithMonday(t, mustRange(t, 9, 0, 12, 0))
	encoded, err := openinghoursencoding.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	fromEncoding, err := openinghoursencoding.Unmarshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	codec := openinghourswire.Codec{}
	wireBytes, err := codec.Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	fromWire, err := codec.Decode(wireBytes)
	if err != nil {
		t.Fatal(err)
	}
	fromConfig, err := openinghoursconfig.Parse(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if err := openinghoursvalidation.Validate(want); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]openinghours.Schedule{
		"encoding": fromEncoding, "wire": fromWire, "config": fromConfig,
	} {
		if !want.Equal(got) {
			t.Errorf("%s adapter changed schedule", name)
		}
	}
}
