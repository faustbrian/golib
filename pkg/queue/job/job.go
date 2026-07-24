package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
)

// TaskFunc is the task function
type TaskFunc func(context.Context) error

var marshalJSON = json.Marshal

// DefaultMaxMessageBytes bounds encoded messages to one mebibyte.
const DefaultMaxMessageBytes = 1024 * 1024

// MaxRetryCount bounds attempts scheduled from one delivered message.
const MaxRetryCount int64 = 100

var (
	// ErrInvalidMessageLimit reports a non-positive decode limit.
	ErrInvalidMessageLimit = errors.New("message byte limit must be positive")
	// ErrMessageTooLarge reports an encoded message above its decode limit.
	ErrMessageTooLarge = errors.New("message exceeds byte limit")
	// ErrInvalidMessage reports unsafe or inconsistent execution metadata.
	ErrInvalidMessage = errors.New("invalid message execution metadata")
)

// Message describes a task and its metadata.
type Message struct {
	Task        TaskFunc `json:"-" msgpack:"-"`
	ack         func() error
	nack        func() error
	nackFailure func(error) error

	// Timeout is the duration the task can be processed by Handler.
	// zero if not specified
	// default is 60 time.Minute
	Timeout time.Duration `json:"timeout" msgpack:"timeout"`

	// Payload is the payload data of the task.
	Body []byte `json:"body" msgpack:"body"`

	// RetryCount set count of retry
	// default is 0, no retry.
	RetryCount int64 `json:"retry_count" msgpack:"retry_count"`

	// RetryDelay set delay between retry
	// default is 100ms
	RetryDelay time.Duration `json:"retry_delay" msgpack:"retry_delay"`

	// RetryFactor is the multiplying factor for each increment step.
	//
	// Defaults to 2.
	RetryFactor float64 `json:"retry_factor" msgpack:"retry_factor"`

	// Minimum value of the counter.
	//
	// Defaults to 100 milliseconds.
	RetryMin time.Duration `json:"retry_min" msgpack:"retry_min"`

	// Maximum value of the counter.
	//
	// Defaults to 10 seconds.
	RetryMax time.Duration `json:"retry_max" msgpack:"retry_max"`

	// Jitter eases contention by randomizing backoff steps
	Jitter bool `json:"jitter" msgpack:"jitter"`

	// Metadata is optional bounded identity retained for operational records.
	Metadata *Metadata `json:"metadata,omitempty" msgpack:"metadata,omitempty"`
}

// SetAcknowledgement attaches backend delivery settlement callbacks.
func (m *Message) SetAcknowledgement(ack func() error, nack func() error) {
	m.ack = ack
	m.nack = nack
	m.nackFailure = nil
}

// SetFailureAcknowledgement attaches settlement callbacks to a delivery whose
// backend needs the handler failure to choose retry or terminal behavior.
func (m *Message) SetFailureAcknowledgement(
	ack func() error,
	nack func(error) error,
) {
	m.ack = ack
	m.nack = nil
	m.nackFailure = nack
}

// AcknowledgementRequired reports whether backend settlement is attached.
func (m *Message) AcknowledgementRequired() bool {
	return m.ack != nil || m.nack != nil || m.nackFailure != nil
}

// Ack acknowledges successful processing when the backend requires it.
func (m *Message) Ack() error {
	if m.ack == nil {
		return nil
	}
	return m.ack()
}

// Nack rejects unsuccessful processing when the backend requires it.
func (m *Message) Nack() error {
	if m.nack != nil {
		return m.nack()
	}
	if m.nackFailure != nil {
		return m.nackFailure(nil)
	}

	return nil
}

// NackFailure rejects unsuccessful processing with its classified cause.
func (m *Message) NackFailure(err error) error {
	if m.nackFailure == nil {
		return m.Nack()
	}

	return m.nackFailure(err)
}

