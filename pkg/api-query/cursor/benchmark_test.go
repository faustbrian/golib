package cursor

import (
	"testing"
	"time"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

func BenchmarkCodec(b *testing.B) {
	now := time.Unix(1_700_000_000, 0)
	keyring, err := NewKeyring(Key{ID: "active", Secret: make([]byte, 32)})
	if err != nil {
		b.Fatal(err)
	}
	codec, err := NewCodec(Config{Version: "v1", Keys: keyring, MaxEncodedBytes: 4096,
		MaxPositions: 4, MaxStringBytes: 64, MaxTTL: time.Hour,
		Clock: func() time.Time { return now }})
	if err != nil {
		b.Fatal(err)
	}
	sorts := []apiquery.SortTerm{{Name: "created_at", Direction: apiquery.Descending},
		{Name: "id", Direction: apiquery.Ascending}}
	payload := Payload{SchemaRevision: "schema-v1", Direction: Forward, Sorts: sorts,
		Positions: []apiquery.Value{apiquery.TimeValue(now), apiquery.StringValue("record-1")},
		ExpiresAt: now.Add(time.Minute)}
	token, err := codec.Encode(payload)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("encode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, encodeErr := codec.Encode(payload); encodeErr != nil {
				b.Fatal(encodeErr)
			}
		}
	})
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, decodeErr := codec.Decode(token, "schema-v1", sorts); decodeErr != nil {
				b.Fatal(decodeErr)
			}
		}
	})
}

func TestCodecAllocationBudgets(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	keyring, err := NewKeyring(Key{ID: "active", Secret: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := NewCodec(Config{Version: "v1", Keys: keyring, MaxEncodedBytes: 4096,
		MaxPositions: 2, MaxStringBytes: 64, MaxTTL: time.Hour,
		Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	sorts := []apiquery.SortTerm{{Name: "id", Direction: apiquery.Ascending}}
	payload := Payload{SchemaRevision: "v1", Direction: Forward, Sorts: sorts,
		Positions: []apiquery.Value{apiquery.StringValue("record-1")}, ExpiresAt: now.Add(time.Minute)}
	token, err := codec.Encode(payload)
	if err != nil {
		t.Fatal(err)
	}
	encodeAllocs := testing.AllocsPerRun(100, func() { _, _ = codec.Encode(payload) })
	decodeAllocs := testing.AllocsPerRun(100, func() { _, _ = codec.Decode(token, "v1", sorts) })
	if encodeAllocs > 30 || decodeAllocs > 70 {
		t.Fatalf("allocations: encode=%.0f/30 decode=%.0f/70", encodeAllocs, decodeAllocs)
	}
}
