package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode"
)

const (
	maximumOutputRecords = 1000
	maximumOutputBytes   = 1 << 20
)

// OutputMode selects a stable presentation contract.
type OutputMode uint8

const (
	// OutputHuman emits plain text for people and pipes.
	OutputHuman OutputMode = iota
	// OutputJSON emits a versioned JSON envelope on stdout.
	OutputJSON
	// OutputQuiet suppresses successful informational output.
	OutputQuiet
)

// OutputPolicy controls one invocation's presentation contract.
type OutputPolicy struct {
	Mode    OutputMode
	NoColor bool
	Width   int
}

// Output buffers bounded invocation output until terminal success is known.
type Output struct {
	mu        sync.Mutex
	infos     []string
	dataJSON  json.RawMessage
	dataHuman string
	hasData   bool
	bytes     int
	dataBytes int
}

// Info records a bounded informational line.
func (output *Output) Info(message string) error {
	if output == nil {
		return newInternalError("write through a nil output", nil)
	}
	output.mu.Lock()
	defer output.mu.Unlock()
	if len(output.infos) >= maximumOutputRecords ||
		output.bytes+output.dataBytes+len(message) > maximumOutputBytes {
		return newClassifiedError(ErrorKindOutput, "output exceeds configured limit", nil, false)
	}
	output.infos = append(output.infos, message)
	output.bytes += len(message)

	return nil
}

// SetData records one success value for human or JSON rendering.
func (output *Output) SetData(value any) error {
	if output == nil {
		return newInternalError("write through a nil output", nil)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return newClassifiedError(ErrorKindOutput, "encode structured output", err, true)
	}
	human := fmt.Sprint(value)
	if len(encoded) > maximumOutputBytes || len(human) > maximumOutputBytes {
		return newClassifiedError(ErrorKindOutput, "output exceeds configured limit", nil, false)
	}
	output.mu.Lock()
	defer output.mu.Unlock()
	dataBytes := max(len(encoded), len(human))
	if output.bytes+dataBytes > maximumOutputBytes {
		return newClassifiedError(ErrorKindOutput, "output exceeds configured limit", nil, false)
	}
	output.dataJSON = append(output.dataJSON[:0], encoded...)
	output.dataHuman = human
	output.hasData = true
	output.dataBytes = dataBytes

	return nil
}

type outputSnapshot struct {
	infos     []string
	dataJSON  json.RawMessage
	dataHuman string
	hasData   bool
}

func (output *Output) snapshot() outputSnapshot {
	if output == nil {
		return outputSnapshot{}
	}
	output.mu.Lock()
	defer output.mu.Unlock()

	return outputSnapshot{
		infos:     cloneStrings(output.infos),
		dataJSON:  append(json.RawMessage(nil), output.dataJSON...),
		dataHuman: output.dataHuman,
		hasData:   output.hasData,
	}
}

func renderSuccess(writer io.Writer, policy OutputPolicy, output *Output) error {
	snapshot := output.snapshot()
	switch policy.Mode {
	case OutputQuiet:
		return nil
	case OutputJSON:
		envelope := struct {
			Schema string          `json:"schema"`
			OK     bool            `json:"ok"`
			Data   json.RawMessage `json:"data,omitempty"`
		}{Schema: "go-cli/v1", OK: true}
		if snapshot.hasData {
			envelope.Data = snapshot.dataJSON
		}
		return encodeAndWrite(writer, envelope)
	case OutputHuman:
		var builder strings.Builder
		for _, message := range snapshot.infos {
			builder.WriteString(sanitizeTerminal(message))
			builder.WriteByte('\n')
		}
		if snapshot.hasData {
			builder.WriteString(sanitizeTerminalMultiline(snapshot.dataHuman))
			builder.WriteByte('\n')
		}
		return writeAll(writer, []byte(builder.String()))
	default:
		return newInternalError("invalid output mode", nil)
	}
}

func renderFailure(stdout, stderr io.Writer, policy OutputPolicy, failure error) error {
	classified := classifiedError(failure)
	kind := ErrorKindCommand
	message := sanitizeTerminal(failure.Error())
	if classified != nil {
		kind = classified.Kind()
		message = sanitizeTerminal(classified.Error())
	}
	if policy.Mode == OutputJSON {
		envelope := struct {
			Schema string `json:"schema"`
			OK     bool   `json:"ok"`
			Error  struct {
				Kind    ErrorKind `json:"kind"`
				Message string    `json:"message"`
			} `json:"error"`
		}{Schema: "go-cli/v1", OK: false}
		envelope.Error.Kind = kind
		envelope.Error.Message = message

		return encodeAndWrite(stdout, envelope)
	}

	return writeAll(stderr, []byte("Error: "+message+"\n"))
}

func classifiedError(err error) *Error {
	var classified *Error
	if errors.As(err, &classified) {
		return classified
	}

	return nil
}

func encodeAndWrite(writer io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')

	return writeAll(writer, encoded)
}

func writeAll(writer io.Writer, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	written, err := writer.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}

	return nil
}

func sanitizeTerminal(value string) string {
	return strings.Map(func(character rune) rune {
		if isUnsafeTerminalRune(character) {
			return -1
		}

		return character
	}, value)
}

func sanitizeTerminalMultiline(value string) string {
	return strings.Map(func(character rune) rune {
		if character == '\n' || character == '\t' {
			return character
		}
		if isUnsafeTerminalRune(character) {
			return -1
		}

		return character
	}, value)
}

func isUnsafeTerminalRune(character rune) bool {
	return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
}
