package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func TestInstantRangeRejectsEitherSubmicrosecondEndpoint(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, endpoints := range [][2]time.Time{
		{base.Add(time.Nanosecond), base.Add(time.Hour)},
		{base, base.Add(time.Hour + time.Nanosecond)},
	} {
		period, err := instant.New(endpoints[0], endpoints[1], temporal.ClosedOpen)
		if err != nil {
			t.Fatalf("instant.New(): %v", err)
		}
		if _, err := InstantRangeValue(period); !errors.Is(err, temporal.ErrPrecision) {
			t.Fatalf("InstantRangeValue(%v..%v) error = %v", endpoints[0], endpoints[1], err)
		}
	}
}

func TestCheckedInputLimitsAcceptExactCardinality(t *testing.T) {
	t.Parallel()

	limits, err := checkedInputLimits(2, temporal.Limits{InputPeriods: 2})
	if err != nil || limits.InputPeriods != 2 {
		t.Fatalf("checkedInputLimits(exact) = %+v, %v", limits, err)
	}
	_, err = checkedInputLimits(3, temporal.Limits{InputPeriods: 2})
	var limitError *temporal.LimitError
	if !errors.As(err, &limitError) || limitError.Field != "input_periods" || limitError.Value != 3 || limitError.Max != 2 {
		t.Fatalf("checkedInputLimits(over) error = %+v", err)
	}
}

func TestRangeShellAcceptsExactStructuralAndByteBoundaries(t *testing.T) {
	t.Parallel()

	lower, upper, body, err := parseRangeShell("(x)")
	if err != nil || !finiteBound(lower) || !finiteBound(upper) || body != "x" {
		t.Fatalf("parseRangeShell(minimum) = %v, %v, %q, %v", lower, upper, body, err)
	}
	maximum := temporal.DefaultLimits().ParseBytes
	value := "(" + strings.Repeat("x", maximum-2) + ")"
	_, _, body, err = parseRangeShell(value)
	if err != nil || len(body) != maximum-2 {
		t.Fatalf("parseRangeShell(exact byte limit) body=%d error=%v", len(body), err)
	}
}

func TestRangeTextSplittersRejectEmptyAndRepeatedMembers(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		`["","2026-01-02T00:00:00Z")`,
		`["2026-01-01T00:00:00Z","")`,
		`["2026-01-01T00:00:00Z","2026-01-02T00:00:00Z","2026-01-03T00:00:00Z")`,
		`[2026-01-01T00:00:00Z","2026-01-02T00:00:00Z")`,
		`["2026-01-01T00:00:00Z","2026-01-02T00:00:00Z)`,
		`["2026-01-01T00:00:00Z2026-01-02T00:00:00Z")`,
	} {
		if _, err := parseInstantRange(value); !errors.Is(err, temporal.ErrParse) {
			t.Fatalf("parseInstantRange(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{
		"[,2026-01-02)",
		"[2026-01-01,)",
		"[2026-01-01,2026-01-02,2026-01-03)",
		"[2026-01-012026-01-02)",
	} {
		if _, err := parseDateRange(value); !errors.Is(err, temporal.ErrParse) {
			t.Fatalf("parseDateRange(%q) error = %v", value, err)
		}
	}
}
