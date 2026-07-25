package jsonapi

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

func TestCanonicalValidDocumentFixturesRoundTrip(t *testing.T) {
	t.Parallel()

	fixtures := []string{
		"testdata/valid/compound-document.json",
		"testdata/valid/error-document.json",
	}
	for _, fixture := range fixtures {
		payload, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatalf("read fixture %s: %v", fixture, err)
		}
		payload = bytes.TrimSpace(payload)
		document, err := Unmarshal(payload)
		if err != nil {
			t.Fatalf("decode fixture %s: %v", fixture, err)
		}
		encoded, err := Marshal(document)
		if err != nil {
			t.Fatalf("encode fixture %s: %v", fixture, err)
		}
		if string(encoded) != string(payload) {
			t.Fatalf("fixture %s changed:\n got: %s\nwant: %s", fixture, encoded, payload)
		}
	}
}

func TestUnmarshalProducesCanonicalValidatedDocument(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"meta":{"requestId":"abc"},
		"included":[{
			"attributes":{"name":"Jane"},
			"id":"9",
			"type":"people"
		}],
		"data":{
			"relationships":{"author":{"data":{"id":"9","type":"people"}}},
			"attributes":{"title":"JSON:API"},
			"id":"1",
			"type":"articles"
		},
		"jsonapi":{"version":"1.1"}
	}`)

	document, err := Unmarshal(payload)
	if err != nil {
		t.Fatalf("unmarshal document: %v", err)
	}
	got, err := Marshal(document)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}

	want := `{"jsonapi":{"version":"1.1"},"data":{"type":"articles","id":"1","attributes":{"title":"JSON:API"},"relationships":{"author":{"data":{"type":"people","id":"9"}}}},"included":[{"type":"people","id":"9","attributes":{"name":"Jane"}}],"meta":{"requestId":"abc"}}`
	if string(got) != want {
		t.Fatalf("unexpected canonical JSON:\n got: %s\nwant: %s", got, want)
	}
}

func TestUnmarshalPreservesNullAndEmptyData(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"null primary data":       `{"data":null}`,
		"empty primary data":      `{"data":[]}`,
		"null relationship data":  `{"data":{"type":"articles","id":"1","relationships":{"author":{"data":null}}}}`,
		"empty relationship data": `{"data":{"type":"articles","id":"1","relationships":{"tags":{"data":[]}}}}`,
	}

	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			document, err := Unmarshal([]byte(payload))
			if err != nil {
				t.Fatalf("unmarshal document: %v", err)
			}
			got, err := Marshal(document)
			if err != nil {
				t.Fatalf("marshal document: %v", err)
			}
			if string(got) != payload {
				t.Fatalf("unexpected round trip: got %s, want %s", got, payload)
			}
		})
	}
}

func TestUnmarshalPreservesExactAttributeNumbers(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"data":{"type":"articles","id":"1","attributes":{"large":9007199254740993,"decimal":1.25}}}`)
	document, err := Unmarshal(payload)
	if err != nil {
		t.Fatalf("decode numeric attributes: %v", err)
	}
	encoded, err := Marshal(document)
	if err != nil {
		t.Fatalf("encode numeric attributes: %v", err)
	}
	want := `{"data":{"type":"articles","id":"1","attributes":{"decimal":1.25,"large":9007199254740993}}}`
	if string(encoded) != want {
		t.Fatalf("attribute numbers changed: got %s, want %s", encoded, want)
	}
}

func TestUnmarshalIgnoresAtMembers(t *testing.T) {
	t.Parallel()

	document, err := Unmarshal([]byte(`{
		"@context":"https://example.com/context",
		"data":{
			"type":"articles",
			"id":"1",
			"@annotation":{"internal":true},
			"attributes":{"title":"JSON:API","@language":"en"}
		}
	}`))
	if err != nil {
		t.Fatalf("unmarshal document: %v", err)
	}
	got, err := Marshal(document)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	want := `{"data":{"type":"articles","id":"1","attributes":{"title":"JSON:API"}}}`
	if string(got) != want {
		t.Fatalf("unexpected JSON: got %s, want %s", got, want)
	}
}

