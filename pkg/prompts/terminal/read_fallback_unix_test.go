//go:build linux || darwin

package terminal

import (
	"context"
	"errors"
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

	event, err := adapter.Next(context.Background())
	if err != nil || event != prompts.RuneEvent('x') {
		t.Fatalf("Next() = %#v, %v", event, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := adapter.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled Next() error = %v", err)
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
			context.Background(), reader, make([]byte, 1), time.Millisecond,
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
			context.Background(), reader, make([]byte, 1), time.Millisecond,
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
			context.Background(), reader, make([]byte, 1), time.Millisecond,
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
			context.Background(), reader, make([]byte, 1), time.Millisecond,
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
			context.Background(), reader, make([]byte, 1), time.Millisecond,
			reader.Stat, poll,
		); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("invalid poll read error = %v", err)
		}
	})
}
