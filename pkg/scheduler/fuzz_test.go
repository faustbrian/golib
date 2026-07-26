package scheduler_test

import (
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
)

func FuzzScheduleCompilation(f *testing.F) {
	f.Add("report", "task", "0 0 * * *", "UTC")
	f.Add("dst", "task", "30 3 * * *", "Europe/Helsinki")
	f.Fuzz(func(_ *testing.T, name, task, expression, timezone string) {
		schedule, err := scheduler.NewSchedule(name, task, scheduler.Cron(expression), scheduler.WithTimezone(timezone))
		if err != nil {
			return
		}
		registry, err := scheduler.Compile(schedule)
		if err != nil {
			return
		}
		_, _ = registry.Next(name, time.Unix(1_700_000_000, 0))
	})
}

func FuzzScheduleOptions(f *testing.F) {
	f.Add("owner", "value", int64(0), uint8(0))
	f.Fuzz(func(_ *testing.T, metadataKey, metadataValue string, jitterNanos int64, policy uint8) {
		_, _ = scheduler.NewSchedule(
			"fuzz", "task", scheduler.Daily(),
			scheduler.WithMetadata(map[string]string{metadataKey: metadataValue}),
			scheduler.WithJitter(time.Duration(jitterNanos)),
			scheduler.WithMissedRuns(scheduler.MissedRunPolicy(policy), 3),
		)
	})
}
