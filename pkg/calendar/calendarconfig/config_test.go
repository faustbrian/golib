package calendarconfig_test

import (
	"errors"
	"testing"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/calendarconfig"
)

func TestDateDecodesStrictConfigValues(t *testing.T) {
	t.Parallel()

	var value calendarconfig.Date
	if err := value.UnmarshalConfigValue("2024-02-29"); err != nil || value.CalendarDate().String() != "2024-02-29" {
		t.Fatalf("UnmarshalConfigValue() = %s, %v", value.CalendarDate(), err)
	}
	for _, input := range []any{nil, 20240229, "2024-2-29"} {
		if err := value.UnmarshalConfigValue(input); err == nil {
			t.Fatalf("input %#v unexpectedly accepted", input)
		}
	}
	zero := calendarconfig.NewDate(calendar.Date{})
	if _, err := zero.MarshalText(); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("zero MarshalText error = %v", err)
	}
	var text calendarconfig.Date
	if err := text.UnmarshalText([]byte("2025-01-02")); err != nil || text.CalendarDate().String() != "2025-01-02" {
		t.Fatalf("UnmarshalText() = %s, %v", text.CalendarDate(), err)
	}
	if err := text.UnmarshalText([]byte("invalid")); err == nil {
		t.Fatal("invalid text accepted")
	}
	var nilDate *calendarconfig.Date
	if err := nilDate.UnmarshalConfigValue("2025-01-02"); err == nil {
		t.Fatal("nil config target accepted")
	}
	if err := nilDate.UnmarshalText([]byte("2025-01-02")); err == nil {
		t.Fatal("nil text target accepted")
	}
}
