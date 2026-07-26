// Package typeid implements the TypeID specification version 0.3.0 with
// canonical prefixes, strict Base32, and UUIDv7-backed generation.
package typeid

import (
	"bytes"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
	oklogulid "github.com/oklog/ulid/v2"
)

const (
	suffixAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"
	zeroSuffix     = "00000000000000000000000000"
)

// ID is an immutable TypeID. Its Go zero value is the official unprefixed
// all-zero TypeID; the validity bit preserves every other prefix/value pair.
type ID struct {
	value  [16]byte
	prefix string
	valid  bool
}

// ValidatePrefix enforces the TypeID 0.3.0 prefix grammar.
func ValidatePrefix(prefix string) error {
	if len(prefix) > 63 {
		return fmt.Errorf("%w: TypeID prefix exceeds 63 characters", identifier.ErrInvalid)
	}
	if prefix == "" {
		return nil
	}
	if prefix[0] < 'a' || prefix[0] > 'z' || prefix[len(prefix)-1] < 'a' || prefix[len(prefix)-1] > 'z' {
		return fmt.Errorf("%w: TypeID prefix must start and end with a letter", identifier.ErrInvalid)
	}
	for index := range len(prefix) {
		character := prefix[index]
		if character != '_' && (character < 'a' || character > 'z') {
			return fmt.Errorf("%w: TypeID prefix contains invalid ASCII", identifier.ErrInvalid)
		}
	}

	return nil
}

// Parse accepts only canonical TypeID 0.3.0 text. Parsing permits every
// 128-bit suffix required by the official vectors; generation remains UUIDv7.
func Parse(text string) (ID, error) {
	if len(text) < 26 || len(text) > 90 {
		return ID{}, fmt.Errorf("%w: TypeID length is %d", identifier.ErrInvalid, len(text))
	}

	prefix := ""
	suffix := text
	if len(text) > 26 {
		separator := len(text) - 27
		if text[separator] != '_' {
			return ID{}, fmt.Errorf("%w: TypeID separator is missing", identifier.ErrInvalid)
		}
		prefix = text[:separator]
		if prefix == "" {
			return ID{}, fmt.Errorf("%w: empty TypeID prefix must omit separator", identifier.ErrInvalid)
		}
		suffix = text[separator+1:]
	}
	if err := ValidatePrefix(prefix); err != nil {
		return ID{}, err
	}
	if suffix[0] > '7' {
		return ID{}, fmt.Errorf("%w: TypeID suffix exceeds 128 bits", identifier.ErrInvalid)
	}
	for index := range len(suffix) {
		if !strings.ContainsRune(suffixAlphabet, rune(suffix[index])) {
			return ID{}, fmt.Errorf("%w: TypeID suffix is not canonical Base32", identifier.ErrInvalid)
		}
	}

	decoded, _ := oklogulid.ParseStrict(strings.ToUpper(suffix))

	return FromBytes(prefix, [16]byte(decoded))
}

// FromBytes encodes any 128-bit value as permitted by the TypeID parser.
func FromBytes(prefix string, value [16]byte) (ID, error) {
	if err := ValidatePrefix(prefix); err != nil {
		return ID{}, err
	}
	if prefix == "" && value == ([16]byte{}) {
		return ID{}, nil
	}

	return ID{value: value, prefix: prefix, valid: true}, nil
}

// UUIDSource is a canonical UUID string or a validated identifier UUID.
type UUIDSource interface {
	string | identifieruuid.ID
}

// FromUUID encodes any 128-bit UUID value with a validated prefix. String
// input permits the non-RFC versions and variants required by the TypeID spec.
func FromUUID[T UUIDSource](prefix string, value T) (ID, error) {
	var bytes [16]byte
	switch typed := any(value).(type) {
	case string:
		parsed, err := parseUUIDText(typed)
		if err != nil {
			return ID{}, err
		}
		bytes = parsed
	case identifieruuid.ID:
		bytes = typed.Bytes()
	}

	return FromBytes(prefix, bytes)
}

