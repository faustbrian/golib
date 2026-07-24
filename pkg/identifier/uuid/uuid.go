// Package uuid implements canonical RFC 9562 UUID parsing and v4/v7
// generation with explicitly owned entropy, clocks, and monotonic state.
package uuid

import (
	"bytes"
	cryptorand "crypto/rand"
	"database/sql/driver"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	gregorianToUnixSeconds = int64(12_219_292_800)
	maximumUnixMillis      = int64(281_474_976_710_655)
)

// ID is an immutable UUID value. Bytes returns a copy rather than an alias.
type ID [16]byte

// Parse accepts only canonical lowercase RFC 9562 text and versions 1 through
// 8 using the RFC variant.
func Parse(text string) (ID, error) {
	if len(text) != 36 || text[8] != '-' || text[13] != '-' || text[18] != '-' || text[23] != '-' {
		return ID{}, fmt.Errorf("%w: UUID must use canonical 8-4-4-4-12 form", identifier.ErrInvalid)
	}

	var compact [32]byte
	index := 0
	for position := range len(text) {
		if position == 8 || position == 13 || position == 18 || position == 23 {
			continue
		}
		character := text[position]
		if !isLowerHex(character) {
			return ID{}, fmt.Errorf("%w: UUID contains non-canonical hexadecimal", identifier.ErrInvalid)
		}
		compact[index] = character
		index++
	}

	var id ID
	_, _ = hex.Decode(id[:], compact[:])

	return validate(id)
}

// FromBytes validates a copied binary UUID value.
func FromBytes(value [16]byte) (ID, error) { return validate(ID(value)) }

func validate(id ID) (ID, error) {
	version := id.Version()
	if version < 1 || version > 8 {
		return ID{}, fmt.Errorf("%w: UUID version %d", identifier.ErrInvalid, version)
	}
	if id[8]&0xc0 != 0x80 {
		return ID{}, fmt.Errorf("%w: UUID is not the RFC variant", identifier.ErrInvalid)
	}

	return id, nil
}

func isLowerHex(character byte) bool {
	return character >= '0' && character <= '9' || character >= 'a' && character <= 'f'
}

// Bytes returns a copy of the 16-byte UUID.
func (id ID) Bytes() [16]byte { return [16]byte(id) }

// IsZero reports whether no UUID has been assigned.
func (id ID) IsZero() bool { return id == ID{} }

// Version returns the RFC version nibble.
func (id ID) Version() int { return int(id[6] >> 4) }

// Compare returns -1, 0, or 1 according to unsigned binary ordering.
func (id ID) Compare(other ID) int { return bytes.Compare(id[:], other[:]) }

// String returns canonical lowercase text.
func (id ID) String() string {
	var text [36]byte
	hex.Encode(text[0:8], id[0:4])
	text[8] = '-'
	hex.Encode(text[9:13], id[4:6])
	text[13] = '-'
	hex.Encode(text[14:18], id[6:8])
	text[18] = '-'
	hex.Encode(text[19:23], id[8:10])
	text[23] = '-'
	hex.Encode(text[24:36], id[10:16])

	return string(text[:])
}

// LogValue redacts the UUID from structured logs.
func (id ID) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// Inspect reports timestamps only for UUID versions that define them.
func (id ID) Inspect() identifier.Inspection {
	inspection := identifier.Inspection{Family: identifier.FamilyUUID, Version: id.Version()}

	switch id.Version() {
	case 1:
		low := uint64(binary.BigEndian.Uint32(id[0:4]))
		middle := uint64(binary.BigEndian.Uint16(id[4:6])) << 32
		high := uint64(binary.BigEndian.Uint16(id[6:8])&0x0fff) << 48
		inspection.Timestamp = gregorianTime(high | middle | low)
		inspection.HasTime = true
	case 6:
		high := uint64(binary.BigEndian.Uint32(id[0:4])) << 28
		middle := uint64(binary.BigEndian.Uint16(id[4:6])) << 12
		low := uint64(binary.BigEndian.Uint16(id[6:8]) & 0x0fff)
		inspection.Timestamp = gregorianTime(high | middle | low)
		inspection.HasTime = true
		inspection.Sortable = true
	case 7:
		milliseconds := int64(id[0])<<40 | int64(id[1])<<32 | int64(id[2])<<24 |
			int64(id[3])<<16 | int64(id[4])<<8 | int64(id[5])
		inspection.Timestamp = time.UnixMilli(milliseconds).UTC()
		inspection.HasTime = true
		inspection.Sortable = true
	}

	return inspection
}

func gregorianTime(ticks uint64) time.Time {
	// #nosec G115 -- an RFC UUID timestamp is 60 bits, so seconds fit int64.
	seconds := int64(ticks/10_000_000) - gregorianToUnixSeconds
	nanoseconds := int64(ticks%10_000_000) * 100

	return time.Unix(seconds, nanoseconds).UTC()
}

