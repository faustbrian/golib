package jsonapi

import (
	"net/url"
	"reflect"
	"sync"
	"testing"
)

func TestConstructorsCopyRegistryInputs(t *testing.T) {
	t.Parallel()

	extensions := []ExtensionDefinition{{
		URI:       "https://example.com/extensions/version",
		Namespace: "version",
		Members: []MemberDefinition{{
			Scope: ResourceMemberScope,
			Name:  "version:id",
		}},
	}}
	profiles := []ProfileDefinition{{URI: "https://example.com/profiles/timestamps"}}
	codec := mustCodec(t, CodecOptions{Extensions: extensions, Profiles: profiles})
	extensions[0].URI = "https://attacker.example/changed"
	extensions[0].Members[0].Name = "version:changed"
	profiles[0].URI = "https://attacker.example/changed"

	document := Document{
		JSONAPI: &JSONAPI{
			Ext:     []string{"https://example.com/extensions/version"},
			Profile: []string{"https://example.com/profiles/timestamps"},
		},
		Data: ResourceData(ResourceObject{
			Type:              "articles",
			ID:                "1",
			AdditionalMembers: Members{"version:id": "1"},
		}),
	}
	if _, err := codec.Marshal(document); err != nil {
		t.Fatalf("mutating codec inputs changed the registry: %v", err)
	}

	supportedExtensions := []string{"https://example.com/extensions/version"}
	supportedProfiles := []string{"https://example.com/profiles/timestamps"}
	negotiator, err := NewNegotiator(supportedExtensions, supportedProfiles)
	if err != nil {
		t.Fatalf("construct negotiator: %v", err)
	}
	supportedExtensions[0] = "https://attacker.example/changed"
	supportedProfiles[0] = "https://attacker.example/changed"
	if _, err := negotiator.CheckContentType(
		MediaTypeJSONAPI + `;ext="https://example.com/extensions/version"`,
	); err != nil {
		t.Fatalf("mutating negotiator inputs changed the registry: %v", err)
	}

	custom := []string{"customFamily"}
	namespaces := []string{"version"}
	parser, err := NewQueryParser(custom, namespaces)
	if err != nil {
		t.Fatalf("construct query parser: %v", err)
	}
	custom[0] = "changedFamily"
	namespaces[0] = "changed"
	if _, err := parser.Parse(url.Values{
		"customFamily[value]":   {"1"},
		"version:filter[value]": {"2"},
	}); err != nil {
		t.Fatalf("mutating parser inputs changed the registry: %v", err)
	}
}

func TestValidationAndMarshalDoNotMutateCallerData(t *testing.T) {
	t.Parallel()

	fixture := func() Document {
		return Document{Data: ResourceData(ResourceObject{
			Type: "articles",
			ID:   "1",
			Attributes: Attributes{
				"title": "Hello",
				"tags":  []any{"go", "jsonapi"},
			},
			Relationships: Relationships{
				"author": {Data: ToOne(Identifier{Type: "people", ID: "9"})},
			},
			Meta: Meta{"nested": map[string]any{"value": "unchanged"}},
		})}
	}
	document := fixture()
	expected := fixture()
	if err := document.Validate(); err != nil {
		t.Fatalf("validate fixture: %v", err)
	}
	if _, err := Marshal(document); err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if !reflect.DeepEqual(document, expected) {
		t.Fatalf("validation or marshal mutated caller data:\nwant %#v\n got %#v", expected, document)
	}
}

func TestConfiguredCodecSupportsConcurrentUse(t *testing.T) {
	codec := mustCodec(t, CodecOptions{})
	document := Document{Data: ResourceData(ResourceObject{
		Type: "articles",
		ID:   "1",
	})}
	payload, err := codec.Marshal(document)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 20 {
				if _, marshalErr := codec.Marshal(document); marshalErr != nil {
					t.Errorf("concurrent marshal: %v", marshalErr)
				}
				if _, unmarshalErr := codec.Unmarshal(payload); unmarshalErr != nil {
					t.Errorf("concurrent unmarshal: %v", unmarshalErr)
				}
			}
		}()
	}
	wait.Wait()
}
