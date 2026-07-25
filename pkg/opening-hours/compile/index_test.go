package compile_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
	"github.com/faustbrian/golib/pkg/opening-hours/compile"
)

func TestIndexPreservesQueriesAndIsSafeForConcurrentReads(t *testing.T) {
	start, _ := openinghours.NewLocalTime(9, 0, 0, 0)
	end, _ := openinghours.NewLocalTime(12, 0, 0, 0)
	item, _ := openinghours.NewRange(start, end)
	rule, _ := openinghours.OpenRanges([]openinghours.Range{item}, openinghours.RejectOverlap)
	schedule, _ := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", Weekly: map[time.Weekday]openinghours.DayRule{time.Monday: rule},
	})
	index, err := compile.New(schedule)
	if err != nil {
		t.Fatal(err)
	}
	instant := time.Date(2026, time.January, 5, 10, 0, 0, 0, time.UTC)

	errors := make(chan error, 32)
	for range 32 {
		go func() {
			result, queryErr := index.IsOpen(instant)
			if queryErr != nil {
				errors <- queryErr
				return
			}
			if !result.Open {
				errors <- &closedError{}
				return
			}
			errors <- nil
		}()
	}
	for range 32 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	local, _ := openinghours.NewLocalTime(10, 0, 0, 0)
	date := openinghours.MustDate(2026, time.January, 5)
	if result, queryErr := index.IsOpenLocal(date, local, openinghours.RejectDST); queryErr != nil || !result.Open {
		t.Fatalf("IsOpenLocal() = %#v error=%v", result, queryErr)
	}
	if transition, queryErr := index.NextTransition(
		time.Date(2026, time.January, 5, 8, 0, 0, 0, time.UTC), 2*time.Hour,
	); queryErr != nil || transition.Kind != openinghours.TransitionOpen {
		t.Fatalf("NextTransition() = %#v error=%v", transition, queryErr)
	}
	if !index.Schedule().Equal(schedule) {
		t.Fatal("Schedule() changed prepared schedule")
	}
}

func TestNewRejectsScheduleBeyondCanonicalLimit(t *testing.T) {
	exceptions := make([]openinghours.Exception, 4096)
	date := openinghours.MustDate(2026, time.January, 1)
	for index := range exceptions {
		revision := fmt.Sprintf("%04d%s", index, strings.Repeat("r", 124))
		var err error
		exceptions[index], err = openinghours.NewException(openinghours.ExceptionConfig{
			Date: date, Operation: openinghours.ExceptionClose,
			Source: strings.Repeat("s", 128), Revision: revision,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", Exceptions: exceptions,
		ConflictPolicy: openinghours.ResolveCanonical,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compile.New(schedule); !openinghours.IsCode(err, openinghours.CodeLimitExceeded) {
		t.Fatalf("compile.New error = %v", err)
	}
}

type closedError struct{}

func (*closedError) Error() string { return "compiled schedule unexpectedly closed" }
