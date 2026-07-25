// Package calendarclock adapts a narrow wall-clock source to civil dates.
package calendarclock

import (
	"errors"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

// ErrClockRequired identifies a missing wall-clock capability.
var ErrClockRequired = errors.New("calendar/clock: clock required")

// Clock is the exact wall-clock capability provided by clock.Clock.
type Clock interface {
	Now() time.Time
}

// Today obtains the civil date observed in an explicit location.
func Today(clock Clock, location *time.Location) (calendar.Date, error) {
	if clock == nil {
		return calendar.Date{}, ErrClockRequired
	}
	return calendar.DateFromTime(clock.Now(), location)
}
