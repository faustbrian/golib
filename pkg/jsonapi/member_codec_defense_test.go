package jsonapi

import (
	"errors"
	"testing"
)

func TestNewCodecRejectsRegistrationConflictsAndUnsupportedScopes(t *testing.T) {
	t.Parallel()

	tests := []CodecOptions{
		{Extensions: []ExtensionDefinition{
			{URI: "https://example.com/one", Namespace: "one"},
			{URI: "https://example.com/one", Namespace: "two"},
		}},
		{Extensions: []ExtensionDefinition{
			{URI: "https://example.com/one", Namespace: "same"},
			{URI: "https://example.com/two", Namespace: "same"},
		}},
		{Extensions: []ExtensionDefinition{{
			URI:       "https://example.com/ext",
			Namespace: "ext",
			Members: []MemberDefinition{{
				Scope: 0,
				Name:  "ext:value",
			}},
		}}},
		{Extensions: []ExtensionDefinition{{
			URI:       "https://example.com/ext",
			Namespace: "ext",
			Members: []MemberDefinition{{
				Scope: ResourceMemberScope,
				Name:  "ext:@value",
			}},
		}}},
		{Extensions: []ExtensionDefinition{{
			URI:       "https://example.com/ext",
			Namespace: "ext",
			Members: []MemberDefinition{
				{Scope: ResourceMemberScope, Name: "ext:value"},
				{Scope: ResourceMemberScope, Name: "ext:value"},
			},
		}}},
	}

	for _, options := range tests {
		if _, err := NewCodec(options); err == nil {
			t.Fatalf("expected codec registration failure: %#v", options)
		}
	}
}

func TestExtensionMemberNameRejectsAnInternalAtSign(t *testing.T) {
	t.Parallel()

	if validExtensionMemberName("ext:@value") {
		t.Fatal("extension member accepted @ outside the first position")
	}
}

func TestStrictMarshalAcceptsAtMembersWithoutRegistration(t *testing.T) {
	t.Parallel()

	document := Document{
		AdditionalMembers: Members{"@context": "https://example.com/context"},
		Data: ResourceData(ResourceObject{
			Type:              "articles",
			ID:                "1",
			AdditionalMembers: Members{"@annotation": true},
		}),
	}
	codec, err := NewCodec(CodecOptions{})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}
	for name, marshal := range map[string]func(Document) ([]byte, error){
		"core":       Marshal,
		"configured": codec.Marshal,
	} {
		payload, err := marshal(document)
		if err != nil {
			t.Fatalf("%s marshal rejected @-Members: %v", name, err)
		}
		want := `{"data":{"type":"articles","id":"1","@annotation":true},"@context":"https://example.com/context"}`
		if string(payload) != want {
			t.Fatalf("unexpected %s payload: got %s, want %s", name, payload, want)
		}
	}
}

func TestStrictMarshalRejectsAnInvalidAtMemberName(t *testing.T) {
	t.Parallel()

	_, err := Marshal(Document{
		Data:              NullData(),
		AdditionalMembers: Members{"@": true},
	})
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/@", "member-name") {
		t.Fatalf("unexpected invalid @-Member error: %T %#v", err, validationError)
	}
}

func TestCodecRejectsMalformedDocumentsAtEverySanitizationBoundary(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}
	tests := []struct {
		payload string
		path    string
		code    string
	}{
		{payload: `{`, path: "", code: "syntax"},
		{payload: `{"data":null,"data":null}`, path: "/data", code: "duplicate-member"},
		{payload: `[]`, path: "", code: "type"},
		{payload: `{"jsonapi":[]}`, path: "/jsonapi", code: "type"},
		{payload: `{"links":[]}`, path: "/links", code: "type"},
		{payload: `{"data":true}`, path: "/data", code: "type"},
		{payload: `{"included":null}`, path: "/included", code: "type"},
		{payload: `{"included":[null]}`, path: "/included/0", code: "type"},
		{payload: `{"errors":null}`, path: "/errors", code: "type"},
		{payload: `{"errors":[null]}`, path: "/errors/0", code: "type"},
		{payload: `{"errors":[{"links":[]}]}`, path: "/errors/0/links", code: "type"},
		{payload: `{"errors":[{"source":[]}]}`, path: "/errors/0/source", code: "type"},
		{payload: `{"data":{"type":"articles","id":"1","links":[]}}`, path: "/data/links", code: "type"},
		{payload: `{"data":{"type":"articles","id":"1","relationships":{"author":null}}}`, path: "/data/relationships/author", code: "type"},
		{payload: `{"data":{"type":"articles","id":"1","relationships":{"author":{"links":[]}}}}`, path: "/data/relationships/author/links", code: "type"},
		{payload: `{"data":{"type":"articles","id":"1","relationships":{"author":{"data":[null]}}}}`, path: "/data/relationships/author/data/0", code: "type"},
	}

	for _, test := range tests {
		_, err := codec.Unmarshal([]byte(test.payload))
		var decodeError *DecodeError
		if !errors.As(err, &decodeError) || decodeError.Path != test.path ||
			decodeError.Code != test.code {
			t.Errorf(
				"unexpected error for %s: got %T %#v, want path %q code %q",
				test.payload,
				err,
				decodeError,
				test.path,
				test.code,
			)
		}
	}
}

