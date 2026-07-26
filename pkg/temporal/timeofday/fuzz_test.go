package timeofday_test

import (
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func FuzzDailyIntervalIteration(f *testing.F) {
	f.Add(uint8(22), uint8(2), uint8(0), int64(time.Minute))
	f.Add(uint8(8), uint8(17), uint8(3), int64(0))
	f.Add(uint8(0), uint8(0), uint8(1), int64(1<<63-1))
	f.Add(uint8(0), uint8(0), uint8(0), int64(-1<<63))
	f.Fuzz(func(t *testing.T, startValue, endValue, mode uint8, stepValue int64) {
		startHour := int(startValue % 24)
		endHour := int(endValue % 24)
		var interval timeofday.Interval
		if startHour == endHour {
			if mode%2 == 0 {
				interval = timeofday.Collapsed(hm(t, startHour, 0))
			} else {
				interval = timeofday.FullDay()
			}
		} else {
			interval = mustInterval(t, startHour, endHour,
				temporal.AllBounds()[int(mode)%len(temporal.AllBounds())])
		}

		limits := temporal.DefaultLimits()
		limits.Steps = 128
		limits.OutputPeriods = 128
		step := time.Duration(stepValue)
		parts, splitErr := interval.Split(step, limits)
		steps, stepsErr := interval.Steps(step, limits)
		if splitErr == nil {
			if len(parts) > limits.Steps || len(parts) > limits.OutputPeriods {
				t.Fatalf("split produced %d parts above limits", len(parts))
			}
			got, err := timeofday.NewIntervalSet(limits, parts...)
			if err != nil {
				t.Fatalf("NewIntervalSet(parts): %v", err)
			}
			want, err := timeofday.NewIntervalSet(limits, interval)
			if err != nil || !got.Equal(want) {
				t.Fatalf("split did not conserve membership: got=%+v want=%+v err=%v",
					got.Intervals(), want.Intervals(), err)
			}
		}
		if stepsErr == nil && len(steps) > limits.Steps {
			t.Fatalf("steps produced %d values above limit", len(steps))
		}
	})
}

func FuzzDailySetNormalization(f *testing.F) {
	f.Add([]byte{22, 2, 0, 8, 17, 1})
	f.Add([]byte{0, 24, 3})
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 192 {
			return
		}
		intervals := make([]timeofday.Interval, 0, len(input)/3)
		for index := 0; index+2 < len(input); index += 3 {
			startHour := int(input[index] % 24)
			endHour := int(input[index+1] % 24)
			if startHour == endHour {
				continue
			}
			start, _ := timeofday.New(startHour, 0, 0, 0, 0)
			end, _ := timeofday.New(endHour, 0, 0, 0, 0)
			interval, _ := timeofday.Between(start, end, temporal.AllBounds()[input[index+2]%4])
			intervals = append(intervals, interval)
		}
		limits := temporal.DefaultLimits()
		limits.InputPeriods = 64
		limits.OutputPeriods = 64
		set, err := timeofday.NewIntervalSet(limits, intervals...)
		if err != nil {
			return
		}
		twice, err := timeofday.NewIntervalSet(limits, set.Intervals()...)
		if err != nil || !twice.Equal(set) {
			t.Fatalf("normalization was not idempotent: %v", err)
		}
	})
}
