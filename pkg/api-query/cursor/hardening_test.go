package cursor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

func TestKeyAndCodecConfigurationFailureMatrix(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	for _, key := range []Key{{ID: "bad.id", Secret: secret}, {ID: "short", Secret: []byte("short")},
		{ID: strings.Repeat("x", 65), Secret: secret}} {
		if _, err := NewKeyring(key); !errors.Is(err, ErrInvalid) {
			t.Fatalf("NewKeyring(%q) error = %v", key.ID, err)
		}
	}
	if _, err := NewKeyring(Key{ID: "same", Secret: secret}, Key{ID: "same", Secret: secret}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("NewKeyring(duplicate) error = %v", err)
	}
	keys, _ := NewKeyring(Key{ID: "valid_ID-1", Secret: secret})
	configs := []Config{
		{}, {Version: "bad.version", Keys: keys, MaxEncodedBytes: 1, MaxPositions: 1, MaxTTL: time.Second},
		{Version: "v1", Keys: nil, MaxEncodedBytes: 1, MaxPositions: 1, MaxTTL: time.Second},
		{Version: "v1", Keys: keys, MaxPositions: 1, MaxTTL: time.Second},
		{Version: "v1", Keys: keys, MaxEncodedBytes: 1, MaxTTL: time.Second},
		{Version: "v1", Keys: keys, MaxEncodedBytes: 1, MaxPositions: 1},
	}
	for index, config := range configs {
		if _, err := NewCodec(config); !errors.Is(err, ErrInvalid) {
			t.Fatalf("NewCodec(case %d) error = %v", index, err)
		}
	}
	if _, _, ok := (*Keyring)(nil).activeKey(); ok {
		t.Fatal("nil keyring has active key")
	}
	if _, ok := (*Keyring)(nil).key("x"); ok {
		t.Fatal("nil keyring resolved key")
	}
	if _, err := NewCodec(Config{Version: "v1", Keys: &Keyring{}, MaxEncodedBytes: 1,
		MaxPositions: 1, MaxTTL: time.Second}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("NewCodec(empty keyring) error = %v", err)
	}
	defaults, err := NewCodec(Config{Version: "v1", Keys: keys, MaxEncodedBytes: 512,
		MaxPositions: 1, MaxTTL: time.Second})
	if err != nil || defaults.clock == nil || defaults.random == nil || defaults.maxStringBytes != 256 {
		t.Fatalf("NewCodec(defaults) = %#v, %v", defaults, err)
	}
	defaults.SetClock(nil)
}

func TestEncodePayloadBoundMatrix(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	codec := internalCodec(t, now, Config{})
	valid := Payload{SchemaRevision: "v1", Direction: Forward,
		Sorts:     []apiquery.SortTerm{{Name: "id", Direction: apiquery.Ascending}},
		Positions: []apiquery.Value{apiquery.StringValue("1")}, ExpiresAt: now.Add(time.Minute)}
	invalid := []Payload{
		{Direction: Forward, Sorts: valid.Sorts, Positions: valid.Positions, ExpiresAt: valid.ExpiresAt},
		{SchemaRevision: strings.Repeat("s", 17), Direction: Forward, Sorts: valid.Sorts, Positions: valid.Positions, ExpiresAt: valid.ExpiresAt},
		{SchemaRevision: "v1", Policy: strings.Repeat("p", 17), Direction: Forward, Sorts: valid.Sorts, Positions: valid.Positions, ExpiresAt: valid.ExpiresAt},
		{SchemaRevision: "v1", Direction: Forward, Sorts: nil, Positions: nil, ExpiresAt: valid.ExpiresAt},
		{SchemaRevision: "v1", Direction: Forward, Sorts: append(valid.Sorts, valid.Sorts...), Positions: append(valid.Positions, valid.Positions...), ExpiresAt: valid.ExpiresAt},
		{SchemaRevision: "v1", Direction: Forward, Sorts: valid.Sorts, Positions: append(valid.Positions, valid.Positions...), ExpiresAt: valid.ExpiresAt},
		{SchemaRevision: "v1", Direction: Forward, Sorts: valid.Sorts, Positions: valid.Positions, ExpiresAt: now},
		{SchemaRevision: "v1", Direction: Forward, Sorts: valid.Sorts, Positions: valid.Positions, ExpiresAt: now.Add(2 * time.Hour)},
		{SchemaRevision: "v1", Direction: Direction("sideways"), Sorts: valid.Sorts, Positions: valid.Positions, ExpiresAt: valid.ExpiresAt},
		{SchemaRevision: "v1", Direction: Forward, Sorts: []apiquery.SortTerm{{Direction: apiquery.Ascending}}, Positions: valid.Positions, ExpiresAt: valid.ExpiresAt},
		{SchemaRevision: "v1", Direction: Forward, Sorts: []apiquery.SortTerm{{Name: strings.Repeat("n", 17), Direction: apiquery.Ascending}}, Positions: valid.Positions, ExpiresAt: valid.ExpiresAt},
		{SchemaRevision: "v1", Direction: Forward, Sorts: []apiquery.SortTerm{{Name: "id", Direction: apiquery.Direction("sideways")}}, Positions: valid.Positions, ExpiresAt: valid.ExpiresAt},
		{SchemaRevision: "v1", Direction: Forward, Sorts: []apiquery.SortTerm{{Name: "id", Direction: apiquery.Ascending, Nulls: apiquery.NullOrder("sideways")}}, Positions: valid.Positions, ExpiresAt: valid.ExpiresAt},
		{SchemaRevision: "v1", Direction: Forward, Sorts: valid.Sorts, Positions: []apiquery.Value{apiquery.StringValue(strings.Repeat("v", 17))}, ExpiresAt: valid.ExpiresAt},
		{SchemaRevision: "v1", Direction: Forward, Sorts: valid.Sorts, Positions: []apiquery.Value{{}}, ExpiresAt: valid.ExpiresAt},
	}
	for index, payload := range invalid {
		if _, err := codec.Encode(payload); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Encode(case %d) error = %v", index, err)
		}
	}
	codec.random = errorReader{}
	if _, err := codec.Encode(valid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Encode(random failure) error = %v", err)
	}
	codec.random = bytes.NewReader(make([]byte, 12))
	codec.maxEncodedBytes = 220
	if _, err := codec.Encode(valid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Encode(size) error = %v", err)
	}
	codec.maxEncodedBytes = 512
	codec.keys.mu.Lock()
	codec.keys.keys = nil
	codec.keys.mu.Unlock()
	if _, err := codec.Encode(valid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Encode(missing key) error = %v", err)
	}
	codec.keys.mu.Lock()
	codec.keys.keys = map[string][]byte{"primary": []byte("short")}
	codec.keys.mu.Unlock()
	if _, err := codec.Encode(valid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Encode(invalid key) error = %v", err)
	}

	late := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	lateCodec := internalCodec(t, late, Config{})
	invalidTime := valid
	invalidTime.ExpiresAt = late.Add(time.Second)
	if _, err := lateCodec.Encode(invalidTime); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Encode(time marshal) error = %v", err)
	}
}

