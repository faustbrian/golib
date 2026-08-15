//go:build linux || darwin

package terminal

import (
	"context"
	"errors"
	"math"
	"os"
	"testing"
	"time"

	prompts "github.com/faustbrian/golib/pkg/prompts"
	"golang.org/x/sys/unix"
)

func TestAdapterPollsReadableFileWhenDeadlinesAreUnsupported(t *testing.T) {
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
	adapter.setDeadline = func(time.Time) error { return os.ErrNoDeadline }
	if _, err := writer.Write([]byte("x")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	event, err := adapter.Next(internalTestContext(t))
	if err != nil || event != prompts.RuneEvent('x') {
		t.Fatalf("Next() = %#v, %v", event, err)
	}
}

func TestReadWithoutDeadlineRejectsUnsafeAndFailedDescriptors(t *testing.T) {
	t.Parallel()

	t.Run("canceled", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatalf("Pipe() error = %v", err)
		}
		defer reader.Close()
		defer writer.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := readWithoutDeadlineUsing(
			ctx, reader, make([]byte, 1), time.Millisecond, reader.Stat, unix.Poll,
		); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled read error = %v", err)
		}
	})

	t.Run("closed", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatalf("Pipe() error = %v", err)
		}
		defer writer.Close()
		if err := reader.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if _, err := readWithoutDeadlineUsing(
			internalTestContext(t), reader, make([]byte, 1), time.Millisecond,
			reader.Stat, unix.Poll,
		); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("closed read error = %v", err)
		}
	})

	t.Run("stat failure", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatalf("Pipe() error = %v", err)
		}
		defer reader.Close()
		defer writer.Close()
		statFailure := errors.New("stat failed")
		if _, err := readWithoutDeadlineUsing(
			internalTestContext(t), reader, make([]byte, 1), time.Millisecond,
			func() (os.FileInfo, error) { return nil, statFailure }, unix.Poll,
		); !errors.Is(err, statFailure) {
			t.Fatalf("stat read error = %v", err)
		}
	})

	t.Run("poll failure", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatalf("Pipe() error = %v", err)
		}
		defer reader.Close()
		defer writer.Close()
		pollFailure := errors.New("poll failed")
		if _, err := readWithoutDeadlineUsing(
			internalTestContext(t), reader, make([]byte, 1), time.Millisecond,
			reader.Stat, func([]unix.PollFd, int) (int, error) { return 0, pollFailure },
		); !errors.Is(err, pollFailure) {
			t.Fatalf("poll read error = %v", err)
		}
	})

	t.Run("poll timeout", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatalf("Pipe() error = %v", err)
		}
		defer reader.Close()
		defer writer.Close()
		if _, err := readWithoutDeadlineUsing(
			internalTestContext(t), reader, make([]byte, 1), time.Millisecond,
			reader.Stat, func([]unix.PollFd, int) (int, error) { return 0, nil },
		); !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("poll timeout error = %v", err)
		}
	})

	t.Run("interrupted then invalid", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatalf("Pipe() error = %v", err)
		}
		defer reader.Close()
		defer writer.Close()
		calls := 0
		poll := func(fds []unix.PollFd, _ int) (int, error) {
			calls++
			if calls == 1 {
				return 0, unix.EINTR
			}
			fds[0].Revents = unix.POLLNVAL
			return 1, nil
		}
		if _, err := readWithoutDeadlineUsing(
			internalTestContext(t), reader, make([]byte, 1), time.Millisecond,
			reader.Stat, poll,
		); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("invalid poll read error = %v", err)
		}
	})
}

func TestPollBoundaryHelpers(t *testing.T) {
	for _, test := range []struct {
		descriptor uintptr
		want       bool
	}{
		{0, true},
		{math.MaxInt32, true},
		{math.MaxInt32 + 1, false},
		{^uintptr(0), false},
	} {
		if got := validPollDescriptor(test.descriptor); got != test.want {
			t.Fatalf("validPollDescriptor(%d) = %v, want %v", test.descriptor, got, test.want)
		}
	}
	now := time.Now()
	pollInterval := 2 * time.Second
	if got := boundedPollWait(context.Background(), pollInterval, now); got != pollInterval {
		t.Fatalf("background poll wait = %v, want %v", got, pollInterval)
	}
	earlier := now.Add(time.Second)
	earlierContext, cancelEarlier := context.WithDeadline(context.Background(), earlier)
	defer cancelEarlier()
	if got := boundedPollWait(earlierContext, pollInterval, now); got != time.Second {
		t.Fatalf("earlier poll wait = %v, want %v", got, time.Second)
	}
	later := now.Add(3 * time.Second)
	laterContext, cancelLater := context.WithDeadline(context.Background(), later)
	defer cancelLater()
	if got := boundedPollWait(laterContext, pollInterval, now); got != pollInterval {
		t.Fatalf("later poll wait = %v, want %v", got, pollInterval)
	}
	for _, test := range []struct {
		wait time.Duration
		want int
	}{
		{0, 1},
		{time.Nanosecond, 1},
		{time.Millisecond, 1},
		{time.Millisecond + time.Nanosecond, 2},
	} {
		if got := pollMilliseconds(test.wait); got != test.want {
			t.Fatalf("pollMilliseconds(%v) = %d, want %d", test.wait, got, test.want)
		}
	}
	for _, test := range []struct {
		events   int16
		invalid  bool
		readable bool
	}{
		{0, false, false},
		{unix.POLLNVAL, true, false},
		{unix.POLLIN, false, true},
		{unix.POLLHUP, false, true},
		{unix.POLLERR, false, true},
		{unix.POLLIN | unix.POLLHUP | unix.POLLERR, false, true},
	} {
		if got := invalidPollEvents(test.events); got != test.invalid {
			t.Fatalf("invalidPollEvents(%d) = %v, want %v", test.events, got, test.invalid)
		}
		if got := readablePollEvents(test.events); got != test.readable {
			t.Fatalf("readablePollEvents(%d) = %v, want %v", test.events, got, test.readable)
		}
	}
}

func TestReadWithoutDeadlineUsesBoundedWaitAndRelevantEvents(t *testing.T) {
	for name, events := range map[string]int16{
		"input": unix.POLLIN, "hangup": unix.POLLHUP, "error": unix.POLLERR,
	} {
		t.Run(name, func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatalf("Pipe() error = %v", err)
			}
			defer reader.Close()
			if _, err := writer.Write([]byte("x")); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			if err := writer.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			pollCalls := 0
			count, readErr := readWithoutDeadlineUsing(
				internalTestContext(t), reader, make([]byte, 1), 1500*time.Microsecond,
				reader.Stat,
				func(fds []unix.PollFd, milliseconds int) (int, error) {
					pollCalls++
					if milliseconds != 2 {
						t.Fatalf("poll milliseconds = %d, want 2", milliseconds)
					}
					if pollCalls == 1 {
						fds[0].Revents = 0
						return 1, nil
					}
					fds[0].Revents = events
					return 1, nil
				},
			)
			if readErr != nil || count != 1 || pollCalls != 2 {
				t.Fatalf("read = %d, %v after %d polls", count, readErr, pollCalls)
			}
		})
	}
}
