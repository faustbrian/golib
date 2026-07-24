package wire

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorKind classifies a failure independently of its underlying cause.
type ErrorKind string

const (
	ErrorKindParse       ErrorKind = "parse"
	ErrorKindValidation  ErrorKind = "validation"
	ErrorKindUnsupported ErrorKind = "unsupported"
	ErrorKindEnvelope    ErrorKind = "envelope"
	ErrorKindFault       ErrorKind = "fault"
	ErrorKindWrite       ErrorKind = "write"
	ErrorKindSizeLimit   ErrorKind = "size-limit"
	ErrorKindTarget      ErrorKind = "target"
	ErrorKindEncode      ErrorKind = "encode"
)

var (
	ErrParse             = errors.New("parse failure")
	ErrValidation        = errors.New("validation failure")
	ErrUnsupportedFormat = errors.New("unsupported format")
	ErrEnvelope          = errors.New("envelope failure")
	ErrSOAPFault         = errors.New("SOAP fault")
	ErrWrite             = errors.New("write failure")
	ErrSizeLimit         = errors.New("size limit exceeded")
	ErrTarget            = errors.New("invalid target")
	ErrEncode            = errors.New("encode failure")
)

// Error describes a wire-format operation failure.
type Error struct {
	Kind   ErrorKind
	Format Format
	Op     string
	Err    error
}

// Error returns a stable, human-readable description of the failure.
func (e *Error) Error() string {
	parts := []string{"wire"}
	context := strings.TrimSpace(strings.Join([]string{string(e.Format), e.Op}, " "))
	if context != "" {
		parts = append(parts, context)
	}
	if kindErr := sentinelForKind(e.Kind); kindErr != nil {
		parts = append(parts, kindErr.Error())
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	if len(parts) == 1 {
		parts = append(parts, "failure")
	}

	return strings.Join(parts, ": ")
}

// Unwrap returns the underlying format-specific failure, if any.
func (e *Error) Unwrap() error {
	return e.Err
}

// Is reports whether target is the sentinel associated with the error kind or
// matches the underlying cause.
func (e *Error) Is(target error) bool {
	return target == sentinelForKind(e.Kind) || errors.Is(e.Err, target)
}

func sentinelForKind(kind ErrorKind) error {
	switch kind {
	case ErrorKindParse:
		return ErrParse
	case ErrorKindValidation:
		return ErrValidation
	case ErrorKindUnsupported:
		return ErrUnsupportedFormat
	case ErrorKindEnvelope:
		return ErrEnvelope
	case ErrorKindFault:
		return ErrSOAPFault
	case ErrorKindWrite:
		return ErrWrite
	case ErrorKindSizeLimit:
		return ErrSizeLimit
	case ErrorKindTarget:
		return ErrTarget
	case ErrorKindEncode:
		return ErrEncode
	default:
		return nil
	}
}

func newError(kind ErrorKind, format Format, op string, err error) error {
	return &Error{Kind: kind, Format: format, Op: op, Err: err}
}

func unsupportedError(op string, payload []byte) error {
	var cause error
	if len(payload) > 0 {
		cause = fmt.Errorf("unrecognized leading byte %q", payload[0])
	}

	return newError(ErrorKindUnsupported, "", op, cause)
}
