package match_test

import (
	"sync"
	"testing"

	localized "github.com/faustbrian/golib/pkg/localized"
	localizedmatch "github.com/faustbrian/golib/pkg/localized/match"
)

func TestBestObserverReceivesBoundedContentFreeOutcome(t *testing.T) {
	t.Parallel()
	value := fixture(t)
	var events []localizedmatch.Event
	result, err := localizedmatch.BestWithOptions(value, localizedmatch.Options{
		MaxCandidates: 4,
		Observer:      localizedmatch.ObserverFunc(func(event localizedmatch.Event) { events = append(events, event) }),
	}, localizedmatch.Preference{Locale: mustLocale(t, "fi"), Weight: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Present || len(events) != 1 {
		t.Fatalf("result = %+v, events = %+v", result, events)
	}
	event := events[0]
	if event.Operation != localizedmatch.OperationMatch || event.Kind != localizedmatch.Exact || event.CandidateCount != 1 {
		t.Fatalf("event = %+v", event)
	}
}

func TestObserverPanicCannotChangeResolution(t *testing.T) {
	t.Parallel()
	result, err := localizedmatch.BestWithOptions(fixture(t), localizedmatch.Options{
		Observer: localizedmatch.ObserverFunc(func(localizedmatch.Event) { panic("observer") }),
	}, localizedmatch.Preference{Locale: mustLocale(t, "fi"), Weight: 1})
	if err != nil || !result.Present || result.Locale != mustLocale(t, "fi") {
		t.Fatalf("BestWithOptions() = %+v, %v", result, err)
	}
}

func TestPlanObserverIsRaceSafeWhenObserverIsRaceSafe(t *testing.T) {
	t.Parallel()
	value, _ := localized.TextFromMap(map[string]string{"en": "Hello"})
	requested := mustLocale(t, "fi")
	var mutex sync.Mutex
	events := make([]localizedmatch.Event, 0, 16)
	plan, err := localizedmatch.NewPlan([]localizedmatch.Chain{{
		From: requested, Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ExactLocale, Locale: mustLocale(t, "en")}},
	}}, localizedmatch.PlanOptions{MaxDepth: 4, MaxCandidates: 4, Observer: localizedmatch.ObserverFunc(func(event localizedmatch.Event) {
		mutex.Lock()
		defer mutex.Unlock()
		events = append(events, event)
	})})
	if err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result := plan.Resolve(value, requested)
			if !result.Present {
				t.Error("missing result")
			}
		}()
	}
	wait.Wait()
	mutex.Lock()
	defer mutex.Unlock()
	if len(events) != 16 {
		t.Fatalf("events = %d", len(events))
	}
	for _, event := range events {
		if event.Operation != localizedmatch.OperationFallback || event.Kind != localizedmatch.Fallback || event.CandidateCount > 4 {
			t.Fatalf("event = %+v", event)
		}
	}
}
