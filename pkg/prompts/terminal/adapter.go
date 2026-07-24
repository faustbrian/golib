// Package terminal provides an explicit application adapter for caller-owned
// terminal files. Importing it performs no detection or terminal mutation.
package terminal

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	prompts "github.com/faustbrian/golib/pkg/prompts"
	"golang.org/x/term"
)

const (
	defaultReadBuffer                 = 4096
	defaultPollInterval time.Duration = 50_000_000
	maximumReadBuffer                 = 1 << 20
	maximumPollInterval               = time.Second
)

// Config bounds reads, cancellation polling, and byte decoding.
type Config struct {
	Decoder      prompts.DecoderConfig
	ReadBuffer   int
	PollInterval time.Duration
}

// Adapter implements prompts.EventSource and prompts.TerminalController for
// explicit files. A single prompt execution owns an Adapter at a time.
type Adapter struct {
	mutex        sync.Mutex
	input        *os.File
	output       *os.File
	decoder      *prompts.Decoder
	readBuffer   int
	pollInterval time.Duration
	state        *term.State
	acquired     bool
	queued       []prompts.InputEvent
	eof          bool
	read         func([]byte) (int, error)
	setDeadline  func(time.Time) error
	setOutput    func(uintptr) error
	restore      func(int, *term.State) error
}

// New constructs an inert adapter without reading or mutating either file.
func New(input, output *os.File, config Config) (*Adapter, error) {
	if input == nil || output == nil || config.ReadBuffer < 0 || config.ReadBuffer > maximumReadBuffer ||
		config.PollInterval < 0 || config.PollInterval > maximumPollInterval {
		return nil, adapterFailure(prompts.ErrorInvalidDefinition, "define terminal adapter", prompts.ErrInvalidDefinition)
	}
	if config.ReadBuffer == 0 {
		config.ReadBuffer = defaultReadBuffer
	}
	if config.PollInterval == 0 {
		config.PollInterval = defaultPollInterval
	}
	decoder, err := prompts.NewDecoder(config.Decoder)
	if err != nil {
		return nil, err
	}

	return &Adapter{
		input: input, output: output, decoder: decoder,
		readBuffer: config.ReadBuffer, pollInterval: config.PollInterval,
		read: input.Read, setDeadline: input.SetReadDeadline,
		setOutput: setOutputProcessing, restore: term.Restore,
	}, nil
}

// Capabilities detects only the explicitly supplied files. It does not inspect
// environment variables or interaction policy.
func (adapter *Adapter) Capabilities() prompts.Capabilities {
	inputTerminal := term.IsTerminal(int(adapter.input.Fd()))
	outputTerminal := term.IsTerminal(int(adapter.output.Fd()))
	width, height, err := term.GetSize(int(adapter.output.Fd()))
	if err != nil {
		width, height = 0, 0
	}

	return prompts.Capabilities{
		InputTerminal: inputTerminal, OutputTerminal: outputTerminal,
		Width: width, Height: height, CursorMovement: outputTerminal,
		Animation: outputTerminal, Unicode: true,
	}
}