// Validate checks that message execution metadata is finite and bounded.
func (m *Message) Validate() error {
	if m.Metadata != nil {
		if err := m.Metadata.Validate(); err != nil {
			return err
		}
	}
	if m.Timeout <= 0 {
		return fmt.Errorf("%w: timeout must be positive", ErrInvalidMessage)
	}
	if m.RetryCount < 0 || m.RetryCount > MaxRetryCount {
		return fmt.Errorf(
			"%w: retry count must be between 0 and %d",
			ErrInvalidMessage,
			MaxRetryCount,
		)
	}
	if m.RetryDelay < 0 {
		return fmt.Errorf("%w: retry delay cannot be negative", ErrInvalidMessage)
	}
	if m.RetryCount == 0 || m.RetryDelay > 0 {
		return nil
	}
	if math.IsNaN(m.RetryFactor) || math.IsInf(m.RetryFactor, 0) || m.RetryFactor < 1 {
		return fmt.Errorf("%w: retry factor must be finite and at least one", ErrInvalidMessage)
	}
	if m.RetryMin <= 0 {
		return fmt.Errorf("%w: retry minimum must be positive", ErrInvalidMessage)
	}
	if m.RetryMax < m.RetryMin {
		return fmt.Errorf("%w: retry maximum must not be below minimum", ErrInvalidMessage)
	}

	return nil
}

// Payload returns the payload data of the Message.
// It returns the byte slice of the payload.
//
// Returns:
//   - A byte slice containing the payload data.
func (m *Message) Payload() []byte {
	return m.Body
}

// Bytes returns the byte slice of the Message struct.
// If the marshalling process encounters an error, the function will panic.
// It returns the marshalled byte slice.
//
// Returns:
//   - A byte slice containing the msgpack-encoded data.
func (m *Message) Bytes() []byte {
	b, err := marshalJSON(m)
	if err != nil {
		panic(err)
	}

	return b
}

// NewMessage create new message
func NewMessage(m core.QueuedMessage, opts ...AllowOption) Message {
	o := NewOptions(opts...)

	return Message{
		RetryCount:  o.retryCount,
		RetryDelay:  o.retryDelay,
		RetryFactor: o.retryFactor,
		RetryMin:    o.retryMin,
		RetryMax:    o.retryMax,
		Jitter:      o.jitter,
		Timeout:     o.timeout,
		Body:        m.Bytes(),
		Metadata:    o.metadata,
	}
}

func NewTask(task TaskFunc, opts ...AllowOption) Message {
	o := NewOptions(opts...)

	return Message{
		Timeout:     o.timeout,
		RetryCount:  o.retryCount,
		RetryDelay:  o.retryDelay,
		RetryFactor: o.retryFactor,
		RetryMin:    o.retryMin,
		RetryMax:    o.retryMax,
		Jitter:      o.jitter,
		Task:        task,
		Metadata:    o.metadata,
	}
}

// Encode takes a Message struct and marshals it into a byte slice using msgpack.
// If the marshalling process encounters an error, the function will panic.
// It returns the marshalled byte slice.
//
// Parameters:
//   - m: A pointer to the Message struct to be encoded.
//
// Returns:
//   - A byte slice containing the msgpack-encoded data.
func Encode(m *Message) []byte {
	b, err := marshalJSON(m)
	if err != nil {
		panic(err)
	}

	return b
}

// Decode takes a byte slice and unmarshals it into a Message struct using msgpack.
// If the unmarshalling process encounters an error, the function will panic.
// It returns a pointer to the unmarshalled Message.
//
// Parameters:
//   - b: A byte slice containing the msgpack-encoded data.
//
// Returns:
//   - A pointer to the decoded Message struct.
func Decode(b []byte) *Message {
	msg, err := DecodeE(b, DefaultMaxMessageBytes)
	if err != nil {
		panic(err)
	}

	return msg
}

// DecodeE decodes a message while enforcing a positive encoded-message limit.
func DecodeE(b []byte, maxBytes int) (*Message, error) {
	if maxBytes <= 0 {
		return nil, ErrInvalidMessageLimit
	}
	if len(b) > maxBytes {
		return nil, fmt.Errorf("%w: got %d bytes, limit %d", ErrMessageTooLarge, len(b), maxBytes)
	}

	var msg Message
	if err := json.Unmarshal(b, &msg); err != nil {
		return nil, fmt.Errorf("decode message: %w", err)
	}
	if err := msg.Validate(); err != nil {
		return nil, err
	}

	return &msg, nil
}
