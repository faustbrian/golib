package business_test

import (
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/business"
)

func FuzzHolidayData(f *testing.F) {
	f.Add("Holiday", "region", "FI")
	f.Add(string([]byte{0xff}), "key", "value")
	f.Fuzz(func(t *testing.T, name, key, value string) {
		metadata := map[string]string{key: value}
		holiday, err := business.NewHoliday(calendar.MustDate(2024, time.January, 1), name, metadata)
		if err != nil {
			return
		}
		metadata[key] = "mutated"
		if holiday.Name() != name || holiday.Metadata()[key] != value {
			t.Fatal("successful holiday construction was mutable")
		}
	})
}

func FuzzCalendarConfiguration(f *testing.F) {
	f.Add("revision-1", int8(time.Saturday))
	f.Fuzz(func(t *testing.T, revision string, rawWeekday int8) {
		cal, err := business.NewCalendar(business.Config{Revision: revision, Weekends: []time.Weekday{time.Weekday(rawWeekday)}})
		if err == nil && !cal.IsValid() {
			t.Fatal("successful calendar construction was invalid")
		}
	})
}
