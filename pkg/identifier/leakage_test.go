package identifier_test

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	identifierksuid "github.com/faustbrian/golib/pkg/identifier/ksuid"
	identifiernanoid "github.com/faustbrian/golib/pkg/identifier/nanoid"
	identifiertypeid "github.com/faustbrian/golib/pkg/identifier/typeid"
	identifierulid "github.com/faustbrian/golib/pkg/identifier/ulid"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
)

func TestTimestampAndTopologyLeakageIsExact(t *testing.T) {
	instant := time.Date(2026, 7, 20, 12, 34, 56, 789_000_000, time.UTC)
	clock := identifier.ClockFunc(func() time.Time { return instant })

	uuidV4, _ := identifieruuid.NewV4Generator(bytes.NewReader(make([]byte, 16))).New()
	if uuidV4.Inspect().HasTime {
		t.Fatal("UUIDv4 exposed a timestamp")
	}

	uuidV7, _ := identifieruuid.NewV7Generator(clock, bytes.NewReader(make([]byte, 10))).New()
	uuidBytes := uuidV7.Bytes()
	if got := uint48(uuidBytes[:6]); got != uint64(instant.UnixMilli()) {
		t.Fatalf("UUIDv7 48-bit timestamp = %d", got)
	}

	ulidValue, _ := identifierulid.NewGenerator(clock, bytes.NewReader(make([]byte, 10))).New()
	ulidBytes := ulidValue.Bytes()
	if got := uint48(ulidBytes[:6]); got != uint64(instant.UnixMilli()) {
		t.Fatalf("ULID 48-bit timestamp = %d", got)
	}

	typeIDGenerator, _ := identifiertypeid.NewGenerator(
		"customer", identifieruuid.NewV7Generator(clock, bytes.NewReader(make([]byte, 10))),
	)
	typeIDValue, _ := typeIDGenerator.New()
	if typeIDValue.Prefix() != "customer" ||
		typeIDValue.Inspect().Timestamp.UnixMilli() != instant.UnixMilli() {
		t.Fatalf("TypeID leakage = %+v, %q", typeIDValue.Inspect(), typeIDValue.Prefix())
	}

	ksuidValue, _ := identifierksuid.NewGenerator(clock, bytes.NewReader(make([]byte, 16))).New()
	ksuidBytes := ksuidValue.Bytes()
	if got := int64(binary.BigEndian.Uint32(ksuidBytes[:4])) + 1_400_000_000; got != instant.Unix() {
		t.Fatalf("KSUID 32-bit timestamp = %d", got)
	}

	nanoIDValue, _ := identifiernanoid.Parse("_____________________")
	if nanoIDValue.Inspect().HasTime || nanoIDValue.Inspect().Sortable {
		t.Fatalf("NanoID leakage = %+v", nanoIDValue.Inspect())
	}

	// Generated families never embed a node identifier. UUIDv1 and UUIDv6 are
	// parse-only and may retain the externally supplied 48-bit node field.
	if !bytes.Equal(uuidBytes[10:], make([]byte, 6)) ||
		!bytes.Equal(ulidBytes[10:], make([]byte, 6)) ||
		!bytes.Equal(ksuidBytes[14:], make([]byte, 6)) {
		t.Fatal("zero entropy fixture unexpectedly exposed topology bytes")
	}
}

func uint48(value []byte) uint64 {
	return uint64(value[0])<<40 | uint64(value[1])<<32 | uint64(value[2])<<24 |
		uint64(value[3])<<16 | uint64(value[4])<<8 | uint64(value[5])
}