func TestNewCodecRejectsNonRFC3986RegistrationURI(t *testing.T) {
	t.Parallel()

	_, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI: "https://example.com/extensions/雪", Namespace: "snow",
	}}})
	if err == nil {
		t.Fatal("codec accepted an extension URI requiring wire escaping")
	}
}

func TestCodecMarshalRejectsInvalidCoreDocument(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}
	if _, err := codec.Marshal(Document{}); err == nil {
		t.Fatal("expected core document validation failure")
	}
}

func TestCodecRejectsInvalidRegisteredMembersAtNestedScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scope   MemberScope
		payload string
		path    string
	}{
		{TopLevelMemberScope, `{"data":null,"ext:value":true}`, "/ext:value"},
		{JSONAPIMemberScope, `{"jsonapi":{"ext:value":true},"data":null}`, "/jsonapi/ext:value"},
		{ErrorMemberScope, `{"errors":[{"ext:value":true}]}`, "/errors/0/ext:value"},
		{ErrorSourceMemberScope, `{"errors":[{"source":{"ext:value":true}}]}`, "/errors/0/source/ext:value"},
		{LinkObjectMemberScope, `{"links":{"self":{"href":"/","ext:value":true}},"data":null}`, "/links/self/ext:value"},
		{LinkObjectMemberScope, `{"links":{"self":{"href":"/","describedby":{"href":"/schema","ext:value":true}}},"data":null}`, "/links/self/describedby/ext:value"},
		{IdentifierMemberScope, `{"data":{"type":"articles","id":"1","relationships":{"author":{"data":{"type":"people","id":"9","ext:value":true}}}}}`, "/data/relationships/author/data/ext:value"},
	}

	for _, test := range tests {
		codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
			URI:       "https://example.com/ext",
			Namespace: "ext",
			Members: []MemberDefinition{{
				Scope: test.scope,
				Name:  "ext:value",
				Validate: func(any) error {
					return errors.New("invalid extension value")
				},
			}},
		}}})
		if err != nil {
			t.Fatalf("construct codec: %v", err)
		}
		_, err = codec.Unmarshal([]byte(test.payload))
		var validationError *ValidationError
		if !errors.As(err, &validationError) ||
			!hasViolation(validationError, test.path, "member-value") {
			t.Errorf("unexpected error at %s: %T %#v", test.path, err, validationError)
		}
	}
}

