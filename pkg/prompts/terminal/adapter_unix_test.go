//go:build !windows

package terminal_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/creack/pty"
	prompts "github.com/faustbrian/golib/pkg/prompts"
	"github.com/faustbrian/golib/pkg/prompts/terminal"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func TestInteractiveTextDoesNotEnableKernelEcho(t *testing.T) {
	t.Parallel()

	primary, replica, err := pty.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer primary.Close()
	defer replica.Close()
	adapter, err := terminal.New(replica, replica, terminal.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observer := &echoObserver{Adapter: adapter, primary: primary}
	prompt, err := prompts.NewText(prompts.TextConfig{ID: "name", Label: "Name"})
	if err != nil {
		t.Fatalf("NewText() error = %v", err)
	}
	result, err := prompts.Run(testContext(t), prompt, prompts.Execution{
		Output: replica, Events: observer, Terminal: adapter,
		Capabilities: adapter.Capabilities(),
		Policy: prompts.InteractionPolicy{
			Mode: prompts.InteractiveRequired, PermitInteraction: true,
		},
	})
	if err != nil || result != "ab" {
		t.Fatalf("Run() = %q, %v", result, err)
	}
	if len(observer.echoed) != 0 {
		t.Fatalf("kernel echoed public input %q", observer.echoed)
	}
}

func TestEchoObserverRetriesInterruptedPoll(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer reader.Close()
	defer writer.Close()
	calls := 0
	observer := echoObserver{
		primary: reader,
		poll: func([]unix.PollFd, int) (int, error) {
			calls++
			if calls == 1 {
				return 0, unix.EINTR
			}

			return 0, nil
		},
	}
	output, err := observer.readAvailable()
	if err != nil || len(output) != 0 || calls != 2 {
		t.Fatalf("readAvailable() = %q, %v after %d polls", output, err, calls)
	}
}

type echoObserver struct {
	*terminal.Adapter
	primary *os.File
	poll    func([]unix.PollFd, int) (int, error)
	wrote   bool
	echoed  []byte
}

func (observer *echoObserver) Next(ctx context.Context) (prompts.InputEvent, error) {
	if !observer.wrote {
		observer.wrote = true
		if err := observer.drain(); err != nil {
			return prompts.InputEvent{}, err
		}
		if _, err := observer.primary.Write([]byte("ab\r")); err != nil {
			return prompts.InputEvent{}, err
		}
		event, nextErr := observer.Adapter.Next(ctx)
		echoed, observeErr := observer.readAvailable()
		if observeErr != nil {
			return prompts.InputEvent{}, observeErr
		}
		observer.echoed = echoed

		return event, nextErr
	}

	return observer.Adapter.Next(ctx)
}

func (observer *echoObserver) drain() error {
	for {
		output, err := observer.readAvailable()
		if err != nil || len(output) == 0 {
			return err
		}
	}
}

func (observer *echoObserver) readAvailable() ([]byte, error) {
	poll := []unix.PollFd{{Fd: int32(observer.primary.Fd()), Events: unix.POLLIN}}
	pollInput := observer.poll
	if pollInput == nil {
		pollInput = unix.Poll
	}
	var ready int
	for {
		var err error
		ready, err = pollInput(poll, int((100 * time.Millisecond).Milliseconds()))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return nil, err
		}
		break
	}
	if ready == 0 || poll[0].Revents&unix.POLLIN == 0 {
		return nil, nil
	}
	buffer := make([]byte, 4096)
	count, err := observer.primary.Read(buffer)
	if err != nil {
		return nil, err
	}

	return append([]byte(nil), buffer[:count]...), nil
}

