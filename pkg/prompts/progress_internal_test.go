package prompts

import (
	"math"
	"testing"
	"time"
)

func TestProgressDurationExactBounds(t *testing.T) {
	t.Parallel()

	if duration, ok := progressDuration(0); !ok || duration != 0 {
		t.Fatalf("zero duration = %v, %t", duration, ok)
	}
	if duration, ok := progressDuration(float64(math.MaxInt64)); ok || duration != 0 {
		t.Fatalf("maximum duration = %v, %t", duration, ok)
	}
	belowMaximum := math.Nextafter(float64(math.MaxInt64), 0)
	if duration, ok := progressDuration(belowMaximum); !ok || duration <= 0 || duration >= time.Duration(math.MaxInt64) {
		t.Fatalf("below-maximum duration = %v, %t", duration, ok)
	}
}
