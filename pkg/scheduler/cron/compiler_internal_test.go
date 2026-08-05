package cron

import (
	"testing"
	"time"
)

type firstOfNextMonthSchedule struct{}

func (firstOfNextMonthSchedule) Next(after time.Time) time.Time {
	return time.Date(after.Year(), after.Month()+1, 1, 0, 0, 0, 0, after.Location())
}

func TestLastDayScheduleBoundsFiltering(t *testing.T) {
	t.Parallel()

	schedule := lastDaySchedule{Schedule: firstOfNextMonthSchedule{}}
	after := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	if next := schedule.Next(after); !next.IsZero() {
		t.Fatalf("Next() = %v, want zero after bounded non-matches", next)
	}
}

type zeroSchedule struct{}

func (zeroSchedule) Next(time.Time) time.Time { return time.Time{} }

func TestLastDaySchedulePreservesExhaustedSchedule(t *testing.T) {
	t.Parallel()

	if next := (lastDaySchedule{Schedule: zeroSchedule{}}).Next(time.Now()); !next.IsZero() {
		t.Fatalf("Next() = %v, want zero", next)
	}
}
