package calendartemporal_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/calendartemporal"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
)

func TestInclusiveDatesBecomeExclusiveInstantPeriod(t *testing.T) {
	t.Parallel()

	helsinki, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		t.Fatal(err)
	}
	period, err := calendartemporal.InclusiveDates(
		calendar.MustDate(2024, time.March, 30),
		calendar.MustDate(2024, time.March, 31),
		helsinki,
		calendartz.Reject,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := period.End().Sub(period.Start()); got != 47*time.Hour {
		t.Fatalf("period length = %s", got)
	}
	if !period.Includes(period.Start()) || period.Includes(period.End()) {
		t.Fatal("period must be start-inclusive and end-exclusive")
	}
}

func TestDateSequenceIsBoundedAndCivil(t *testing.T) {
	t.Parallel()

	dates, err := calendartemporal.Sequence(
		calendar.MustDate(2024, time.February, 28),
		calendar.MustDate(2024, time.March, 1),
		3,
	)
	if err != nil || len(dates) != 3 || dates[1].String() != "2024-02-29" {
		t.Fatalf("Sequence() = %#v, %v", dates, err)
	}
	if _, err := calendartemporal.Sequence(dates[0], dates[2], 2); err == nil {
		t.Fatal("undersized sequence limit unexpectedly accepted")
	}
	if _, err := calendartemporal.Sequence(dates[2], dates[0], 3); !errors.Is(err, calendartemporal.ErrReversed) {
		t.Fatalf("reversed sequence error = %v", err)
	}
	if _, err := calendartemporal.Sequence(calendar.Date{}, dates[0], 3); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("invalid sequence error = %v", err)
	}
}

func TestInclusiveDatesRejectInvalidReversedAndUnresolvableRanges(t *testing.T) {
	t.Parallel()

	utc := time.UTC
	first := calendar.MustDate(2024, time.January, 2)
	last := calendar.MustDate(2024, time.January, 1)
	if _, err := calendartemporal.InclusiveDates(first, last, utc, calendartz.Reject); !errors.Is(err, calendartemporal.ErrReversed) {
		t.Fatalf("reversed range error = %v", err)
	}
	if _, err := calendartemporal.InclusiveDates(calendar.Date{}, last, utc, calendartz.Reject); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("invalid range error = %v", err)
	}
	if _, err := calendartemporal.InclusiveDates(last, last, nil, calendartz.Reject); err == nil {
		t.Fatal("nil range location accepted")
	}
	apia, err := time.LoadLocation("Pacific/Apia")
	if err != nil {
		t.Fatal(err)
	}
	skipped := calendar.MustDate(2011, time.December, 30)
	if _, err := calendartemporal.InclusiveDates(skipped, skipped, apia, calendartz.Reject); !errors.Is(err, calendartz.ErrNonexistent) {
		t.Fatalf("skipped range error = %v", err)
	}
	previous := calendar.MustDate(2011, time.December, 29)
	if _, err := calendartemporal.InclusiveDates(previous, previous, apia, calendartz.Reject); !errors.Is(err, calendartz.ErrNonexistent) {
		t.Fatalf("skipped end error = %v", err)
	}
	maximum := calendar.MustDate(calendar.MaxYear, time.December, 31)
	if _, err := calendartemporal.InclusiveDates(maximum, maximum, utc, calendartz.Reject); !errors.Is(err, calendar.ErrArithmetic) {
		t.Fatalf("maximum end error = %v", err)
	}
}
