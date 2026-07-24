package cron_test

import (
	"errors"
	"testing"
	"time"

	schedulercron "github.com/faustbrian/golib/pkg/scheduler/cron"
)

func TestCompileCalculatesInExplicitTimezone(t *testing.T) {
	t.Parallel()

	compiled, err := schedulercron.Compile("30 3 * * *", "Europe/Helsinki")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	first := compiled.Next(time.Date(2026, time.October, 24, 23, 31, 0, 0, time.UTC))
	second := compiled.Next(first)
	if !first.Equal(time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC)) {
		t.Fatalf("first fold occurrence = %v", first)
	}
	if !second.Equal(time.Date(2026, time.October, 25, 1, 30, 0, 0, time.UTC)) {
		t.Fatalf("second fold occurrence = %v", second)
	}
}

func TestCompileSupportsDescriptors(t *testing.T) {
	t.Parallel()

	compiled, err := schedulercron.Compile("@hourly", "UTC")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	after := time.Date(2026, time.January, 1, 12, 34, 56, 0, time.UTC)
	want := time.Date(2026, time.January, 1, 13, 0, 0, 0, time.UTC)
	if got := compiled.Next(after); !got.Equal(want) {
		t.Fatalf("Next() = %v, want %v", got, want)
	}
}

func TestCompileSearchesTheCompleteGregorianCycle(t *testing.T) {
	t.Parallel()

	compiled, err := schedulercron.Compile("0 0 29 2 *", "UTC")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	after := time.Date(2096, time.March, 1, 0, 0, 0, 0, time.UTC)
	want := time.Date(2104, time.February, 29, 0, 0, 0, 0, time.UTC)
	if got := compiled.Next(after); !got.Equal(want) {
		t.Fatalf("Next() = %v, want %v", got, want)
	}
}

func TestCompileReturnsZeroForUnsatisfiableCalendar(t *testing.T) {
	t.Parallel()

	compiled, err := schedulercron.Compile("0 0 31 2 *", "UTC")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	after := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	if got := compiled.Next(after); !got.IsZero() {
		t.Fatalf("Next() = %v, want zero time", got)
	}
}

func TestCompileClassifiesInvalidInput(t *testing.T) {
	t.Parallel()

	if _, err := schedulercron.Compile("invalid", "UTC"); !errors.Is(err, schedulercron.ErrInvalidExpression) {
		t.Fatalf("Compile(invalid expression) error = %v", err)
	}
	if _, err := schedulercron.Compile("* * * * *", "Invalid/Zone"); !errors.Is(err, schedulercron.ErrInvalidTimezone) {
		t.Fatalf("Compile(invalid timezone) error = %v", err)
	}
}
