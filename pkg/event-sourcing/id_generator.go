package eventsourcing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
)

const randomMessageIDBytes = 16

// MessageIDGenerator supplies stable message identifiers without global
// replacement.
type MessageIDGenerator interface {
	NewMessageID(context.Context) (MessageID, error)
}

// MessageIDGeneratorFunc adapts a context-aware function to MessageIDGenerator.
type MessageIDGeneratorFunc func(context.Context) (MessageID, error)

// NewMessageID validates context, generator output, and error diagnostics.
func (function MessageIDGeneratorFunc) NewMessageID(
	ctx context.Context,
) (MessageID, error) {
	if ctx == nil || function == nil {
		return MessageID{}, ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return MessageID{}, err
	}

	id, err := function(ctx)
	if err != nil {
		return MessageID{}, &MessageIDGenerationError{Cause: err}
	}
	if id.IsZero() {
		return MessageID{}, invalid("message_id", "generator returned an unassigned identifier")
	}

	return id, nil
}

// RandomMessageIDGenerator reads 128 bits from an application-selected entropy
// source and encodes them as 32 lowercase hexadecimal characters.
//
// Context cancellation is checked before reading. A read function has no
// cancellation contract, so callers must supply a bounded function and own its
// concurrency safety.
type RandomMessageIDGenerator struct {
	read func([]byte) (int, error)
}

// NewRandomMessageIDGenerator validates an explicit entropy read function.
func NewRandomMessageIDGenerator(
	read func([]byte) (int, error),
) (*RandomMessageIDGenerator, error) {
	if read == nil {
		return nil, invalid("read", "must be assigned")
	}

	return &RandomMessageIDGenerator{read: read}, nil
}

// NewCryptoRandomMessageIDGenerator constructs the standard cryptographically
// secure generator.
func NewCryptoRandomMessageIDGenerator() *RandomMessageIDGenerator {
	return &RandomMessageIDGenerator{read: rand.Read}
}

// NewMessageID reads and encodes one random identifier.
func (generator *RandomMessageIDGenerator) NewMessageID(
	ctx context.Context,
) (MessageID, error) {
	if ctx == nil || generator == nil || generator.read == nil {
		return MessageID{}, ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return MessageID{}, err
	}

	var entropy [randomMessageIDBytes]byte
	if _, err := io.ReadFull(entropyReader(generator.read), entropy[:]); err != nil {
		return MessageID{}, &MessageIDGenerationError{Cause: err}
	}

	return MessageID{value: hex.EncodeToString(entropy[:])}, nil
}

// MessageIDGenerationError redacts an entropy source or application
// generator diagnostic while preserving its cause for inspection.
type MessageIDGenerationError struct {
	Cause error
}

// Error implements error without exposing the wrapped diagnostic.
func (*MessageIDGenerationError) Error() string {
	return "message ID generation failed"
}

// Unwrap preserves the underlying cause for errors.Is and errors.As.
func (err *MessageIDGenerationError) Unwrap() error {
	return err.Cause
}

type entropyReader func([]byte) (int, error)

func (read entropyReader) Read(buffer []byte) (int, error) {
	return read(buffer)
}

var (
	_ MessageIDGenerator = MessageIDGeneratorFunc(nil)
	_ MessageIDGenerator = (*RandomMessageIDGenerator)(nil)
	_ error              = (*MessageIDGenerationError)(nil)
	_ interface {
		Unwrap() error
	} = (*MessageIDGenerationError)(nil)
)
