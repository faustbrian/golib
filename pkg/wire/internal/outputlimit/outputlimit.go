// Package outputlimit provides bounded in-memory encoding destinations.
package outputlimit

import (
	"bytes"
	"errors"
)

// ErrLimit identifies encoded output that exceeds its configured byte limit.
var ErrLimit = errors.New("encoded output exceeds size limit")

// Buffer is an io.Writer that retains at most its configured byte limit.
type Buffer struct {
	buffer bytes.Buffer
	max    int64
}

// New returns a buffer using fallback when configured is zero.
func New(configured, fallback int64) (*Buffer, error) {
	if configured < 0 {
		return nil, errors.New("max bytes must not be negative")
	}
	if configured == 0 {
		configured = fallback
	}
	return &Buffer{max: configured}, nil
}

// Write retains p when it fits and otherwise returns ErrLimit without
// retaining bytes beyond the configured maximum.
func (b *Buffer) Write(payload []byte) (int, error) {
	remaining := b.max - int64(b.buffer.Len())
	if int64(len(payload)) <= remaining {
		return b.buffer.Write(payload)
	}
	if remaining <= 0 {
		return 0, ErrLimit
	}
	written, _ := b.buffer.Write(payload[:int(remaining)])
	return written, ErrLimit
}

// Bytes returns the retained encoded bytes.
func (b *Buffer) Bytes() []byte {
	return b.buffer.Bytes()
}

// Len returns the number of retained bytes.
func (b *Buffer) Len() int {
	return b.buffer.Len()
}
