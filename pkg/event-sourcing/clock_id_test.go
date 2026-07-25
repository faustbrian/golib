package eventsourcing_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestClocksReturnCanonicalTime(t *testing.T) {
	t.Parallel()

	local := time.Date(2026, time.July, 25, 11, 22, 33, 456789123, time.FixedZone("EEST", 3*60*60))
	want := time.Date(2026, time.July, 25, 8, 22, 33, 456789000, time.UTC)
	fixed, err := eventsourcing.NewFixedClock(local)
	if err != nil {
		t.Fatal(err)
	}
	if got := fixed.Now(); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("fixed.Now() = %v, want %v in UTC", got, want)
	}

	function := eventsourcing.ClockFunc(func() time.Time { return local })
	if got := function.Now(); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("ClockFunc.Now() = %v, want %v in UTC", got, want)
	}

	before := time.Now().UTC().Add(-time.Second)
	got := (eventsourcing.SystemClock{}).Now()
	after := time.Now().UTC().Add(time.Second)
	if got.Before(before) || got.After(after) || got.Location() != time.UTC ||
		got.Nanosecond()%1_000 != 0 {
		t.Fatalf("SystemClock.Now() = %v, want current UTC microsecond time", got)
	}
}

func TestFixedClockRejectsZeroTime(t *testing.T) {
	t.Parallel()

	if _, err := eventsourcing.NewFixedClock(time.Time{}); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewFixedClock() error = %v", err)
	}
}

func TestManualClockMovesDeterministically(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 25, 11, 22, 33, 456789123, time.FixedZone("EEST", 3*60*60))
	clock, err := eventsourcing.NewManualClock(start)
	if err != nil {
		t.Fatal(err)
	}
	wantStart := time.Date(2026, time.July, 25, 8, 22, 33, 456789000, time.UTC)
	if got := clock.Now(); !got.Equal(wantStart) || got.Location() != time.UTC {
		t.Fatalf("Now() = %v, want %v", got, wantStart)
	}
	if err := clock.Advance(90 * time.Second); err != nil {
		t.Fatal(err)
	}
	if got := clock.Now(); !got.Equal(wantStart.Add(90 * time.Second)) {
		t.Fatalf("advanced Now() = %v", got)
	}

	replacement := start.Add(-time.Hour)
	if err := clock.Set(replacement); err != nil {
		t.Fatal(err)
	}
	if got := clock.Now(); !got.Equal(wantStart.Add(-time.Hour)) {
		t.Fatalf("set Now() = %v", got)
	}
}

func TestManualClockValidatesConstructionAndMovement(t *testing.T) {
	t.Parallel()

	if _, err := eventsourcing.NewManualClock(time.Time{}); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewManualClock() error = %v", err)
	}
	clock, err := eventsourcing.NewManualClock(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := clock.Advance(0); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Advance(0) error = %v", err)
	}
	if err := clock.Advance(-time.Second); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("Advance(negative) error = %v", err)
	}
	if err := clock.Set(time.Time{}); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Set(zero) error = %v", err)
	}
	var nilClock *eventsourcing.ManualClock
	if err := nilClock.Set(time.Now()); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil Set() error = %v", err)
	}
	if err := nilClock.Advance(time.Second); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("nil Advance() error = %v", err)
	}
	zeroClock := &eventsourcing.ManualClock{}
	if err := zeroClock.Set(time.Now()); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("zero Set() error = %v", err)
	}
	if err := zeroClock.Advance(time.Second); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("zero Advance() error = %v", err)
	}
}

func TestManualClockIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 25, 8, 0, 0, 0, time.UTC)
	clock, err := eventsourcing.NewManualClock(start)
	if err != nil {
		t.Fatal(err)
	}

	const workers = 32
	var wait sync.WaitGroup
	wait.Add(workers)
	for range workers {
		go func() {
			defer wait.Done()

			if err := clock.Advance(time.Microsecond); err != nil {
				t.Error(err)
			}
			_ = clock.Now()
		}()
	}
	wait.Wait()
	if got := clock.Now(); !got.Equal(start.Add(workers * time.Microsecond)) {
		t.Fatalf("Now() = %v", got)
	}
}

