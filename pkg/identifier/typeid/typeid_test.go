package typeid_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	identifiertypeid "github.com/faustbrian/golib/pkg/identifier/typeid"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
)

func TestOfficialVectorsRoundTrip(t *testing.T) {
	vectors := []struct {
		text   string
		prefix string
		bytes  [16]byte
	}{
		{text: "00000000000000000000000000"},
		{text: "00000000000000000000000001", bytes: [16]byte{15: 1}},
		{
			text:   "prefix_01h455vb4pex5vsknk084sn02q",
			prefix: "prefix",
			bytes:  [16]byte{0x01, 0x89, 0x0a, 0x5d, 0xac, 0x96, 0x77, 0x4b, 0xbc, 0xce, 0xb3, 0x02, 0x09, 0x9a, 0x80, 0x57},
		},
		{
			text:   "pre_fix_00000000000000000000000000",
			prefix: "pre_fix",
		},
	}

	for _, vector := range vectors {
		t.Run(vector.text, func(t *testing.T) {
			id, err := identifiertypeid.Parse(vector.text)
			if err != nil {
				t.Fatal(err)
			}
			if id.String() != vector.text || id.Prefix() != vector.prefix || id.Bytes() != vector.bytes {
				t.Fatalf("Parse() = %q, %q, %x", id, id.Prefix(), id.Bytes())
			}
			encoded, err := identifiertypeid.FromBytes(vector.prefix, vector.bytes)
			if err != nil || encoded != id {
				t.Fatalf("FromBytes() = %q, %v", encoded, err)
			}
		})
	}
}

func TestPrefixAndSuffixValidation(t *testing.T) {
	invalid := []string{
		"x",
		"_01h455vb4pex5vsknk084sn02q",
		"prefix__01h455vb4pex5vsknk084sn02q",
		"Prefix_01h455vb4pex5vsknk084sn02q",
		"123_01h455vb4pex5vsknk084sn02q",
		"prefix_01H455VB4PEX5VSKNK084SN02Q",
		"prefix_8zzzzzzzzzzzzzzzzzzzzzzzzz",
		"prefix_01h455vb4pex5vsknk084sn02i",
	}
	for _, input := range invalid {
		if _, err := identifiertypeid.Parse(input); !errors.Is(err, identifier.ErrInvalid) {
			t.Errorf("Parse(%q) error = %v", input, err)
		}
	}

	if err := identifiertypeid.ValidatePrefix("my__type"); err != nil {
		t.Fatalf("consecutive underscores must be accepted: %v", err)
	}
	if err := identifiertypeid.ValidatePrefix(string(bytes.Repeat([]byte{'a'}, 64))); !errors.Is(err, identifier.ErrInvalid) {
		t.Fatalf("long prefix error = %v", err)
	}
	for _, prefix := range []string{"a", "z", "a_z", string(bytes.Repeat([]byte{'z'}, 63))} {
		if err := identifiertypeid.ValidatePrefix(prefix); err != nil {
			t.Fatalf("boundary prefix %q: %v", prefix, err)
		}
	}
	maximum := string(bytes.Repeat([]byte{'z'}, 63)) + "_00000000000000000000000000"
	if id, err := identifiertypeid.Parse(maximum); err != nil || id.String() != maximum {
		t.Fatalf("maximum TypeID = %s, %v", id, err)
	}
	if id, err := identifiertypeid.Parse("7zzzzzzzzzzzzzzzzzzzzzzzzz"); err != nil || id.String() != "7zzzzzzzzzzzzzzzzzzzzzzzzz" {
		t.Fatalf("maximum suffix = %s, %v", id, err)
	}
}

func TestGeneratorUsesOwnedUUIDv7Generator(t *testing.T) {
	instant := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	uuidGenerator := identifieruuid.NewV7Generator(
		identifier.ClockFunc(func() time.Time { return instant }),
		bytes.NewReader(make([]byte, 10)),
	)
	generator, err := identifiertypeid.NewGenerator("user", uuidGenerator)
	if err != nil {
		t.Fatal(err)
	}

	first, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	second, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	if first.Prefix() != "user" || first.Compare(second) >= 0 || first.Inspect().Version != 7 {
		t.Fatalf("generated TypeIDs = %s, %s", first, second)
	}

	if _, err := identifiertypeid.NewGenerator("bad_", uuidGenerator); !errors.Is(err, identifier.ErrInvalid) {
		t.Fatalf("invalid generator prefix error = %v", err)
	}
}

func TestSerializationAndSQLRoundTrips(t *testing.T) {
	original, _ := identifiertypeid.Parse("prefix_01h455vb4pex5vsknk084sn02q")
	if original.LogValue().String() != "[REDACTED]" {
		t.Fatal("TypeID log value was not redacted")
	}

	text, _ := original.MarshalText()
	var decoded identifiertypeid.ID
	if err := decoded.UnmarshalText(text); err != nil || decoded != original {
		t.Fatalf("text round trip = %s, %v", decoded, err)
	}
	binary, _ := original.MarshalBinary()
	if err := decoded.UnmarshalBinary(binary); err != nil || decoded != original {
		t.Fatalf("binary round trip = %s, %v", decoded, err)
	}
	data, _ := json.Marshal(original)
	if err := json.Unmarshal(data, &decoded); err != nil || decoded != original {
		t.Fatalf("JSON round trip = %s, %v", decoded, err)
	}
	value, err := original.Value()
	if err != nil || value != original.String() {
		t.Fatalf("Value() = %v, %v", value, err)
	}
	for _, source := range []any{original.String(), []byte(original.String())} {
		if err := decoded.Scan(source); err != nil || decoded != original {
			t.Fatalf("Scan(%T) = %s, %v", source, decoded, err)
		}
	}
}

func TestDecodersRejectInvalidValuesAndHandleNull(t *testing.T) {
	var id identifiertypeid.ID
	for name, decode := range map[string]func() error{
		"text":   func() error { return id.UnmarshalText([]byte("bad")) },
		"binary": func() error { return id.UnmarshalBinary([]byte("bad")) },
		"json":   func() error { return json.Unmarshal([]byte("42"), &id) },
		"scan":   func() error { return id.Scan(42) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := decode(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if err := id.Scan(nil); err != nil || !id.IsZero() {
		t.Fatalf("Scan(nil) = %s, %v", id, err)
	}
	value, err := id.Value()
	if err != nil || value != "00000000000000000000000000" {
		t.Fatalf("zero Value() = %v, %v", value, err)
	}
}
