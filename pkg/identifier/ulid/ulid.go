// Package ulid implements canonical ULIDs with explicitly owned monotonic
// generation state compatible with existing 26-character ULID storage.
package ulid

import (
	"bytes"
	cryptorand "crypto/rand"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	oklogulid "github.com/oklog/ulid/v2"
)

const (
	alphabet          = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	maximumUnixMillis = int64(281_474_976_710_655)
)

// ID is an immutable ULID. The validity bit distinguishes an unassigned Go
// zero value from the valid all-zero ULID.
type ID struct {
	value [16]byte
	valid bool
}

// Parse accepts only the canonical uppercase Crockford Base32 form.
func Parse(text string) (ID, error) {
	if len(text) != 26 {
		return ID{}, fmt.Errorf("%w: ULID length is %d", identifier.ErrInvalid, len(text))
	}
	if text[0] > '7' {
		return ID{}, fmt.Errorf("%w: ULID exceeds 128 bits", identifier.ErrInvalid)
	}
	for index := range len(text) {
		if !stringsContainsByte(alphabet, text[index]) {
			return ID{}, fmt.Errorf("%w: ULID contains non-canonical Base32", identifier.ErrInvalid)
		}
	}

	parsed, _ := oklogulid.ParseStrict(text)

	return ID{value: [16]byte(parsed), valid: true}, nil
}

func stringsContainsByte(text string, target byte) bool {
	for index := range len(text) {
		if text[index] == target {
			return true
		}
	}

	return false
}

// FromBytes copies a valid 128-bit ULID representation.
func FromBytes(value [16]byte) ID { return ID{value: value, valid: true} }

// Bytes returns a copy of the binary representation.
func (id ID) Bytes() [16]byte { return id.value }

// IsZero reports whether no ULID has been assigned.
func (id ID) IsZero() bool { return !id.valid }

// Compare returns -1, 0, or 1 according to ULID binary ordering.
func (id ID) Compare(other ID) int { return bytes.Compare(id.value[:], other.value[:]) }

// String returns canonical text, or an empty string for an unassigned value.
func (id ID) String() string {
	if !id.valid {
		return ""
	}

	return oklogulid.ULID(id.value).String()
}

// LogValue redacts the ULID from structured logs.
func (id ID) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// Inspect returns the millisecond timestamp exposed by the ULID.
func (id ID) Inspect() identifier.Inspection {
	inspection := identifier.Inspection{Family: identifier.FamilyULID, Sortable: true}
	if id.valid {
		milliseconds := id.value[0:6]
		value := int64(milliseconds[0])<<40 | int64(milliseconds[1])<<32 |
			int64(milliseconds[2])<<24 | int64(milliseconds[3])<<16 |
			int64(milliseconds[4])<<8 | int64(milliseconds[5])
		inspection.Timestamp = time.UnixMilli(value).UTC()
		inspection.HasTime = true
	}

	return inspection
}

// MarshalText implements encoding.TextMarshaler.
func (id ID) MarshalText() ([]byte, error) {
	if !id.valid {
		return nil, fmt.Errorf("%w: unassigned ULID", identifier.ErrInvalid)
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

// MarshalBinary returns a copied 16-byte representation.
func (id ID) MarshalBinary() ([]byte, error) {
	if !id.valid {
		return nil, fmt.Errorf("%w: unassigned ULID", identifier.ErrInvalid)
	}

	result := make([]byte, len(id.value))
	copy(result, id.value[:])

	return result, nil
}

// UnmarshalBinary validates a 16-byte representation.
func (id *ID) UnmarshalBinary(data []byte) error {
	if len(data) != 16 {
		return fmt.Errorf("%w: ULID binary length is %d", identifier.ErrInvalid, len(data))
	}

	var value [16]byte
	copy(value[:], data)
	*id = FromBytes(value)

	return nil
}

// MarshalJSON encodes canonical text or null for an unassigned value.
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
		return fmt.Errorf("decode ULID: %w", err)
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

// Scan implements sql.Scanner for text, raw bytes, and NULL.
func (id *ID) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		*id = ID{}

		return nil
	case string:
		return id.UnmarshalText([]byte(value))
	case []byte:
		if len(value) == 16 {
			return id.UnmarshalBinary(value)
		}

		return id.UnmarshalText(value)
	default:
		return fmt.Errorf("%w: cannot scan ULID from %T", identifier.ErrInvalid, src)
	}
}

// Generator owns its clock, entropy source, mutex, and monotonic state.
type Generator struct {
	mutex       sync.Mutex
	clock       identifier.Clock
	entropy     io.Reader
	initialized bool
	lastMillis  int64
	last        ID
}

// NewGenerator constructs a monotonic ULID generator. Nil inputs select a
// system clock and crypto/rand.Reader for this generator instance.
func NewGenerator(clock identifier.Clock, entropy io.Reader) *Generator {
	if clock == nil {
		clock = identifier.ClockFunc(time.Now)
	}
	if entropy == nil {
		entropy = cryptorand.Reader
	}

	return &Generator{clock: clock, entropy: entropy}
}

// New generates a ULID, incrementing entropy within a millisecond and
// reporting clock rollback or entropy exhaustion.
func (generator *Generator) New() (ID, error) {
	generator.mutex.Lock()
	defer generator.mutex.Unlock()

	milliseconds := generator.clock.Now().UnixMilli()
	if milliseconds < 0 || milliseconds > maximumUnixMillis {
		return ID{}, fmt.Errorf("%w: ULID timestamp is outside 48 bits", identifier.ErrInvalid)
	}
	if generator.initialized && milliseconds < generator.lastMillis {
		return ID{}, fmt.Errorf("%w: ULID moved from %d to %d", identifier.ErrClockRollback, generator.lastMillis, milliseconds)
	}
	if generator.initialized && milliseconds == generator.lastMillis {
		id := generator.last
		if !increment(&id.value) {
			return ID{}, fmt.Errorf("%w: ULID entropy field", identifier.ErrOverflow)
		}
		generator.last = id

		return id, nil
	}

	var value [16]byte
	timestamp := uint64(milliseconds)
	for position := 5; position >= 0; position-- {
		// #nosec G115 -- the low byte is selected intentionally before shifting.
		value[position] = byte(timestamp)
		timestamp >>= 8
	}
	if _, err := io.ReadFull(generator.entropy, value[6:]); err != nil {
		return ID{}, fmt.Errorf("%w: ULID: %w", identifier.ErrEntropy, err)
	}

	id := FromBytes(value)
	generator.initialized = true
	generator.lastMillis = milliseconds
	generator.last = id

	return id, nil
}

func increment(value *[16]byte) bool {
	for position := 15; position >= 6; position-- {
		value[position]++
		if value[position] != 0 {
			return true
		}
	}

	return false
}
