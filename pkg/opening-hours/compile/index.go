// Package compile provides an immutable prepared schedule handle for repeated
// queries. The current implementation relies on the root schedule's bounded
// sorted exception index and adds no cache, lock, goroutine, or global state.
package compile

import (
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

// Index is safe for concurrent reads because Schedule is immutable.
type Index struct {
	schedule openinghours.Schedule
}

// New validates the canonical form and returns an owned immutable index.
func New(schedule openinghours.Schedule) (Index, error) {
	encoded, err := schedule.CanonicalJSON()
	if err != nil {
		return Index{}, err
	}
	owned, _ := openinghours.ParseJSON(encoded) // CanonicalJSON always emits strict parseable bytes.

	return Index{schedule: owned}, nil
}

// IsOpen delegates an instant query to the prepared schedule.
func (index Index) IsOpen(instant time.Time) (openinghours.Availability, error) {
	return index.schedule.IsOpen(instant)
}

// IsOpenLocal delegates an explicitly resolved civil query to the prepared schedule.
func (index Index) IsOpenLocal(date openinghours.Date, localTime openinghours.LocalTime,
	policy openinghours.LocalResolutionPolicy,
) (openinghours.Availability, error) {
	return index.schedule.IsOpenLocal(date, localTime, policy)
}

// NextTransition delegates a bounded transition search.
func (index Index) NextTransition(instant time.Time, horizon time.Duration) (openinghours.Transition, error) {
	return index.schedule.NextTransition(instant, horizon)
}

// Schedule returns the immutable prepared schedule value.
func (index Index) Schedule() openinghours.Schedule { return index.schedule }
