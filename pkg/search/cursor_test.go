package search_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func TestCursorRoundTripBindsQueryTenantIndexAndSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC)
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, 4096)
	if err != nil {
		t.Fatalf("NewCursorCodec() error = %v", err)
	}
	binding := search.CursorBinding{
		Tenant: "tenant-a", Index: "locations-read", QueryFingerprint: "query-sha256", IndexFingerprint: "mapping-v3",
	}
	state := search.CursorState{
		PointInTime: "pit-secret", SortValues: []json.RawMessage{json.RawMessage(`1234`), json.RawMessage(`"location-7"`)},
		Page: 2, Items: 50, Bytes: 8192, ExpiresAt: now.Add(time.Minute),
	}

	token, err := codec.Encode(binding, state)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := codec.Decode(token, binding, search.DefaultLimits())
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.PointInTime != state.PointInTime || decoded.Page != state.Page || decoded.Items != state.Items || decoded.Bytes != state.Bytes {
		t.Fatalf("Decode() = %#v", decoded)
	}
	decoded.SortValues[0][0] = '9'
	if string(state.SortValues[0]) != "1234" {
		t.Fatal("decoded sort values alias caller state")
	}
}

func TestCursorRejectsTamperingExpiryBindingAndTraversalLimits(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC)
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, 4096)
	if err != nil {
		t.Fatal(err)
	}
	binding := search.CursorBinding{Tenant: "tenant-a", Index: "events", QueryFingerprint: "query-a", IndexFingerprint: "mapping-v1"}
	state := search.CursorState{PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`"event-1"`)}, Page: 1, Items: 10, Bytes: 100, ExpiresAt: now.Add(time.Minute)}
	token, err := codec.Encode(binding, state)
	if err != nil {
		t.Fatal(err)
	}

	tampered := token[:len(token)-1] + "A"
	if _, err := codec.Decode(tampered, binding, search.DefaultLimits()); !errors.Is(err, search.ErrInvalidCursor) {
		t.Fatalf("tampered Decode() error = %v", err)
	}

	changed := binding
	changed.QueryFingerprint = "query-b"
	if _, err := codec.Decode(token, changed, search.DefaultLimits()); !errors.Is(err, search.ErrCursorBinding) {
		t.Fatalf("query-bound Decode() error = %v", err)
	}
	changed = binding
	changed.IndexFingerprint = "mapping-v2"
	if _, err := codec.Decode(token, changed, search.DefaultLimits()); !errors.Is(err, search.ErrIndexChanged) {
		t.Fatalf("index-bound Decode() error = %v", err)
	}

	now = now.Add(2 * time.Minute)
	if _, err := codec.Decode(token, binding, search.DefaultLimits()); !errors.Is(err, search.ErrCursorExpired) {
		t.Fatalf("expired Decode() error = %v", err)
	}

	now = now.Add(-2 * time.Minute)
	limits := search.DefaultLimits()
	limits.MaxPages = 1
	state.Page = 2
	limited, err := codec.Encode(binding, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(limited, binding, limits); !errors.Is(err, search.ErrPageLimit) {
		t.Fatalf("limited Decode() error = %v", err)
	}

	state.Page = 1
	state.ExpiresAt = now.Add(limits.MaxCursorDuration + time.Nanosecond)
	overlong, err := codec.Encode(binding, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(overlong, binding, limits); !errors.Is(err, search.ErrPageLimit) {
		t.Fatalf("overlong Decode() error = %v, want ErrPageLimit", err)
	}
}

func TestCursorCodecRejectsWeakKeysAndOversizedTokens(t *testing.T) {
	t.Parallel()

	if _, err := search.NewCursorCodec([]byte("short"), time.Now, 1024); !errors.Is(err, search.ErrInvalidCursorCodec) {
		t.Fatalf("NewCursorCodec() error = %v", err)
	}
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), time.Now, 32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode("a-token-larger-than-thirty-two-bytes-long", search.CursorBinding{}, search.DefaultLimits()); !errors.Is(err, search.ErrInvalidCursor) {
		t.Fatalf("Decode() error = %v", err)
	}
}

func TestCursorDecodeRejectsUnboundedLimits(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC)
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, 4096)
	if err != nil {
		t.Fatal(err)
	}
	binding := search.CursorBinding{Tenant: "tenant", Index: "events", QueryFingerprint: "query", IndexFingerprint: "index"}
	token, err := codec.Encode(binding, search.CursorState{PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`"id"`)}, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(token, binding, search.Limits{MaxCursorDuration: time.Hour}); !errors.Is(err, search.ErrPageLimit) {
		t.Fatalf("Decode() error = %v, want ErrPageLimit", err)
	}
}
