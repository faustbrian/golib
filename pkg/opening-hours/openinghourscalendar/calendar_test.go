package openinghourscalendar_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/business"
	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
	"github.com/faustbrian/golib/pkg/opening-hours/openinghourscalendar"
)

func TestDateConversionAndHolidayClosures(t *testing.T) {
	calendarDate := calendar.MustDate(2026, time.December, 25)
	date, err := openinghourscalendar.FromDate(calendarDate)
	if err != nil || date != openinghours.MustDate(2026, time.December, 25) {
		t.Fatalf("FromDate = %#v, %v", date, err)
	}
	if got, err := openinghourscalendar.ToDate(date); err != nil || !got.Equal(calendarDate) {
		t.Fatalf("ToDate = %#v, %v", got, err)
	}
	holiday := business.MustHoliday(calendarDate, "Christmas", nil)
	businessCalendar, err := business.NewCalendar(business.Config{
		Revision: "2026", Holidays: []business.Holiday{holiday},
	})
	if err != nil {
		t.Fatal(err)
	}
	exceptions, err := openinghourscalendar.HolidayClosures(
		businessCalendar,
		calendar.MustDate(2026, time.December, 24),
		calendar.MustDate(2026, time.December, 26),
		3, 100, "public-holidays",
	)
	if err != nil || len(exceptions) != 1 || exceptions[0].Operation() != openinghours.ExceptionClose ||
		exceptions[0].Revision() != "2026" {
		t.Fatalf("HolidayClosures = %#v, %v", exceptions, err)
	}
}

func TestCalendarAdapterRejectsInvalidAndUnboundedInput(t *testing.T) {
	if _, err := openinghourscalendar.FromDate(calendar.Date{}); err == nil {
		t.Fatal("invalid calendar date accepted")
	}
	if _, err := openinghourscalendar.ToDate(openinghours.Date{}); err == nil {
		t.Fatal("invalid opening-hours date accepted")
	}
	_, err := openinghourscalendar.HolidayClosures(
		business.Calendar{}, calendar.Date{}, calendar.Date{}, 0, 0, "",
	)
	if err == nil {
		t.Fatal("unbounded holiday expansion accepted")
	}
	date := calendar.MustDate(2026, time.December, 25)
	holiday := business.MustHoliday(date, "Christmas", nil)
	businessCalendar, _ := business.NewCalendar(business.Config{Revision: "2026", Holidays: []business.Holiday{holiday}})
	if _, err := openinghourscalendar.HolidayClosures(
		businessCalendar, date, calendar.MustDate(2026, time.December, 26), 1, 0, "source",
	); !errors.Is(err, openinghourscalendar.ErrExpansionLimit) {
		t.Fatalf("expansion error = %v", err)
	}
	if _, err := openinghourscalendar.HolidayClosures(
		businessCalendar, date, date, 1, 1_000_001, "source",
	); err == nil {
		t.Fatal("invalid exception provenance accepted")
	}
}