func parseUUIDText(text string) ([16]byte, error) {
	if len(text) != 36 || text[8] != '-' || text[13] != '-' || text[18] != '-' || text[23] != '-' {
		return [16]byte{}, fmt.Errorf("%w: UUID must use canonical 8-4-4-4-12 form", identifier.ErrInvalid)
	}

	var compact [32]byte
	index := 0
	for position := range len(text) {
		if position == 8 || position == 13 || position == 18 || position == 23 {
			continue
		}
		character := text[position]
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return [16]byte{}, fmt.Errorf("%w: UUID contains non-canonical hexadecimal", identifier.ErrInvalid)
		}
		compact[index] = character
		index++
	}

	var value [16]byte
	_, _ = hex.Decode(value[:], compact[:])

	return value, nil
}

// Bytes returns a copy of the 128-bit suffix value.
func (id ID) Bytes() [16]byte { return id.value }

// Prefix returns the validated type prefix.
func (id ID) Prefix() string { return id.prefix }

// IsZero reports whether the ID is the unprefixed all-zero TypeID.
func (id ID) IsZero() bool { return !id.valid }

// Compare orders prefixes lexically and suffixes by unsigned bytes.
func (id ID) Compare(other ID) int {
	if compared := strings.Compare(id.prefix, other.prefix); compared != 0 {
		return compared
	}

	return bytes.Compare(id.value[:], other.value[:])
}

// String returns canonical lowercase text, including the canonical zero ID.
func (id ID) String() string {
	if !id.valid {
		return zeroSuffix
	}

	suffix := strings.ToLower(oklogulid.ULID(id.value).String())
	if id.prefix == "" {
		return suffix
	}

	return id.prefix + "_" + suffix
}

// LogValue redacts the TypeID from structured logs.
func (id ID) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// Inspect reports the suffix version and UUIDv7 timestamp when present.
func (id ID) Inspect() identifier.Inspection {
	inspection := identifier.Inspection{
		Family:   identifier.FamilyTypeID,
		Version:  int(id.value[6] >> 4),
		Sortable: id.valid,
	}
	if id.valid && inspection.Version == 7 && id.value[8]&0xc0 == 0x80 {
		milliseconds := int64(id.value[0])<<40 | int64(id.value[1])<<32 |
			int64(id.value[2])<<24 | int64(id.value[3])<<16 |
			int64(id.value[4])<<8 | int64(id.value[5])
		inspection.Timestamp = time.UnixMilli(milliseconds).UTC()
		inspection.HasTime = true
	}

	return inspection
}

// MarshalText implements encoding.TextMarshaler.
func (id ID) MarshalText() ([]byte, error) {
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

// MarshalBinary encodes canonical text because the prefix is part of a TypeID.
func (id ID) MarshalBinary() ([]byte, error) { return id.MarshalText() }

// UnmarshalBinary validates canonical text.
func (id *ID) UnmarshalBinary(data []byte) error { return id.UnmarshalText(data) }

// MarshalJSON encodes canonical text, including the canonical zero TypeID.
func (id ID) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.String())
}

// UnmarshalJSON decodes canonical text and accepts null as the zero TypeID.
func (id *ID) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*id = ID{}

		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("decode TypeID: %w", err)
	}

	return id.UnmarshalText([]byte(text))
}

// Value implements driver.Valuer using canonical text.
func (id ID) Value() (driver.Value, error) {
	return id.String(), nil
}

// Scan implements sql.Scanner for text and NULL.
func (id *ID) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		*id = ID{}

		return nil
	case string:
		return id.UnmarshalText([]byte(value))
	case []byte:
		return id.UnmarshalText(value)
	default:
		return fmt.Errorf("%w: cannot scan TypeID from %T", identifier.ErrInvalid, src)
	}
}

// Generator owns a prefix and UUIDv7 generator.
type Generator struct {
	prefix string
	uuid   *identifieruuid.V7Generator
}

// NewGenerator validates prefix and binds it to an owned UUIDv7 generator.
func NewGenerator(prefix string, generator *identifieruuid.V7Generator) (*Generator, error) {
	if err := ValidatePrefix(prefix); err != nil {
		return nil, err
	}
	if generator == nil {
		generator = identifieruuid.NewV7Generator(nil, nil)
	}

	return &Generator{prefix: prefix, uuid: generator}, nil
}

// New generates a UUIDv7-backed TypeID.
func (generator *Generator) New() (ID, error) {
	value, err := generator.uuid.New()
	if err != nil {
		return ID{}, fmt.Errorf("generate TypeID: %w", err)
	}

	return FromUUID(generator.prefix, value)
}