// Acquire places the explicit input terminal into raw mode.
func (adapter *Adapter) Acquire(ctx context.Context) error {
	if ctx == nil {
		return adapterFailure(
			prompts.ErrorInvalidDefinition, "acquire terminal adapter", prompts.ErrInvalidDefinition,
		)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	if adapter.acquired {
		return adapterFailure(prompts.ErrorAdapter, "acquire terminal adapter", prompts.ErrAdapter)
	}
	state, err := term.MakeRaw(int(adapter.input.Fd()))
	if err != nil {
		return adapterFailure(prompts.ErrorAdapter, "acquire terminal adapter", err)
	}
	if err := adapter.setOutput(adapter.input.Fd()); err != nil {
		restoreErr := adapter.restore(int(adapter.input.Fd()), state)
		return adapterFailure(
			prompts.ErrorAdapter, "configure terminal output", errors.Join(err, restoreErr),
		)
	}
	adapter.state = state
	adapter.acquired = true

	return nil
}

// SetEcho changes only the echo flag of an acquired raw terminal.
func (adapter *Adapter) SetEcho(enabled bool) error {
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	if !adapter.acquired {
		return adapterFailure(prompts.ErrorAdapter, "configure terminal echo", prompts.ErrAdapter)
	}
	if err := setEcho(adapter.input.Fd(), enabled); err != nil {
		return adapterFailure(prompts.ErrorAdapter, "configure terminal echo", err)
	}

	return nil
}

// Release restores the state captured by Acquire. It is idempotent.
func (adapter *Adapter) Release() error {
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	if !adapter.acquired {
		return nil
	}
	state := adapter.state
	adapter.state = nil
	adapter.acquired = false
	if err := adapter.restore(int(adapter.input.Fd()), state); err != nil {
		return adapterFailure(prompts.ErrorAdapter, "release terminal adapter", err)
	}

	return nil
}

// Next reads and decodes one semantic event. Short file deadlines keep context
// cancellation bounded without a hidden goroutine.
func (adapter *Adapter) Next(ctx context.Context) (prompts.InputEvent, error) {
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	if ctx == nil {
		return prompts.InputEvent{}, adapterFailure(
			prompts.ErrorInvalidDefinition, "read terminal adapter", prompts.ErrInvalidDefinition,
		)
	}
	if len(adapter.queued) > 0 {
		return adapter.dequeue(), nil
	}
	if adapter.eof {
		return prompts.InputEvent{}, io.EOF
	}

	buffer := make([]byte, adapter.readBuffer)
	defer clear(buffer)
	defer func() { _ = adapter.setDeadline(time.Time{}) }()
	for {
		if err := ctx.Err(); err != nil {
			return prompts.InputEvent{}, err
		}
		deadline := time.Now().Add(adapter.pollInterval)
		if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
			deadline = contextDeadline
		}
		var count int
		var readErr error
		if err := adapter.setDeadline(deadline); errors.Is(err, os.ErrNoDeadline) {
			count, readErr = readWithoutDeadline(ctx, adapter.input, buffer, adapter.pollInterval)
			if errors.Is(readErr, os.ErrNoDeadline) {
				return prompts.InputEvent{}, adapterFailure(prompts.ErrorAdapter, "set terminal deadline", err)
			}
		} else if err != nil {
			if failure := adapter.readFailure("set terminal deadline", err); errors.Is(failure, prompts.ErrTerminalDetached) {
				return prompts.InputEvent{}, failure
			}
			return prompts.InputEvent{}, adapterFailure(prompts.ErrorAdapter, "set terminal deadline", err)
		} else {
			count, readErr = adapter.read(buffer)
		}
		if count > 0 {
			events, decodeErr := adapter.decoder.Feed(buffer[:count])
			if decodeErr != nil {
				return prompts.InputEvent{}, decodeErr
			}
			adapter.queued = append(adapter.queued, events...)
		}
		if errors.Is(readErr, io.EOF) {
			flushed, flushErr := adapter.decoder.Flush()
			if flushErr != nil {
				return prompts.InputEvent{}, flushErr
			}
			adapter.queued = append(adapter.queued, flushed...)
			adapter.eof = true
		} else if readErr != nil {
			var timeout interface{ Timeout() bool }
			if errors.As(readErr, &timeout) && timeout.Timeout() {
				flushed, flushErr := adapter.decoder.Flush()
				if flushErr != nil {
					return prompts.InputEvent{}, flushErr
				}
				adapter.queued = append(adapter.queued, flushed...)
				if len(adapter.queued) > 0 {
					return adapter.dequeue(), nil
				}
				continue
			}
			return prompts.InputEvent{}, adapter.readFailure("read terminal input", readErr)
		}
		if len(adapter.queued) > 0 {
			return adapter.dequeue(), nil
		}
		if adapter.eof {
			return prompts.InputEvent{}, io.EOF
		}
	}
}

func (adapter *Adapter) dequeue() prompts.InputEvent {
	event := adapter.queued[0]
	copy(adapter.queued, adapter.queued[1:])
	adapter.queued[len(adapter.queued)-1] = prompts.InputEvent{}
	adapter.queued = adapter.queued[:len(adapter.queued)-1]

	return event
}

func adapterFailure(kind prompts.ErrorKind, operation string, cause error) error {
	return &prompts.Error{Kind: kind, Operation: operation, Cause: cause}
}

func (adapter *Adapter) readFailure(operation string, cause error) error {
	_, statErr := adapter.input.Stat()
	if adapter.input.Fd() == ^uintptr(0) || errors.Is(cause, os.ErrClosed) || errors.Is(statErr, os.ErrClosed) {
		return adapterFailure(
			prompts.ErrorTerminalDetached, operation,
			errors.Join(prompts.ErrTerminalDetached, cause),
		)
	}

	return adapterFailure(prompts.ErrorReader, operation, cause)
}

var _ prompts.EventSource = (*Adapter)(nil)
var _ prompts.TerminalController = (*Adapter)(nil)
