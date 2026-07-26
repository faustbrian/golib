// Package openinghoursconfig adapts canonical schedule documents from
// configuration sources while leaving environment/file ownership to config.
package openinghoursconfig

import (
	"errors"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

// ErrInvalidValue reports a configuration value that is not canonical JSON
// text. The rejected value is never included.
var ErrInvalidValue = errors.New("openinghoursconfig: invalid value")

// Parse strictly decodes one bounded canonical configuration value.
func Parse(value string) (openinghours.Schedule, error) {
	return openinghours.ParseJSON([]byte(value))
}

// Value implements config's value-unmarshal seam without owning sources.
type Value struct{ schedule openinghours.Schedule }

// NewValue wraps an immutable schedule for configuration serialization.
func NewValue(schedule openinghours.Schedule) Value { return Value{schedule: schedule} }

// Schedule returns the decoded immutable schedule.
func (value Value) Schedule() openinghours.Schedule { return value.schedule }

// UnmarshalConfigValue decodes canonical JSON text supplied by config.
func (value *Value) UnmarshalConfigValue(input any) error {
	if value == nil {
		return ErrInvalidValue
	}
	text, ok := input.(string)
	if !ok {
		return ErrInvalidValue
	}
	schedule, err := Parse(text)
	if err != nil {
		return err
	}
	value.schedule = schedule
	return nil
}

// MarshalText returns canonical configuration text.
func (value Value) MarshalText() ([]byte, error) { return value.schedule.CanonicalJSON() }
