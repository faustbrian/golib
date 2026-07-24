package instant_test

import (
	"errors"
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func TestDurationConstructorsAreExplicitAndImmutable(t *testing.T) {
	t.Parallel()

	after, err := instant.After(at(1), 2*time.Hour, temporal.ClosedOpen)
	if err != nil || !after.SetEqual(mustPeriod(t, 1, 3, temporal.ClosedOpen)) {
		t.Fatalf("After() = %+v, %v", after, err)
	}
	before, err := instant.Before(at(3), 2*time.Hour, temporal.OpenClosed)
	if err != nil || !before.SetEqual(mustPeriod(t, 1, 3, temporal.OpenClosed)) {
		t.Fatalf("Before() = %+v, %v", before, err)
	}
	around, err := instant.Around(at(2), time.Hour, temporal.Closed)
	if err != nil || !around.SetEqual(mustPeriod(t, 1, 3, temporal.Closed)) {
		t.Fatalf("Around() = %+v, %v", around, err)
	}
	point := instant.Point(at(2))
	if !point.IsSingleton() || !point.Includes(at(2)) {
		t.Fatalf("Point() = %+v", point)
	}
}

func TestDurationConstructorsRejectNegativeDurations(t *testing.T) {
	t.Parallel()

	for name, construct := range map[string]func() error{
		"after": func() error {
			_, err := instant.After(at(1), -time.Second, temporal.ClosedOpen)
			return err
		},
		"before": func() error {
			_, err := instant.Before(at(1), -time.Second, temporal.ClosedOpen)
			return err
		},
		"around": func() error {
			_, err := instant.Around(at(1), -time.Second, temporal.ClosedOpen)
			return err
		},
	} {
		if err := construct(); !errors.Is(err, temporal.ErrStep) {
			t.Fatalf("%s error = %v, want ErrStep", name, err)
		}
	}
}

func TestSplitForwardConservesBoundsAndCoverage(t *testing.T) {
	t.Parallel()

	period := mustPeriod(t, 0, 5, temporal.OpenClosed)
	parts, err := period.SplitForward(2*time.Hour, temporal.Limits{})
	if err != nil {
		t.Fatalf("SplitForward(): %v", err)
	}
	if len(parts) != 3 {
		t.Fatalf("len(parts) = %d, want 3", len(parts))
	}

	want := []instant.Period{
		mustPeriod(t, 0, 2, temporal.Open),
		mustPeriod(t, 2, 4, temporal.ClosedOpen),
		mustPeriod(t, 4, 5, temporal.Closed),
	}
	for index := range want {
		if !parts[index].SetEqual(want[index]) {
			t.Fatalf("parts[%d] = %+v, want %+v", index, parts[index], want[index])
		}
	}

	normalized := mustSet(t, parts...)
	if normalized.Len() != 1 || !normalized.Periods()[0].SetEqual(period) {
		t.Fatal("forward split did not conserve the represented set")
	}
}

func TestSplitBackwardConservesBoundsAndCoverage(t *testing.T) {
	t.Parallel()

	period := mustPeriod(t, 0, 5, temporal.ClosedOpen)
	parts, err := period.SplitBackward(2*time.Hour, temporal.Limits{})
	if err != nil {
		t.Fatalf("SplitBackward(): %v", err)
	}
	want := []instant.Period{
		mustPeriod(t, 0, 1, temporal.Closed),
		mustPeriod(t, 1, 3, temporal.OpenClosed),
		mustPeriod(t, 3, 5, temporal.Open),
	}
	if len(parts) != len(want) {
		t.Fatalf("len(parts) = %d, want %d", len(parts), len(want))
	}
	for index := range want {
		if !parts[index].SetEqual(want[index]) {
			t.Fatalf("parts[%d] = %+v, want %+v", index, parts[index], want[index])
		}
	}

	if normalized := mustSet(t, parts...); normalized.Len() != 1 || !normalized.Periods()[0].SetEqual(period) {
		t.Fatal("backward split did not conserve the represented set")
	}
}

func TestSplitRejectsInvalidStepLimitsAndConfiguration(t *testing.T) {
	t.Parallel()

	period := mustPeriod(t, 0, 3, temporal.ClosedOpen)
	for _, step := range []time.Duration{0, -time.Second} {
		if _, err := period.SplitForward(step, temporal.Limits{}); !errors.Is(err, temporal.ErrStep) {
			t.Fatalf("SplitForward(%v) error = %v", step, err)
		}
		if _, err := period.SplitBackward(step, temporal.Limits{}); !errors.Is(err, temporal.ErrStep) {
			t.Fatalf("SplitBackward(%v) error = %v", step, err)
		}
	}
	if _, err := period.SplitForward(time.Hour, temporal.Limits{Steps: 2}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("SplitForward(limit) error = %v", err)
	}
	if _, err := period.SplitBackward(time.Hour, temporal.Limits{Steps: 2}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("SplitBackward(limit) error = %v", err)
	}
	if _, err := period.SplitForward(
		time.Hour,
		temporal.Limits{OutputPeriods: 2},
	); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("SplitForward(output limit) error = %v", err)
	}
	if _, err := period.SplitForward(time.Hour, temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("SplitForward(invalid limits) error = %v", err)
	}
}

func TestSplitEmptySingletonAndOversizedStep(t *testing.T) {
	t.Parallel()

	empty := mustPeriod(t, 1, 1, temporal.Open)
	if parts, err := empty.SplitForward(time.Hour, temporal.Limits{}); err != nil || parts != nil {
		t.Fatalf("empty SplitForward() = %+v, %v", parts, err)
	}
	if parts, err := empty.SplitBackward(time.Hour, temporal.Limits{}); err != nil || parts != nil {
		t.Fatalf("empty SplitBackward() = %+v, %v", parts, err)
	}

	point := instant.Point(at(1))
	for _, split := range []func(time.Duration, temporal.Limits) ([]instant.Period, error){
		point.SplitForward,
		point.SplitBackward,
	} {
		parts, err := split(time.Hour, temporal.Limits{})
		if err != nil || len(parts) != 1 || !parts[0].SetEqual(point) {
			t.Fatalf("singleton split = %+v, %v", parts, err)
		}
	}

	period := mustPeriod(t, 1, 2, temporal.ClosedOpen)
	parts, err := period.SplitForward(2*time.Hour, temporal.Limits{})
	if err != nil || len(parts) != 1 || !parts[0].SetEqual(period) {
		t.Fatalf("oversized split = %+v, %v", parts, err)
	}
}
