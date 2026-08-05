package scheduler_test

import (
	"errors"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
)

func TestDayConstraintsFilterScheduleBoundaries(t *testing.T) {
	t.Parallel()

	after := time.Date(2026, time.January, 4, 0, 0, 0, 0, time.UTC) // Sunday.
	tests := map[string]struct {
		option scheduler.Option
		want   time.Time
	}{
		"weekdays":   {scheduler.WithWeekdays(), time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)},
		"weekends":   {scheduler.WithWeekends(), time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)},
		"sundays":    {scheduler.WithSundays(), time.Date(2026, time.January, 11, 0, 0, 0, 0, time.UTC)},
		"mondays":    {scheduler.WithMondays(), time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)},
		"tuesdays":   {scheduler.WithTuesdays(), time.Date(2026, time.January, 6, 0, 0, 0, 0, time.UTC)},
		"wednesdays": {scheduler.WithWednesdays(), time.Date(2026, time.January, 7, 0, 0, 0, 0, time.UTC)},
		"thursdays":  {scheduler.WithThursdays(), time.Date(2026, time.January, 8, 0, 0, 0, 0, time.UTC)},
		"fridays":    {scheduler.WithFridays(), time.Date(2026, time.January, 9, 0, 0, 0, 0, time.UTC)},
		"saturdays":  {scheduler.WithSaturdays(), time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)},
		"days":       {scheduler.WithDays(time.Monday, time.Wednesday), time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			schedule, err := scheduler.NewSchedule(name, "task", scheduler.Daily(), test.option)
			if err != nil {
				t.Fatalf("NewSchedule() error = %v", err)
			}
			registry, err := scheduler.Compile(schedule)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			got, err := registry.Next(name, after)
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}
			if !got.Equal(test.want) {
				t.Fatalf("Next() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTimeConstraintsFilterScheduleBoundariesInScheduleTimezone(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		interval scheduler.Interval
		options  []scheduler.Option
		after    time.Time
		want     time.Time
	}{
		"between": {
			scheduler.EveryMinute(),
			[]scheduler.Option{scheduler.WithBetween("7:15", "22:30")},
			time.Date(2026, time.January, 1, 6, 59, 0, 0, time.UTC),
			time.Date(2026, time.January, 1, 7, 15, 0, 0, time.UTC),
		},
		"unless between overnight": {
			scheduler.Hourly(),
			[]scheduler.Option{scheduler.WithUnlessBetween("23:00", "4:00")},
			time.Date(2026, time.January, 1, 22, 0, 0, 0, time.UTC),
			time.Date(2026, time.January, 2, 5, 0, 0, 0, time.UTC),
		},
		"timezone": {
			scheduler.Hourly(),
			[]scheduler.Option{scheduler.WithBetween("8:00", "9:00"), scheduler.WithTimezone("Europe/Helsinki")},
			time.Date(2026, time.January, 1, 5, 0, 0, 0, time.UTC),
			time.Date(2026, time.January, 1, 6, 0, 0, 0, time.UTC),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			schedule, err := scheduler.NewSchedule(name, "task", test.interval, test.options...)
			if err != nil {
				t.Fatalf("NewSchedule() error = %v", err)
			}
			registry, err := scheduler.Compile(schedule)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			got, err := registry.Next(name, test.after)
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}
			if !got.Equal(test.want) {
				t.Fatalf("Next() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestBetweenIncludesItsEndMinute(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"end", "task", scheduler.Hourly(), scheduler.WithBetween("7:00", "22:00"),
	)
	registry, _ := scheduler.Compile(schedule)
	after := time.Date(2026, time.January, 1, 21, 0, 0, 0, time.UTC)
	want := time.Date(2026, time.January, 1, 22, 0, 0, 0, time.UTC)
	got, err := registry.Next("end", after)
	if err != nil || !got.Equal(want) {
		t.Fatalf("Next() = %v, %v; want %v", got, err, want)
	}
}

func TestSubMinuteConstraintAdvancesToExactNextAllowedMinute(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"seconds", "task", scheduler.EverySecond(), scheduler.WithBetween("0:01", "0:01"),
	)
	registry, _ := scheduler.Compile(schedule)
	after := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	want := after.Add(time.Minute)
	got, err := registry.Next("seconds", after)
	if err != nil || !got.Equal(want) {
		t.Fatalf("Next() = %v, %v; want %v", got, err, want)
	}
}

func TestConstraintOptionsRejectInvalidDefinitions(t *testing.T) {
	t.Parallel()

	tests := map[string]scheduler.Option{
		"empty days":    scheduler.WithDays(),
		"invalid day":   scheduler.WithDays(time.Weekday(7)),
		"invalid start": scheduler.WithBetween("bad", "12:00"),
		"invalid end":   scheduler.WithUnlessBetween("12:00", "25:00"),
	}
	for name, option := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := scheduler.NewSchedule(name, "task", scheduler.Hourly(), option); !errors.Is(err, scheduler.ErrInvalidConstraint) {
				t.Fatalf("NewSchedule() error = %v, want ErrInvalidConstraint", err)
			}
		})
	}
}

