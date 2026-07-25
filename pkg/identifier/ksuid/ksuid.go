// Package ksuid implements canonical, interoperable KSUIDs with explicitly
// owned clock, entropy, and optional same-second monotonic state.
package ksuid

import (
	"bytes"
	cryptorand "crypto/rand"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"sync"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	segmentksuid "github.com/segmentio/ksuid"
)

const epoch = int64(1_400_000_000)

// ID is an immutable KSUID. A validity bit distinguishes an unassigned value
// from the valid all-zero KSUID.
type ID struct {
	value [20]byte
	valid bool
}

// Parse accepts exactly the canonical 27-character Base62 representation.
func Parse(text string) (ID, error) {
	if len(text) != 27 {
		return ID{}, fmt.Errorf("%w: KSUID length is %d", identifier.ErrInvalid, len(text))
	}

	parsed, err := segmentksuid.Parse(text)
	if err != nil {
		return ID{}, fmt.Errorf("%w: decode KSUID: %w", identifier.ErrInvalid, err)
	}
	if parsed.String() != text {
		return ID{}, fmt.Errorf("%w: non-canonical KSUID", identifier.ErrInvalid)
	}

	return ID{value: [20]byte(parsed), valid: true}, nil
}

// FromBytes copies a 20-byte KSUID representation.
func FromBytes(value [20]byte) ID { return ID{value: value, valid: true} }

// Bytes returns a copy of the KSUID representation.
func (id ID) Bytes() [20]byte { return id.value }

// IsZero reports whether no KSUID has been assigned.
func (id ID) IsZero() bool { return !id.valid }

// Compare returns -1, 0, or 1 according to binary ordering.
func (id ID) Compare(other ID) int { return bytes.Compare(id.value[:], other.value[:]) }

// String returns canonical Base62 text, or empty text when unassigned.
func (id ID) String() string {
	if !id.valid {
		return ""
	}

	return segmentksuid.KSUID(id.value).String()
}

// LogValue redacts the KSUID from structured logs.
func (id ID) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// Inspect returns the second-resolution timestamp exposed by a KSUID.
func (id ID) Inspect() identifier.Inspection {
	inspection := identifier.Inspection{Family: identifier.FamilyKSUID, Sortable: id.valid}
	if id.valid {
		seconds := int64(binary.BigEndian.Uint32(id.value[0:4])) + epoch
		inspection.Timestamp = time.Unix(seconds, 0).UTC()
		inspection.HasTime = true
	}

	return inspection
}

// MarshalText implements encoding.TextMarshaler.
func (id ID) MarshalText() ([]byte, error) {
	if !id.valid {
		return nil, fmt.Errorf("%w: unassigned KSUID", identifier.ErrInvalid)
	}

	return []byte(id.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (id *ID) UnmarshalText(text []byte) error {
	parsed, err := Parse(string(text))
	if err != nil {
		return err
	}
	*id = parsed

	return nil
}

// MarshalBinary returns a copied 20-byte representation.
func (id ID) MarshalBinary() ([]byte, error) {
	if !id.valid {
		return nil, fmt.Errorf("%w: unassigned KSUID", identifier.ErrInvalid)
	}

	result := make([]byte, len(id.value))
	copy(result, id.value[:])

	return result, nil
}

// UnmarshalBinary validates a 20-byte representation.
func (id *ID) UnmarshalBinary(data []byte) error {
	if len(data) != 20 {
		return fmt.Errorf("%w: KSUID binary length is %d", identifier.ErrInvalid, len(data))
	}

	var value [20]byte
	copy(value[:], data)
	*id = FromBytes(value)

	return nil
}

// MarshalJSON encodes canonical text or null when unassigned.
func (id ID) MarshalJSON() ([]byte, error) {
	if !id.valid {
		return []byte("null"), nil
	}

	return json.Marshal(id.String())
}

// UnmarshalJSON decodes canonical text and accepts null as unassigned.
func (id *ID) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*id = ID{}

		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("decode KSUID: %w", err)
	}

	return id.UnmarshalText([]byte(text))
}

// Value implements driver.Valuer using canonical text.
func (id ID) Value() (driver.Value, error) {
	if !id.valid {
		return nil, nil
	}

	return id.String(), nil
}

// Scan implements sql.Scanner for text, binary bytes, and NULL.
func (id *ID) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		*id = ID{}

		return nil
	case string:
		return id.UnmarshalText([]byte(value))
	case []byte:
		if len(value) == 20 {
			return id.UnmarshalBinary(value)
		}

		return id.UnmarshalText(value)
	default:
		return fmt.Errorf("%w: cannot scan KSUID from %T", identifier.ErrInvalid, src)
	}
}

// Generator owns a clock, entropy source, mutex, and monotonic state. Within a
// second, it increments the initial random payload; this leaks local issuance
// order and differs from independent-random KSUID generation while preserving
// wire interoperability.
type Generator struct {
	mutex       sync.Mutex
	clock       identifier.Clock
	entropy     io.Reader
	initialized bool
	lastSecond  int64
	last        ID
}

// NewGenerator creates a monotonic KSUID generator. Nil inputs select a system
// clock and crypto/rand.Reader for this generator instance.
func NewGenerator(clock identifier.Clock, entropy io.Reader) *Generator {
	if clock == nil {
		clock = identifier.ClockFunc(time.Now)
	}
	if entropy == nil {
		entropy = cryptorand.Reader
	}

	return &Generator{clock: clock, entropy: entropy}
}

// New generates a KSUID and reports rollback or payload exhaustion.
func (generator *Generator) New() (ID, error) {
	generator.mutex.Lock()
	defer generator.mutex.Unlock()

	seconds := generator.clock.Now().Unix()
	if seconds < epoch || seconds > epoch+int64(math.MaxUint32) {
		return ID{}, fmt.Errorf("%w: KSUID timestamp is outside its epoch", identifier.ErrInvalid)
	}
	if generator.initialized && seconds < generator.lastSecond {
		return ID{}, fmt.Errorf("%w: KSUID moved from %d to %d", identifier.ErrClockRollback, generator.lastSecond, seconds)
	}
	if generator.initialized && seconds == generator.lastSecond {
		id := generator.last
		if !increment(&id.value) {
			return ID{}, fmt.Errorf("%w: KSUID payload", identifier.ErrOverflow)
		}
		generator.last = id

		return id, nil
	}

	var value [20]byte
	binary.BigEndian.PutUint32(value[0:4], uint32(seconds-epoch))
	if _, err := io.ReadFull(generator.entropy, value[4:]); err != nil {
		return ID{}, fmt.Errorf("%w: KSUID: %w", identifier.ErrEntropy, err)
	}

	id := FromBytes(value)
	generator.initialized = true
	generator.lastSecond = seconds
	generator.last = id

	return id, nil
}

func increment(value *[20]byte) bool {
	for position := 19; position >= 4; position-- {
		value[position]++
		if value[position] != 0 {
			return true
		}
	}

	return false
}
