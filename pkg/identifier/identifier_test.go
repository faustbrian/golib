package identifier_test

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"testing"

	identifier "github.com/faustbrian/golib/pkg/identifier"
)

type userTag struct{}

func (userTag) Validate(value string) error {
	if value != "usr_01" {
		return identifier.ErrInvalid
	}

	return nil
}

type orderTag struct{}

func (orderTag) Validate(string) error { return nil }

func TestTypedIDsValidateAndRemainDomainDistinct(t *testing.T) {
	userID, err := identifier.Parse[userTag]("usr_01")
	if err != nil {
		t.Fatalf("parse user ID: %v", err)
	}

	if userID.String() != "usr_01" || userID.IsZero() {
		t.Fatalf("unexpected typed ID: %q", userID.String())
	}

	if _, parseErr := identifier.Parse[userTag]("order_01"); !errors.Is(parseErr, identifier.ErrInvalid) {
		t.Fatalf("parse invalid ID error = %v, want ErrInvalid", parseErr)
	}

	orderID, err := identifier.Parse[orderTag]("usr_01")
	if err != nil {
		t.Fatalf("parse order ID: %v", err)
	}

	_ = orderID // Its distinct static type prevents assignment to userID.
}

func TestTypedIDSerializationRoundTrips(t *testing.T) {
	original, err := identifier.Parse[userTag]("usr_01")
	if err != nil {
		t.Fatal(err)
	}

	text, err := original.MarshalText()
	if err != nil || string(text) != "usr_01" {
		t.Fatalf("MarshalText() = %q, %v", text, err)
	}

	var fromText identifier.ID[userTag]
	if decodeErr := fromText.UnmarshalText(text); decodeErr != nil || fromText != original {
		t.Fatalf("text round trip = %v, %v", fromText, decodeErr)
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var fromJSON identifier.ID[userTag]
	if decodeErr := json.Unmarshal(data, &fromJSON); decodeErr != nil || fromJSON != original {
		t.Fatalf("JSON round trip = %v, %v", fromJSON, decodeErr)
	}

	binary, err := original.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	var fromBinary identifier.ID[userTag]
	if decodeErr := fromBinary.UnmarshalBinary(binary); decodeErr != nil || fromBinary != original {
		t.Fatalf("binary round trip = %v, %v", fromBinary, decodeErr)
	}

	value, err := original.Value()
	if err != nil || value != driver.Value("usr_01") {
		t.Fatalf("Value() = %v, %v", value, err)
	}

	for _, input := range []any{"usr_01", []byte("usr_01")} {
		var scanned identifier.ID[userTag]
		if err := scanned.Scan(input); err != nil || scanned != original {
			t.Fatalf("Scan(%T) = %v, %v", input, scanned, err)
		}
	}
}

func TestTypedIDRejectsInvalidDecodingAndScanning(t *testing.T) {
	var id identifier.ID[userTag]

	for name, decode := range map[string]func() error{
		"text":   func() error { return id.UnmarshalText([]byte("bad")) },
		"binary": func() error { return id.UnmarshalBinary([]byte("bad")) },
		"json":   func() error { return json.Unmarshal([]byte(`"bad"`), &id) },
		"scan":   func() error { return id.Scan(42) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := decode(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestTypedIDOrderingAndZeroSQLValue(t *testing.T) {
	left, _ := identifier.Parse[orderTag]("a")
	right, _ := identifier.Parse[orderTag]("b")
	equal := left
	if left.Compare(right) >= 0 || right.Compare(left) <= 0 || left.Compare(equal) != 0 {
		t.Fatal("typed ID lexical ordering is inconsistent")
	}

	var zero identifier.ID[userTag]
	value, err := zero.Value()
	if err != nil || value != nil || !zero.IsZero() {
		t.Fatalf("zero Value() = %v, %v", value, err)
	}
	if err := zero.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
}
