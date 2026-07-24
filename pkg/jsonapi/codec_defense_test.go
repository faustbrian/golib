package jsonapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestInternalDecodersRejectMissingOrTruncatedValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		decode func() error
		path   string
	}{
		{
			decode: func() error { _, err := decodePrimaryData(nil, "/data"); return err },
			path:   "/data",
		},
		{
			decode: func() error { _, err := decodeRelationshipData(nil, "/data"); return err },
			path:   "/data",
		},
		{
			decode: func() error { _, err := decodeLink(nil, "/links/self"); return err },
			path:   "/links/self",
		},
		{
			decode: func() error { _, err := decodeHreflang(nil, "/links/self/hreflang"); return err },
			path:   "/links/self/hreflang",
		},
		{
			decode: func() error { _, err := decodeObject([]byte("{"), "/object"); return err },
			path:   "/object",
		},
		{
			decode: func() error { _, err := decodeRelationshipData([]byte("["), "/data"); return err },
			path:   "/data",
		},
		{
			decode: func() error { _, err := decodeLink([]byte(`"`), "/links/self"); return err },
			path:   "/links/self",
		},
		{
			decode: func() error { _, err := decodeLink([]byte("{"), "/links/self"); return err },
			path:   "/links/self",
		},
		{
			decode: func() error { _, err := decodeHreflang([]byte(`"`), "/links/self/hreflang"); return err },
			path:   "/links/self/hreflang",
		},
	}
	for _, test := range tests {
		err := test.decode()
		var decodeError *DecodeError
		if !errors.As(err, &decodeError) || decodeError.Path != test.path {
			t.Fatalf("unexpected defensive decode error: %T %#v", err, decodeError)
		}
	}
}

func TestDuplicateScannerRejectsTruncatedAndUnexpectedTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		payload string
		path    string
	}{
		{payload: "", path: ""},
		{payload: `{"member":`, path: "/member"},
		{payload: `[`, path: ""},
		{payload: `}`, path: ""},
		{payload: `{1`, path: ""},
	}
	for _, test := range tests {
		decoder := json.NewDecoder(bytes.NewBufferString(test.payload))
		err := scanJSONValue(decoder, "")
		var decodeError *DecodeError
		if !errors.As(err, &decodeError) || decodeError.Path != test.path ||
			decodeError.Code != "syntax" {
			t.Fatalf("unexpected scan error for %q: %T %#v", test.payload, err, decodeError)
		}
	}

	decoder := json.NewDecoder(bytes.NewBufferString(`[]`))
	if _, err := decoder.Token(); err != nil {
		t.Fatalf("enter array: %v", err)
	}
	err := scanJSONValue(decoder, "/unexpected")
	var decodeError *DecodeError
	if !errors.As(err, &decodeError) || decodeError.Path != "/unexpected" ||
		decodeError.Code != "syntax" {
		t.Fatalf("unexpected closing delimiter error: %T %#v", err, decodeError)
	}
}

func TestLinkMarshalHandlesEmptyScalarHreflang(t *testing.T) {
	t.Parallel()

	link := LinkFromObject(LinkObject{
		Href:     "/articles",
		Hreflang: &LinkHreflang{},
	})
	payload, err := link.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal empty scalar hreflang: %v", err)
	}
	if got, want := string(payload), `{"href":"/articles","hreflang":""}`; got != want {
		t.Fatalf("unexpected link: got %s, want %s", got, want)
	}
}

func TestUnmarshalIgnoresAtMembersInDefinedContainers(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"@context":{},
		"data":{
			"type":"articles",
			"id":"1",
			"relationships":{
				"@annotation":{},
				"author":{"data":null}
			},
			"links":{"@annotation":{},"self":"/articles/1"},
			"meta":{"@annotation":{},"kept":true}
		},
		"links":{"@annotation":{},"self":"/articles/1"},
		"meta":{"@annotation":{},"kept":true}
	}`)
	document, err := Unmarshal(payload)
	if err != nil {
		t.Fatalf("decode @-member document: %v", err)
	}
	encoded, err := Marshal(document)
	if err != nil {
		t.Fatalf("encode @-member document: %v", err)
	}
	want := `{"links":{"self":"/articles/1"},"data":{"type":"articles","id":"1","relationships":{"author":{"data":null}},"links":{"self":"/articles/1"},"meta":{"kept":true}},"meta":{"kept":true}}`
	if string(encoded) != want {
		t.Fatalf("unexpected stripped document: got %s, want %s", encoded, want)
	}
}

func TestUnmarshalPreservesNullLink(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"links":{"self":null},"data":null}`)
	document, err := Unmarshal(payload)
	if err != nil {
		t.Fatalf("decode null link: %v", err)
	}
	encoded, err := Marshal(document)
	if err != nil || string(encoded) != string(payload) {
		t.Fatalf("unexpected null link round trip: got %s, err %v", encoded, err)
	}
}

func TestAllDocumentCodecsRejectInvalidUTF8(t *testing.T) {
	t.Parallel()

	payload := []byte{'{', '"', 'm', 'e', 't', 'a', '"', ':', '{', '"', 'x', '"', ':', '"', 0xff, '"', '}', '}'}
	codec, err := NewCodec(CodecOptions{})
	if err != nil {
		t.Fatalf("construct configured codec: %v", err)
	}
	tests := map[string]func([]byte) error{
		"core": func(payload []byte) error {
			_, err := Unmarshal(payload)
			return err
		},
		"atomic": func(payload []byte) error {
			_, err := UnmarshalAtomic(payload)
			return err
		},
		"configured": func(payload []byte) error {
			_, err := codec.Unmarshal(payload)
			return err
		},
	}
	for name, decode := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := decode(payload)
			var decodeError *DecodeError
			if !errors.As(err, &decodeError) || decodeError.Code != "encoding" {
				t.Fatalf("expected UTF-8 encoding error, got %T: %#v", err, decodeError)
			}
		})
	}
}
