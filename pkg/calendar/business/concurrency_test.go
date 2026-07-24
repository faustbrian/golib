package business_test

import (
	"sync"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/business"
)

func TestCalendarConcurrentReads(t *testing.T) {
	t.Parallel()

	date := calendar.MustDate(2024, time.December, 25)
	cal, err := business.NewCalendar(business.Config{
		Revision: "concurrent-v1",
		Weekends: []time.Weekday{time.Saturday, time.Sunday},
		Holidays: []business.Holiday{business.MustHoliday(date, "Holiday", map[string]string{"source": "test"})},
	})
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				_ = cal.IsBusinessDay(date)
				_ = cal.Holidays(date)[0].Metadata()
				if _, err := cal.AddBusinessDays(date, 5, 20); err != nil {
					t.Errorf("AddBusinessDays: %v", err)
				}
			}
		}()
	}
	wait.Wait()
}
