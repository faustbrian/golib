package scheduler

import (
	"testing"
	"time"
)

type noOccurrenceSchedule struct{}

func (noOccurrenceSchedule) Next(time.Time) time.Time { return time.Time{} }

func TestConstrainedSchedulePreservesExhaustedSchedule(t *testing.T) {
	t.Parallel()

	schedule := constrainedSchedule{Schedule: noOccurrenceSchedule{}, location: time.UTC}
	if next := schedule.Next(time.Now()); !next.IsZero() {
		t.Fatalf("Next() = %v, want zero", next)
	}
}

type dailyNoonSchedule struct{}

func (dailyNoonSchedule) Next(after time.Time) time.Time {
	next := time.Date(after.Year(), after.Month(), after.Day(), 12, 0, 0, 0, time.UTC)
	if !next.After(after) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func TestConstrainedScheduleBoundsImpossibleWindowSearch(t *testing.T) {
	t.Parallel()

	schedule := constrainedSchedule{
		Schedule: dailyNoonSchedule{},
		windows:  []TimeWindow{{Start: 0, End: 0}},
		location: time.UTC,
	}
	if next := schedule.Next(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)); !next.IsZero() {
		t.Fatalf("Next() = %v, want zero after bounded non-matches", next)
	}
}
