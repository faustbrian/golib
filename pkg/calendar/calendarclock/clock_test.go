package calendarclock_test

import (
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/calendar/calendarclock"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestTodayUsesClockAndExplicitLocation(t *testing.T) {
	t.Parallel()

	helsinki, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		t.Fatal(err)
	}
	clock := fixedClock{now: time.Date(2024, 1, 1, 22, 30, 0, 0, time.UTC)}
	today, err := calendarclock.Today(clock, helsinki)
	if err != nil || today.String() != "2024-01-02" {
		t.Fatalf("Today() = %s, %v", today, err)
	}
	if _, err := calendarclock.Today(nil, helsinki); !errors.Is(err, calendarclock.ErrClockRequired) {
		t.Fatalf("nil clock error = %v", err)
	}
	if _, err := calendarclock.Today(clock, nil); err == nil {
		t.Fatal("nil location unexpectedly accepted")
	}
}
