package wire_test

import (
	"errors"
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
	"github.com/faustbrian/golib/pkg/cache/internal/wire"
)

func TestRecordRoundTripDoesNotAliasPayload(t *testing.T) {
	t.Parallel()

	want := cache.Record{
		Payload:   []byte{0, 1, 2, 255},
		ExpiresAt: time.Date(2026, 7, 15, 12, 0, 0, 123, time.UTC),
		StaleAt:   time.Date(2026, 7, 15, 12, 5, 0, 456, time.UTC),
	}
	encoded, err := wire.Encode(want, 128)
	if err != nil {
		t.Fatal(err)
	}
	want.Payload[0] = 9
	got, err := wire.Decode(encoded, 128)
	if err != nil {
		t.Fatal(err)
	}
	if got.Payload[0] != 0 || !got.ExpiresAt.Equal(want.ExpiresAt) || !got.StaleAt.Equal(want.StaleAt) || got.Negative {
		t.Fatalf("record round trip mismatch: %#v", got)
	}
	encoded[len(encoded)-1] = 8
	if got.Payload[len(got.Payload)-1] != 255 {
		t.Fatal("decoded payload aliases encoded buffer")
	}
	negative := cache.Record{ExpiresAt: want.ExpiresAt, StaleAt: want.StaleAt, Negative: true}
	encoded, err = wire.Encode(negative, 128)
	if err != nil {
		t.Fatal(err)
	}
	got, err = wire.Decode(encoded, 128)
	if err != nil || !got.Negative || len(got.Payload) != 0 {
		t.Fatalf("negative round trip: record=%#v err=%v", got, err)
	}
}

func TestRecordEnvelopeRejectsLimitsVersionsAndMalformedData(t *testing.T) {
	t.Parallel()

	record := cache.Record{Payload: []byte("value"), ExpiresAt: time.Now(), StaleAt: time.Now()}
	if _, err := wire.Encode(record, 4); !errors.Is(err, cache.ErrValueTooLarge) {
		t.Fatalf("expected encode limit error, got %v", err)
	}

	encoded, err := wire.Encode(record, 128)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wire.Decode(encoded, len(encoded)-1); !errors.Is(err, cache.ErrValueTooLarge) {
		t.Fatalf("expected decode limit error, got %v", err)
	}
	encoded[0]++
	if _, err := wire.Decode(encoded, 128); !errors.Is(err, cache.ErrSchemaMismatch) {
		t.Fatalf("expected envelope version error, got %v", err)
	}
	if _, err := wire.Decode([]byte{1}, 128); !errors.Is(err, cache.ErrDecode) {
		t.Fatalf("expected malformed envelope error, got %v", err)
	}

	encoded, err = wire.Encode(record, 128)
	if err != nil {
		t.Fatal(err)
	}
	encoded[4] = 2
	if _, err := wire.Decode(encoded, 128); !errors.Is(err, cache.ErrDecode) {
		t.Fatalf("unknown envelope flags returned %v", err)
	}
	if _, err := wire.Encode(cache.Record{}, 128); !errors.Is(err, cache.ErrInvalidRecord) {
		t.Fatalf("invalid record encode returned %v", err)
	}
	encoded[4] = 1
	encoded[len(encoded)-1] = 'x'
	if _, err := wire.Decode(encoded, 128); !errors.Is(err, cache.ErrInvalidRecord) {
		t.Fatalf("negative record with payload returned %v", err)
	}
}