func TestRepeatedDaysAreCanonicalAndNilSkipIsIgnored(t *testing.T) {
	t.Parallel()

	schedule, err := scheduler.NewSchedule(
		"canonical", "task", scheduler.Daily(),
		scheduler.WithDays(time.Wednesday, time.Monday, time.Monday),
		scheduler.WithSkip(nil),
	)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	if len(schedule.DaysOfWeek) != 2 || schedule.DaysOfWeek[0] != time.Monday || schedule.DaysOfWeek[1] != time.Wednesday {
		t.Fatalf("DaysOfWeek = %v", schedule.DaysOfWeek)
	}
	if len(schedule.Conditions) != 0 {
		t.Fatalf("Conditions = %d, want 0", len(schedule.Conditions))
	}
}

func TestScheduleRejectsInvalidPublicConstraintFields(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*scheduler.Schedule){
		"weekday": func(schedule *scheduler.Schedule) {
			schedule.DaysOfWeek = []time.Weekday{7}
		},
		"window start": func(schedule *scheduler.Schedule) {
			schedule.TimeWindows = []scheduler.TimeWindow{{Start: -time.Minute, End: time.Hour}}
		},
		"window start at day end": func(schedule *scheduler.Schedule) {
			schedule.TimeWindows = []scheduler.TimeWindow{{Start: 24 * time.Hour, End: time.Hour}}
		},
		"window end negative": func(schedule *scheduler.Schedule) {
			schedule.TimeWindows = []scheduler.TimeWindow{{Start: time.Hour, End: -time.Minute}}
		},
		"window end": func(schedule *scheduler.Schedule) {
			schedule.TimeWindows = []scheduler.TimeWindow{{Start: time.Hour, End: 24 * time.Hour}}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := scheduler.NewSchedule(name, "task", scheduler.Hourly(), func(schedule *scheduler.Schedule) error {
				mutate(schedule)
				return nil
			})
			if !errors.Is(err, scheduler.ErrInvalidConstraint) {
				t.Fatalf("NewSchedule() error = %v, want ErrInvalidConstraint", err)
			}
		})
	}
}

func TestConstraintOptionRejectsInvalidDayBeforeMutation(t *testing.T) {
	t.Parallel()

	schedule := scheduler.Schedule{}
	err := scheduler.WithDays(time.Weekday(7))(&schedule)
	if !errors.Is(err, scheduler.ErrInvalidConstraint) || len(schedule.DaysOfWeek) != 0 {
		t.Fatalf("WithDays() error/days = %v/%v", err, schedule.DaysOfWeek)
	}
}

func TestScheduleAcceptsExactTimeWindowLimit(t *testing.T) {
	t.Parallel()

	options := make([]scheduler.Option, scheduler.MaxTimeWindows)
	for index := range options {
		options[index] = scheduler.WithBetween("0:00", "23:59")
	}
	if _, err := scheduler.NewSchedule("windows", "task", scheduler.Hourly(), options...); err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	if _, err := scheduler.NewSchedule(
		"midnight-end", "task", scheduler.Hourly(), scheduler.WithBetween("23:00", "0:00"),
	); err != nil {
		t.Fatalf("NewSchedule(midnight end) error = %v", err)
	}
}
