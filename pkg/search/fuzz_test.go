package search_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func FuzzDocumentSource(f *testing.F) {
	f.Add([]byte(`{"name":"Helsinki"}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"nested":[1,null,"ä"]}`))
	f.Fuzz(func(t *testing.T, source []byte) {
		limits := search.DefaultLimits()
		limits.MaxSourceBytes = 4096
		_, _ = search.NewDocument("tenant", "locations", "id", 1, json.RawMessage(source), limits)
	})
}

func FuzzCursorDecode(f *testing.F) {
	now := time.Unix(1_800_000_000, 0)
	codec, _ := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, 4096)
	binding := search.CursorBinding{Tenant: "tenant", Index: "locations", QueryFingerprint: "query", IndexFingerprint: "mapping"}
	valid, _ := codec.Encode(binding, search.CursorState{PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`"id"`)}, ExpiresAt: now.Add(time.Minute)})
	f.Add(valid)
	f.Add("")
	f.Add("not-a-cursor")
	f.Fuzz(func(t *testing.T, token string) {
		_, _ = codec.Decode(token, binding, search.DefaultLimits())
	})
}
