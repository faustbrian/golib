package cache_test

import (
	"errors"
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
)

func FuzzKeySpace(f *testing.F) {
	for _, seed := range []string{"", "tenant:42", "åäö", "a/b/c", "\x00secret"} {
		f.Add(seed)
	}
	space, err := cache.NewKeySpace("fuzz", "keys", 1, cache.StringKeyEncoder{}, 128)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, logical string) {
		first, err := space.Key(logical)
		if err != nil {
			t.Fatalf("encode key: %v", err)
		}
		second, err := space.Key(logical)
		if err != nil {
			t.Fatalf("encode key twice: %v", err)
		}
		if first != second || len(first) > 128 {
			t.Fatalf("non-deterministic or oversized key: %q %q", first, second)
		}
	})
}

func FuzzJSONCodec(f *testing.F) {
	for _, seed := range [][]byte{nil, {1}, {1, '"', 'o', 'k', '"'}, {2, '{'}, {1, 'n', 'u', 'l', 'l'}} {
		f.Add(seed)
	}
	codec := cache.JSONCodec[string]{Version: 1, MaxEncodedSize: 256}
	f.Fuzz(func(t *testing.T, encoded []byte) {
		value, err := codec.Decode(encoded)
		if err != nil {
			if !errors.Is(err, cache.ErrDecode) && !errors.Is(err, cache.ErrSchemaMismatch) &&
				!errors.Is(err, cache.ErrValueTooLarge) {
				t.Fatalf("unclassified decode error: %v", err)
			}
			return
		}
		roundTrip, err := codec.Encode(value)
		if err != nil {
			if errors.Is(err, cache.ErrValueTooLarge) {
				return
			}
			t.Fatalf("re-encode decoded value: %v", err)
		}
		decoded, err := codec.Decode(roundTrip)
		if err != nil || decoded != value {
			t.Fatalf("round trip: decoded=%q err=%v want=%q", decoded, err, value)
		}
	})
}

func FuzzPayloadVersions(f *testing.F) {
	f.Add(byte(1), "value")
	f.Add(byte(255), "åäö")
	f.Fuzz(func(t *testing.T, version byte, value string) {
		if version == 0 {
			return
		}
		codec := cache.JSONCodec[string]{Version: version, MaxEncodedSize: 1024}
		encoded, err := codec.Encode(value)
		if err != nil {
			if !errors.Is(err, cache.ErrValueTooLarge) {
				t.Fatalf("encode: %v", err)
			}
			return
		}
		encoded[0] = version%255 + 1
		if encoded[0] != version {
			if _, err := codec.Decode(encoded); !errors.Is(err, cache.ErrSchemaMismatch) {
				t.Fatalf("version mismatch returned %v", err)
			}
		}
	})
}

func FuzzOptionCombinations(f *testing.F) {
	f.Add(int64(time.Minute), int64(0), int64(0), int64(0), false, false)
	f.Add(int64(-1), int64(-1), int64(-1), int64(-1), true, true)
	f.Fuzz(func(t *testing.T, ttlNanos, staleNanos, negativeNanos, jitterNanos int64, swr, sie bool) {
		space, err := cache.NewKeySpace("fuzz", "options", 1, cache.StringKeyEncoder{}, 128)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = cache.New(cache.Config[string, string]{
			Backend:  newRecordingBackend(),
			Keys:     space,
			Codec:    cache.JSONCodec[string]{Version: 1},
			TTL:      cache.TTLPolicy{TTL: time.Duration(ttlNanos), StaleFor: time.Duration(staleNanos)},
			Clock:    cache.SystemClock{},
			MaxValue: 1024,
			Load: cache.LoadPolicy{
				NegativeTTL:          time.Duration(negativeNanos),
				RefreshJitter:        time.Duration(jitterNanos),
				StaleWhileRevalidate: swr,
				StaleIfError:         sie,
			},
		})
	})
}
