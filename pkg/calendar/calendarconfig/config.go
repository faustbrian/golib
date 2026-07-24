// Package calendarconfig provides strict config-compatible civil values.
package calendarconfig

import (
	"fmt"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

// Date is a config ValueUnmarshaler for a required canonical civil date.
type Date struct{ date calendar.Date }

// NewDate wraps a calendar Date.
func NewDate(date calendar.Date) Date { return Date{date: date} }

// CalendarDate returns the decoded civil date.
func (d Date) CalendarDate() calendar.Date { return d.date }

// UnmarshalConfigValue accepts only a canonical date string.
func (d *Date) UnmarshalConfigValue(value any) error {
	if d == nil {
		return calendar.ErrInvalidDate
	}
	text, ok := value.(string)
	if !ok {
		return fmt.Errorf("calendar/config: expected date string, received %T", value)
	}
	parsed, err := calendar.ParseDate(text)
	if err != nil {
		return err
	}
	d.date = parsed
	return nil
}

// MarshalText returns the canonical configuration value.
func (d Date) MarshalText() ([]byte, error) { return d.date.MarshalText() }

// UnmarshalText decodes a canonical configuration value.
func (d *Date) UnmarshalText(text []byte) error {
	if d == nil {
		return calendar.ErrInvalidDate
	}
	parsed, err := calendar.ParseDate(string(text))
	if err != nil {
		return err
	}
	d.date = parsed
	return nil
}
