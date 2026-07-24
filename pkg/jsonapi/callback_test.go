package jsonapi

import (
	"errors"
	"strings"
	"testing"
)

type callbackFailureContract interface {
	error
	CallbackPhase() string
	CallbackPanicValue() (any, bool)
}

func TestConfiguredCodecRedactsAndPreservesCallbackFailures(t *testing.T) {
	t.Parallel()

	secret := errors.New("private extension value: shipment-123")
	codec := mustCodec(t, CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/extensions/version",
		Namespace: "version",
		Members: []MemberDefinition{{
			Scope: ResourceMemberScope,
			Name:  "version:id",
			Validate: func(any) error {
				return secret
			},
		}},
	}}})
	document := Document{Data: ResourceData(ResourceObject{
		Type:              "articles",
		ID:                "1",
		AdditionalMembers: Members{"version:id": "shipment-123"},
	})}

	_, err := codec.Marshal(document)
	if err == nil || !errors.Is(err, secret) {
		t.Fatalf("extension validator cause was not preserved: %T %v", err, err)
	}
	if strings.Contains(err.Error(), "shipment-123") {
		t.Fatalf("extension validator text leaked through public error: %v", err)
	}
	_, err = codec.Unmarshal([]byte(`{
		"jsonapi":{"ext":["https://example.com/extensions/version"]},
		"data":{"type":"articles","id":"1","version:id":"shipment-123"}
	}`))
	if err == nil || !errors.Is(err, secret) {
		t.Fatalf("decode validator cause was not preserved: %T %v", err, err)
	}
	if strings.Contains(err.Error(), "shipment-123") {
		t.Fatalf("decode validator text leaked through public error: %v", err)
	}

	profileSecret := errors.New("private profile value: customer-456")
	profileCodec := mustCodec(t, CodecOptions{Profiles: []ProfileDefinition{{
		URI: "https://example.com/profiles/private",
		ValidateDocument: func(Document) error {
			return profileSecret
		},
	}}})
	_, err = profileCodec.Marshal(Document{Data: NullData()})
	if err == nil || !errors.Is(err, profileSecret) {
		t.Fatalf("profile validator cause was not preserved: %T %v", err, err)
	}
	if strings.Contains(err.Error(), "customer-456") {
		t.Fatalf("profile validator text leaked through public error: %v", err)
	}
}

func TestConfiguredCodecConvertsCallbackPanics(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		codec    *Codec
		document Document
		phase    string
	}{
		"extension member": {
			codec: mustCodec(t, CodecOptions{Extensions: []ExtensionDefinition{{
				URI:       "https://example.com/extensions/version",
				Namespace: "version",
				Members: []MemberDefinition{{
					Scope: ResourceMemberScope,
					Name:  "version:id",
					Validate: func(any) error {
						panic("private extension panic")
					},
				}},
			}}}),
			document: Document{Data: ResourceData(ResourceObject{
				Type:              "articles",
				ID:                "1",
				AdditionalMembers: Members{"version:id": "1"},
			})},
			phase: "extension-member",
		},
		"profile": {
			codec: mustCodec(t, CodecOptions{Profiles: []ProfileDefinition{{
				URI: "https://example.com/profiles/private",
				ValidateDocument: func(Document) error {
					panic("private profile panic")
				},
			}}}),
			document: Document{Data: NullData()},
			phase:    "profile",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := marshalWithoutPanic(t, test.codec, test.document)
			assertCallbackPanic(t, err, test.phase)
		})
	}
}

func TestCursorPaginationConvertsCallbackPanics(t *testing.T) {
	t.Parallel()

	cursor := mustCursorPagination(t, CursorPaginationConfig{
		DefaultSize: 10,
		ValidateCursor: func(string) error {
			panic("private cursor panic")
		},
	})
	_, err := parseCursorWithoutPanic(t, cursor, ParameterFamily{
		"page[after]": {"opaque"},
	})
	assertCallbackPanic(t, err, "cursor")

	sortPagination := mustCursorPagination(t, CursorPaginationConfig{
		DefaultSize: 10,
		ValidateSort: func([]SortField) error {
			panic("private sort panic")
		},
	})
	_, err = parseCursorQueryWithoutPanic(t, sortPagination, Query{})
	assertCallbackPanic(t, err, "sort")
}

func mustCodec(t *testing.T, options CodecOptions) *Codec {
	t.Helper()
	codec, err := NewCodec(options)
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}
	return codec
}

func mustCursorPagination(t *testing.T, config CursorPaginationConfig) *CursorPagination {
	t.Helper()
	pagination, err := NewCursorPagination(config)
	if err != nil {
		t.Fatalf("construct cursor pagination: %v", err)
	}
	return pagination
}

func marshalWithoutPanic(t *testing.T, codec *Codec, document Document) (payload []byte, err error) {
	t.Helper()
	defer func() {
		if value := recover(); value != nil {
			t.Fatalf("configured codec callback escaped as panic: %v", value)
		}
	}()
	return codec.Marshal(document)
}

func parseCursorWithoutPanic(
	t *testing.T,
	pagination *CursorPagination,
	family ParameterFamily,
) (request CursorPageRequest, err error) {
	t.Helper()
	defer func() {
		if value := recover(); value != nil {
			t.Fatalf("cursor callback escaped as panic: %v", value)
		}
	}()
	return pagination.Parse(family)
}

func parseCursorQueryWithoutPanic(
	t *testing.T,
	pagination *CursorPagination,
	query Query,
) (request CursorPageRequest, err error) {
	t.Helper()
	defer func() {
		if value := recover(); value != nil {
			t.Fatalf("sort callback escaped as panic: %v", value)
		}
	}()
	return pagination.ParseQuery(query)
}

func assertCallbackPanic(t *testing.T, err error, phase string) {
	t.Helper()
	if err == nil {
		t.Fatal("callback panic was accepted")
	}
	var callbackFailure callbackFailureContract
	if !errors.As(err, &callbackFailure) || callbackFailure.CallbackPhase() != phase {
		t.Fatalf("unexpected callback panic error: %T %v", err, err)
	}
	value, panicked := callbackFailure.CallbackPanicValue()
	if !panicked || value == nil {
		t.Fatalf("callback panic value is not inspectable: %T %#v", err, callbackFailure)
	}
	if strings.Contains(err.Error(), "private") {
		t.Fatalf("callback panic value leaked through public error: %v", err)
	}
}
