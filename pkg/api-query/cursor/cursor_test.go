package cursor_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
	"github.com/faustbrian/golib/pkg/api-query/cursor"
)

func TestCodecEncryptsAndBindsCursorState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	codec := mustCodec(t, now)
	payload := cursor.Payload{
		SchemaRevision: "orders-v3",
		Direction:      cursor.Forward,
		Sorts: []apiquery.SortTerm{
			{Name: "created_at", Direction: apiquery.Descending, Nulls: apiquery.NullsLast},
			{Name: "id", Direction: apiquery.Ascending},
		},
		Positions: []apiquery.Value{
			apiquery.TimeValue(now.Add(-time.Hour)), apiquery.StringValue("order-secret-42"),
		},
		ExpiresAt: now.Add(time.Hour),
		Policy:    "default",
	}

	token, err := codec.Encode(payload)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if strings.Contains(token, "order-secret-42") || strings.Contains(token, "orders-v3") {
		t.Fatalf("encoded cursor exposed protected payload: %q", token)
	}
	decoded, err := codec.Decode(token, "orders-v3", payload.Sorts)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.Direction != cursor.Forward || decoded.Policy != "default" || len(decoded.Positions) != 2 {
		t.Fatalf("Decode() = %#v", decoded)
	}
	if decoded.Positions[1].String() != "order-secret-42" {
		t.Fatalf("decoded position = %q", decoded.Positions[1].String())
	}
	state, err := codec.DecodeCursor(t.Context(), token, "orders-v3", payload.Sorts)
	if err != nil || state.Direction != apiquery.CursorForward || state.Policy != "default" ||
		state.Positions[1].String() != "order-secret-42" {
		t.Fatalf("DecodeCursor() = %#v, %v", state, err)
	}
	state.Positions[1] = apiquery.StringValue("mutated")
	if decoded.Positions[1].String() != "order-secret-42" {
		t.Fatal("DecodeCursor returned payload-owned positions")
	}
	if _, err := codec.DecodeCursor(t.Context(), "invalid", "orders-v3", payload.Sorts); err == nil {
		t.Fatal("DecodeCursor accepted invalid token")
	}

	tampered := token[:len(token)-1] + replacement(token[len(token)-1])
	if _, err := codec.Decode(tampered, "orders-v3", payload.Sorts); !errors.Is(err, cursor.ErrInvalid) {
		t.Fatalf("Decode(tampered) error = %v, want ErrInvalid", err)
	}
	if _, err := codec.Decode(token, "orders-v4", payload.Sorts); !errors.Is(err, cursor.ErrSchema) {
		t.Fatalf("Decode(wrong schema) error = %v, want ErrSchema", err)
	}
}

