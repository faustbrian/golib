package jsonapi

import (
	"encoding/json"
	"testing"
)

func TestRoundTripPreservesExplicitEmptyMembers(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"empty errors array":         `{"errors":[]}`,
		"empty top-level containers": `{"links":{},"data":[],"included":[],"meta":{}}`,
		"empty resource containers":  `{"data":{"type":"articles","id":"1","attributes":{},"relationships":{},"links":{},"meta":{}}}`,
		"empty relationship meta":    `{"data":{"type":"articles","id":"1","relationships":{"author":{"meta":{}}}}}`,
		"empty identifier meta":      `{"data":{"type":"articles","id":"1","relationships":{"author":{"data":{"type":"people","id":"9","meta":{}}}}}}`,
		"empty JSON API containers":  `{"jsonapi":{"ext":[],"profile":[],"meta":{}},"data":null}`,
		"empty link object meta":     `{"links":{"self":{"href":"/articles","meta":{}}},"data":null}`,
		"empty error meta":           `{"errors":[{"meta":{}}]}`,
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

func TestRoundTripPreservesExplicitEmptyStringMembers(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"resource and identifier identities": `{"data":{"type":"articles","id":"","relationships":{"author":{"data":{"type":"people","id":""}},"editor":{"data":{"type":"people","lid":""}}}}}`,
		"jsonapi and error strings":          `{"jsonapi":{"version":""},"errors":[{"id":"","status":"400","code":"","title":"","detail":"","source":{"pointer":"","parameter":"","header":""}}]}`,
		"empty URI references":               `{"links":{"related":{"href":"","rel":"alternate","title":"","type":"text/plain"},"self":""},"meta":{}}`,
	}

	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			document, err := Unmarshal([]byte(payload))
			if err != nil {
				t.Fatalf("unmarshal document: %v", err)
			}
			encoded, err := Marshal(document)
			if err != nil {
				t.Fatalf("marshal document: %v", err)
			}
			if string(encoded) != payload {
				t.Fatalf("unexpected round trip: got %s, want %s", encoded, payload)
			}
		})
	}
}

func TestExplicitEmptyIdentityBuilders(t *testing.T) {
	t.Parallel()

	resource := ResourceObject{Type: "articles"}.WithID("")
	identifier := Identifier{Type: "people"}.WithLID("")
	document := Document{Data: ResourceData(resource)}

	encoded, err := MarshalWith(document, ValidationOptions{Context: Response})
	if err != nil {
		t.Fatalf("marshal resource with empty id: %v", err)
	}
	if string(encoded) != `{"data":{"type":"articles","id":""}}` {
		t.Fatalf("unexpected resource JSON: %s", encoded)
	}

	encoded, err = json.Marshal(identifier)
	if err != nil {
		t.Fatalf("marshal identifier with empty lid: %v", err)
	}
	if string(encoded) != `{"type":"people","lid":""}` {
		t.Fatalf("unexpected identifier JSON: %s", encoded)
	}

	resource = ResourceObject{Type: "articles"}.WithLID("")
	identifier = Identifier{Type: "people"}.WithID("")
	encoded, err = json.Marshal(struct {
		Resource   ResourceObject `json:"resource"`
		Identifier Identifier     `json:"identifier"`
	}{resource, identifier})
	if err != nil {
		t.Fatalf("marshal alternate identities: %v", err)
	}
	want := `{"resource":{"type":"articles","lid":""},"identifier":{"type":"people","id":""}}`
	if string(encoded) != want {
		t.Fatalf("unexpected alternate identity JSON: got %s, want %s", encoded, want)
	}
}

func TestExplicitEmptyOptionalStringBuilders(t *testing.T) {
	t.Parallel()

	link := ObjectLink("", nil).
		WithRel("alternate").
		WithTitle("").
		WithType("text/plain")
	apiError := (ErrorObject{}).
		WithID("").
		WithStatus("").
		WithCode("").
		WithTitle("").
		WithDetail("")
	source := (ErrorSource{}).
		WithPointer("").
		WithParameter("").
		WithHeader("")
	value := struct {
		JSONAPI JSONAPI     `json:"jsonapi"`
		Link    Link        `json:"link"`
		Error   ErrorObject `json:"error"`
		Source  ErrorSource `json:"source"`
	}{
		JSONAPI: (JSONAPI{}).WithVersion(""),
		Link:    link,
		Error:   apiError,
		Source:  source,
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal presence-aware values: %v", err)
	}
	want := `{"jsonapi":{"version":""},"link":{"href":"","rel":"alternate","title":"","type":"text/plain"},"error":{"id":"","status":"","code":"","title":"","detail":""},"source":{"pointer":"","parameter":"","header":""}}`
	if string(encoded) != want {
		t.Fatalf("unexpected optional-string JSON: got %s, want %s", encoded, want)
	}
}

func TestMarshalPreservesEmptyRequiredTopLevelMember(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		document Document
		want     string
	}{
		"errors": {
			document: Document{Errors: []ErrorObject{}},
			want:     `{"errors":[]}`,
		},
		"meta": {
			document: Document{Meta: Meta{}},
			want:     `{"meta":{}}`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := Marshal(test.document)
			if err != nil {
				t.Fatalf("marshal document: %v", err)
			}
			if string(got) != test.want {
				t.Fatalf("unexpected JSON: got %s, want %s", got, test.want)
			}
		})
	}
}
