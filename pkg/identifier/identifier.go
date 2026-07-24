// Package identifier defines contracts shared by concrete identifier families.
package identifier

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

var (
	// ErrInvalid reports malformed, non-canonical, or disallowed input.
	ErrInvalid = errors.New("invalid identifier")
	// ErrEntropy reports failure while acquiring random bytes.
	ErrEntropy = errors.New("identifier entropy failure")
	// ErrClockRollback reports a clock moving behind supported generator state.
	ErrClockRollback = errors.New("identifier clock rollback")
	// ErrOverflow reports exhausted monotonic or sequence space.
	ErrOverflow = errors.New("identifier sequence overflow")
	// ErrUnsupported reports a valid but unsupported identifier feature.
	ErrUnsupported = errors.New("unsupported identifier")
)

// Clock is an explicitly owned time source.
type Clock interface {
	Now() time.Time
}

// ClockFunc adapts a function to Clock.
type ClockFunc func() time.Time

// Now calls the underlying function.
func (clock ClockFunc) Now() time.Time { return clock() }

// Generator produces identifiers without relying on package-global mutable state.
type Generator[T any] interface {
	New() (T, error)
}

// Validator validates the canonical text used by a typed ID. Implementations
// must be usable as their zero value so decoding never needs registration.
type Validator interface {
	Validate(string) error
}

// ID is a strongly typed canonical identifier. Different Tag arguments are
// distinct Go types and cannot be mixed without an explicit conversion.
type ID[Tag Validator] struct {
	text string
}

// Parse validates canonical text for Tag.
func Parse[Tag Validator](text string) (ID[Tag], error) {
	var tag Tag

	if text == "" {
		return ID[Tag]{}, fmt.Errorf("%w: empty typed ID", ErrInvalid)
	}

	if err := tag.Validate(text); err != nil {
		return ID[Tag]{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	return ID[Tag]{text: text}, nil
}

// String returns canonical text.
func (id ID[Tag]) String() string { return id.text }

// LogValue redacts the identifier from structured logs. Callers must opt in
// explicitly with String when an identifier has been approved for logging.
func (id ID[Tag]) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// IsZero reports whether no identifier has been assigned.
func (id ID[Tag]) IsZero() bool { return id.text == "" }

// Compare returns -1, 0, or 1 according to canonical lexical ordering.
func (id ID[Tag]) Compare(other ID[Tag]) int {
	if id.text < other.text {
		return -1
	}
	if id.text > other.text {
		return 1
	}

	return 0
}

// MarshalText implements encoding.TextMarshaler.
func (id ID[Tag]) MarshalText() ([]byte, error) { return []byte(id.text), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (id *ID[Tag]) UnmarshalText(text []byte) error {
	parsed, err := Parse[Tag](string(text))
	if err != nil {
		return err
	}

	*id = parsed

	return nil
}

// MarshalBinary uses canonical text so typed values remain family-neutral.
func (id ID[Tag]) MarshalBinary() ([]byte, error) { return id.MarshalText() }

// UnmarshalBinary validates canonical text.
func (id *ID[Tag]) UnmarshalBinary(data []byte) error { return id.UnmarshalText(data) }

// MarshalJSON encodes the canonical identifier as a JSON string.
func (id ID[Tag]) MarshalJSON() ([]byte, error) { return json.Marshal(id.text) }

// UnmarshalJSON decodes and validates a canonical JSON string.
func (id *ID[Tag]) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("decode typed identifier: %w", err)
	}

	return id.UnmarshalText([]byte(text))
}

// Value implements driver.Valuer. An unassigned ID is stored as SQL NULL.
func (id ID[Tag]) Value() (driver.Value, error) {
	if id.IsZero() {
		return nil, nil
	}

	return id.text, nil
}

// Scan implements sql.Scanner for strings, byte slices, and NULL.
func (id *ID[Tag]) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		*id = ID[Tag]{}

		return nil
	case string:
		return id.UnmarshalText([]byte(value))
	case []byte:
		return id.UnmarshalText(value)
	default:
		return fmt.Errorf("%w: cannot scan typed ID from %T", ErrInvalid, src)
	}
}

// Family identifies an encoding without implying equivalent guarantees.
type Family string

const (
	// FamilyUUID identifies RFC UUID values.
	FamilyUUID Family = "uuid"
	// FamilyULID identifies canonical ULID values.
	FamilyULID Family = "ulid"
	// FamilyTypeID identifies TypeID values.
	FamilyTypeID Family = "typeid"
	// FamilyKSUID identifies Segment-compatible KSUID values.
	FamilyKSUID Family = "ksuid"
	// FamilyNanoID identifies configured NanoID values.
	FamilyNanoID Family = "nanoid"
)

// Inspection exposes only properties actually defined by an identifier family.
type Inspection struct {
	Family    Family
	Version   int
	Timestamp time.Time
	HasTime   bool
	Sortable  bool
}
