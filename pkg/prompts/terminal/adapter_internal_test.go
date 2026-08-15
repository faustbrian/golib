package terminal

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/creack/pty"
	prompts "github.com/faustbrian/golib/pkg/prompts"
	"golang.org/x/term"
)

func TestAdapterRestoresWhenOutputConfigurationFails(t *testing.T) {
	t.Parallel()

	primary, replica, err := pty.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer primary.Close()
	defer replica.Close()
	before, err := term.GetState(int(replica.Fd()))
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}
	adapter, err := New(replica, replica, Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	outputFailure := errors.New("output configuration failed")
	adapter.setOutput = func(uintptr) error { return outputFailure }
	if err := adapter.Acquire(internalTestContext(t)); !errors.Is(err, prompts.ErrAdapter) ||
		!errors.Is(err, outputFailure) {
		t.Fatalf("Acquire() error = %v", err)
	}
	after, err := term.GetState(int(replica.Fd()))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("restored state = %#v, %v; want %#v", after, err, before)
	}
	if err := setOutputProcessing(^uintptr(0)); err == nil {
		t.Fatal("setOutputProcessing() error = nil")
	}
}

func TestAdapterPropagatesDecoderAndReaderFailures(t *testing.T) {
	t.Parallel()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer reader.Close()
	defer writer.Close()
	adapter, err := New(reader, writer, Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	adapter.setDeadline = func(time.Time) error { return nil }
	adapter.read = func(buffer []byte) (int, error) {
		buffer[0] = 0xff
		return 1, nil
	}
	if _, err := adapter.Next(internalTestContext(t)); !errors.Is(err, prompts.ErrReader) {
		t.Fatalf("decoder Next() error = %v", err)
	}

	readFailure := errors.New("read failed")
	adapter.read = func([]byte) (int, error) { return 0, readFailure }
	if _, err := adapter.Next(internalTestContext(t)); !errors.Is(err, prompts.ErrReader) || !errors.Is(err, readFailure) {
		t.Fatalf("reader Next() error = %v", err)
	}
}

func TestAdapterRejectsUnsupportedDeadlineFailure(t *testing.T) {
	t.Parallel()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer reader.Close()
	defer writer.Close()
	adapter, err := New(reader, writer, Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	deadlineFailure := errors.New("deadline failed")
	adapter.setDeadline = func(time.Time) error { return deadlineFailure }
	adapter.read = func([]byte) (int, error) { return 0, errors.New("unexpected read") }
	if _, err := adapter.Next(internalTestContext(t)); !errors.Is(err, prompts.ErrAdapter) || !errors.Is(err, deadlineFailure) {
		t.Fatalf("Next() error = %v", err)
	}
}

func TestAdapterUsesEarlierContextDeadline(t *testing.T) {
	t.Parallel()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer reader.Close()
	defer writer.Close()
	adapter, err := New(reader, writer, Config{PollInterval: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	want := time.Now().Add(100 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), want)
	defer cancel()
	var got time.Time
	adapter.setDeadline = func(deadline time.Time) error {
		if !deadline.IsZero() {
			got = deadline
		}

		return nil
	}
	adapter.read = func([]byte) (int, error) { return 0, os.ErrClosed }
	if _, err := adapter.Next(ctx); !errors.Is(err, prompts.ErrTerminalDetached) {
		t.Fatalf("Next() error = %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("read deadline = %v, want %v", got, want)
	}
}

func TestAdapterConfigurationAndReadBoundaries(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer reader.Close()
	defer writer.Close()

	defaults, err := New(reader, writer, Config{})
	if err != nil || defaults.readBuffer != defaultReadBuffer || defaults.pollInterval != defaultPollInterval {
		t.Fatalf("default adapter = %#v, %v", defaults, err)
	}
	limits, err := New(reader, writer, Config{
		ReadBuffer: maximumReadBuffer, PollInterval: maximumPollInterval,
	})
	if err != nil || limits.readBuffer != maximumReadBuffer || limits.pollInterval != maximumPollInterval {
		t.Fatalf("limit adapter = %#v, %v", limits, err)
	}
	if validConfig(reader, writer, Config{ReadBuffer: maximumReadBuffer + 1}) ||
		validConfig(reader, writer, Config{PollInterval: maximumPollInterval + 1}) {
		t.Fatal("validConfig accepted values above the documented limits")
	}
	for count, want := range map[int]bool{-1: false, 0: false, 1: true} {
		if got := hasReadBytes(count); got != want {
			t.Fatalf("hasReadBytes(%d) = %v, want %v", count, got, want)
		}
	}
}

func TestAdapterSelectsBoundedReadDeadline(t *testing.T) {
	now := time.Now()
	pollInterval := 2 * time.Second
	if got, want := nextReadDeadline(context.Background(), pollInterval, now), now.Add(pollInterval); !got.Equal(want) {
		t.Fatalf("background deadline = %v, want %v", got, want)
	}
	earlier := now.Add(time.Second)
	earlierContext, cancelEarlier := context.WithDeadline(context.Background(), earlier)
	defer cancelEarlier()
	if got := nextReadDeadline(earlierContext, pollInterval, now); !got.Equal(earlier) {
		t.Fatalf("earlier context deadline = %v, want %v", got, earlier)
	}
	later := now.Add(3 * time.Second)
	laterContext, cancelLater := context.WithDeadline(context.Background(), later)
	defer cancelLater()
	if got, want := nextReadDeadline(laterContext, pollInterval, now), now.Add(pollInterval); !got.Equal(want) {
		t.Fatalf("later context deadline = %v, want %v", got, want)
	}
}

func TestAdapterReadLoopHandlesTimeoutAndEmptyProgress(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer reader.Close()
	defer writer.Close()

	t.Run("timeout flushes a pending escape", func(t *testing.T) {
		adapter, newErr := New(reader, writer, Config{})
		if newErr != nil {
			t.Fatalf("New() error = %v", newErr)
		}
		adapter.setDeadline = func(time.Time) error { return nil }
		adapter.read = func(buffer []byte) (int, error) {
			buffer[0] = 0x1b
			return 1, testTimeoutError{}
		}
		event, nextErr := adapter.Next(internalTestContext(t))
		if nextErr != nil || event != prompts.KeyEvent(prompts.KeyEscape) {
			t.Fatalf("Next() = %#v, %v", event, nextErr)
		}
	})

	t.Run("timeout without an event observes cancellation", func(t *testing.T) {
		adapter, newErr := New(reader, writer, Config{})
		if newErr != nil {
			t.Fatalf("New() error = %v", newErr)
		}
		ctx, cancel := context.WithCancel(context.Background())
		adapter.setDeadline = func(time.Time) error { return nil }
		reads := 0
		adapter.read = func([]byte) (int, error) {
			reads++
			cancel()
			return 0, testTimeoutError{}
		}
		if _, nextErr := adapter.Next(ctx); !errors.Is(nextErr, context.Canceled) || reads != 1 {
			t.Fatalf("Next() error = %v after %d reads", nextErr, reads)
		}
	})

	t.Run("empty successful read observes cancellation", func(t *testing.T) {
		adapter, newErr := New(reader, writer, Config{})
		if newErr != nil {
			t.Fatalf("New() error = %v", newErr)
		}
		ctx, cancel := context.WithCancel(context.Background())
		adapter.setDeadline = func(time.Time) error { return nil }
		reads := 0
		adapter.read = func([]byte) (int, error) {
			reads++
			cancel()
			return 0, nil
		}
		if _, nextErr := adapter.Next(ctx); !errors.Is(nextErr, context.Canceled) || reads != 1 {
			t.Fatalf("Next() error = %v after %d reads", nextErr, reads)
		}
	})
}

type testTimeoutError struct{}

func (testTimeoutError) Error() string { return "timeout" }

func (testTimeoutError) Timeout() bool { return true }

func internalTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	return ctx
}
