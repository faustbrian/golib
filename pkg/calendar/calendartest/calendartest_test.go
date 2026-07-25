package calendartest_test

import (
	"sync"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/calendarclock"
	"github.com/faustbrian/golib/pkg/calendar/calendartest"
)

func TestFixturesAndTransitionCorpus(t *testing.T) {
	t.Parallel()

	clock := calendartest.FixedClock{Instant: time.Date(2024, 2, 29, 12, 0, 0, 0, time.UTC)}
	today, err := calendarclock.Today(clock, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	calendartest.AssertDate(t, today, "2024-02-29")
	if calendartest.MustLocation(t, "UTC") != time.UTC {
		t.Fatal("UTC location identity changed")
	}
	for _, vector := range calendartest.TransitionVectors() {
		vector := vector
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()
			calendartest.VerifyTransition(t, vector)
		})
	}
	if !calendar.MustDate(2024, time.February, 29).IsValid() {
		t.Fatal("fixture sanity failed")
	}
}

func TestTransitionCorpusIsFreshAndConcurrent(t *testing.T) {
	t.Parallel()

	first := calendartest.TransitionVectors()
	second := calendartest.TransitionVectors()
	first[0].Name = "mutated"
	if second[0].Name == "mutated" {
		t.Fatal("transition corpus shares mutable generated metadata")
	}
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				vectors := calendartest.TransitionVectors()
				if len(vectors) != len(second) || vectors[0].Name != second[0].Name {
					t.Error("transition corpus generation is nondeterministic")
					return
				}
			}
		}()
	}
	wait.Wait()
}
