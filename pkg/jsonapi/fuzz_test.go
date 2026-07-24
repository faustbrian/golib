package jsonapi

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
	"unicode/utf8"
)

func FuzzUnmarshal(f *testing.F) {
	seeds := [][]byte{
		[]byte(`{"data":null}`),
		[]byte(`{"data":[]}`),
		[]byte(`{"data":{"type":"articles","id":"1"}}`),
		[]byte(`{"errors":[{"status":"400","title":"Bad request"}]}`),
		[]byte(`{"data":null,"data":[]}`),
		[]byte(`{"data":`),
		{'{', '"', 'm', 'e', 't', 'a', '"', ':', '{', '"', 'x', '"', ':', '"', 0xff, '"', '}', '}'},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, payload []byte) {
		document, err := Unmarshal(payload)
		if err != nil {
			return
		}
		canonical, err := Marshal(document)
		if err != nil {
			t.Fatalf("accepted document cannot be marshaled: %v", err)
		}
		if _, err := Unmarshal(canonical); err != nil {
			t.Fatalf("canonical document cannot be decoded: %v", err)
		}
	})
}

func FuzzUnmarshalAtomic(f *testing.F) {
	seeds := [][]byte{
		[]byte(`{"atomic:operations":[{"op":"remove","href":"/articles/1"}]}`),
		[]byte(`{"atomic:results":[{}]}`),
		[]byte(`{"errors":[{"status":"409"}]}`),
		[]byte(`{"data":null}`),
		[]byte(`{"atomic:operations":[{"op":"add"}]}`),
		{'{', '"', 'm', 'e', 't', 'a', '"', ':', '{', '"', 'x', '"', ':', '"', 0xff, '"', '}', '}'},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, payload []byte) {
		document, err := UnmarshalAtomic(payload)
		if err != nil {
			return
		}
		canonical, err := MarshalAtomic(document)
		if err != nil {
			t.Fatalf("accepted Atomic document cannot be marshaled: %v", err)
		}
		if _, err := UnmarshalAtomic(canonical); err != nil {
			t.Fatalf("canonical Atomic document cannot be decoded: %v", err)
		}
	})
}

func FuzzParseQuery(f *testing.F) {
	seeds := []string{
		"include=author.comments&fields%5Barticles%5D=title,body&sort=-createdAt",
		"page%5Bsize%5D=25&page%5Bafter%5D=opaque&filter%5Bstatus%5D=published",
		"include=%",
		"fields%5B%5D=title",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, rawQuery string) {
		values, err := url.ParseQuery(rawQuery)
		if err != nil {
			return
		}
		first, err := ParseQuery(values)
		if err != nil {
			return
		}
		second, err := ParseQuery(values)
		if err != nil {
			t.Fatalf("accepted query cannot be parsed again: %v", err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("query parsing is not deterministic: %#v != %#v", first, second)
		}
	})
}

func FuzzCursorPaginationQuery(f *testing.F) {
	pagination, err := NewCursorPagination(CursorPaginationConfig{
		DefaultSize: 20,
		MaxSize:     100,
		AllowRange:  true,
	})
	if err != nil {
		f.Fatal(err)
	}
	seeds := []string{
		"page%5Bsize%5D=25&page%5Bafter%5D=opaque",
		"page%5Bbefore%5D=older",
		"page%5Bafter%5D=a&page%5Bbefore%5D=b",
		"page%5Bsize%5D=-1",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, rawQuery string) {
		values, err := url.ParseQuery(rawQuery)
		if err != nil {
			return
		}
		query, err := ParseQuery(values)
		if err != nil {
			return
		}
		request, err := pagination.ParseQuery(query)
		if err != nil {
			return
		}
		if request.Size < 1 || request.Size > 100 {
			t.Fatalf("accepted page size is outside configured bounds: %d", request.Size)
		}
	})
}

func FuzzNegotiation(f *testing.F) {
	const extension = "https://example.com/extensions/version"
	const profile = "https://example.com/profiles/timestamps"
	negotiator, err := NewNegotiator([]string{extension}, []string{profile})
	if err != nil {
		f.Fatal(err)
	}
	seeds := []string{
		MediaTypeJSONAPI,
		MediaTypeJSONAPI + `;ext="` + extension + `"`,
		MediaTypeJSONAPI + `;profile="` + profile + `"`,
		"application/json, */*;q=0.5",
		MediaTypeJSONAPI + ";q=0, */*;q=1",
		"not a media type",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, header string) {
		if mediaType, err := negotiator.CheckContentType(header); err == nil {
			if _, err := negotiator.CheckContentType(mediaType.String()); err != nil {
				t.Fatalf("canonical accepted content type was rejected: %v", err)
			}
		}
		if selected, err := negotiator.NegotiateAccept(header); err == nil {
			if _, err := negotiator.CheckContentType(selected.ContentType); err != nil {
				t.Fatalf("negotiated content type was rejected: %v", err)
			}
		}
	})
}

