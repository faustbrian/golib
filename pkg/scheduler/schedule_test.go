package scheduler_test

import (
	"errors"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
)

func TestNewScheduleValidatesIdentityAndPolicy(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		name string
		task string
		want error
	}{
		"missing name": {task: "reports.generate", want: scheduler.ErrScheduleNameRequired},
		"missing task": {name: "daily-report", want: scheduler.ErrTaskNameRequired},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := scheduler.NewSchedule(test.name, test.task, scheduler.Daily())
			if !errors.Is(err, test.want) {
				t.Fatalf("NewSchedule() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestScheduleOptionsProduceStableIdentity(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	schedule, err := scheduler.NewSchedule(
		"tenant-report",
		"reports.generate",
		scheduler.Cron("15 8 * * 1-5"),
		scheduler.WithTimezone("Europe/Helsinki"),
		scheduler.WithParameters(map[string]any{"tenant": "acme", "format": "pdf"}),
		scheduler.WithEnvironments("production", "staging"),
		scheduler.WithDateBounds(start, end),
		scheduler.WithMissedRuns(scheduler.MissedRunCatchUp, 3),
		scheduler.WithOverlap(scheduler.OverlapSkip),
		scheduler.WithMetadata(map[string]string{"owner": "finance"}),
	)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}

	if schedule.Timezone != "Europe/Helsinki" {
		t.Fatalf("Timezone = %q", schedule.Timezone)
	}
	if schedule.MissedRunPolicy != scheduler.MissedRunCatchUp || schedule.MaxCatchUp != 3 {
		t.Fatalf("missed-run policy = %v/%d", schedule.MissedRunPolicy, schedule.MaxCatchUp)
	}
	if schedule.OverlapPolicy != scheduler.OverlapSkip {
		t.Fatalf("overlap policy = %v", schedule.OverlapPolicy)
	}
	if schedule.Identity == "" || schedule.ParameterIdentity == "" {
		t.Fatal("schedule identities must not be empty")
	}

	reordered, err := scheduler.NewSchedule(
		"tenant-report",
		"reports.generate",
		scheduler.Cron("15 8 * * 1-5"),
		scheduler.WithTimezone("Europe/Helsinki"),
		scheduler.WithParameters(map[string]any{"format": "pdf", "tenant": "acme"}),
	)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	if reordered.ParameterIdentity != schedule.ParameterIdentity {
		t.Fatalf("parameter identities differ: %q != %q", reordered.ParameterIdentity, schedule.ParameterIdentity)
	}
}

