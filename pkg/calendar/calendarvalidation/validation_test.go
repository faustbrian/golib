package calendarvalidation_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/calendarvalidation"
)

func TestDateValidatorsComposeWithGoValidation(t *testing.T) {
	t.Parallel()

	valid := calendar.MustDate(2024, time.February, 29)
	if err := calendarvalidation.ValidDate()(valid); err != nil {
		t.Fatalf("valid error = %v", err)
	}
	if err := calendarvalidation.ValidDate()(calendar.Date{}); !errors.Is(err, calendarvalidation.ErrInvalidDate) {
		t.Fatalf("invalid error = %v", err)
	}
	minimum := calendar.MustDate(2024, time.January, 1)
	maximum := calendar.MustDate(2024, time.December, 31)
	validator, err := calendarvalidation.DateRange(minimum, maximum)
	if err != nil {
		t.Fatal(err)
	}
	if err := validator(calendar.MustDate(2025, time.January, 1)); !errors.Is(err, calendarvalidation.ErrDateOutOfRange) {
		t.Fatalf("range error = %v", err)
	}
	if err := validator(calendar.MustDate(2024, time.June, 1)); err != nil {
		t.Fatalf("in-range error = %v", err)
	}
	if err := validator(calendar.Date{}); !errors.Is(err, calendarvalidation.ErrInvalidDate) {
		t.Fatalf("zero range error = %v", err)
	}
	if _, err := calendarvalidation.DateRange(maximum, minimum); !errors.Is(err, calendarvalidation.ErrDateOutOfRange) {
		t.Fatalf("reversed bounds error = %v", err)
	}
	if _, err := calendarvalidation.DateRange(calendar.Date{}, maximum); !errors.Is(err, calendarvalidation.ErrDateOutOfRange) {
		t.Fatalf("invalid bounds error = %v", err)
	}
}
