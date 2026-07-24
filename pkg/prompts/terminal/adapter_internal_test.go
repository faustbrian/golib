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
	if err := adapter.Acquire(context.Background()); !errors.Is(err, prompts.ErrAdapter) ||
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
	if _, err := adapter.Next(context.Background()); !errors.Is(err, prompts.ErrReader) {
		t.Fatalf("decoder Next() error = %v", err)
	}

	readFailure := errors.New("read failed")
	adapter.read = func([]byte) (int, error) { return 0, readFailure }
	if _, err := adapter.Next(context.Background()); !errors.Is(err, prompts.ErrReader) || !errors.Is(err, readFailure) {
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
	if _, err := adapter.Next(context.Background()); !errors.Is(err, prompts.ErrAdapter) || !errors.Is(err, deadlineFailure) {
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