func TestDecodeEnvelopeFailureMatrix(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	codec := internalCodec(t, now, Config{})
	sorts := []apiquery.SortTerm{{Name: "id", Direction: apiquery.Ascending}}
	payload := Payload{SchemaRevision: "v1", Direction: Forward, Sorts: sorts,
		Positions: []apiquery.Value{apiquery.StringValue("1")}, ExpiresAt: now.Add(time.Minute)}
	token, _ := codec.Encode(payload)
	for _, value := range []string{"", strings.Repeat("x", 513), "one.two", "v2.key.data",
		"v1.unknown.data", "v1.primary.%", "v1.primary." + base64.RawURLEncoding.EncodeToString([]byte("short"))} {
		if _, err := codec.Decode(value, "v1", sorts); err == nil {
			t.Fatalf("Decode(%q) accepted", value)
		}
	}
	if _, err := codec.Decode(token, "v1", []apiquery.SortTerm{{Name: "other", Direction: apiquery.Ascending}}); !errors.Is(err, ErrSort) {
		t.Fatalf("Decode(sort mismatch) error = %v", err)
	}
	if _, err := codec.Decode(token, "v1", append(sorts, sorts...)); !errors.Is(err, ErrSort) {
		t.Fatalf("Decode(sort length) error = %v", err)
	}

	validWire := wirePayload{Version: "v1", SchemaRevision: "v1", Direction: Forward,
		Sorts: sorts, Positions: payload.Positions, ExpiresAt: payload.ExpiresAt}
	wrongVersion := validWire
	wrongVersion.Version = "v2"
	invalidPayload := validWire
	invalidPayload.Direction = Direction("sideways")
	for index, plain := range [][]byte{[]byte(`{`), []byte(`{"unknown":1}`),
		append(mustJSON(t, validWire), []byte(` true`)...), mustJSON(t, wrongVersion), mustJSON(t, invalidPayload)} {
		raw := sealPlain(t, plain)
		if _, err := codec.Decode(raw, "v1", sorts); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Decode(raw case %d) error = %v", index, err)
		}
	}
}

func TestInternalHelpersAndPageFailures(t *testing.T) {
	t.Parallel()

	if _, err := newAEAD([]byte("short")); err == nil {
		t.Fatal("newAEAD accepted short key")
	}
	decoder := json.NewDecoder(strings.NewReader("#"))
	if ensureJSONEnd(decoder) == nil {
		t.Fatal("ensureJSONEnd accepted malformed trailing data")
	}
	if equalSorts(nil, []apiquery.SortTerm{{}}) {
		t.Fatal("equalSorts accepted length mismatch")
	}
	if equalSorts([]apiquery.SortTerm{{Name: "a"}}, []apiquery.SortTerm{{Name: "b"}}) {
		t.Fatal("equalSorts accepted term mismatch")
	}
	if !validKeyID("ABC_123-x") || validKeyID("") || validKeyID("bad.id") {
		t.Fatal("validKeyID contract failed")
	}

	failure := errors.New("encode boundary")
	if _, err := BuildPage([]int{1}, true, false, func(int, Direction) (string, error) { return "", failure }); !errors.Is(err, failure) {
		t.Fatalf("BuildPage(previous failure) error = %v", err)
	}
	if _, err := BuildPage([]int{1}, false, true, func(int, Direction) (string, error) { return "", failure }); !errors.Is(err, failure) {
		t.Fatalf("BuildPage(next failure) error = %v", err)
	}
}

func internalCodec(t *testing.T, now time.Time, extra Config) *Codec {
	t.Helper()
	keys, err := NewKeyring(Key{ID: "primary", Secret: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	config := Config{Version: "v1", Keys: keys, MaxEncodedBytes: 512, MaxPositions: 1,
		MaxStringBytes: 16, MaxTTL: time.Hour, Clock: func() time.Time { return now }, Random: bytes.NewReader(make([]byte, 1024))}
	if extra.ReplayGuard != nil {
		config.ReplayGuard = extra.ReplayGuard
	}
	codec, err := NewCodec(config)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func sealPlain(t *testing.T, plain []byte) string {
	t.Helper()
	aead, err := newAEAD([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	sealed := aead.Seal(nonce, nonce, plain, []byte("v1.primary"))
	return "v1.primary." + base64.RawURLEncoding.EncodeToString(sealed)
}

func mustJSON(t *testing.T, value wirePayload) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("random failed") }
