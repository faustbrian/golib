// Package calendarvalidation provides dependency-neutral rules that can be
// wrapped by validation ValidatorFunc without coupling calendar core to it.
package calendarvalidation

import (
	"errors"
	"fmt"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

var (
	// ErrInvalidDate identifies an invalid Date value.
	ErrInvalidDate = errors.New("calendar/validation: invalid date")
	// ErrDateOutOfRange identifies a Date outside inclusive configured bounds.
	ErrDateOutOfRange = errors.New("calendar/validation: date out of range")
)

// Rule is a deterministic, side-effect-free date validation function.
type Rule func(calendar.Date) error

// ValidDate returns a rule that rejects the Date zero value.
func ValidDate() Rule {
	return func(date calendar.Date) error {
		if !date.IsValid() {
			return ErrInvalidDate
		}
		return nil
	}
}

// DateRange returns an inclusive bounded date rule.
func DateRange(minimum, maximum calendar.Date) (Rule, error) {
	comparison, err := minimum.Compare(maximum)
	if err != nil || comparison > 0 {
		return nil, fmt.Errorf("%w: invalid bounds", ErrDateOutOfRange)
	}
	return func(date calendar.Date) error {
		if !date.IsValid() {
			return ErrInvalidDate
		}
		below, _ := date.Compare(minimum)
		above, _ := date.Compare(maximum)
		if below < 0 || above > 0 {
			return ErrDateOutOfRange
		}
		return nil
	}, nil
}
