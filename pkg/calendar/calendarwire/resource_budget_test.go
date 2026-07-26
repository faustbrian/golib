package calendarwire_test

import (
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/calendarwire"
)

var (
	budgetWireBytes []byte
	budgetWireDate  calendar.Date
	budgetWireErr   error
)

func TestWireOutputAndAllocationBudgets(t *testing.T) {
	date := calendar.MustDate(2024, time.February, 29)
	maximumAllocations := float64(4 + raceAllocationSlack)
	if allocations := testing.AllocsPerRun(1_000, func() {
		budgetWireBytes, budgetWireErr = calendarwire.EncodeDate(date)
	}); allocations > maximumAllocations {
		t.Fatalf("wire encode allocations = %.0f, budget %.0f", allocations, maximumAllocations)
	}
	if budgetWireErr != nil || len(budgetWireBytes) > calendarwire.MaxBytes {
		t.Fatalf("wire output = %d bytes, %v", len(budgetWireBytes), budgetWireErr)
	}
	payload := []byte(`"2024-02-29"`)
	if allocations := testing.AllocsPerRun(1_000, func() {
		budgetWireDate, budgetWireErr = calendarwire.DecodeDate(payload)
	}); allocations > maximumAllocations {
		t.Fatalf("wire decode allocations = %.0f, budget %.0f", allocations, maximumAllocations)
	}
}
