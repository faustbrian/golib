package cursor

import (
	"testing"
	"time"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

func FuzzDecode(f *testing.F) {
	keyring, err := NewKeyring(Key{ID: "active", Secret: make([]byte, 32)})
	if err != nil {
		f.Fatal(err)
	}
	codec, err := NewCodec(Config{Version: "v1", Keys: keyring, MaxEncodedBytes: 4096,
		MaxPositions: 4, MaxStringBytes: 64, MaxTTL: time.Hour,
		Clock: func() time.Time { return time.Unix(1_700_000_000, 0) }})
	if err != nil {
		f.Fatal(err)
	}
	sorts := []apiquery.SortTerm{{Name: "id", Direction: apiquery.Ascending}}
	valid, err := codec.Encode(Payload{SchemaRevision: "schema-v1", Direction: Forward,
		Sorts: sorts, Positions: []apiquery.Value{apiquery.StringValue("record-1")},
		ExpiresAt: time.Unix(1_700_000_000, 0).Add(time.Minute)})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add("v1.active.invalid")
	f.Add(string([]byte{0xff, 0, 1}))
	f.Fuzz(func(t *testing.T, token string) {
		if len(token) > 4096 {
			t.Skip()
		}
		_, _ = codec.Decode(token, "schema-v1", sorts)
	})
}
