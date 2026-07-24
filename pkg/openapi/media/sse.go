// Package media maps media-type representations into the JSON data model.
package media

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

var (
	// ErrInvalidServerSentEvents reports invalid parser inputs or limits.
	ErrInvalidServerSentEvents = errors.New("invalid server-sent event stream")
	// ErrServerSentEventLimit reports bounded parser resource exhaustion.
	ErrServerSentEventLimit = errors.New("server-sent event limit exceeded")
)

// ServerSentEventLimits bounds independent event-stream growth axes.
type ServerSentEventLimits struct {
	MaxBytes     int64
	MaxLineBytes int
	MaxDataBytes int
	MaxEvents    int
}

// DefaultServerSentEventLimits returns conservative untrusted-input bounds.
func DefaultServerSentEventLimits() ServerSentEventLimits {
	return ServerSentEventLimits{
		MaxBytes:     16 * 1024 * 1024,
		MaxLineBytes: 1024 * 1024,
		MaxDataBytes: 1024 * 1024,
		MaxEvents:    100_000,
	}
}

// ServerSentEventError is a safe, stable parser diagnostic.
type ServerSentEventError struct {
	Code   string
	Offset int64
	Kind   error
	Cause  error
}

// Error returns bounded metadata without including stream contents.
func (parseError *ServerSentEventError) Error() string {
	if parseError == nil {
		return "parse server-sent events: <nil>"
	}
	return fmt.Sprintf(
		"parse server-sent events: %s at byte %d",
		parseError.Code,
		parseError.Offset,
	)
}

// Unwrap exposes the stable classification and underlying cause.
func (parseError *ServerSentEventError) Unwrap() []error {
	if parseError == nil {
		return nil
	}
	var causes []error
	if parseError.Kind != nil {
		causes = append(causes, parseError.Kind)
	}
	if parseError.Cause != nil {
		causes = append(causes, parseError.Cause)
	}
	return causes
}

type eventStreamParser struct {
	reader *bufio.Reader
	limits ServerSentEventLimits
	offset int64
}

type eventState struct {
	data      []string
	dataBytes int
	event     string
	id        string
	haveID    bool
	retry     string
}

// ParseServerSentEvents maps a bounded WHATWG text/event-stream into ordered
// OpenAPI 3.2 JSON values. It performs no I/O beyond the supplied reader.
func ParseServerSentEvents(
	ctx context.Context,
	reader io.Reader,
	limits ServerSentEventLimits,
) ([]jsonvalue.Value, error) {
	if ctx == nil {
		return nil, &ServerSentEventError{
			Code: "invalid_context", Kind: ErrInvalidServerSentEvents,
		}
	}
	if reader == nil {
		return nil, &ServerSentEventError{
			Code: "nil_reader", Kind: ErrInvalidServerSentEvents,
		}
	}
	if limits.MaxBytes < 1 || limits.MaxLineBytes < 1 ||
		limits.MaxDataBytes < 1 || limits.MaxEvents < 1 {
		return nil, &ServerSentEventError{
			Code: "invalid_limits", Kind: ErrInvalidServerSentEvents,
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parser := eventStreamParser{
		reader: bufio.NewReader(reader),
		limits: limits,
	}
	state := eventState{}
	items := make([]jsonvalue.Value, 0)
	firstLine := true
	for {
		line, eof, err := parser.line(ctx)
		switch err {
		case nil:
		default:
			return nil, err
		}
		if eof {
			return items, nil
		}
		if firstLine {
			line = strings.TrimPrefix(line, "\ufeff")
			firstLine = false
		}
		if line == "" {
			item, dispatch := state.dispatch()
			if !dispatch {
				continue
			}
			if len(items) >= limits.MaxEvents {
				return nil, parser.limit("event_limit")
			}
			items = append(items, item)
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		if err := state.field(field, value, limits.MaxDataBytes); err != nil {
			return nil, parser.limit("data_limit")
		}
	}
}

func (parser *eventStreamParser) line(ctx context.Context) (string, bool, error) {
	line := make([]byte, 0)
	for {
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		default:
		}
		character, err := parser.reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", true, nil
			}
			return "", false, &ServerSentEventError{
				Code: "read_error", Offset: parser.offset,
				Kind: ErrInvalidServerSentEvents, Cause: err,
			}
		}
		parser.offset++
		if parser.offset > parser.limits.MaxBytes {
			return "", false, parser.limit("byte_limit")
		}
		switch character {
		case '\n':
			return strings.ToValidUTF8(string(line), "\ufffd"), false, nil
		case '\r':
			if next, peekErr := parser.reader.Peek(1); peekErr == nil && next[0] == '\n' {
				_, _ = parser.reader.ReadByte()
				parser.offset++
				if parser.offset > parser.limits.MaxBytes {
					return "", false, parser.limit("byte_limit")
				}
			}
			return strings.ToValidUTF8(string(line), "\ufffd"), false, nil
		default:
			line = append(line, character)
			if len(line) > parser.limits.MaxLineBytes {
				return "", false, parser.limit("line_limit")
			}
		}
	}
}

func (parser *eventStreamParser) limit(code string) error {
	return &ServerSentEventError{
		Code: code, Offset: parser.offset, Kind: ErrServerSentEventLimit,
	}
}

func (state *eventState) field(name string, value string, maxDataBytes int) error {
	switch name {
	case "event":
		state.event = value
	case "data":
		state.dataBytes += len(value) + 1
		if state.dataBytes > maxDataBytes {
			return ErrServerSentEventLimit
		}
		state.data = append(state.data, value)
	case "id":
		if !strings.ContainsRune(value, '\x00') {
			state.id = value
			state.haveID = true
		}
	case "retry":
		if asciiDigits(value) {
			state.retry = canonicalDigits(value)
		}
	}
	return nil
}

func (state *eventState) dispatch() (jsonvalue.Value, bool) {
	if len(state.data) == 0 {
		state.event = ""
		state.retry = ""
		return jsonvalue.Value{}, false
	}
	members := make([]jsonvalue.Member, 0, 4)
	data, _ := jsonvalue.String(strings.Join(state.data, "\n"))
	members = append(members, jsonvalue.Member{Name: "data", Value: data})
	if state.event != "" {
		event, _ := jsonvalue.String(state.event)
		members = append(members, jsonvalue.Member{Name: "event", Value: event})
	}
	if state.haveID {
		identifier, _ := jsonvalue.String(state.id)
		members = append(members, jsonvalue.Member{Name: "id", Value: identifier})
	}
	if state.retry != "" {
		retry, _ := jsonvalue.Number(state.retry)
		members = append(members, jsonvalue.Member{Name: "retry", Value: retry})
	}
	state.data = nil
	state.dataBytes = 0
	state.event = ""
	state.retry = ""
	item, _ := jsonvalue.Object(members)
	return item, true
}

func asciiDigits(value string) bool {
	if value == "" {
		return false
	}
	for index := range len(value) {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func canonicalDigits(value string) string {
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return "0"
	}
	return value
}