func FuzzConstructedDocumentValidation(f *testing.F) {
	f.Add("articles", "1", "title", "Hello")
	f.Add("@type", "", "bad/name", "\x00")

	f.Fuzz(func(t *testing.T, resourceType, id, field, value string) {
		document := Document{Data: ResourceData(ResourceObject{
			Type:       resourceType,
			ID:         id,
			Attributes: Attributes{field: value},
		})}
		first := document.Validate()
		second := document.Validate()
		if (first == nil) != (second == nil) {
			t.Fatalf("constructed validation is not deterministic: %v != %v", first, second)
		}
	})
}

func FuzzMemberRegistry(f *testing.F) {
	f.Add("version", "id", `"1"`)
	f.Add("bad:name", "@value", `{}`)

	f.Fuzz(func(t *testing.T, namespace, suffix, rawValue string) {
		var value any
		if err := json.Unmarshal([]byte(rawValue), &value); err != nil {
			return
		}
		name := namespace + ":" + suffix
		codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
			URI:       "https://example.com/extensions/fuzz",
			Namespace: namespace,
			Members: []MemberDefinition{{
				Scope: ResourceMemberScope,
				Name:  name,
			}},
		}}})
		if err != nil {
			return
		}
		document := Document{
			JSONAPI: &JSONAPI{Ext: []string{"https://example.com/extensions/fuzz"}},
			Data: ResourceData(ResourceObject{
				Type:              "articles",
				ID:                "1",
				AdditionalMembers: Members{name: value},
			}),
		}
		payload, err := codec.Marshal(document)
		if err != nil {
			return
		}
		if _, err := codec.Unmarshal(payload); err != nil {
			t.Fatalf("registered member did not round trip: %v", err)
		}
	})
}

func FuzzCursorMetadata(f *testing.F) {
	f.Add("page", int64(0), int64(1), true, "opaque")
	f.Add("customPage", int64(-1), int64(0), false, "")

	f.Fuzz(func(t *testing.T, member string, total, estimate int64, truncated bool, cursor string) {
		metadata := CursorPageMeta{
			RangeTruncated: &truncated,
			Total:          &total,
			EstimatedTotal: &CursorEstimatedTotal{BestGuess: &estimate},
		}
		meta, err := metadata.MetaAs(member)
		if err == nil {
			decoded, present, parseErr := ParseCursorPageMetaAs(meta, member)
			if parseErr != nil || !present || !reflect.DeepEqual(decoded, metadata) {
				t.Fatalf("page metadata did not round trip: %#v present=%v err=%v", decoded, present, parseErr)
			}
		}
		item, err := CursorItemMetaAs(member, cursor)
		if err != nil {
			return
		}
		decodedCursor, present, err := ParseCursorItemMetaAs(item, member)
		if err != nil || !present || decodedCursor != cursor {
			t.Fatalf("item cursor did not round trip: %q present=%v err=%v", decodedCursor, present, err)
		}
	})
}

func FuzzMarshalUnmarshalRoundTrip(f *testing.F) {
	f.Add("articles", "1", "Hello", "opaque")
	f.Add("invalid/type", "", "\x00", "")

	f.Fuzz(func(t *testing.T, resourceType, id, title, cursor string) {
		if !utf8.ValidString(resourceType) || !utf8.ValidString(id) ||
			!utf8.ValidString(title) || !utf8.ValidString(cursor) {
			return
		}
		document := Document{Data: ResourceData(ResourceObject{
			Type:       resourceType,
			ID:         id,
			Attributes: Attributes{"title": title},
			Meta:       CursorItemMeta(cursor),
		})}
		payload, err := Marshal(document)
		if err != nil {
			return
		}
		decoded, err := Unmarshal(payload)
		if err != nil {
			t.Fatalf("constructed document could not be decoded: %v", err)
		}
		canonical, err := Marshal(decoded)
		if err != nil {
			t.Fatalf("decoded document could not be marshaled: %v", err)
		}
		if !reflect.DeepEqual(payload, canonical) {
			t.Fatalf("round trip changed canonical bytes: %q != %q", payload, canonical)
		}
	})
}
