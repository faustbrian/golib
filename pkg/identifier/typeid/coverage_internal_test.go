package typeid

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
)

func TestRemainingTypeIDBoundaries(t *testing.T) {
	if err := ValidatePrefix("ab1cd"); !errors.Is(err, identifier.ErrInvalid) {
		t.Fatalf("invalid interior prefix error = %v", err)
	}
	if _, err := Parse("prefixX01h455vb4pex5vsknk084sn02q"); !errors.Is(err, identifier.ErrInvalid) {
		t.Fatalf("missing separator error = %v", err)
	}
	if _, err := FromBytes("bad_", [16]byte{}); !errors.Is(err, identifier.ErrInvalid) {
		t.Fatalf("FromBytes prefix error = %v", err)
	}
	zeroUUID, err := FromUUID("user", identifieruuid.ID{})
	if err != nil || zeroUUID.String() != "user_00000000000000000000000000" {
		t.Fatalf("zero UUID = %s, %v", zeroUUID, err)
	}
	uuidValue, _ := identifieruuid.Parse("017f22e2-79b0-7cc3-98c4-dc0c0c07398f")
	if _, err := FromUUID("user", uuidValue); err != nil {
		t.Fatal(err)
	}

	var zero ID
	if zero.String() != zeroSuffix || zero.Compare(ID{}) != 0 {
		t.Fatal("zero TypeID state")
	}
	if text, err := zero.MarshalText(); err != nil || string(text) != zeroSuffix {
		t.Fatalf("zero text = %q, %v", text, err)
	}
	if data, err := json.Marshal(zero); err != nil || string(data) != `"00000000000000000000000000"` {
		t.Fatalf("zero JSON = %s, %v", data, err)
	}
	for _, text := range []string{
		"",
		"000000000000000000000000000000000000",
		"00000000-0000-0000-0000-00000000000A",
		"00000000-0000-0000-0000-00000000000z",
	} {
		if _, err := FromUUID("user", text); !errors.Is(err, identifier.ErrInvalid) {
			t.Errorf("FromUUID(%q) error = %v", text, err)
		}
	}
	assigned, _ := Parse("prefix_01h455vb4pex5vsknk084sn02q")
	if err := json.Unmarshal([]byte("null"), &assigned); err != nil || !assigned.IsZero() {
		t.Fatalf("JSON null = %s, %v", assigned, err)
	}
	other, _ := Parse("z_01h455vb4pex5vsknk084sn02q")
	if assigned.Compare(other) >= 0 {
		t.Fatal("prefix ordering failed")
	}
	if _, err := NewGenerator("", nil); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(10)
	calls := 0
	clock := identifier.ClockFunc(func() time.Time {
		calls++
		if calls == 1 {
			return now
		}

		return now.Add(-time.Millisecond)
	})
	failing, _ := NewGenerator("user", identifieruuid.NewV7Generator(clock, bytes.NewReader(make([]byte, 10))))
	if _, err := failing.New(); err != nil {
		t.Fatal(err)
	}
	if _, err := failing.New(); !errors.Is(err, identifier.ErrClockRollback) {
		t.Fatalf("wrapped generator error = %v", err)
	}
}

func TestInspectRequiresAssignedUUIDv7Variant(t *testing.T) {
	versionOne := ID{valid: true}
	versionOne.value[6] = 0x10
	versionOne.value[8] = 0x80
	if versionOne.Inspect().HasTime {
		t.Fatal("non-v7 TypeID exposed a timestamp")
	}

	invalidVariant := ID{valid: true}
	invalidVariant.value[6] = 0x70
	if invalidVariant.Inspect().HasTime {
		t.Fatal("non-RFC variant TypeID exposed a timestamp")
	}

	unassigned := ID{}
	unassigned.value[6] = 0x70
	unassigned.value[8] = 0x80
	if unassigned.Inspect().HasTime {
		t.Fatal("unassigned TypeID exposed a timestamp")
	}
}
