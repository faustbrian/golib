package terminal_test

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	prompts "github.com/faustbrian/golib/pkg/prompts"
	"github.com/faustbrian/golib/pkg/prompts/terminal"
)

func TestAdapterReadsQueuedEventsAndEOF(t *testing.T) {
	t.Parallel()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer reader.Close()
	adapter, err := terminal.New(reader, writer, terminal.Config{PollInterval: time.Millisecond})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := writer.Write([]byte("ab")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	for index, want := range []rune{'a', 'b'} {
		event, nextErr := adapter.Next(context.Background())
		if nextErr != nil || event != prompts.RuneEvent(want) {
			t.Fatalf("Next(%d) = %#v, %v", index, event, nextErr)
		}
	}
	if _, err := adapter.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("EOF Next() error = %v", err)
	}
	if _, err := adapter.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("repeated EOF Next() error = %v", err)
	}
}

func TestAdapterDecodesBytePasteWithoutStringPayload(t *testing.T) {
	t.Parallel()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer reader.Close()
	adapter, err := terminal.New(reader, writer, terminal.Config{
		Decoder: prompts.DecoderConfig{ByteInput: true},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := writer.Write([]byte("\x1b[200~secret\x1b[201~")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	event, err := adapter.Next(context.Background())
	if err != nil || event.Text != "" || string(event.Bytes.Reveal()) != "secret" {
		t.Fatalf("Next() = %#v, %v", event, err)
	}
	event.Destroy()
}

func TestAdapterFlushesEscapeAndRejectsTruncation(t *testing.T) {
	t.Parallel()

	for name, input := range map[string][]byte{
		"escape":    {0x1b},
		"truncated": []byte("\x1b["),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatalf("Pipe() error = %v", err)
			}
			defer reader.Close()
			adapter, err := terminal.New(reader, writer, terminal.Config{})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, _ = writer.Write(input)
			_ = writer.Close()
			event, nextErr := adapter.Next(context.Background())
			if name == "escape" {
				if nextErr != nil || event != prompts.KeyEvent(prompts.KeyEscape) {
					t.Fatalf("Next() = %#v, %v", event, nextErr)
				}
				return
			}
			if !errors.Is(nextErr, prompts.ErrReader) {
				t.Fatalf("Next() error = %v", nextErr)
			}
		})
	}
}

func TestAdapterResolvesEscapeWhileInputRemainsOpen(t *testing.T) {
	t.Parallel()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer reader.Close()
	defer writer.Close()
	adapter, err := terminal.New(reader, writer, terminal.Config{PollInterval: time.Millisecond})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := writer.Write([]byte{0x1b}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := adapter.Next(ctx)
	if err != nil || event != prompts.KeyEvent(prompts.KeyEscape) {
		t.Fatalf("Next() = %#v, %v", event, err)
	}
}

func TestAdapterRejectsTimedOutPartialSequence(t *testing.T) {
	t.Parallel()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer reader.Close()
	defer writer.Close()
	adapter, err := terminal.New(reader, writer, terminal.Config{PollInterval: time.Millisecond})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := writer.Write([]byte("\x1b[")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := adapter.Next(ctx); !errors.Is(err, prompts.ErrReader) {
		t.Fatalf("Next() error = %v", err)
	}
}

func TestAdapterCancellationAndReadFailures(t *testing.T) {
	t.Parallel()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer reader.Close()
	defer writer.Close()
	adapter, err := terminal.New(reader, writer, terminal.Config{PollInterval: time.Nanosecond})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Next(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Next() error = %v", err)
	}
	deadline, stop := context.WithTimeout(context.Background(), time.Millisecond)
	defer stop()
	if _, err := adapter.Next(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline Next() error = %v", err)
	}

	closedReader, closedWriter, _ := os.Pipe()
	closedAdapter, _ := terminal.New(closedReader, closedWriter, terminal.Config{})
	_ = closedReader.Close()
	defer closedWriter.Close()
	if _, err := closedAdapter.Next(context.Background()); !errors.Is(err, prompts.ErrTerminalDetached) {
		t.Fatalf("closed Next() error = %v", err)
	}

	file, err := os.CreateTemp(t.TempDir(), "terminal-adapter")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	defer file.Close()
	fileAdapter, _ := terminal.New(file, file, terminal.Config{})
	if _, err := fileAdapter.Next(context.Background()); !errors.Is(err, prompts.ErrAdapter) {
		t.Fatalf("deadline failure = %v", err)
	}
}

func TestAdapterValidatesConfigAndNonTerminalControl(t *testing.T) {
	t.Parallel()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer reader.Close()
	defer writer.Close()
	for _, test := range []struct {
		input, output *os.File
		config        terminal.Config
	}{
		{nil, writer, terminal.Config{}},
		{reader, nil, terminal.Config{}},
		{reader, writer, terminal.Config{ReadBuffer: -1}},
		{reader, writer, terminal.Config{ReadBuffer: 1 << 21}},
		{reader, writer, terminal.Config{PollInterval: -1}},
		{reader, writer, terminal.Config{PollInterval: 2 * time.Second}},
		{reader, writer, terminal.Config{Decoder: prompts.DecoderConfig{MaxPasteBytes: 2, MaxBufferBytes: 1}}},
	} {
		if _, err := terminal.New(test.input, test.output, test.config); !errors.Is(err, prompts.ErrInvalidDefinition) {
			t.Fatalf("New(%#v) error = %v", test.config, err)
		}
	}
	adapter, err := terminal.New(reader, writer, terminal.Config{ReadBuffer: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	capabilities := adapter.Capabilities()
	if capabilities.InputTerminal || capabilities.OutputTerminal || capabilities.Width != 0 ||
		capabilities.Height != 0 || !capabilities.Unicode {
		t.Fatalf("Capabilities() = %#v", capabilities)
	}
	if err := adapter.SetEcho(false); !errors.Is(err, prompts.ErrAdapter) {
		t.Fatalf("SetEcho() error = %v", err)
	}
	if err := adapter.Acquire(context.Background()); !errors.Is(err, prompts.ErrAdapter) {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := adapter.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	var nilContext context.Context
	if err := adapter.Acquire(nilContext); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("nil context Acquire() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := adapter.Acquire(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Acquire() error = %v", err)
	}
	if _, err := adapter.Next(nilContext); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("nil context Next() error = %v", err)
	}
}
