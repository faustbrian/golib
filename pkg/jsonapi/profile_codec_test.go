package jsonapi

import (
	"errors"
	"testing"
)

func TestCodecAppliesRegisteredProfileDocumentValidation(t *testing.T) {
	t.Parallel()

	profileError := errors.New("timestamps attribute is required")
	codec, err := NewCodec(CodecOptions{Profiles: []ProfileDefinition{{
		URI: "https://example.com/profiles/timestamps",
		ValidateDocument: func(document Document) error {
			if document.Data == nil || document.Data.one == nil ||
				document.Data.one.Attributes["timestamps"] == nil {
				return profileError
			}
			return nil
		},
	}}})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	payload := []byte(`{"data":{"type":"articles","id":"1"}}`)
	if _, err := codec.Unmarshal(payload); !errors.Is(err, profileError) {
		t.Fatalf("unexpected profile decode error: %v", err)
	}
	if _, err := codec.Marshal(Document{Data: ResourceData(ResourceObject{
		Type: "articles",
		ID:   "1",
	})}); !errors.Is(err, profileError) {
		t.Fatalf("unexpected profile encode error: %v", err)
	}

	valid := Document{Data: ResourceData(ResourceObject{
		Type:       "articles",
		ID:         "1",
		Attributes: Attributes{"timestamps": map[string]any{"created": "now"}},
	})}
	if _, err := codec.Marshal(valid); err != nil {
		t.Fatalf("validate profile document: %v", err)
	}
}

func TestProfileValidatorCannotMutateValidatedDocument(t *testing.T) {
	t.Parallel()

	codec := mustCodec(t, CodecOptions{Profiles: []ProfileDefinition{{
		URI: "https://example.com/profiles/mutating",
		ValidateDocument: func(document Document) error {
			document.Data.one.Type = ""
			return nil
		},
	}}})
	document := Document{Data: ResourceData(ResourceObject{
		Type: "articles",
		ID:   "1",
	})}

	_, err := codec.Marshal(document)
	var callbackError *CallbackError
	if !errors.As(err, &callbackError) || callbackError.CallbackPhase() != "profile" {
		t.Fatalf("profile mutation was accepted: %T %v", err, err)
	}

	extensionCodec := mustCodec(t, CodecOptions{
		Extensions: []ExtensionDefinition{{
			URI:       "https://example.com/extensions/version",
			Namespace: "version",
			Members: []MemberDefinition{{
				Scope: ResourceMemberScope,
				Name:  "version:current",
				Validate: func(value any) error {
					if value != true {
						return errors.New("current version is required")
					}
					return nil
				},
			}},
		}},
		Profiles: []ProfileDefinition{{
			URI: "https://example.com/profiles/mutating",
			ValidateDocument: func(document Document) error {
				document.Data.one.AdditionalMembers["version:current"] = false
				return nil
			},
		}},
	})
	document = Document{Data: ResourceData(ResourceObject{
		Type:              "articles",
		ID:                "1",
		AdditionalMembers: Members{"version:current": true},
	})}
	_, err = extensionCodec.Marshal(document)
	if !errors.As(err, &callbackError) || callbackError.CallbackPhase() != "profile" {
		t.Fatalf("profile extension mutation was accepted: %T %v", err, err)
	}
}

func TestProfileValidationRejectsUnencodableDocumentSnapshot(t *testing.T) {
	t.Parallel()

	codec := mustCodec(t, CodecOptions{Profiles: []ProfileDefinition{{
		URI:              "https://example.com/profiles/inspect",
		ValidateDocument: func(Document) error { return nil },
	}}})
	_, err := codec.Marshal(Document{Data: ResourceData(ResourceObject{
		Type:       "articles",
		ID:         "1",
		Attributes: Attributes{"unsupported": func() {}},
	})})
	if err == nil {
		t.Fatal("unencodable profile document was accepted")
	}
}

func TestNewCodecRejectsInvalidProfileDefinitions(t *testing.T) {
	t.Parallel()

	tests := []CodecOptions{
		{Profiles: []ProfileDefinition{{URI: "/relative"}}},
		{Profiles: []ProfileDefinition{
			{URI: "https://example.com/profiles/one"},
			{URI: "https://example.com/profiles/one"},
		}},
	}
	for _, options := range tests {
		if _, err := NewCodec(options); err == nil {
			t.Fatalf("expected invalid codec options: %#v", options)
		}
	}
}

func TestCodecValidatesOptionalAppliedURIDeclarations(t *testing.T) {
	t.Parallel()

	extensionURI := "https://example.com/ext/audit"
	profileURI := "https://example.com/profiles/audit"
	codec, err := NewCodec(CodecOptions{
		Extensions: []ExtensionDefinition{{
			URI: extensionURI, Namespace: "audit",
		}},
		Profiles: []ProfileDefinition{{URI: profileURI}},
	})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	tests := []struct {
		object JSONAPI
		path   string
		code   string
	}{
		{JSONAPI{Ext: []string{}}, "/jsonapi/ext", "missing-extension"},
		{
			JSONAPI{Ext: []string{extensionURI, "https://example.com/ext/other"}},
			"/jsonapi/ext/1",
			"unsupported-extension",
		},
		{JSONAPI{Profile: []string{}}, "/jsonapi/profile", "missing-profile"},
	}
	for _, test := range tests {
		document := Document{
			JSONAPI: &test.object,
			Data:    NullData(),
		}
		_, err := codec.Marshal(document)
		assertValidationViolation(t, err, test.path, test.code)
	}
	_, err = codec.Unmarshal([]byte(`{"jsonapi":{"ext":[]},"data":null}`))
	assertValidationViolation(t, err, "/jsonapi/ext", "missing-extension")

	valid := Document{
		JSONAPI: &JSONAPI{
			Ext:     []string{extensionURI},
			Profile: []string{profileURI, "https://example.com/profiles/unknown"},
		},
		Data: NullData(),
	}
	if _, err := codec.Marshal(valid); err != nil {
		t.Fatalf("matching declarations were rejected: %v", err)
	}
	if _, err := codec.Marshal(Document{Data: NullData()}); err != nil {
		t.Fatalf("optional jsonapi object was required: %v", err)
	}
}
