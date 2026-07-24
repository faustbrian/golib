package openinghours

import clock "github.com/faustbrian/golib/pkg/clock"

// Clock is the clock current-time capability. Core schedule queries never
// read a process-global clock.
type Clock = clock.Clock

// ElapsedClock is the clock monotonic elapsed-time capability. Observation
// helpers accept it separately so measuring a query never reads wall time.
type ElapsedClock = clock.ElapsedClock

// IsOpenNow evaluates an injected clock's current instant.
func (s Schedule) IsOpenNow(clock Clock) (Availability, error) {
	if clock == nil {
		return Availability{}, newError("is open now", CodeInvalidClock)
	}

	return s.IsOpen(clock.Now())
}
