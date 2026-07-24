// Package cron compiles documented five-field cron expressions in an explicit
// IANA time zone without exposing the underlying parser implementation.
package cron

import (
	"errors"
	"fmt"
	"time"

	robfigcron "github.com/robfig/cron/v3"
)

var (
	// ErrInvalidExpression reports a cron expression rejected by the parser.
	ErrInvalidExpression = errors.New("scheduler cron: invalid expression")
	// ErrInvalidTimezone reports an unavailable IANA time-zone name.
	ErrInvalidTimezone = errors.New("scheduler cron: invalid timezone")
)

// Schedule calculates the first occurrence strictly after a timestamp.
type Schedule interface {
	Next(time.Time) time.Time
}

// Compile validates an expression and time zone and returns an immutable
// schedule. Expressions use the standard five-field format or descriptors
// supported by robfig/cron/v3. Calendar searches cover a complete 400-year
// Gregorian cycle before reporting no future occurrence.
func Compile(expression, timezone string) (Schedule, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrInvalidTimezone, timezone, err)
	}
	parser := robfigcron.NewParser(
		robfigcron.Minute |
			robfigcron.Hour |
			robfigcron.Dom |
			robfigcron.Month |
			robfigcron.Dow |
			robfigcron.Descriptor,
	)
	parsed, err := parser.Parse(expression)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidExpression, err)
	}
	return localized{Schedule: parsed, location: location}, nil
}

type localized struct {
	Schedule
	location *time.Location
}

func (schedule localized) Next(after time.Time) time.Time {
	cursor := after.In(schedule.location)
	for range 80 {
		if next := schedule.Schedule.Next(cursor); !next.IsZero() {
			return next
		}
		cursor = cursor.AddDate(5, 0, 0)
	}
	return time.Time{}
}
