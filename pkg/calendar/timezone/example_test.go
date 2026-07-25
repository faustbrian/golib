package timezone_test

import (
	"fmt"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
)

func ExampleResolve() {
	location, _ := calendartz.LoadLocation("America/New_York")
	local := calendartz.MustLocalDateTime(calendar.MustDate(2024, time.November, 3), 1, 30, 0, 0)
	earlier, _ := calendartz.Resolve(local, location, calendartz.Earlier)
	later, _ := calendartz.Resolve(local, location, calendartz.Later)
	fmt.Println(earlier.Format(time.RFC3339))
	fmt.Println(later.Format(time.RFC3339))
	// Output:
	// 2024-11-03T01:30:00-04:00
	// 2024-11-03T01:30:00-05:00
}

func ExampleDayRange() {
	location, _ := calendartz.LoadLocation("Europe/Helsinki")
	date := calendar.MustDate(2024, time.March, 31)
	start, end, _ := calendartz.DayRange(date, location, calendartz.Reject)
	fmt.Println(end.Sub(start))
	// Output:
	// 23h0m0s
}
