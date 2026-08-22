//go:build !race

package decimal_test

import (
	"errors"
	"strings"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
)

// Race instrumentation adds size-dependent allocations that do not exist in
// production, so allocation bounds are measured only by the normal test build.
func TestParserRejectsHostileInputWithoutScalingAllocations(t *testing.T) {
	limits := gomath.DefaultLimits()
	limits.MaxInputDigits = 8
	measureObjects := func(input string, want error) float64 {
		return testing.AllocsPerRun(10, func() {
			if _, err := decimal.ParseWithOptions(input, decimal.ParseOptions{Limits: limits}); !errors.Is(err, want) {
				panic("hostile decimal did not retain its error identity")
			}
		})
	}
	boundaryDigits := measureObjects(strings.Repeat("9", limits.MaxInputDigits+1), gomath.ErrLimitExceeded)
	hostileDigits := measureObjects(strings.Repeat("9", 1<<20), gomath.ErrLimitExceeded)
	if hostileDigits > boundaryDigits+1 {
		t.Fatalf(
			"attacker-sized input allocated %.0f objects; near-boundary rejection allocated %.0f",
			hostileDigits, boundaryDigits,
		)
	}

	measureBytes := func(input string, want error) int64 {
		result := testing.Benchmark(func(benchmark *testing.B) {
			for benchmark.Loop() {
				if _, err := decimal.ParseWithOptions(input, decimal.ParseOptions{Limits: limits}); !errors.Is(err, want) {
					panic("hostile decimal did not retain its error identity")
				}
			}
		})

		return result.AllocedBytesPerOp()
	}
	boundarySeparators := measureBytes("1..", decimal.ErrInvalid)
	hostileSeparators := measureBytes(strings.Repeat(".", 1<<20), decimal.ErrInvalid)
	if hostileSeparators > boundarySeparators+1024 {
		t.Fatalf(
			"attacker-sized separators allocated %d bytes; near-boundary rejection allocated %d",
			hostileSeparators, boundarySeparators,
		)
	}
}
