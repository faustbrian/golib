package calendarwire_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/calendarwire"
)

func TestCanonicalDateWireRoundTrip(t *testing.T) {
	t.Parallel()

	want := calendar.MustDate(2024, time.February, 29)
	payload, err := calendarwire.EncodeDate(want)
	if err != nil || string(payload) != `"2024-02-29"` {
		t.Fatalf("EncodeDate() = %s, %v", payload, err)
	}
	got, err := calendarwire.DecodeDate(payload)
	if err != nil || got != want {
		t.Fatalf("DecodeDate() = %s, %v", got, err)
	}
	if calendarwire.Version != 1 {
		t.Fatalf("Version = %d", calendarwire.Version)
	}
}

func TestDateWireRejectsNullTrailingAndOversizedInput(t *testing.T) {
	t.Parallel()

	for _, payload := range [][]byte{
		[]byte("null"),
		[]byte(`"2024-01-01" true`),
		[]byte(`"2024\u002d01-01"`),
		make([]byte, calendarwire.MaxBytes+1),
	} {
		if _, err := calendarwire.DecodeDate(payload); err == nil {
			t.Fatalf("payload %q unexpectedly accepted", payload)
		}
	}
	if _, err := calendarwire.EncodeDate(calendar.Date{}); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("zero encode error = %v", err)
	}
}
