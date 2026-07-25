// Package postgres provides nullable JSONB persistence values. The root
// Schedule directly implements database/sql and the interfaces selected by
// pgx JSON/JSONB codecs, so no connection-global type registration is needed.
package postgres

import (
	"database/sql/driver"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

// JSONB is a nullable immutable schedule database value.
type JSONB struct {
	schedule openinghours.Schedule
	valid    bool
}

// New returns a valid nullable JSONB value.
func New(schedule openinghours.Schedule) JSONB {
	return JSONB{schedule: schedule, valid: true}
}

// Get returns the stored schedule and validity flag.
func (j JSONB) Get() (openinghours.Schedule, bool) {
	return j.schedule, j.valid
}

// Value implements driver.Valuer.
func (j JSONB) Value() (driver.Value, error) {
	if !j.valid {
		return nil, nil
	}

	return j.schedule.Value()
}

// Scan implements sql.Scanner and pgx JSONB scanning.
func (j *JSONB) Scan(source any) error {
	if j == nil {
		var schedule *openinghours.Schedule
		return schedule.Scan(source)
	}
	if source == nil {
		j.schedule = openinghours.Schedule{}
		j.valid = false
		return nil
	}
	var schedule openinghours.Schedule
	if err := schedule.Scan(source); err != nil {
		return err
	}
	j.schedule = schedule
	j.valid = true

	return nil
}