func TestCodecRejectsReplayWhenConfigured(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	keyring, err := cursor.NewKeyring(cursor.Key{ID: "one", Secret: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatalf("NewKeyring() error = %v", err)
	}
	seen := make(map[[32]byte]struct{})
	codec, err := cursor.NewCodec(cursor.Config{Version: "v1", Keys: keyring,
		MaxEncodedBytes: 512, MaxPositions: 2, MaxTTL: time.Hour,
		Clock: func() time.Time { return now },
		ReplayGuard: func(fingerprint [32]byte, _ time.Time) bool {
			if _, exists := seen[fingerprint]; exists {
				return false
			}
			seen[fingerprint] = struct{}{}
			return true
		},
	})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	sorts := []apiquery.SortTerm{{Name: "id", Direction: apiquery.Ascending}}
	token, err := codec.Encode(cursor.Payload{SchemaRevision: "v1", Direction: cursor.Forward,
		Sorts: sorts, Positions: []apiquery.Value{apiquery.StringValue("1")}, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if _, err := codec.Decode(token, "v1", sorts); err != nil {
		t.Fatalf("first Decode() error = %v", err)
	}
	if _, err := codec.Decode(token, "v1", sorts); !errors.Is(err, cursor.ErrReplay) {
		t.Fatalf("second Decode() error = %v, want ErrReplay", err)
	}
}

func TestKeyRotationIsRaceSafe(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	first := cursor.Key{ID: "first", Secret: []byte("0123456789abcdef0123456789abcdef")}
	second := cursor.Key{ID: "second", Secret: []byte("abcdef0123456789abcdef0123456789")}
	keyring, err := cursor.NewKeyring(first, second)
	if err != nil {
		t.Fatalf("NewKeyring() error = %v", err)
	}
	codec, err := cursor.NewCodec(cursor.Config{Version: "v1", Keys: keyring,
		MaxEncodedBytes: 512, MaxPositions: 2, MaxTTL: time.Hour, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	sorts := []apiquery.SortTerm{{Name: "id", Direction: apiquery.Ascending}}
	payload := cursor.Payload{SchemaRevision: "v1", Direction: cursor.Forward,
		Sorts: sorts, Positions: []apiquery.Value{apiquery.StringValue("1")}, ExpiresAt: now.Add(time.Minute)}
	var wait sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 50; iteration++ {
				token, encodeErr := codec.Encode(payload)
				if encodeErr != nil {
					t.Errorf("Encode() error = %v", encodeErr)
					return
				}
				if _, decodeErr := codec.Decode(token, "v1", sorts); decodeErr != nil {
					t.Errorf("Decode() error = %v", decodeErr)
					return
				}
			}
		}()
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		for iteration := 0; iteration < 50; iteration++ {
			if iteration%2 == 0 {
				_ = keyring.Rotate(second, first)
			} else {
				_ = keyring.Rotate(first, second)
			}
		}
	}()
	wait.Wait()
}

func TestCodecEnforcesExpiryVersionAndBounds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	codec := mustCodec(t, now)
	sorts := []apiquery.SortTerm{{Name: "id", Direction: apiquery.Ascending}}
	token, err := codec.Encode(cursor.Payload{
		SchemaRevision: "v1", Direction: cursor.Backward, Sorts: sorts,
		Positions: []apiquery.Value{apiquery.StringValue("1")},
		ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	codec.SetClock(func() time.Time { return now.Add(2 * time.Minute) })
	if _, err := codec.Decode(token, "v1", sorts); !errors.Is(err, cursor.ErrExpired) {
		t.Fatalf("Decode(expired) error = %v, want ErrExpired", err)
	}
	if _, err := codec.Decode(strings.Repeat("x", 513), "v1", sorts); !errors.Is(err, cursor.ErrInvalid) {
		t.Fatalf("Decode(oversized) error = %v, want ErrInvalid", err)
	}
}

func TestCodecRoundTripsNullSortPosition(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	codec := mustCodec(t, now)
	sorts := []apiquery.SortTerm{{Name: "deleted_at", Direction: apiquery.Ascending,
		Nulls: apiquery.NullsLast}}
	payload := cursor.Payload{SchemaRevision: "schema-v1", Direction: cursor.Forward, Sorts: sorts,
		Positions: []apiquery.Value{apiquery.NullValue()}, ExpiresAt: now.Add(time.Minute)}
	token, err := codec.Encode(payload)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(token, "schema-v1", sorts)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Positions[0].Type() != apiquery.TypeNull {
		t.Fatalf("position type = %q, want null", decoded.Positions[0].Type())
	}
}

func TestKeyRotationRetainsThenRetiresOldCursor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	oldKey := cursor.Key{ID: "old", Secret: []byte("0123456789abcdef0123456789abcdef")}
	newKey := cursor.Key{ID: "new", Secret: []byte("abcdef0123456789abcdef0123456789")}
	keyring, err := cursor.NewKeyring(oldKey)
	if err != nil {
		t.Fatalf("NewKeyring() error = %v", err)
	}
	codec, err := cursor.NewCodec(cursor.Config{
		Version: "v1", Keys: keyring, MaxEncodedBytes: 512,
		MaxPositions: 4, MaxTTL: time.Hour, Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	sorts := []apiquery.SortTerm{{Name: "id", Direction: apiquery.Ascending}}
	token, err := codec.Encode(cursor.Payload{SchemaRevision: "v1", Direction: cursor.Forward,
		Sorts: sorts, Positions: []apiquery.Value{apiquery.StringValue("1")}, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if err := keyring.Rotate(newKey, oldKey); err != nil {
		t.Fatalf("Rotate(retain old) error = %v", err)
	}
	if _, err := codec.Decode(token, "v1", sorts); err != nil {
		t.Fatalf("Decode(retained key) error = %v", err)
	}
	if err := keyring.Rotate(newKey); err != nil {
		t.Fatalf("Rotate(retire old) error = %v", err)
	}
	if _, err := codec.Decode(token, "v1", sorts); !errors.Is(err, cursor.ErrInvalid) {
		t.Fatalf("Decode(retired key) error = %v, want ErrInvalid", err)
	}
}

func mustCodec(t *testing.T, now time.Time) *cursor.Codec {
	t.Helper()

	keyring, err := cursor.NewKeyring(cursor.Key{
		ID: "primary", Secret: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("NewKeyring() error = %v", err)
	}
	codec, err := cursor.NewCodec(cursor.Config{
		Version: "v1", Keys: keyring, MaxEncodedBytes: 512,
		MaxPositions: 4, MaxTTL: 2 * time.Hour, Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	return codec
}

func replacement(value byte) string {
	if value == 'A' {
		return "B"
	}
	return "A"
}
