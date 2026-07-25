package business_test

import (
	"fmt"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/business"
)

func ExampleCalendar_AddBusinessDays() {
	holiday := business.MustHoliday(calendar.MustDate(2024, time.December, 25), "Christmas Day", nil)
	cal, _ := business.NewCalendar(business.Config{
		Revision: "company-2024.1",
		Weekends: []time.Weekday{time.Saturday, time.Sunday},
		Holidays: []business.Holiday{holiday},
	})
	due, _ := cal.AddBusinessDays(calendar.MustDate(2024, time.December, 24), 3, 10)
	fmt.Println(due)
	// Output:
	// 2024-12-30
}
