package dateperiod_test

import (
	"testing"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
)

func FuzzDateSplitting(f *testing.F) {
	f.Add(int64(0), int64(10), int64(3), uint8(0))
	f.Add(int64(10), int64(0), int64(-1), uint8(255))
	f.Add(int64(0), int64(10), int64(1<<63-1), uint8(1))
	f.Fuzz(func(t *testing.T, firstOffset, secondOffset, stepValue int64, boundValue uint8) {
		firstOffset %= 10_000
		secondOffset %= 10_000
		if firstOffset > secondOffset {
			firstOffset, secondOffset = secondOffset, firstOffset
		}
		period := mustDatePeriod(t, dayOffset(int(firstOffset)), dayOffset(int(secondOffset)),
			temporal.AllBounds()[int(boundValue)%len(temporal.AllBounds())])
		limits := temporal.DefaultLimits()
		limits.Steps = 128
		limits.OutputPeriods = 128
		parts, err := period.SplitDays(int(stepValue), limits)
		if err != nil {
			return
		}
		if len(parts) > limits.Steps || len(parts) > limits.OutputPeriods {
			t.Fatalf("split produced %d parts above limits", len(parts))
		}
		got, err := dateperiod.NewSet(limits, parts...)
		if err != nil {
			t.Fatalf("NewSet(parts): %v", err)
		}
		want, err := dateperiod.NewSet(limits, period)
		if err != nil || !got.Equal(want) {
			t.Fatalf("split did not conserve membership: got=%+v want=%+v err=%v",
				got.Periods(), want.Periods(), err)
		}
	})
}
