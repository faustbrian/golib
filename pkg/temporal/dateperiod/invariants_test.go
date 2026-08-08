package dateperiod

import (
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

func TestPeriodDaysRejectsEitherInvalidEndpoint(t *testing.T) {
	t.Parallel()

	valid := calendar.MustDate(2026, time.January, 1)
	if got := (Period{end: valid, bounds: temporal.Closed}).Days(); got != 0 {
		t.Fatalf("Days(invalid start) = %d, want 0", got)
	}
	if got := (Period{start: valid, bounds: temporal.Closed}).Days(); got != 0 {
		t.Fatalf("Days(invalid end) = %d, want 0", got)
	}
}
