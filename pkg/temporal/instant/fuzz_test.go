package instant_test

import (
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func FuzzInstantSplitting(f *testing.F) {
	f.Add(int64(0), int64(10), int64(1))
	f.Add(int64(10), int64(0), int64(-1))
	f.Add(int64(0), int64(1), int64(1<<63-1))
	f.Add(int64(0), int64(1), int64(-1<<63))
	f.Fuzz(func(t *testing.T, startHours, spanHours, stepNanoseconds int64) {
		startHours %= 1_000_000
		spanHours %= 10_000
		base := time.Unix(0, 0).UTC().Add(time.Duration(startHours) * time.Hour)
		end := base.Add(time.Duration(spanHours) * time.Hour)
		period, err := instant.New(base, end, temporal.ClosedOpen)
		if err != nil {
			return
		}
		limits := temporal.DefaultLimits()
		limits.Steps = 128
		step := time.Duration(stepNanoseconds)
		for _, split := range []func(time.Duration, temporal.Limits) ([]instant.Period, error){period.SplitForward, period.SplitBackward} {
			parts, err := split(step, limits)
			if err == nil && len(parts) > limits.Steps {
				t.Fatalf("split produced %d parts above limit %d", len(parts), limits.Steps)
			}
		}
	})
}

func BenchmarkInstantRelation(b *testing.B) {
	base := time.Unix(0, 0).UTC()
	left, _ := instant.Range(base, base.Add(time.Hour))
	right, _ := instant.Range(base.Add(30*time.Minute), base.Add(2*time.Hour))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := left.RelationTo(right); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInstantSplit(b *testing.B) {
	base := time.Unix(0, 0).UTC()
	period, _ := instant.Range(base, base.Add(24*time.Hour))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := period.SplitForward(15*time.Minute, temporal.DefaultLimits()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInstantSetLimitRejection(b *testing.B) {
	base := time.Unix(0, 0).UTC()
	period, _ := instant.Range(base, base.Add(time.Hour))
	limits := temporal.DefaultLimits()
	limits.InputPeriods = 1
	b.ReportAllocs()
	for b.Loop() {
		if _, err := instant.NewSet(limits, period, period); err == nil {
			b.Fatal("expected limit error")
		}
	}
}
