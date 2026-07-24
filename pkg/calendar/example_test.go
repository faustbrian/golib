package calendar_test

import (
	"fmt"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

func ExampleDate() {
	date, err := calendar.ParseDate("2024-02-29")
	fmt.Println(date, err)
	fmt.Println(date.Weekday(), date.DayOfYear())
	// Output:
	// 2024-02-29 <nil>
	// Thursday 60
}

func ExampleDate_AddMonths() {
	date := calendar.MustDate(2023, time.January, 31)
	clamped, _ := date.AddMonths(1, calendar.Clamp)
	overflowed, _ := date.AddMonths(1, calendar.Overflow)
	_, rejected := date.AddMonths(1, calendar.Reject)
	fmt.Println(clamped)
	fmt.Println(overflowed)
	fmt.Println(rejected != nil)
	// Output:
	// 2023-02-28
	// 2023-03-03
	// true
}

func ExampleWeekPolicy() {
	policy, _ := calendar.NewWeekPolicy(time.Sunday)
	date := calendar.MustDate(2024, time.May, 22)
	fmt.Println(policy.StartOfWeek(date), policy.EndOfWeek(date))
	// Output:
	// 2024-05-19 2024-05-25
}