func TestAdapterPreservesTerminalOutputLineEndings(t *testing.T) {
	t.Parallel()

	primary, replica, err := pty.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer primary.Close()
	defer replica.Close()
	adapter, err := terminal.New(replica, replica, terminal.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := adapter.Acquire(testContext(t)); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if _, err := replica.Write([]byte("label\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	fds := []unix.PollFd{{Fd: int32(primary.Fd()), Events: unix.POLLIN}}
	ready, err := unix.Poll(fds, 2000)
	if err != nil || ready != 1 || fds[0].Revents&unix.POLLIN == 0 {
		t.Fatalf("Poll() = %d, %#v, %v", ready, fds, err)
	}
	buffer := make([]byte, len("label\r\n"))
	count, err := primary.Read(buffer)
	if err != nil || count != len(buffer) {
		t.Fatalf("Read() = %d, %v", count, err)
	}
	if got, want := string(buffer), "label\r\n"; got != want {
		t.Fatalf("terminal output = %q, want %q", got, want)
	}
	if err := adapter.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

func TestAdapterAcquiresEchoesAndRestoresPTY(t *testing.T) {
	t.Parallel()

	primary, replica, err := pty.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer primary.Close()
	defer replica.Close()
	if err := pty.Setsize(replica, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		t.Fatalf("Setsize() error = %v", err)
	}
	adapter, err := terminal.New(replica, replica, terminal.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	capabilities := adapter.Capabilities()
	if !capabilities.InputTerminal || !capabilities.OutputTerminal ||
		capabilities.Width != 80 || capabilities.Height != 24 {
		t.Fatalf("Capabilities() = %#v", capabilities)
	}
	if err := adapter.Acquire(testContext(t)); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := adapter.Acquire(testContext(t)); !errors.Is(err, prompts.ErrAdapter) {
		t.Fatalf("repeated Acquire() error = %v", err)
	}
	if err := adapter.SetEcho(false); err != nil {
		t.Fatalf("SetEcho(false) error = %v", err)
	}
	if err := adapter.SetEcho(true); err != nil {
		t.Fatalf("SetEcho(true) error = %v", err)
	}
	if err := adapter.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if err := adapter.Release(); err != nil {
		t.Fatalf("repeated Release() error = %v", err)
	}
}

func TestAdapterRestoresSecretPTYAfterWriterFailure(t *testing.T) {
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
	adapter, err := terminal.New(replica, replica, terminal.Config{
		Decoder: prompts.DecoderConfig{ByteInput: true},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prompt, err := prompts.NewSecretBytesPrompt(prompts.SecretBytesConfig{
		ID: "token", Label: "Token", Class: prompts.SecretToken,
	})
	if err != nil {
		t.Fatalf("NewSecretBytesPrompt() error = %v", err)
	}
	_, err = prompts.Run(testContext(t), prompt, prompts.Execution{
		Output: terminalErrorWriter{}, Events: adapter, Terminal: adapter,
		Capabilities: adapter.Capabilities(),
		Policy: prompts.InteractionPolicy{
			Mode: prompts.InteractiveRequired, PermitInteraction: true,
		},
	})
	if !errors.Is(err, prompts.ErrWriter) {
		t.Fatalf("Run() error = %v", err)
	}
	after, err := term.GetState(int(replica.Fd()))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("restored state = %#v, %v; want %#v", after, err, before)
	}
}

type terminalErrorWriter struct{}

func (terminalErrorWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestAdapterRestoresByteSecretPromptPTY(t *testing.T) {
	t.Parallel()

	for name, input := range map[string][]byte{
		"submit": []byte("\x1b[200~secret-value\x1b[201~\r"),
		"cancel": {0x03},
	} {
		t.Run(name, func(t *testing.T) {
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
			adapter, err := terminal.New(replica, replica, terminal.Config{
				Decoder: prompts.DecoderConfig{ByteInput: true},
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			prompt, err := prompts.NewSecretBytesPrompt(prompts.SecretBytesConfig{
				ID: "token", Label: "Token", Class: prompts.SecretToken,
			})
			if err != nil {
				t.Fatalf("NewSecretBytesPrompt() error = %v", err)
			}
			interaction := make(chan error, 1)
			go func() {
				buffer := make([]byte, 4096)
				for {
					count, readErr := primary.Read(buffer)
					if bytes.Contains(buffer[:count], []byte("Token")) {
						_, writeErr := primary.Write(input)
						interaction <- writeErr
						return
					}
					if readErr != nil {
						interaction <- readErr
						return
					}
				}
			}()
			runContext, cancelRun := context.WithTimeout(context.Background(), 2*time.Second)
			result, runErr := prompts.Run(runContext, prompt, prompts.Execution{
				Output: replica, Error: replica, Events: adapter, Terminal: adapter,
				Capabilities: adapter.Capabilities(),
				Policy: prompts.InteractionPolicy{
					Mode: prompts.InteractiveRequired, PermitInteraction: true,
				},
			})
			cancelRun()
			_ = primary.Close()
			select {
			case interactionErr := <-interaction:
				if interactionErr != nil && runErr == nil {
					t.Fatalf("interaction error = %v", interactionErr)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatal("interaction did not stop after prompt completion")
			}
			if name == "submit" {
				if runErr != nil || string(result.Reveal()) != "secret-value" {
					t.Fatalf("Run() = %v, %v", result, runErr)
				}
				result.Destroy()
			} else if !errors.Is(runErr, prompts.ErrCanceled) {
				t.Fatalf("cancel Run() error = %v", runErr)
			}
			after, err := term.GetState(int(replica.Fd()))
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("restored state = %#v, %v; want %#v", after, err, before)
			}
		})
	}
}

func TestAdapterReportsPTYControlFailures(t *testing.T) {
	t.Parallel()

	primary, replica, err := pty.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer primary.Close()
	adapter, err := terminal.New(replica, replica, terminal.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := adapter.Acquire(testContext(t)); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := replica.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := adapter.SetEcho(false); !errors.Is(err, prompts.ErrAdapter) {
		t.Fatalf("SetEcho() error = %v", err)
	}
	if err := adapter.Release(); !errors.Is(err, prompts.ErrAdapter) {
		t.Fatalf("Release() error = %v", err)
	}
}
