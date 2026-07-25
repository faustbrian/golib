package breakertest_test

import (
	"errors"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	"github.com/faustbrian/golib/pkg/circuit-breaker/breakertest"
)

func TestRecorderRetainsBoundedEventCopies(t *testing.T) {
	t.Parallel()

	recorder, err := breakertest.NewRecorder(2)
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	for generation := uint64(1); generation <= 3; generation++ {
		if err := recorder.Observe(breaker.TransitionEvent{Generation: generation}); err != nil {
			t.Fatalf("Observe() error = %v", err)
		}
	}
	events := recorder.Events()
	if len(events) != 2 || events[0].Generation != 2 || events[1].Generation != 3 {
		t.Fatalf("Events() = %+v", events)
	}
	events[0].Generation = 99
	if got := recorder.Events()[0].Generation; got != 2 {
		t.Fatalf("Events() exposed internal storage, generation = %d", got)
	}
	if recorder.Dropped() != 1 {
		t.Fatalf("Dropped() = %d, want 1", recorder.Dropped())
	}
	recorder.Reset()
	if len(recorder.Events()) != 0 || recorder.Dropped() != 0 {
		t.Fatalf("recorder after Reset() = %+v, dropped %d", recorder.Events(), recorder.Dropped())
	}
}

func TestNewRecorderRejectsNonPositiveCapacity(t *testing.T) {
	t.Parallel()

	if _, err := breakertest.NewRecorder(0); err == nil {
		t.Fatal("NewRecorder() error = nil")
	}
}

func TestScriptedClassifierReturnsSequenceThenFallback(t *testing.T) {
	t.Parallel()

	classifier := breakertest.NewScriptedClassifier(
		breaker.OutcomeIgnored,
		breaker.OutcomeFailure,
		breaker.OutcomeSuccess,
	)
	if classifier.Remaining() != 2 {
		t.Fatalf("initial Remaining() = %d, want 2", classifier.Remaining())
	}
	largeResult := make([]byte, 1<<20)
	wantErr := errors.New("secret-bearing operation error")
	completion := breaker.Completion{
		Result:   largeResult,
		Err:      wantErr,
		Duration: time.Second,
	}

	want := []breaker.Outcome{
		breaker.OutcomeFailure,
		breaker.OutcomeSuccess,
		breaker.OutcomeIgnored,
	}
	for index, expected := range want {
		if got := classifier.Classify(completion); got != expected {
			t.Fatalf("Classify() call %d = %v, want %v", index, got, expected)
		}
	}
	if classifier.Calls() != 3 || classifier.Remaining() != 0 {
		t.Fatalf("classifier calls/remaining = %d/%d", classifier.Calls(), classifier.Remaining())
	}
}
