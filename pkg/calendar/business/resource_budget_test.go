package business_test

import (
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/business"
)

var (
	budgetBusinessDate calendar.Date
	budgetBusinessBool bool
	budgetBusinessErr  error
)

func TestBusinessAllocationBudgets(t *testing.T) {
	date := calendar.MustDate(2024, time.January, 1)
	cal, err := business.NewCalendar(business.Config{
		Revision: "allocation-budget-v1",
		Weekends: []time.Weekday{time.Saturday, time.Sunday},
	})
	if err != nil {
		t.Fatal(err)
	}
	if allocations := testing.AllocsPerRun(1_000, func() {
		budgetBusinessBool = cal.IsBusinessDay(date)
	}); allocations != 0 {
		t.Fatalf("business lookup allocations = %.0f, budget 0", allocations)
	}
	if allocations := testing.AllocsPerRun(1_000, func() {
		budgetBusinessDate, budgetBusinessErr = cal.AddBusinessDays(date, 20, 40)
	}); allocations != 0 {
		t.Fatalf("business movement allocations = %.0f, budget 0", allocations)
	}
}