// MarshalText implements encoding.TextMarshaler.
func (id ID) MarshalText() ([]byte, error) {
	if id.IsZero() {
		return nil, fmt.Errorf("%w: zero UUID", identifier.ErrInvalid)
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
	if id.IsZero() {
		return nil, fmt.Errorf("%w: zero UUID", identifier.ErrInvalid)
	}

	result := make([]byte, len(id))
	copy(result, id[:])

	return result, nil
}

// UnmarshalBinary validates a 16-byte representation.
func (id *ID) UnmarshalBinary(data []byte) error {
	if len(data) != 16 {
		return fmt.Errorf("%w: UUID binary length is %d", identifier.ErrInvalid, len(data))
	}

	var value [16]byte
	copy(value[:], data)
	parsed, err := FromBytes(value)
	if err != nil {
		return err
	}
	*id = parsed

	return nil
}

// MarshalJSON encodes a UUID as a JSON string or an unassigned value as null.
func (id ID) MarshalJSON() ([]byte, error) {
	if id.IsZero() {
		return []byte("null"), nil
	}

	return json.Marshal(id.String())
}

// UnmarshalJSON decodes canonical UUID text and accepts null as unassigned.
func (id *ID) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*id = ID{}

		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("decode UUID: %w", err)
	}

	return id.UnmarshalText([]byte(text))
}

// Value implements driver.Valuer using PostgreSQL-compatible canonical text.
func (id ID) Value() (driver.Value, error) {
	if id.IsZero() {
		return nil, nil
	}

	return id.String(), nil
}

// Scan implements sql.Scanner for canonical text, raw bytes, and NULL.
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
		return fmt.Errorf("%w: cannot scan UUID from %T", identifier.ErrInvalid, src)
	}
}

// ScanUUID implements pgtype.UUIDScanner.
func (id *ID) ScanUUID(value pgtype.UUID) error {
	if !value.Valid {
		*id = ID{}

		return nil
	}

	parsed, err := FromBytes(value.Bytes)
	if err != nil {
		return err
	}
	*id = parsed

	return nil
}

// UUIDValue implements pgtype.UUIDValuer.
func (id ID) UUIDValue() (pgtype.UUID, error) {
	if id.IsZero() {
		return pgtype.UUID{}, nil
	}

	return pgtype.UUID{Bytes: id.Bytes(), Valid: true}, nil
}

// V4Generator owns its entropy source and serializes access to it.
type V4Generator struct {
	mutex   sync.Mutex
	entropy io.Reader
}

// NewV4Generator constructs a random UUID generator. A nil reader selects
// crypto/rand.Reader for this generator instance.
func NewV4Generator(entropy io.Reader) *V4Generator {
	if entropy == nil {
		entropy = cryptorand.Reader
	}

	return &V4Generator{entropy: entropy}
}

// New generates a UUIDv4.
func (generator *V4Generator) New() (ID, error) {
	generator.mutex.Lock()
	defer generator.mutex.Unlock()

	var id ID
	if _, err := io.ReadFull(generator.entropy, id[:]); err != nil {
		return ID{}, fmt.Errorf("%w: UUIDv4: %w", identifier.ErrEntropy, err)
	}
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80

	return id, nil
}

// V7Generator owns a clock, entropy source, mutex, and monotonic state.
type V7Generator struct {
	mutex       sync.Mutex
	clock       identifier.Clock
	entropy     io.Reader
	initialized bool
	lastMillis  int64
	last        ID
}

// NewV7Generator constructs a monotonic UUIDv7 generator. Nil inputs select a
// system clock and crypto/rand.Reader for this generator instance.
func NewV7Generator(clock identifier.Clock, entropy io.Reader) *V7Generator {
	if clock == nil {
		clock = identifier.ClockFunc(time.Now)
	}
	if entropy == nil {
		entropy = cryptorand.Reader
	}

	return &V7Generator{clock: clock, entropy: entropy}
}

// New generates a UUIDv7. Same-millisecond calls increment the random field;
// rollback and exhaustion are reported rather than hidden.
func (generator *V7Generator) New() (ID, error) {
	generator.mutex.Lock()
	defer generator.mutex.Unlock()

	milliseconds := generator.clock.Now().UnixMilli()
	if milliseconds < 0 || milliseconds > maximumUnixMillis {
		return ID{}, fmt.Errorf("%w: UUIDv7 timestamp is outside 48 bits", identifier.ErrInvalid)
	}
	if generator.initialized && milliseconds < generator.lastMillis {
		return ID{}, fmt.Errorf("%w: UUIDv7 moved from %d to %d", identifier.ErrClockRollback, generator.lastMillis, milliseconds)
	}
	if generator.initialized && milliseconds == generator.lastMillis {
		id := generator.last
		if !incrementV7(&id) {
			return ID{}, fmt.Errorf("%w: UUIDv7 random field", identifier.ErrOverflow)
		}
		generator.last = id

		return id, nil
	}

	var random [10]byte
	if _, err := io.ReadFull(generator.entropy, random[:]); err != nil {
		return ID{}, fmt.Errorf("%w: UUIDv7: %w", identifier.ErrEntropy, err)
	}

	var id ID
	value := uint64(milliseconds)
	for position := 5; position >= 0; position-- {
		// #nosec G115 -- the low byte is selected intentionally before shifting.
		id[position] = byte(value)
		value >>= 8
	}
	id[6] = 0x70 | random[0]&0x0f
	id[7] = random[1]
	id[8] = 0x80 | random[2]&0x3f
	copy(id[9:], random[3:])

	generator.initialized = true
	generator.lastMillis = milliseconds
	generator.last = id

	return id, nil
}

func incrementV7(id *ID) bool {
	for position := 15; position >= 9; position-- {
		id[position]++
		if id[position] != 0 {
			return true
		}
	}

	lowVariant := (id[8] & 0x3f) + 1
	id[8] = 0x80 | lowVariant&0x3f
	if lowVariant != 0x40 {
		return true
	}

	id[7]++
	if id[7] != 0 {
		return true
	}

	lowVersion := (id[6] & 0x0f) + 1
	id[6] = 0x70 | lowVersion&0x0f

	return lowVersion != 0x10
}