func TestConvenienceIntervalsExposeCronExpression(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		interval scheduler.Interval
		want     string
	}{
		"cron":                  {scheduler.Cron("7 6 * * *"), "7 6 * * *"},
		"every second":          {scheduler.EverySecond(), "* * * * * *"},
		"every two seconds":     {scheduler.EveryTwoSeconds(), "*/2 * * * * *"},
		"every five seconds":    {scheduler.EveryFiveSeconds(), "*/5 * * * * *"},
		"every ten seconds":     {scheduler.EveryTenSeconds(), "*/10 * * * * *"},
		"every fifteen seconds": {scheduler.EveryFifteenSeconds(), "*/15 * * * * *"},
		"every twenty seconds":  {scheduler.EveryTwentySeconds(), "*/20 * * * * *"},
		"every thirty seconds":  {scheduler.EveryThirtySeconds(), "*/30 * * * * *"},
		"every minute":          {scheduler.EveryMinute(), "* * * * *"},
		"every two minutes":     {scheduler.EveryTwoMinutes(), "*/2 * * * *"},
		"every three minutes":   {scheduler.EveryThreeMinutes(), "*/3 * * * *"},
		"every four minutes":    {scheduler.EveryFourMinutes(), "*/4 * * * *"},
		"every five minutes":    {scheduler.EveryFiveMinutes(), "*/5 * * * *"},
		"every ten minutes":     {scheduler.EveryTenMinutes(), "*/10 * * * *"},
		"every fifteen minutes": {scheduler.EveryFifteenMinutes(), "*/15 * * * *"},
		"every thirty minutes":  {scheduler.EveryThirtyMinutes(), "*/30 * * * *"},
		"hourly":                {scheduler.Hourly(), "0 * * * *"},
		"hourly at":             {scheduler.HourlyAt(17), "17 * * * *"},
		"every odd hour":        {scheduler.EveryOddHour(), "0 1-23/2 * * *"},
		"every odd hour at":     {scheduler.EveryOddHour(17), "17 1-23/2 * * *"},
		"every two hours":       {scheduler.EveryTwoHours(), "0 */2 * * *"},
		"every three hours":     {scheduler.EveryThreeHours(3), "3 */3 * * *"},
		"every four hours":      {scheduler.EveryFourHours(4), "4 */4 * * *"},
		"every six hours":       {scheduler.EverySixHours(6), "6 */6 * * *"},
		"daily":                 {scheduler.Daily(), "0 0 * * *"},
		"daily at":              {scheduler.DailyAt("13:00"), "0 13 * * *"},
		"at":                    {scheduler.At("2:00"), "0 2 * * *"},
		"twice daily":           {scheduler.TwiceDaily(1, 13), "0 1,13 * * *"},
		"twice daily at":        {scheduler.TwiceDailyAt(1, 13, 15), "15 1,13 * * *"},
		"days of month":         {scheduler.DaysOfMonth(1, 10, 20), "0 0 1,10,20 * *"},
		"weekly":                {scheduler.Weekly(), "0 0 * * 0"},
		"weekly on":             {scheduler.WeeklyOn(time.Monday, "8:00"), "0 8 * * 1"},
		"monthly":               {scheduler.Monthly(), "0 0 1 * *"},
		"monthly on":            {scheduler.MonthlyOn(4, "15:00"), "0 15 4 * *"},
		"twice monthly":         {scheduler.TwiceMonthly(1, 16, "13:00"), "0 13 1,16 * *"},
		"last day of month":     {scheduler.LastDayOfMonth("15:00"), "0 15 L * *"},
		"quarterly":             {scheduler.Quarterly(), "0 0 1 1,4,7,10 *"},
		"quarterly on":          {scheduler.QuarterlyOn(4, "14:00"), "0 14 4 1,4,7,10 *"},
		"yearly":                {scheduler.Yearly(), "0 0 1 1 *"},
		"yearly on":             {scheduler.YearlyOn(time.June, 1, "17:00"), "0 17 1 6 *"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if test.interval.Expression() != test.want {
				t.Fatalf("Expression() = %q, want %q", test.interval.Expression(), test.want)
			}
			schedule, err := scheduler.NewSchedule(name, "task", test.interval)
			if err != nil {
				t.Fatalf("NewSchedule() error = %v", err)
			}
			registry, err := scheduler.Compile(schedule)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			if next, err := registry.Next(name, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)); err != nil || next.IsZero() {
				t.Fatalf("Next() = %v, %v", next, err)
			}
		})
	}
}

func TestInvalidConvenienceIntervalsFailCompilation(t *testing.T) {
	t.Parallel()

	tests := map[string]scheduler.Interval{
		"hourly minute":        scheduler.HourlyAt(60),
		"hour optional count":  scheduler.EveryOddHour(1, 2),
		"time shape":           scheduler.DailyAt("12"),
		"time hour":            scheduler.DailyAt("24:00"),
		"negative time hour":   scheduler.DailyAt("-1:00"),
		"time minute":          scheduler.DailyAt("12:no"),
		"negative time minute": scheduler.DailyAt("12:-1"),
		"month days empty":     scheduler.DaysOfMonth(),
		"month day":            scheduler.MonthlyOn(32, "12:00"),
		"weekday":              scheduler.WeeklyOn(time.Weekday(7), "12:00"),
		"month":                scheduler.YearlyOn(time.Month(13), 1, "12:00"),
	}
	for name, interval := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			schedule, err := scheduler.NewSchedule(name, "task", interval)
			if err != nil {
				t.Fatalf("NewSchedule() error = %v", err)
			}
			if _, err := scheduler.Compile(schedule); !errors.Is(err, scheduler.ErrInvalidExpression) {
				t.Fatalf("Compile() error = %v, want ErrInvalidExpression", err)
			}
		})
	}
}

func TestScheduleVersionAndTimingChangeIdentity(t *testing.T) {
	t.Parallel()

	base, _ := scheduler.NewSchedule("report", "task", scheduler.Daily())
	versioned, _ := scheduler.NewSchedule("report", "task", scheduler.Daily(), scheduler.WithVersion("2"))
	rezoned, _ := scheduler.NewSchedule("report", "task", scheduler.Daily(), scheduler.WithTimezone("Europe/Helsinki"))
	rescheduled, _ := scheduler.NewSchedule("report", "task", scheduler.Hourly())
	for name, changed := range map[string]scheduler.Schedule{
		"version":    versioned,
		"timezone":   rezoned,
		"expression": rescheduled,
	} {
		if changed.Identity == base.Identity {
			t.Fatalf("%s did not change schedule identity", name)
		}
		if changed.CoordinationID != base.CoordinationID {
			t.Fatalf("%s changed coordination identity", name)
		}
	}
	if base.Version != "1" || versioned.Version != "2" {
		t.Fatalf("versions = %q, %q", base.Version, versioned.Version)
	}
}