func TestUnmarshalIgnoresNestedAtMembersInsideArrays(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"data":{"type":"articles","id":"1","attributes":{"blocks":[{"@type":"Note","body":"kept"},["value",{"@id":"ignored","name":"kept"}]]}}}`)
	document, err := Unmarshal(payload)
	if err != nil {
		t.Fatalf("unmarshal document: %v", err)
	}
	encoded, err := Marshal(document)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	want := `{"data":{"type":"articles","id":"1","attributes":{"blocks":[{"body":"kept"},["value",{"name":"kept"}]]}}}`
	if string(encoded) != want {
		t.Fatalf("unexpected JSON: got %s, want %s", encoded, want)
	}
}

func TestUnmarshalRejectsMalformedDocuments(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		payload string
		path    string
		code    string
	}{
		"invalid JSON": {
			payload: `{"data":`,
			path:    "",
			code:    "syntax",
		},
		"root is not object": {
			payload: `[]`,
			path:    "",
			code:    "type",
		},
		"jsonapi is not object": {
			payload: `{"jsonapi":[],"data":null}`,
			path:    "/jsonapi",
			code:    "type",
		},
		"unknown jsonapi member": {
			payload: `{"jsonapi":{"unknown":true},"data":null}`,
			path:    "/jsonapi/unknown",
			code:    "unknown-member",
		},
		"jsonapi version is not string": {
			payload: `{"jsonapi":{"version":1},"data":null}`,
			path:    "/jsonapi/version",
			code:    "type",
		},
		"jsonapi ext is not array": {
			payload: `{"jsonapi":{"ext":null},"data":null}`,
			path:    "/jsonapi/ext",
			code:    "type",
		},
		"jsonapi ext item is not string": {
			payload: `{"jsonapi":{"ext":[1]},"data":null}`,
			path:    "/jsonapi/ext",
			code:    "type",
		},
		"jsonapi profile is not array": {
			payload: `{"jsonapi":{"profile":{}},"data":null}`,
			path:    "/jsonapi/profile",
			code:    "type",
		},
		"jsonapi meta is not object": {
			payload: `{"jsonapi":{"meta":[]},"data":null}`,
			path:    "/jsonapi/meta",
			code:    "type",
		},
		"unknown top-level member": {
			payload: `{"data":null,"unknown":true}`,
			path:    "/unknown",
			code:    "unknown-member",
		},
		"unknown resource member": {
			payload: `{"data":{"type":"articles","id":"1","unknown":true}}`,
			path:    "/data/unknown",
			code:    "unknown-member",
		},
		"resource collection is null": {
			payload: `{"data":null,"included":null}`,
			path:    "/included",
			code:    "type",
		},
		"resource collection item is not object": {
			payload: `{"data":[null]}`,
			path:    "/data/0",
			code:    "type",
		},
		"resource type is not string": {
			payload: `{"data":{"type":1,"id":"1"}}`,
			path:    "/data/type",
			code:    "type",
		},
		"resource id is not string": {
			payload: `{"data":{"type":"articles","id":1}}`,
			path:    "/data/id",
			code:    "type",
		},
		"resource lid is not string": {
			payload: `{"data":{"type":"articles","lid":1}}`,
			path:    "/data/lid",
			code:    "type",
		},
		"primary data has scalar shape": {
			payload: `{"data":"articles"}`,
			path:    "/data",
			code:    "type",
		},
		"relationship data has scalar shape": {
			payload: `{"data":{"type":"articles","id":"1","relationships":{"author":{"data":"9"}}}}`,
			path:    "/data/relationships/author/data",
			code:    "type",
		},
		"attributes is not object": {
			payload: `{"data":{"type":"articles","id":"1","attributes":[]}}`,
			path:    "/data/attributes",
			code:    "type",
		},
		"relationships is not object": {
			payload: `{"data":{"type":"articles","id":"1","relationships":[]}}`,
			path:    "/data/relationships",
			code:    "type",
		},
		"resource links is not object": {
			payload: `{"data":{"type":"articles","id":"1","links":[]}}`,
			path:    "/data/links",
			code:    "type",
		},
		"resource meta is not object": {
			payload: `{"data":{"type":"articles","id":"1","meta":[]}}`,
			path:    "/data/meta",
			code:    "type",
		},
		"relationship is not object": {
			payload: `{"data":{"type":"articles","id":"1","relationships":{"author":null}}}`,
			path:    "/data/relationships/author",
			code:    "type",
		},
		"unknown relationship member": {
			payload: `{"data":{"type":"articles","id":"1","relationships":{"author":{"unknown":true}}}}`,
			path:    "/data/relationships/author/unknown",
			code:    "unknown-member",
		},
		"relationship links is not object": {
			payload: `{"data":{"type":"articles","id":"1","relationships":{"author":{"links":[]}}}}`,
			path:    "/data/relationships/author/links",
			code:    "type",
		},
		"relationship meta is not object": {
			payload: `{"data":{"type":"articles","id":"1","relationships":{"author":{"meta":[]}}}}`,
			path:    "/data/relationships/author/meta",
			code:    "type",
		},
		"identifier item is not object": {
			payload: `{"data":{"type":"articles","id":"1","relationships":{"tags":{"data":[null]}}}}`,
			path:    "/data/relationships/tags/data/0",
			code:    "type",
		},
		"unknown identifier member": {
			payload: `{"data":{"type":"articles","id":"1","relationships":{"author":{"data":{"type":"people","id":"9","unknown":true}}}}}`,
			path:    "/data/relationships/author/data/unknown",
			code:    "unknown-member",
		},
		"identifier type is not string": {
			payload: `{"data":{"type":"articles","id":"1","relationships":{"author":{"data":{"type":1,"id":"9"}}}}}`,
			path:    "/data/relationships/author/data/type",
			code:    "type",
		},
		"identifier id is not string": {
			payload: `{"data":{"type":"articles","id":"1","relationships":{"author":{"data":{"type":"people","id":9}}}}}`,
			path:    "/data/relationships/author/data/id",
			code:    "type",
		},
		"identifier lid is not string": {
			payload: `{"data":{"type":"articles","id":"1","relationships":{"author":{"data":{"type":"people","lid":9}}}}}`,
			path:    "/data/relationships/author/data/lid",
			code:    "type",
		},
		"identifier meta is not object": {
			payload: `{"data":{"type":"articles","id":"1","relationships":{"author":{"data":{"type":"people","id":"9","meta":[]}}}}}`,
			path:    "/data/relationships/author/data/meta",
			code:    "type",
		},
		"links is not object": {
			payload: `{"data":null,"links":[]}`,
			path:    "/links",
			code:    "type",
		},
		"link has invalid shape": {
			payload: `{"data":null,"links":{"self":42}}`,
			path:    "/links/self",
			code:    "type",
		},
		"unknown link object member": {
			payload: `{"data":null,"links":{"self":{"href":"/articles","unknown":true}}}`,
			path:    "/links/self/unknown",
			code:    "unknown-member",
		},
		"link href is not string": {
			payload: `{"data":null,"links":{"self":{"href":1}}}`,
			path:    "/links/self/href",
			code:    "type",
		},
		"link rel is not string": {
			payload: `{"data":null,"links":{"self":{"href":"/articles","rel":1}}}`,
			path:    "/links/self/rel",
			code:    "type",
		},
		"link title is not string": {
			payload: `{"data":null,"links":{"self":{"href":"/articles","title":1}}}`,
			path:    "/links/self/title",
			code:    "type",
		},
		"link type is not string": {
			payload: `{"data":null,"links":{"self":{"href":"/articles","type":1}}}`,
			path:    "/links/self/type",
			code:    "type",
		},
		"link describedby has invalid shape": {
			payload: `{"data":null,"links":{"self":{"href":"/articles","describedby":1}}}`,
			path:    "/links/self/describedby",
			code:    "type",
		},
		"link hreflang has invalid shape": {
			payload: `{"data":null,"links":{"self":{"href":"/articles","hreflang":1}}}`,
			path:    "/links/self/hreflang",
			code:    "type",
		},
		"link hreflang item is not string": {
			payload: `{"data":null,"links":{"self":{"href":"/articles","hreflang":[1]}}}`,
			path:    "/links/self/hreflang",
			code:    "type",
		},
		"link meta is not object": {
			payload: `{"data":null,"links":{"self":{"href":"/articles","meta":[]}}}`,
			path:    "/links/self/meta",
			code:    "type",
		},
		"errors is not array": {
			payload: `{"errors":null}`,
			path:    "/errors",
			code:    "type",
		},
		"error item is not object": {
			payload: `{"errors":[null]}`,
			path:    "/errors/0",
			code:    "type",
		},
		"unknown error member": {
			payload: `{"errors":[{"unknown":true}]}`,
			path:    "/errors/0/unknown",
			code:    "unknown-member",
		},
		"error status is not string": {
			payload: `{"errors":[{"status":409}]}`,
			path:    "/errors/0/status",
			code:    "type",
		},
		"error source is not object": {
			payload: `{"errors":[{"source":[]}]}`,
			path:    "/errors/0/source",
			code:    "type",
		},
		"error links is not object": {
			payload: `{"errors":[{"links":[]}]}`,
			path:    "/errors/0/links",
			code:    "type",
		},
		"unknown error source member": {
			payload: `{"errors":[{"source":{"unknown":true}}]}`,
			path:    "/errors/0/source/unknown",
			code:    "unknown-member",
		},
		"error meta is not object": {
			payload: `{"errors":[{"meta":[]}]}`,
			path:    "/errors/0/meta",
			code:    "type",
		},
		"top-level meta is not object": {
			payload: `{"meta":[]}`,
			path:    "/meta",
			code:    "type",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := Unmarshal([]byte(test.payload))
			if err == nil {
				t.Fatal("expected decode error")
			}
			var decodeError *DecodeError
			if !errors.As(err, &decodeError) {
				t.Fatalf("expected DecodeError, got %T: %v", err, err)
			}
			if decodeError.Path != test.path || decodeError.Code != test.code {
				t.Fatalf(
					"unexpected error: got path %q code %q, want path %q code %q",
					decodeError.Path,
					decodeError.Code,
					test.path,
					test.code,
				)
			}
		})
	}
}

func TestMarshalRejectsInvalidDocument(t *testing.T) {
	t.Parallel()

	_, err := Marshal(Document{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
}

func TestUnmarshalReportsErrorSourceFieldsDeterministically(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"errors":[{"source":{
		"pointer":1,
		"parameter":2,
		"header":3
	}}]}`)
	for range 100 {
		_, err := Unmarshal(payload)
		var decodeError *DecodeError
		if !errors.As(err, &decodeError) {
			t.Fatalf("expected DecodeError, got %T: %v", err, err)
		}
		if decodeError.Path != "/errors/0/source/pointer" {
			t.Fatalf("unexpected first error source path: %q", decodeError.Path)
		}
	}
}
