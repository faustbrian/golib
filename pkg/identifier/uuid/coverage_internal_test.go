package uuid

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestRemainingUUIDBoundaries(t *testing.T) {
	var zero ID
	if zero.Compare(ID{}) != 0 || zero.Inspect().HasTime {
		t.Fatal("zero UUID inspection or ordering")
	}
	if _, err := zero.MarshalText(); err == nil {
		t.Fatal("zero text must fail")
	}
	if _, err := zero.MarshalBinary(); err == nil {
		t.Fatal("zero binary must fail")
	}
	if data, err := json.Marshal(zero); err != nil || string(data) != "null" {
		t.Fatalf("zero JSON = %s, %v", data, err)
	}
	assigned, _ := Parse("017f22e2-79b0-7cc3-98c4-dc0c0c07398f")
	if err := json.Unmarshal([]byte("null"), &assigned); err != nil || !assigned.IsZero() {
		t.Fatalf("JSON null = %s, %v", assigned, err)
	}

	bad := make([]byte, 16)
	if err := zero.UnmarshalBinary(bad); !errors.Is(err, identifier.ErrInvalid) {
		t.Fatalf("invalid binary error = %v", err)
	}
	if err := zero.ScanUUID(pgtype.UUID{Bytes: [16]byte{}, Valid: true}); err == nil {
		t.Fatal("invalid PostgreSQL UUID must fail")
	}
	if value, err := zero.UUIDValue(); err != nil || value.Valid {
		t.Fatalf("zero UUIDValue = %+v, %v", value, err)
	}

	v6 := ID{0x1e, 0x1d, 0x07, 0xde, 0xc6, 0x20, 0x6c, 0x00, 0x80, 0x00}
	if inspection := v6.Inspect(); !inspection.HasTime || !inspection.Sortable {
		t.Fatalf("v6 inspection = %+v", inspection)
	}
	preUnix := ID{6: 0x10, 8: 0x80}
	if !preUnix.Inspect().Timestamp.Before(time.Unix(0, 0)) {
		t.Fatal("pre-Unix UUIDv1 timestamp not preserved")
	}

	if _, err := NewV7Generator(identifier.ClockFunc(func() time.Time {
		return time.UnixMilli(-1)
	}), bytes.NewReader(make([]byte, 10))).New(); !errors.Is(err, identifier.ErrInvalid) {
		t.Fatalf("negative timestamp error = %v", err)
	}
	defaultV4 := NewV4Generator(nil)
	if _, err := defaultV4.New(); err != nil {
		t.Fatal(err)
	}
	defaultV7 := NewV7Generator(nil, nil)
	if _, err := defaultV7.New(); err != nil {
		t.Fatal(err)
	}

	carry := ID{6: 0x70, 7: 0xff, 8: 0xbf, 9: 0xff, 10: 0xff, 11: 0xff, 12: 0xff, 13: 0xff, 14: 0xff, 15: 0xff}
	if !incrementV7(&carry) || carry[6] != 0x71 {
		t.Fatalf("v7 carry = %x", carry)
	}
	variantCarry := ID{6: 0x70, 8: 0x80, 9: 0xff, 10: 0xff, 11: 0xff, 12: 0xff, 13: 0xff, 14: 0xff, 15: 0xff}
	if !incrementV7(&variantCarry) || variantCarry[8] != 0x81 {
		t.Fatalf("v7 variant carry = %x", variantCarry)
	}
	middleCarry := ID{6: 0x70, 8: 0xbf, 9: 0xff, 10: 0xff, 11: 0xff, 12: 0xff, 13: 0xff, 14: 0xff, 15: 0xff}
	if !incrementV7(&middleCarry) || middleCarry[7] != 1 {
		t.Fatalf("v7 middle carry = %x", middleCarry)
	}
}
