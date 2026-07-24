package calendar_test

import (
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

var (
	budgetDate calendar.Date
	budgetYear int
	budgetWeek int
	budgetErr  error
)

func TestCoreAllocationBudgets(t *testing.T) {
	date := calendar.MustDate(2024, time.January, 31)
	assertAllocationBudget(t, "parse date", 0, func() {
		budgetDate, budgetErr = calendar.ParseDate("2024-02-29")
	})
	assertAllocationBudget(t, "add month", 0, func() {
		budgetDate, budgetErr = date.AddMonths(1, calendar.Clamp)
	})
	assertAllocationBudget(t, "ISO week", 0, func() {
		budgetYear, budgetWeek = date.ISOWeek()
	})
}

func assertAllocationBudget(t *testing.T, name string, maximum float64, operation func()) {
	t.Helper()
	if allocations := testing.AllocsPerRun(1_000, operation); allocations > maximum {
		t.Fatalf("%s allocations = %.0f, budget %.0f", name, allocations, maximum)
	}
}
