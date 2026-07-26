package temporal_test

import (
	"errors"
	"testing"

	temporal "github.com/faustbrian/golib/pkg/temporal"
)

func TestDefaultLimitsAreBounded(t *testing.T) {
	t.Parallel()

	limits := temporal.DefaultLimits()
	if err := limits.Validate(); err != nil {
		t.Fatalf("DefaultLimits().Validate(): %v", err)
	}

	if limits.ParseBytes != 64*1024 || limits.Precision != 9 {
		t.Fatalf("DefaultLimits() = %+v", limits)
	}
}

func TestZeroLimitsResolveToDefaults(t *testing.T) {
	t.Parallel()

	if got := (temporal.Limits{}).Resolve(); got != temporal.DefaultLimits() {
		t.Fatalf("Limits{}.Resolve() = %+v, want defaults", got)
	}
}

func TestLimitsRejectNegativeAndExcessiveValues(t *testing.T) {
	t.Parallel()

	for _, limits := range []temporal.Limits{
		{ParseBytes: -1},
		{Precision: 10},
		{InputPeriods: temporal.HardMaxPeriods + 1},
		{OutputPeriods: temporal.HardMaxPeriods + 1},
		{Steps: temporal.HardMaxSteps + 1},
	} {
		if err := limits.Validate(); !errors.Is(err, temporal.ErrLimit) {
			t.Fatalf("Validate(%+v) error = %v, want ErrLimit", limits, err)
		}
	}
}

func TestLimitErrorDescribesViolation(t *testing.T) {
	t.Parallel()

	err := &temporal.LimitError{Field: "steps", Value: 2, Max: 1}
	if got := err.Error(); got != "temporal: resource limit exceeded: steps=2 (maximum 1)" {
		t.Fatalf("Error() = %q", got)
	}
}
