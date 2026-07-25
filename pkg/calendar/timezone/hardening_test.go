package timezone

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

func TestResolutionFailureContracts(t *testing.T) {
	local := MustLocalDateTime(calendar.MustDate(2024, time.January, 1), 12, 0, 0, 0)
	if _, err := Resolve(LocalDateTime{}, time.UTC, Reject); !errors.Is(err, ErrInvalidLocalTime) {
		t.Fatalf("invalid local error = %v", err)
	}
	var nilPolicy Resolution
	if _, err := Resolve(local, time.UTC, nilPolicy); !errors.Is(err, ErrInvalidLocalTime) {
		t.Fatalf("nil policy error = %v", err)
	}
	if _, err := Resolve(local, time.UTC, Choice(99)); !errors.Is(err, ErrInvalidLocalTime) {
		t.Fatalf("unknown policy error = %v", err)
	}
	for _, policy := range []Resolution{Reject, Earlier, Later, MatchOffset(0)} {
		if _, err := Resolve(local, time.UTC, policy); err != nil {
			t.Fatalf("ordinary time with %T: %v", policy, err)
		}
	}
	assertPanic(t, func() { MustLocalDateTime(calendar.Date{}, 0, 0, 0, 0) })
}

func TestInstantAndDayRangeFailureContracts(t *testing.T) {
	if _, err := FromInstant(time.Now(), nil); !errors.Is(err, ErrInvalidLocation) {
		t.Fatalf("nil instant location error = %v", err)
	}
	if _, err := FromInstant(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), time.UTC); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("out-of-range instant error = %v", err)
	}
	if _, _, err := DayRange(calendar.Date{}, time.UTC, Reject); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("invalid day error = %v", err)
	}
	maximum := calendar.MustDate(calendar.MaxYear, time.December, 31)
	if _, _, err := DayRange(maximum, time.UTC, Reject); !errors.Is(err, calendar.ErrArithmetic) {
		t.Fatalf("maximum day error = %v", err)
	}
	ordinary := calendar.MustDate(2024, time.January, 1)
	if _, _, err := DayRange(ordinary, nil, Reject); !errors.Is(err, ErrInvalidLocation) {
		t.Fatalf("start boundary error = %v", err)
	}
	apia, err := time.LoadLocation("Pacific/Apia")
	if err != nil {
		t.Fatal(err)
	}
	previous := calendar.MustDate(2011, time.December, 29)
	if _, _, err := DayRange(previous, apia, Reject); !errors.Is(err, ErrNonexistent) {
		t.Fatalf("end boundary error = %v", err)
	}
}

func assertPanic(t *testing.T, operation func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("operation did not panic")
		}
	}()
	operation()
}
