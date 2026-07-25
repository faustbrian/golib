package jsonapi

import (
	"errors"
	"testing"
)

func TestCodecRoundTripsRegisteredResourceExtensionMember(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext/version",
		Namespace: "version",
		Members: []MemberDefinition{{
			Scope: ResourceMemberScope,
			Name:  "version:id",
			Validate: func(value any) error {
				if _, ok := value.(string); !ok {
					return errors.New("version id must be a string")
				}
				return nil
			},
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	payload := []byte(`{
		"data":{"type":"articles","id":"1","version:id":"42"},
		"jsonapi":{"version":"1.1","ext":["https://example.com/ext/version"]}
	}`)
	document, err := codec.Unmarshal(payload)
	if err != nil {
		t.Fatalf("decode extension document: %v", err)
	}
	resource := document.Data.one
	if resource == nil || resource.AdditionalMembers["version:id"] != "42" {
		t.Fatalf("extension member was not preserved: %#v", resource)
	}

	encoded, err := codec.Marshal(document)
	if err != nil {
		t.Fatalf("encode extension document: %v", err)
	}
	want := `{"jsonapi":{"version":"1.1","ext":["https://example.com/ext/version"]},"data":{"type":"articles","id":"1","version:id":"42"}}`
	if string(encoded) != want {
		t.Fatalf("unexpected extension document:\n got: %s\nwant: %s", encoded, want)
	}
}

func TestCodecRoundTripsRegisteredLinksObjectMember(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{
		Extensions: []ExtensionDefinition{
			{
				URI:       "https://example.com/ext/linkage",
				Namespace: "linkage",
				Members: []MemberDefinition{
					{
						Scope: LinksObjectMemberScope,
						Name:  "linkage:target",
						Validate: func(value any) error {
							object, ok := value.(map[string]any)
							if !ok || object["token"] != "x" {
								return errors.New("target token must be x")
							}
							return nil
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}
	payload := []byte(`{"data":{"type":"articles","id":"1","relationships":{"related":{"links":{"linkage:target":{"token":"x"}}}}}}`)
	document, err := codec.Unmarshal(payload)
	if err != nil {
		t.Fatalf("decode links-object extension member: %v", err)
	}
	link := document.Data.one.Relationships["related"].Links["linkage:target"]
	value, present := link.ExtensionValue()
	object, valid := value.(map[string]any)
	if !present || !valid || object["token"] != "x" {
		t.Fatalf("links-object member was not retained: %#v", value)
	}
	encoded, err := codec.Marshal(document)
	if err != nil {
		t.Fatalf("encode links-object extension member: %v", err)
	}
	if string(encoded) != string(payload) {
		t.Fatalf("unexpected round trip: got %s, want %s", encoded, payload)
	}

	document = Document{Data: ResourceData(ResourceObject{
		Type: "articles",
		ID:   "1",
		Relationships: Relationships{"related": {Links: Links{
			"linkage:target": ExtensionLinkValue(map[string]any{"token": "x"}),
		}}},
	})}
	if _, err := codec.Marshal(document); err != nil {
		t.Fatalf("marshal constructed links-object extension member: %v", err)
	}
	if _, err := Marshal(document); err == nil {
		t.Fatal("core codec accepted an unregistered links-object member")
	}
	if _, err := codec.Unmarshal([]byte(`{"meta":{},"links":{"linkage:target":{"token":"y"}}}`)); err == nil {
		t.Fatal("codec accepted an invalid links-object extension member")
	}
}

func TestCodecRoundTripsLinksObjectMembersAtEveryLinksContainer(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext/linkage",
		Namespace: "linkage",
		Members: []MemberDefinition{{
			Scope: LinksObjectMemberScope,
			Name:  "linkage:target",
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	payloads := []string{
		`{"links":{"linkage:target":true},"meta":{}}`,
		`{"data":{"type":"articles","id":"1","links":{"linkage:target":true}}}`,
		`{"errors":[{"links":{"linkage:target":true},"code":"failure"}]}`,
	}
	for _, payload := range payloads {
		document, decodeErr := codec.Unmarshal([]byte(payload))
		if decodeErr != nil {
			t.Fatalf("decode %s: %v", payload, decodeErr)
		}
		encoded, encodeErr := codec.Marshal(document)
		if encodeErr != nil {
			t.Fatalf("encode %s: %v", payload, encodeErr)
		}
		if string(encoded) != payload {
			t.Fatalf("unexpected round trip: got %s, want %s", encoded, payload)
		}
	}
}

func TestCodecRejectsInvalidRegisteredMemberValue(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext/version",
		Namespace: "version",
		Members: []MemberDefinition{{
			Scope: ResourceMemberScope,
			Name:  "version:id",
			Validate: func(value any) error {
				if _, ok := value.(string); !ok {
					return errors.New("version id must be a string")
				}
				return nil
			},
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	_, err = codec.Unmarshal([]byte(`{
		"data":{"type":"articles","id":"1","version:id":42}
	}`))
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if !hasViolation(validationError, "/data/version:id", "member-value") {
		t.Fatalf("unexpected violations: %#v", validationError.Violations)
	}
}

func TestCoreCodecRejectsUnregisteredExtensionMember(t *testing.T) {
	t.Parallel()

	_, err := Unmarshal([]byte(`{
		"data":{"type":"articles","id":"1","version:id":"42"}
	}`))
	var decodeError *DecodeError
	if !errors.As(err, &decodeError) || decodeError.Path != "/data/version:id" ||
		decodeError.Code != "unknown-member" {
		t.Fatalf("unexpected unregistered member error: %T %#v", err, decodeError)
	}
}

func TestCoreMarshalRejectsUnregisteredExtensionMember(t *testing.T) {
	t.Parallel()

	document := Document{Data: ResourceData(ResourceObject{
		Type:              "articles",
		ID:                "1",
		AdditionalMembers: Members{"version:id": "42"},
	})}
	_, err := Marshal(document)
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/data/version:id", "unregistered-member") {
		t.Fatalf("unexpected marshal error: %T %#v", err, validationError)
	}
}

func TestNewCodecRejectsInvalidExtensionDefinitions(t *testing.T) {
	t.Parallel()

	tests := []CodecOptions{
		{Extensions: []ExtensionDefinition{{URI: "/relative", Namespace: "version"}}},
		{Extensions: []ExtensionDefinition{{URI: "https://example.com/ext", Namespace: "bad-name"}}},
		{Extensions: []ExtensionDefinition{{
			URI:       "https://example.com/ext",
			Namespace: "version",
			Members: []MemberDefinition{{
				Scope: ResourceMemberScope,
				Name:  "other:id",
			}},
		}}},
		{Extensions: []ExtensionDefinition{
			{
				URI:       "https://example.com/one",
				Namespace: "one",
				Members:   []MemberDefinition{{Scope: ResourceMemberScope, Name: "one:id"}},
			},
			{
				URI:       "https://example.com/two",
				Namespace: "two",
				Members:   []MemberDefinition{{Scope: ResourceMemberScope, Name: "one:id"}},
			},
		}},
	}
	for _, options := range tests {
		if _, err := NewCodec(options); err == nil {
			t.Fatalf("expected invalid codec options: %#v", options)
		}
	}
}

func TestCodecRejectsUnregisteredApplicationMemberOnMarshal(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}
	document := Document{Data: ResourceData(ResourceObject{
		Type:              "articles",
		ID:                "1",
		AdditionalMembers: Members{"version:id": "42"},
	})}
	_, err = codec.Marshal(document)
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if !hasViolation(validationError, "/data/version:id", "unregistered-member") {
		t.Fatalf("unexpected violations: %#v", validationError.Violations)
	}
}

func TestCodecRoundTripsTopLevelExtensionMemberAsSemanticContent(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext/version",
		Namespace: "version",
		Members: []MemberDefinition{{
			Scope: TopLevelMemberScope,
			Name:  "version:manifest",
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	payload := []byte(`{"version:manifest":{"revision":42}}`)
	document, err := codec.Unmarshal(payload)
	if err != nil {
		t.Fatalf("decode top-level extension document: %v", err)
	}
	manifest, ok := document.AdditionalMembers["version:manifest"].(map[string]any)
	if !ok || manifest["revision"] == nil {
		t.Fatalf("top-level member was not preserved: %#v", document.AdditionalMembers)
	}
	encoded, err := codec.Marshal(document)
	if err != nil {
		t.Fatalf("encode top-level extension document: %v", err)
	}
	if string(encoded) != string(payload) {
		t.Fatalf("unexpected round trip: got %s, want %s", encoded, payload)
	}
}

func TestCoreCodecRejectsTopLevelExtensionMemberAsSemanticContent(t *testing.T) {
	t.Parallel()

	_, err := Unmarshal([]byte(`{"version:manifest":{"revision":42}}`))
	var decodeError *DecodeError
	if !errors.As(err, &decodeError) || decodeError.Path != "/version:manifest" ||
		decodeError.Code != "unknown-member" {
		t.Fatalf("unexpected core decode error: %T %#v", err, decodeError)
	}

	_, err = Marshal(Document{
		AdditionalMembers: Members{"version:manifest": map[string]any{"revision": 42}},
	})
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/version:manifest", "unregistered-member") {
		t.Fatalf("unexpected core marshal error: %T %#v", err, validationError)
	}
}

func TestCodecRoundTripsRelationshipExtensionMemberAsSemanticContent(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext/version",
		Namespace: "version",
		Members: []MemberDefinition{{
			Scope: RelationshipMemberScope,
			Name:  "version:state",
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	payload := []byte(`{"data":{"type":"articles","id":"1","relationships":{"history":{"version:state":"archived"}}}}`)
	document, err := codec.Unmarshal(payload)
	if err != nil {
		t.Fatalf("decode relationship extension document: %v", err)
	}
	relationship := document.Data.one.Relationships["history"]
	if relationship.AdditionalMembers["version:state"] != "archived" {
		t.Fatalf("relationship member was not preserved: %#v", relationship)
	}
	encoded, err := codec.Marshal(document)
	if err != nil {
		t.Fatalf("encode relationship extension document: %v", err)
	}
	if string(encoded) != string(payload) {
		t.Fatalf("unexpected round trip: got %s, want %s", encoded, payload)
	}
}

func TestCoreCodecRejectsRelationshipExtensionMember(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"data":{"type":"articles","id":"1","relationships":{"history":{"version:state":"archived"}}}}`)
	_, err := Unmarshal(payload)
	var decodeError *DecodeError
	if !errors.As(err, &decodeError) ||
		decodeError.Path != "/data/relationships/history/version:state" ||
		decodeError.Code != "unknown-member" {
		t.Fatalf("unexpected core decode error: %T %#v", err, decodeError)
	}

	_, err = Marshal(Document{Data: ResourceData(ResourceObject{
		Type: "articles",
		ID:   "1",
		Relationships: Relationships{"history": {
			AdditionalMembers: Members{"version:state": "archived"},
		}},
	})})
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(
			validationError,
			"/data/relationships/history/version:state",
			"unregistered-member",
		) {
		t.Fatalf("unexpected core marshal error: %T %#v", err, validationError)
	}
}

func TestCodecRejectsInvalidRelationshipExtensionMemberValue(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext/version",
		Namespace: "version",
		Members: []MemberDefinition{{
			Scope: RelationshipMemberScope,
			Name:  "version:state",
			Validate: func(value any) error {
				if value != "archived" {
					return errors.New("state must be archived")
				}
				return nil
			},
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	_, err = codec.Unmarshal([]byte(`{"data":{"type":"articles","id":"1","relationships":{"history":{"version:state":"active"}}}}`))
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(
			validationError,
			"/data/relationships/history/version:state",
			"member-value",
		) {
		t.Fatalf("unexpected value error: %T %#v", err, validationError)
	}
}

func TestCodecRoundTripsIdentifierExtensionMember(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext/version",
		Namespace: "version",
		Members: []MemberDefinition{{
			Scope: IdentifierMemberScope,
			Name:  "version:etag",
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	payload := []byte(`{"data":{"type":"articles","id":"1","relationships":{"author":{"data":{"type":"people","id":"9","version:etag":"abc"}}}}}`)
	document, err := codec.Unmarshal(payload)
	if err != nil {
		t.Fatalf("decode identifier extension document: %v", err)
	}
	identifier := document.Data.one.Relationships["author"].Data.one
	if identifier == nil || identifier.AdditionalMembers["version:etag"] != "abc" {
		t.Fatalf("identifier member was not preserved: %#v", identifier)
	}
	encoded, err := codec.Marshal(document)
	if err != nil {
		t.Fatalf("encode identifier extension document: %v", err)
	}
	if string(encoded) != string(payload) {
		t.Fatalf("unexpected round trip: got %s, want %s", encoded, payload)
	}
}

func TestCoreCodecRejectsIdentifierExtensionMember(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"data":{"type":"articles","id":"1","relationships":{"author":{"data":{"type":"people","id":"9","version:etag":"abc"}}}}}`)
	_, err := Unmarshal(payload)
	var decodeError *DecodeError
	if !errors.As(err, &decodeError) ||
		decodeError.Path != "/data/relationships/author/data/version:etag" ||
		decodeError.Code != "unknown-member" {
		t.Fatalf("unexpected core decode error: %T %#v", err, decodeError)
	}

	_, err = Marshal(Document{Data: ResourceData(ResourceObject{
		Type: "articles",
		ID:   "1",
		Relationships: Relationships{"author": {
			Data: ToOne(Identifier{
				Type:              "people",
				ID:                "9",
				AdditionalMembers: Members{"version:etag": "abc"},
			}),
		}},
	})})
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(
			validationError,
			"/data/relationships/author/data/version:etag",
			"unregistered-member",
		) {
		t.Fatalf("unexpected core marshal error: %T %#v", err, validationError)
	}
}

func TestCodecRoundTripsToManyIdentifierExtensionMembers(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext/version",
		Namespace: "version",
		Members: []MemberDefinition{{
			Scope: IdentifierMemberScope,
			Name:  "version:etag",
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	payload := []byte(`{"data":{"type":"articles","id":"1","relationships":{"comments":{"data":[{"type":"comments","id":"1","version:etag":"a"},{"type":"comments","id":"2","version:etag":"b"}]}}}}`)
	document, err := codec.Unmarshal(payload)
	if err != nil {
		t.Fatalf("decode to-many identifier members: %v", err)
	}
	identifiers := document.Data.one.Relationships["comments"].Data.many
	if len(identifiers) != 2 ||
		identifiers[0].AdditionalMembers["version:etag"] != "a" ||
		identifiers[1].AdditionalMembers["version:etag"] != "b" {
		t.Fatalf("identifier members were not preserved: %#v", identifiers)
	}
	encoded, err := codec.Marshal(document)
	if err != nil {
		t.Fatalf("encode to-many identifier members: %v", err)
	}
	if string(encoded) != string(payload) {
		t.Fatalf("unexpected round trip: got %s, want %s", encoded, payload)
	}
}

func TestCodecRoundTripsJSONAPIObjectExtensionMember(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext/version",
		Namespace: "version",
		Members: []MemberDefinition{{
			Scope: JSONAPIMemberScope,
			Name:  "version:build",
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	payload := []byte(`{"jsonapi":{"version":"1.1","version:build":"2026.07"},"data":null}`)
	document, err := codec.Unmarshal(payload)
	if err != nil {
		t.Fatalf("decode jsonapi extension document: %v", err)
	}
	if document.JSONAPI == nil ||
		document.JSONAPI.AdditionalMembers["version:build"] != "2026.07" {
		t.Fatalf("jsonapi member was not preserved: %#v", document.JSONAPI)
	}
	encoded, err := codec.Marshal(document)
	if err != nil {
		t.Fatalf("encode jsonapi extension document: %v", err)
	}
	if string(encoded) != string(payload) {
		t.Fatalf("unexpected round trip: got %s, want %s", encoded, payload)
	}
}

func TestCoreCodecRejectsJSONAPIObjectExtensionMember(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"jsonapi":{"version":"1.1","version:build":"2026.07"},"data":null}`)
	_, err := Unmarshal(payload)
	var decodeError *DecodeError
	if !errors.As(err, &decodeError) ||
		decodeError.Path != "/jsonapi/version:build" ||
		decodeError.Code != "unknown-member" {
		t.Fatalf("unexpected core decode error: %T %#v", err, decodeError)
	}

	_, err = Marshal(Document{
		JSONAPI: &JSONAPI{
			Version:           "1.1",
			AdditionalMembers: Members{"version:build": "2026.07"},
		},
		Data: NullData(),
	})
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(
			validationError,
			"/jsonapi/version:build",
			"unregistered-member",
		) {
		t.Fatalf("unexpected core marshal error: %T %#v", err, validationError)
	}
}

func TestCodecRoundTripsErrorObjectExtensionMember(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext/version",
		Namespace: "version",
		Members: []MemberDefinition{{
			Scope: ErrorMemberScope,
			Name:  "version:retryable",
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	payload := []byte(`{"errors":[{"status":"409","version:retryable":true}]}`)
	document, err := codec.Unmarshal(payload)
	if err != nil {
		t.Fatalf("decode error extension document: %v", err)
	}
	if len(document.Errors) != 1 ||
		document.Errors[0].AdditionalMembers["version:retryable"] != true {
		t.Fatalf("error member was not preserved: %#v", document.Errors)
	}
	encoded, err := codec.Marshal(document)
	if err != nil {
		t.Fatalf("encode error extension document: %v", err)
	}
	if string(encoded) != string(payload) {
		t.Fatalf("unexpected round trip: got %s, want %s", encoded, payload)
	}
}

func TestRegisteredExtensionMemberCanQualifyErrorObject(t *testing.T) {
	t.Parallel()

	codec := mustCodec(t, CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/extensions/problem",
		Namespace: "problem",
		Members: []MemberDefinition{{
			Scope: ErrorMemberScope,
			Name:  "problem:retryable",
		}},
	}}})
	document := Document{
		JSONAPI: &JSONAPI{Ext: []string{"https://example.com/extensions/problem"}},
		Errors: []ErrorObject{{
			AdditionalMembers: Members{"problem:retryable": true},
		}},
	}

	payload, err := codec.Marshal(document)
	if err != nil {
		t.Fatalf("registered extension-only error was rejected: %v", err)
	}
	if _, err := codec.Unmarshal(payload); err != nil {
		t.Fatalf("registered extension-only error did not round trip: %v", err)
	}
}

func TestCoreCodecRejectsErrorObjectExtensionMember(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"errors":[{"status":"409","version:retryable":true}]}`)
	_, err := Unmarshal(payload)
	var decodeError *DecodeError
	if !errors.As(err, &decodeError) ||
		decodeError.Path != "/errors/0/version:retryable" ||
		decodeError.Code != "unknown-member" {
		t.Fatalf("unexpected core decode error: %T %#v", err, decodeError)
	}

	_, err = Marshal(Document{Errors: []ErrorObject{{
		Status:            "409",
		AdditionalMembers: Members{"version:retryable": true},
	}}})
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(
			validationError,
			"/errors/0/version:retryable",
			"unregistered-member",
		) {
		t.Fatalf("unexpected core marshal error: %T %#v", err, validationError)
	}
}

func TestCodecRoundTripsErrorSourceExtensionMember(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext/version",
		Namespace: "version",
		Members: []MemberDefinition{{
			Scope: ErrorSourceMemberScope,
			Name:  "version:input",
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	payload := []byte(`{"errors":[{"source":{"pointer":"/data","version:input":"body"}}]}`)
	document, err := codec.Unmarshal(payload)
	if err != nil {
		t.Fatalf("decode error source extension document: %v", err)
	}
	if len(document.Errors) != 1 || document.Errors[0].Source == nil ||
		document.Errors[0].Source.AdditionalMembers["version:input"] != "body" {
		t.Fatalf("error source member was not preserved: %#v", document.Errors)
	}
	encoded, err := codec.Marshal(document)
	if err != nil {
		t.Fatalf("encode error source extension document: %v", err)
	}
	if string(encoded) != string(payload) {
		t.Fatalf("unexpected round trip: got %s, want %s", encoded, payload)
	}
}

func TestCoreCodecRejectsErrorSourceExtensionMember(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"errors":[{"source":{"pointer":"/data","version:input":"body"}}]}`)
	_, err := Unmarshal(payload)
	var decodeError *DecodeError
	if !errors.As(err, &decodeError) ||
		decodeError.Path != "/errors/0/source/version:input" ||
		decodeError.Code != "unknown-member" {
		t.Fatalf("unexpected core decode error: %T %#v", err, decodeError)
	}

	_, err = Marshal(Document{Errors: []ErrorObject{{Source: &ErrorSource{
		Pointer:           "/data",
		AdditionalMembers: Members{"version:input": "body"},
	}}}})
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(
			validationError,
			"/errors/0/source/version:input",
			"unregistered-member",
		) {
		t.Fatalf("unexpected core marshal error: %T %#v", err, validationError)
	}
}

func TestCodecRoundTripsNestedLinkObjectExtensionMembers(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext/version",
		Namespace: "version",
		Members: []MemberDefinition{{
			Scope: LinkObjectMemberScope,
			Name:  "version:cache",
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	payload := []byte(`{"links":{"self":{"href":"/articles","describedby":{"href":"/schema","version:cache":"miss"},"version:cache":"hit"}},"data":null}`)
	document, err := codec.Unmarshal(payload)
	if err != nil {
		t.Fatalf("decode link extension document: %v", err)
	}
	link := document.Links["self"]
	if link.additionalMembers["version:cache"] != "hit" ||
		link.describedBy == nil ||
		link.describedBy.additionalMembers["version:cache"] != "miss" {
		t.Fatalf("link members were not preserved: %#v", link)
	}
	encoded, err := codec.Marshal(document)
	if err != nil {
		t.Fatalf("encode link extension document: %v", err)
	}
	if string(encoded) != string(payload) {
		t.Fatalf("unexpected round trip: got %s, want %s", encoded, payload)
	}
}

func TestCoreCodecRejectsLinkObjectExtensionMember(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"links":{"self":{"href":"/articles","version:cache":"hit"}},"data":null}`)
	_, err := Unmarshal(payload)
	var decodeError *DecodeError
	if !errors.As(err, &decodeError) ||
		decodeError.Path != "/links/self/version:cache" ||
		decodeError.Code != "unknown-member" {
		t.Fatalf("unexpected core decode error: %T %#v", err, decodeError)
	}

	_, err = Marshal(Document{
		Links: Links{"self": LinkFromObject(LinkObject{
			Href:              "/articles",
			AdditionalMembers: Members{"version:cache": "hit"},
		})},
		Data: NullData(),
	})
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(
			validationError,
			"/links/self/version:cache",
			"unregistered-member",
		) {
		t.Fatalf("unexpected core marshal error: %T %#v", err, validationError)
	}
}

func TestCodecRoundTripsLinkObjectMembersAtEveryLinksContainer(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext/version",
		Namespace: "version",
		Members: []MemberDefinition{{
			Scope: LinkObjectMemberScope,
			Name:  "version:cache",
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	payloads := []string{
		`{"data":{"type":"articles","id":"1","links":{"self":{"href":"/articles/1","version:cache":"resource"}}}}`,
		`{"data":{"type":"articles","id":"1","relationships":{"author":{"links":{"related":{"href":"/author","version:cache":"relationship"}}}}}}`,
		`{"errors":[{"links":{"about":{"href":"/errors/1","version:cache":"error"}}}]}`,
	}
	for _, payload := range payloads {
		document, decodeErr := codec.Unmarshal([]byte(payload))
		if decodeErr != nil {
			t.Fatalf("decode link placement %s: %v", payload, decodeErr)
		}
		encoded, encodeErr := codec.Marshal(document)
		if encodeErr != nil {
			t.Fatalf("encode link placement %s: %v", payload, encodeErr)
		}
		if string(encoded) != payload {
			t.Fatalf("unexpected round trip: got %s, want %s", encoded, payload)
		}
	}
}

func TestCodecRoundTripsExtensionMembersAcrossResourceCollections(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{
		Extensions: []ExtensionDefinition{{
			URI:       "https://example.com/ext/version",
			Namespace: "version",
			Members: []MemberDefinition{
				{Scope: ResourceMemberScope, Name: "version:id"},
				{Scope: IdentifierMemberScope, Name: "version:etag"},
			},
		}},
		Profiles: []ProfileDefinition{{
			URI: "https://example.com/profiles/timestamps",
		}},
	})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	payload := []byte(`{"data":[{"type":"articles","id":"1","relationships":{"author":{"data":{"type":"people","id":"9","version:etag":"person-9"}}},"version:id":"article-1"},{"type":"articles","id":"2","version:id":"article-2"}],"included":[{"type":"people","id":"9","version:id":"person-9"}]}`)
	document, err := codec.Unmarshal(payload)
	if err != nil {
		t.Fatalf("decode collection extension document: %v", err)
	}
	resources := document.Data.many
	if len(resources) != 2 ||
		resources[0].AdditionalMembers["version:id"] != "article-1" ||
		resources[1].AdditionalMembers["version:id"] != "article-2" {
		t.Fatalf("primary members were not preserved: %#v", resources)
	}
	identifier := resources[0].Relationships["author"].Data.one
	if identifier == nil || identifier.AdditionalMembers["version:etag"] != "person-9" {
		t.Fatalf("identifier member was not preserved: %#v", identifier)
	}
	if len(document.Included) != 1 ||
		document.Included[0].AdditionalMembers["version:id"] != "person-9" {
		t.Fatalf("included member was not preserved: %#v", document.Included)
	}
	encoded, err := codec.Marshal(document)
	if err != nil {
		t.Fatalf("encode collection extension document: %v", err)
	}
	if string(encoded) != string(payload) {
		t.Fatalf("unexpected round trip: got %s, want %s", encoded, payload)
	}

	nullPayload := []byte(`{"data":null}`)
	nullDocument, err := codec.Unmarshal(nullPayload)
	if err != nil {
		t.Fatalf("decode null extension document: %v", err)
	}
	encoded, err = codec.Marshal(nullDocument)
	if err != nil || string(encoded) != string(nullPayload) {
		t.Fatalf("unexpected null round trip: got %s, err %v", encoded, err)
	}
}
