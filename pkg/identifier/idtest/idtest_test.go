package idtest_test

import (
	"errors"
	"io"
	"testing"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	"github.com/faustbrian/golib/pkg/identifier/idtest"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
)

func TestClockSupportsDeterministicConcurrentSafeControl(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := idtest.NewClock(initial)
	if got := clock.Now(); !got.Equal(initial) {
		t.Fatalf("Now() = %v", got)
	}
	clock.Advance(time.Second)
	if got := clock.Now(); !got.Equal(initial.Add(time.Second)) {
		t.Fatalf("advanced Now() = %v", got)
	}
	clock.Set(initial.Add(time.Hour))
	if got := clock.Now(); !got.Equal(initial.Add(time.Hour)) {
		t.Fatalf("set Now() = %v", got)
	}
}

func TestReaderIsDeterministicAndFailureIsInjectable(t *testing.T) {
	left := idtest.NewReader([]byte("seed"))
	right := idtest.NewReader([]byte("seed"))
	leftBytes := make([]byte, 80)
	rightBytes := make([]byte, 80)
	if _, err := io.ReadFull(left, leftBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(right, rightBytes); err != nil {
		t.Fatal(err)
	}
	if string(leftBytes) != string(rightBytes) {
		t.Fatal("readers with the same seed diverged")
	}

	want := errors.New("entropy unavailable")
	buffer := make([]byte, 1)
	if _, err := idtest.ErrorReader(want).Read(buffer); !errors.Is(err, want) {
		t.Fatalf("ErrorReader error = %v", err)
	}
}

func TestAssertionsExerciseRealGeneratorAndCodec(t *testing.T) {
	clock := idtest.NewClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	generator := identifieruuid.NewV7Generator(clock, idtest.NewReader([]byte("uuid")))
	values := idtest.AssertUnique(t, generator, 1000)
	if len(values) != 1000 {
		t.Fatalf("AssertUnique returned %d values", len(values))
	}
	idtest.AssertCanonical(t, values[0], identifieruuid.Parse)
	idtest.AssertStrictlyOrdered(t, values, func(left, right identifieruuid.ID) int {
		return left.Compare(right)
	})

	var _ identifier.Clock = clock
}