func TestCodecContinuesFromSanitizationIntoCoreValidation(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext",
		Namespace: "ext",
		Members: []MemberDefinition{{
			Scope: ResourceMemberScope,
			Name:  "ext:value",
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}
	tests := []struct {
		payload string
		path    string
		code    string
	}{
		{`{"data":{"type":1}}`, "/data/type", "type"},
		{`{"data":{"id":"1"}}`, "/data/type", "required"},
		{`{"data":{"type":"articles","id":"1","relationships":[]}}`, "/data/relationships", "type"},
		{`{"data":{"type":"articles","id":"1","relationships":{"author":{"data":true}}}}`, "/data/relationships/author/data", "type"},
	}
	for _, test := range tests {
		_, err := codec.Unmarshal([]byte(test.payload))
		var decodeError *DecodeError
		var validationError *ValidationError
		if errors.As(err, &decodeError) {
			if decodeError.Path != test.path || decodeError.Code != test.code {
				t.Errorf("unexpected decode error: %#v", decodeError)
			}
			continue
		}
		if !errors.As(err, &validationError) ||
			!hasViolation(validationError, test.path, test.code) {
			t.Errorf("unexpected validation error: %T %#v", err, validationError)
		}
	}

	if _, err := codec.Unmarshal([]byte(`{"links":{"self":"/articles"},"data":{"type":"articles","id":"1"}}`)); err != nil {
		t.Fatalf("decode scalar link without registered member: %v", err)
	}
	if _, err := codec.Unmarshal([]byte(`{"data":{"type":"articles","id":"1","relationships":{"author":{"data":null}}}}`)); err != nil {
		t.Fatalf("decode null linkage without registered member: %v", err)
	}
}

func TestCodecMarshalRunsRegisteredMemberValidatorsAtEveryScope(t *testing.T) {
	t.Parallel()

	reject := func(any) error { return errors.New("invalid extension value") }
	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext",
		Namespace: "ext",
		Members: []MemberDefinition{
			{Scope: TopLevelMemberScope, Name: "ext:top", Validate: reject},
			{Scope: ResourceMemberScope, Name: "ext:resource", Validate: reject},
			{Scope: RelationshipMemberScope, Name: "ext:relationship", Validate: reject},
		},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}
	document := Document{
		AdditionalMembers: Members{"ext:top": true},
		Data: ResourceData(ResourceObject{
			Type:              "articles",
			ID:                "1",
			AdditionalMembers: Members{"ext:resource": true},
			Relationships: Relationships{"author": {
				Data:              NullRelationship(),
				AdditionalMembers: Members{"ext:relationship": true},
			}},
		}),
	}
	_, err = codec.Marshal(document)
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/ext:top", "member-value") ||
		!hasViolation(validationError, "/data/ext:resource", "member-value") ||
		!hasViolation(
			validationError,
			"/data/relationships/author/ext:relationship",
			"member-value",
		) {
		t.Fatalf("unexpected member validation error: %T %#v", err, validationError)
	}
}

func TestCodecUnmarshalRunsEachRegisteredMemberValidatorOnce(t *testing.T) {
	t.Parallel()

	calls := 0
	codec, err := NewCodec(CodecOptions{Extensions: []ExtensionDefinition{{
		URI:       "https://example.com/ext",
		Namespace: "ext",
		Members: []MemberDefinition{{
			Scope: ResourceMemberScope,
			Name:  "ext:value",
			Validate: func(any) error {
				calls++
				if calls > 1 {
					return errors.New("validator called more than once")
				}
				return nil
			},
		}},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}
	if _, err := codec.Unmarshal([]byte(
		`{"data":{"type":"articles","id":"1","ext:value":true}}`,
	)); err != nil {
		t.Fatalf("decode registered member: %v", err)
	}
	if calls != 1 {
		t.Fatalf("validator called %d times, want 1", calls)
	}
}

func TestMemberCodecHelpersRejectInvalidSyntheticValues(t *testing.T) {
	t.Parallel()

	if _, err := marshalObjectWithMembers(42, Members{"ext:value": true}); err == nil {
		t.Fatal("expected non-object core failure")
	}
	if _, err := marshalObjectWithMembers(
		struct {
			Type string `json:"type"`
		}{Type: "articles"},
		Members{"type": "people"},
	); err == nil {
		t.Fatal("expected core member collision")
	}
	if _, err := marshalObjectWithMembers(
		struct{}{},
		Members{"ext:value": make(chan int)},
	); err == nil {
		t.Fatal("expected additional member encoding failure")
	}
}

func TestMemberAttachmentHelpersIgnoreMissingDecodedTargets(t *testing.T) {
	t.Parallel()

	resource := ResourceObject{}
	attachResourceMembers(&resource, resourceMemberState{
		relationships: map[string]relationshipMemberState{"missing": {}},
	})
	links := Links{}
	attachLinkMembers(links, linksMemberState{
		links: map[string]linkMemberState{"missing": {}},
	})

	nested := LinkFromObject(LinkObject{Href: "/nested"})
	link := LinkFromObject(LinkObject{Href: "/", DescribedBy: &nested})
	attachLinkState(&link, linkMemberState{
		describedBy: &linkMemberState{members: Members{"ext:value": true}},
	})
	if link.describedBy.additionalMembers["ext:value"] != true {
		t.Fatalf("nested members were not attached: %#v", link.describedBy)
	}
}