func TestUnconstrainedScheduleRetainsCompatibleIdentity(t *testing.T) {
	t.Parallel()

	schedule, err := scheduler.NewSchedule("report", "task", scheduler.Daily())
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	const want = "87b26878159d9c28446e5d04210eeeda2616bfa8feee3f71b927a8a737d806c6"
	if schedule.Identity != want {
		t.Fatalf("Identity = %q, want compatible identity %q", schedule.Identity, want)
	}
}

func TestRecurringConstraintsChangeIdentityAndAreCloned(t *testing.T) {
	t.Parallel()

	base, _ := scheduler.NewSchedule("report", "task", scheduler.Daily())
	constrained, err := scheduler.NewSchedule(
		"report", "task", scheduler.Daily(),
		scheduler.WithMondays(), scheduler.WithBetween("8:00", "17:00"),
	)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	if constrained.Identity == base.Identity || constrained.CoordinationID != base.CoordinationID {
		t.Fatalf("constrained identities = %q/%q, base = %q/%q", constrained.Identity, constrained.CoordinationID, base.Identity, base.CoordinationID)
	}
	dayOnly, _ := scheduler.NewSchedule("report", "task", scheduler.Daily(), scheduler.WithMondays())
	windowOnly, _ := scheduler.NewSchedule("report", "task", scheduler.Daily(), scheduler.WithBetween("8:00", "17:00"))
	if dayOnly.Identity == base.Identity || windowOnly.Identity == base.Identity {
		t.Fatalf("single constraint identities = %q/%q, base = %q", dayOnly.Identity, windowOnly.Identity, base.Identity)
	}

	registry, err := scheduler.Compile(constrained)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	constrained.DaysOfWeek[0] = time.Sunday
	constrained.TimeWindows[0].Start = 0
	listed := registry.Schedules()[0]
	if listed.DaysOfWeek[0] != time.Monday || listed.TimeWindows[0].Start != 8*time.Hour {
		t.Fatalf("compiled constraints mutated: %+v %+v", listed.DaysOfWeek, listed.TimeWindows)
	}
}

func TestScheduleTaskAndParametersChangeCoordinationIdentity(t *testing.T) {
	t.Parallel()

	base, _ := scheduler.NewSchedule(
		"report", "reports.generate", scheduler.Daily(),
		scheduler.WithParameters(map[string]any{"tenant": "acme"}),
	)
	task, _ := scheduler.NewSchedule(
		"report", "reports.archive", scheduler.Daily(),
		scheduler.WithParameters(map[string]any{"tenant": "acme"}),
	)
	parameters, _ := scheduler.NewSchedule(
		"report", "reports.generate", scheduler.Daily(),
		scheduler.WithParameters(map[string]any{"tenant": "other"}),
	)
	if base.CoordinationID == "" {
		t.Fatal("CoordinationID is empty")
	}
	if task.CoordinationID == base.CoordinationID {
		t.Fatal("task change did not change coordination identity")
	}
	if parameters.CoordinationID == base.CoordinationID {
		t.Fatal("parameter change did not change coordination identity")
	}
}

func TestScheduleOperationalOptions(t *testing.T) {
	t.Parallel()

	schedule, err := scheduler.NewSchedule(
		"maintenance-report",
		"task",
		scheduler.Daily(),
		scheduler.WithEnabled(false),
		scheduler.WithMaintenancePolicy(scheduler.MaintenanceRun),
		scheduler.WithJitter(30*time.Second),
	)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	if schedule.Enabled || schedule.MaintenancePolicy != scheduler.MaintenanceRun || schedule.Jitter != 30*time.Second {
		t.Fatalf("schedule options = %+v", schedule)
	}
}

func TestScheduleRejectsUnsafeBounds(t *testing.T) {
	t.Parallel()

	tests := map[string]scheduler.Option{
		"empty version":   scheduler.WithVersion(""),
		"negative jitter": scheduler.WithJitter(-time.Second),
		"large jitter":    scheduler.WithJitter(24*time.Hour + time.Nanosecond),
	}
	for name, option := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := scheduler.NewSchedule("report", "task", scheduler.Daily(), option); err == nil {
				t.Fatal("NewSchedule() error = nil")
			}
		})
	}
}
