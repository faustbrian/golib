//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package terminal

func setEcho(descriptor uintptr, enabled bool) error {
	return setEchoUsing(descriptor, enabled, readTerminalState, writeTerminalState)
}

func setEchoUsing(
	descriptor uintptr,
	enabled bool,
	read func(uintptr) (*terminalState, error),
	write func(uintptr, *terminalState) error,
) error {
	state, err := read(descriptor)
	if err != nil {
		return err
	}
	if enabled {
		state.Lflag |= terminalEchoFlag
	} else {
		state.Lflag &^= terminalEchoFlag
	}

	return write(descriptor, state)
}

func setOutputProcessing(descriptor uintptr) error {
	state, err := readTerminalState(descriptor)
	if err != nil {
		return err
	}
	state.Oflag |= terminalOutputProcessingFlag

	return writeTerminalState(descriptor, state)
}
