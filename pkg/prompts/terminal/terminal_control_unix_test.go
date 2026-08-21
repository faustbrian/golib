//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package terminal

import (
	"errors"
	"testing"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func TestTerminalControlMutatesRequestedEchoState(t *testing.T) {
	for _, test := range
		[]struct {
			name string
			initial bool
			enabled bool
			preserveUnrelated bool
		}{
			{name: "disable", initial: true, enabled: false},
			{name: "enable", initial: false, enabled: true, preserveUnrelated: true},
		} {
		t.Run(
			test.name,
			func(t *testing.T) {
				state := &terminalState{}
				if test.initial {
					state.Lflag |= terminalEchoFlag
				}
				unrelatedFlag := (state.Lflag | terminalEchoFlag) << 1
				if test.preserveUnrelated {
					state.Lflag |= unrelatedFlag
				}
				written := false
				err := setEchoUsing(
					23,
					test.enabled,
					func(descriptor uintptr) (*terminalState, error) {
						if descriptor != 23 {
							t.Fatalf("read descriptor = %d", descriptor)
						}
						return state, nil
					},
					func(descriptor uintptr, got *terminalState) error {
						written = true
						if descriptor != 23 || got != state {
							t.Fatalf(
								"write = %d, %p; want 23, %p",
								descriptor,
								got,
								state,
							)
						}
						return nil
					},
				)
				if err != nil || !written {
					t.Fatalf("setEchoUsing() = %v, written %v", err, written)
				}
				if got := state.Lflag & terminalEchoFlag != 0; got != test.enabled {
					t.Fatalf("echo enabled = %v, want %v", got, test.enabled)
				}
				if test.preserveUnrelated && state.Lflag & unrelatedFlag == 0 {
					t.Fatal("setEchoUsing() removed an unrelated local flag")
				}
			},
		)
	}

	readFailure := errors.New("read failed")
	if err := setEchoUsing(
		23,
		true,
		func(uintptr) (*terminalState, error) {
			return nil, readFailure
		},
		func(uintptr, *terminalState) error {
			t.Fatal("write called after read failure")
			return nil
		},
	);
		!errors.Is(err, readFailure) {
		t.Fatalf("read failure = %v", err)
	}
	writeFailure := errors.New("write failed")
	if err := setEchoUsing(
		23,
		true,
		func(uintptr) (*terminalState, error) {
			return &terminalState{}, nil
		},
		func(uintptr, *terminalState) error {
			return writeFailure
		},
	);
		!errors.Is(err, writeFailure) {
		t.Fatalf("write failure = %v", err)
	}
}

func TestTerminalControlAppliesEchoAndOutputFlags(t *testing.T) {
	primary, replica, err := pty.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer primary.Close()
	defer replica.Close()
	original, err := term.GetState(int(replica.Fd()))
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}
	defer func() {
		if restoreErr := term.Restore(int(replica.Fd()), original); restoreErr != nil {
			t.Errorf("Restore() error = %v", restoreErr)
		}
	}()

	state, err := readTerminalState(replica.Fd())
	if err != nil {
		t.Fatalf("read initial state error = %v", err)
	}
	state.Lflag |= terminalEchoFlag
	if err := writeTerminalState(replica.Fd(), state); err != nil {
		t.Fatalf("enable initial echo error = %v", err)
	}
	if err := setEcho(replica.Fd(), false); err != nil {
		t.Fatalf("setEcho(false) error = %v", err)
	}
	current, err := readTerminalState(replica.Fd())
	if err != nil || current.Lflag & terminalEchoFlag != 0 {
		t.Fatalf("disabled echo state = %#v, %v", current, err)
	}
	current.Lflag &^= terminalEchoFlag
	if err := writeTerminalState(replica.Fd(), current); err != nil {
		t.Fatalf("disable initial echo error = %v", err)
	}
	if err := setEcho(replica.Fd(), true); err != nil {
		t.Fatalf("setEcho(true) error = %v", err)
	}
	current, err = readTerminalState(replica.Fd())
	if err != nil || current.Lflag & terminalEchoFlag == 0 {
		t.Fatalf("enabled echo state = %#v, %v", current, err)
	}
	current.Oflag &^= terminalOutputProcessingFlag
	if err := writeTerminalState(replica.Fd(), current); err != nil {
		t.Fatalf("clear output processing error = %v", err)
	}
	if err := setOutputProcessing(replica.Fd()); err != nil {
		t.Fatalf("setOutputProcessing() error = %v", err)
	}
	current, err = readTerminalState(replica.Fd())
	if err != nil || current.Oflag & terminalOutputProcessingFlag == 0 {
		t.Fatalf("output processing state = %#v, %v", current, err)
	}
}

func TestTerminalControlRejectsInvalidDescriptor(t *testing.T) {
	if err := setEcho(^uintptr(0), false); err == nil {
		t.Fatal("setEcho() error = nil")
	}
	if err := setOutputProcessing(^uintptr(0)); err == nil {
		t.Fatal("setOutputProcessing() error = nil")
	}
}
