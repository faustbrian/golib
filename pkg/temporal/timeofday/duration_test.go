package timeofday_test

import (
	"errors"
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func TestDurationConstructionAndComparison(t *testing.T) {
	t.Parallel()

	value := timeofday.NewDuration(90 * time.Minute)
	if value.Value() != 90*time.Minute || value.IsZero() {
		t.Fatalf("NewDuration() = %v", value.Value())
	}
	if !timeofday.ZeroDuration().IsZero() {
		t.Fatal("ZeroDuration() is nonzero")
	}
	if value.Compare(timeofday.NewDuration(time.Hour)) <= 0 || value.Compare(value) != 0 || value.Compare(timeofday.NewDuration(2*time.Hour)) >= 0 {
		t.Fatal("Duration.Compare() ordering failed")
	}
}

func TestDurationCheckedSumNegateAndAbsolute(t *testing.T) {
	t.Parallel()

	value := timeofday.NewDuration(time.Hour)
	sum, err := value.Add(timeofday.NewDuration(30*time.Minute), timeofday.NewDuration(-15*time.Minute))
	if err != nil || sum.Value() != 75*time.Minute {
		t.Fatalf("Add() = %v, %v", sum.Value(), err)
	}
	negative, err := value.Negate()
	if err != nil || negative.Value() != -time.Hour {
		t.Fatalf("Negate() = %v, %v", negative.Value(), err)
	}
	abs, err := negative.Abs()
	if err != nil || abs.Value() != time.Hour {
		t.Fatalf("Abs() = %v, %v", abs.Value(), err)
	}
	if same, err := value.Abs(); err != nil || same != value {
		t.Fatalf("positive Abs() = %v, %v", same, err)
	}

	maximum := timeofday.NewDuration(time.Duration(1<<63 - 1))
	if _, err := maximum.Add(timeofday.NewDuration(1)); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("positive overflow error = %v", err)
	}
	minimum := timeofday.NewDuration(time.Duration(-1 << 63))
	if _, err := minimum.Add(timeofday.NewDuration(-1)); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("negative overflow error = %v", err)
	}
	if _, err := minimum.Negate(); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("minimum Negate() error = %v", err)
	}
	if _, err := minimum.Abs(); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("minimum Abs() error = %v", err)
	}
}

func TestDurationMultiplyDivideAndRemainder(t *testing.T) {
	t.Parallel()

	value := timeofday.NewDuration(5 * time.Second)
	product, err := value.Multiply(-3)
	if err != nil || product.Value() != -15*time.Second {
		t.Fatalf("Multiply() = %v, %v", product.Value(), err)
	}
	quotient, remainder, err := value.Divide(2)
	if err != nil || quotient.Value() != 2500*time.Millisecond || remainder != 0 {
		t.Fatalf("Divide() = %v remainder %v, %v", quotient.Value(), remainder, err)
	}
	odd := timeofday.NewDuration(5)
	quotient, remainder, err = odd.Divide(2)
	if err != nil || quotient.Value() != 2 || remainder != 1 {
		t.Fatalf("odd Divide() = %v remainder %v, %v", quotient.Value(), remainder, err)
	}
	if _, _, err := value.Divide(0); !errors.Is(err, temporal.ErrStep) {
		t.Fatalf("Divide(0) error = %v", err)
	}
	if _, _, err := timeofday.NewDuration(time.Duration(-1 << 63)).Divide(-1); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Divide(minimum) error = %v", err)
	}
	if _, err := timeofday.NewDuration(time.Duration(1<<63 - 1)).Multiply(2); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Multiply(overflow) error = %v", err)
	}
	if _, err := timeofday.NewDuration(time.Duration(-1 << 63)).Multiply(-1); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Multiply(minimum) error = %v", err)
	}
	if zero, err := value.Multiply(0); err != nil || !zero.IsZero() {
		t.Fatalf("Multiply(0) = %v, %v", zero, err)
	}
}

func TestDurationClampAndRound(t *testing.T) {
	t.Parallel()

	minimum := timeofday.NewDuration(time.Second)
	maximum := timeofday.NewDuration(3 * time.Second)
	for _, test := range []struct {
		value time.Duration
		want  time.Duration
	}{
		{0, time.Second},
		{2 * time.Second, 2 * time.Second},
		{4 * time.Second, 3 * time.Second},
	} {
		got, err := timeofday.NewDuration(test.value).Clamp(minimum, maximum)
		if err != nil || got.Value() != test.want {
			t.Fatalf("Clamp(%v) = %v, %v", test.value, got.Value(), err)
		}
	}
	if _, err := minimum.Clamp(maximum, minimum); !errors.Is(err, temporal.ErrStep) {
		t.Fatalf("Clamp(reversed) error = %v", err)
	}

	value := timeofday.NewDuration(2500 * time.Millisecond)
	for mode, want := range map[timeofday.RoundingMode]time.Duration{
		timeofday.RoundFloor:   2 * time.Second,
		timeofday.RoundNearest: 3 * time.Second,
		timeofday.RoundCeil:    3 * time.Second,
	} {
		got, err := value.Round(time.Second, mode)
		if err != nil || got.Value() != want {
			t.Fatalf("Round(%v) = %v, %v", mode, got.Value(), err)
		}
	}
	negative := timeofday.NewDuration(-2500 * time.Millisecond)
	floor, _ := negative.Round(time.Second, timeofday.RoundFloor)
	ceil, _ := negative.Round(time.Second, timeofday.RoundCeil)
	nearest, _ := negative.Round(time.Second, timeofday.RoundNearest)
	if floor.Value() != -3*time.Second || ceil.Value() != -2*time.Second {
		t.Fatalf("negative floor/ceil = %v/%v", floor.Value(), ceil.Value())
	}
	if nearest.Value() != -3*time.Second {
		t.Fatalf("negative nearest = %v", nearest.Value())
	}
	if _, err := value.Round(0, timeofday.RoundNearest); !errors.Is(err, temporal.ErrStep) {
		t.Fatalf("Round(unit 0) error = %v", err)
	}
	if _, err := value.Round(time.Second, timeofday.RoundingMode(255)); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("Round(mode) error = %v", err)
	}
}
