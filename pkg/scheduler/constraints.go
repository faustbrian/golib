package scheduler

import (
	"fmt"
	"slices"
	"time"
)

// WithDays restricts occurrences to the requested local weekdays.
func WithDays(days ...time.Weekday) Option {
	return func(schedule *Schedule) error {
		if len(days) == 0 {
			return fmt.Errorf("%w: at least one weekday is required", ErrInvalidConstraint)
		}
		for _, day := range days {
			if day < time.Sunday || day > time.Saturday {
				return fmt.Errorf("%w: weekday %d", ErrInvalidConstraint, day)
			}
			if !slices.Contains(schedule.DaysOfWeek, day) {
				schedule.DaysOfWeek = append(schedule.DaysOfWeek, day)
			}
		}
		slices.Sort(schedule.DaysOfWeek)
		return nil
	}
}

// WithWeekdays restricts occurrences to Monday through Friday.
func WithWeekdays() Option {
	return WithDays(time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday)
}

// WithWeekends restricts occurrences to Saturday and Sunday.
func WithWeekends() Option { return WithDays(time.Saturday, time.Sunday) }

// WithSundays restricts occurrences to Sunday.
func WithSundays() Option { return WithDays(time.Sunday) }

// WithMondays restricts occurrences to Monday.
func WithMondays() Option { return WithDays(time.Monday) }

// WithTuesdays restricts occurrences to Tuesday.
func WithTuesdays() Option { return WithDays(time.Tuesday) }

// WithWednesdays restricts occurrences to Wednesday.
func WithWednesdays() Option { return WithDays(time.Wednesday) }

// WithThursdays restricts occurrences to Thursday.
func WithThursdays() Option { return WithDays(time.Thursday) }

// WithFridays restricts occurrences to Friday.
func WithFridays() Option { return WithDays(time.Friday) }

// WithSaturdays restricts occurrences to Saturday.
func WithSaturdays() Option { return WithDays(time.Saturday) }

// WithBetween restricts occurrences to an inclusive recurring local-time window.
func WithBetween(start, end string) Option { return withTimeWindow(start, end, false) }

// WithUnlessBetween excludes occurrences in an inclusive recurring local-time window.
func WithUnlessBetween(start, end string) Option { return withTimeWindow(start, end, true) }

func withTimeWindow(start, end string, excluded bool) Option {
	return func(schedule *Schedule) error {
		startHour, startMinute, startOK := parseTimeOfDay(start)
		endHour, endMinute, endOK := parseTimeOfDay(end)
		if !startOK || !endOK {
			return fmt.Errorf("%w: time windows require HH:MM values", ErrInvalidConstraint)
		}
		schedule.TimeWindows = append(schedule.TimeWindows, TimeWindow{
			Start:    time.Duration(startHour*60+startMinute) * time.Minute,
			End:      time.Duration(endHour*60+endMinute) * time.Minute,
			Excluded: excluded,
		})
		return nil
	}
}

// WithSkip appends the inverse of a trusted application condition.
func WithSkip(skip Condition) Option {
	if skip == nil {
		return WithCondition(nil)
	}
	return WithCondition(func(context Context) (bool, error) {
		skipped, err := skip(context)
		return !skipped, err
	})
}