func TestMessageIDGeneratorFuncValidatesItsBoundary(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), generatorContextKey{}, "expected")
	generator := eventsourcing.MessageIDGeneratorFunc(
		func(received context.Context) (eventsourcing.MessageID, error) {
			if received.Value(generatorContextKey{}) != "expected" {
				t.Fatal("generator did not receive context")
			}

			return eventsourcing.NewMessageID("message-42")
		},
	)
	id, err := generator.NewMessageID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if id.String() != "message-42" {
		t.Fatalf("NewMessageID() = %q", id.String())
	}

	secretFailure := errors.New("credential-secret")
	failing := eventsourcing.MessageIDGeneratorFunc(
		func(context.Context) (eventsourcing.MessageID, error) {
			return eventsourcing.MessageID{}, secretFailure
		},
	)
	_, err = failing.NewMessageID(context.Background())
	if !errors.Is(err, secretFailure) {
		t.Fatalf("NewMessageID() error = %v, want wrapped cause", err)
	}
	if strings.Contains(err.Error(), secretFailure.Error()) {
		t.Fatalf("NewMessageID() disclosed cause: %q", err)
	}

	malformed := eventsourcing.MessageIDGeneratorFunc(
		func(context.Context) (eventsourcing.MessageID, error) {
			return eventsourcing.MessageID{}, nil
		},
	)
	if _, err := malformed.NewMessageID(context.Background()); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("malformed NewMessageID() error = %v", err)
	}

	var nilGenerator eventsourcing.MessageIDGeneratorFunc
	if _, err := nilGenerator.NewMessageID(context.Background()); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("nil NewMessageID() error = %v", err)
	}
	var nilContext context.Context
	if _, err := generator.NewMessageID(nilContext); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("nil-context NewMessageID() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := generator.NewMessageID(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled NewMessageID() error = %v", err)
	}
}

func TestRandomMessageIDGeneratorUsesSuppliedEntropy(t *testing.T) {
	t.Parallel()

	entropy := bytes.NewReader([]byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77,
		0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff,
	})
	generator, err := eventsourcing.NewRandomMessageIDGenerator(entropy.Read)
	if err != nil {
		t.Fatal(err)
	}
	id, err := generator.NewMessageID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id.String() != "00112233445566778899aabbccddeeff" {
		t.Fatalf("NewMessageID() = %q", id.String())
	}

	short, err := eventsourcing.NewRandomMessageIDGenerator(bytes.NewReader(nil).Read)
	if err != nil {
		t.Fatal(err)
	}
	_, err = short.NewMessageID(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("short NewMessageID() error = %v, want EOF", err)
	}
	if strings.Contains(err.Error(), io.EOF.Error()) {
		t.Fatalf("short NewMessageID() disclosed cause: %q", err)
	}

	if _, err := eventsourcing.NewRandomMessageIDGenerator(nil); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewRandomMessageIDGenerator(nil) error = %v", err)
	}
	if _, err := eventsourcing.NewRandomMessageIDGenerator(valueReader{}.Read); err != nil {
		t.Fatalf("NewRandomMessageIDGenerator(value reader) error = %v", err)
	}
	var zeroGenerator *eventsourcing.RandomMessageIDGenerator
	if _, err := zeroGenerator.NewMessageID(context.Background()); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("zero NewMessageID() error = %v", err)
	}
}

func TestRandomMessageIDGeneratorHonorsCancellationBeforeReading(t *testing.T) {
	t.Parallel()

	reader := &countingReader{}
	generator, err := eventsourcing.NewRandomMessageIDGenerator(reader.Read)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := generator.NewMessageID(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("NewMessageID() error = %v, want context.Canceled", err)
	}
	if reader.calls != 0 {
		t.Fatalf("reader calls = %d, want 0", reader.calls)
	}
}

func TestCryptoRandomMessageIDGeneratorProducesCanonicalID(t *testing.T) {
	t.Parallel()

	generator := eventsourcing.NewCryptoRandomMessageIDGenerator()
	id, err := generator.NewMessageID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(id.String()) != 32 {
		t.Fatalf("NewMessageID() length = %d, want 32", len(id.String()))
	}
	if _, err := eventsourcing.NewMessageID(id.String()); err != nil {
		t.Fatalf("NewMessageID() produced invalid ID: %v", err)
	}
}

type generatorContextKey struct{}

type countingReader struct {
	calls int
}

func (reader *countingReader) Read([]byte) (int, error) {
	reader.calls++

	return 0, io.EOF
}

type valueReader struct{}

func (valueReader) Read([]byte) (int, error) {
	return 0, io.EOF
}
