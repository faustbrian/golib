package timezone_test

import (
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
)

var (
	budgetInstant time.Time
	budgetZoneErr error
)

func TestTimezoneResolutionAllocationBudget(t *testing.T) {
	location, err := calendartz.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	local := calendartz.MustLocalDateTime(
		calendar.MustDate(2024, time.November, 3), 1, 30, 0, 0,
	)
	if allocations := testing.AllocsPerRun(1_000, func() {
		budgetInstant, budgetZoneErr = calendartz.Resolve(local, location, calendartz.Earlier)
	}); allocations > 4 {
		t.Fatalf("timezone resolution allocations = %.0f, budget 4", allocations)
	}
}
