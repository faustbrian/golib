package cache_test

import (
	"errors"
	"testing"

	cache "github.com/faustbrian/golib/pkg/cache"
)

type testPayload struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestJSONCodecRoundTripAndVersionMismatch(t *testing.T) {
	t.Parallel()

	codec := cache.JSONCodec[testPayload]{Version: 2}
	want := testPayload{Name: "widgets", Count: 3}
	encoded, err := codec.Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round trip mismatch: got %#v want %#v", got, want)
	}

	encoded[0] = 1
	_, err = codec.Decode(encoded)
	if !errors.Is(err, cache.ErrSchemaMismatch) {
		t.Fatalf("expected schema mismatch, got %v", err)
	}
}

func TestJSONCodecRejectsMalformedAndOversizedPayloads(t *testing.T) {
	t.Parallel()

	codec := cache.JSONCodec[testPayload]{Version: 1, MaxEncodedSize: 16}
	_, err := codec.Encode(testPayload{Name: "this is too long"})
	if !errors.Is(err, cache.ErrValueTooLarge) {
		t.Fatalf("expected value limit error, got %v", err)
	}

	codec.MaxEncodedSize = 1024
	_, err = codec.Decode([]byte{1, '{'})
	if !errors.Is(err, cache.ErrDecode) {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestJSONCodecRejectsInvalidVersionsAndAmbiguousJSON(t *testing.T) {
	t.Parallel()

	if _, err := (cache.JSONCodec[string]{}).Encode("value"); !errors.Is(err, cache.ErrSchemaMismatch) {
		t.Fatalf("zero version encode returned %v", err)
	}
	if _, err := (cache.JSONCodec[func()]{Version: 1}).Encode(func() {}); !errors.Is(err, cache.ErrDecode) {
		t.Fatalf("unsupported value encode returned %v", err)
	}
	codec := cache.JSONCodec[testPayload]{Version: 1, MaxEncodedSize: 8}
	if _, err := codec.Decode(make([]byte, 9)); !errors.Is(err, cache.ErrValueTooLarge) {
		t.Fatalf("oversized decode returned %v", err)
	}
	if _, err := codec.Decode(nil); !errors.Is(err, cache.ErrSchemaMismatch) {
		t.Fatalf("empty decode returned %v", err)
	}
	if _, err := codec.Decode([]byte{1, '{', '}', '{', '}'}); !errors.Is(err, cache.ErrDecode) {
		t.Fatalf("trailing JSON returned %v", err)
	}
}

func TestJSONCodecPreservesNilPointersAndDeterministicMaps(t *testing.T) {
	t.Parallel()

	pointerCodec := cache.JSONCodec[*testPayload]{Version: 1}
	encodedNil, err := pointerCodec.Encode(nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(encodedNil) != "\x01null" {
		t.Fatalf("nil pointer encoded as %q", encodedNil)
	}
	decodedNil, err := pointerCodec.Decode(encodedNil)
	if err != nil || decodedNil != nil {
		t.Fatalf("nil pointer round trip: value=%#v err=%v", decodedNil, err)
	}

	mapCodec := cache.JSONCodec[map[string]int]{Version: 1}
	first, err := mapCodec.Encode(map[string]int{"z": 1, "a": 2})
	if err != nil {
		t.Fatal(err)
	}
	second, err := mapCodec.Encode(map[string]int{"a": 2, "z": 1})
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != "\x01{\"a\":2,\"z\":1}" || string(first) != string(second) {
		t.Fatalf("map encoding is not deterministic: %q != %q", first, second)
	}
}
