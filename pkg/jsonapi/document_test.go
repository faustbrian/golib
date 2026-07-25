package jsonapi

import (
	"encoding/json"
	"testing"
)

func TestMarshalDocumentDeterministically(t *testing.T) {
	t.Parallel()

	author := ResourceObject{
		Type: "people",
		ID:   "9",
		Attributes: Attributes{
			"lastName":  "Doe",
			"firstName": "Jane",
		},
		Links: Links{
			"self": URI("https://example.com/people/9"),
		},
	}
	article := ResourceObject{
		Type: "articles",
		ID:   "1",
		Attributes: Attributes{
			"title": "JSON:API paints my bikeshed!",
		},
		Relationships: Relationships{
			"author": {
				Links: Links{
					"related": URI("https://example.com/articles/1/author"),
				},
				Data: ToOne(Identifier{Type: "people", ID: "9"}),
			},
		},
	}
	document := Document{
		JSONAPI: &JSONAPI{
			Version: "1.1",
			Profile: []string{"https://example.com/profiles/a"},
		},
		Links: Links{
			"self": ObjectLink(
				"https://example.com/articles/1",
				Meta{"z": true, "a": "stable"},
			),
		},
		Data:     ResourceData(article),
		Included: []ResourceObject{author},
		Meta:     Meta{"requestId": "abc"},
	}

	first, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	second, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal document again: %v", err)
	}

	want := `{"jsonapi":{"version":"1.1","profile":["https://example.com/profiles/a"]},"links":{"self":{"href":"https://example.com/articles/1","meta":{"a":"stable","z":true}}},"data":{"type":"articles","id":"1","attributes":{"title":"JSON:API paints my bikeshed!"},"relationships":{"author":{"links":{"related":"https://example.com/articles/1/author"},"data":{"type":"people","id":"9"}}}},"included":[{"type":"people","id":"9","attributes":{"firstName":"Jane","lastName":"Doe"},"links":{"self":"https://example.com/people/9"}}],"meta":{"requestId":"abc"}}`
	if string(first) != want {
		t.Fatalf("unexpected JSON:\n got: %s\nwant: %s", first, want)
	}
	if string(second) != string(first) {
		t.Fatalf("serialization is not deterministic:\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestMarshalPrimaryDataShapes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		data *PrimaryData
		want string
	}{
		"absent":     {data: nil, want: `{"meta":{"status":"ok"}}`},
		"null":       {data: NullData(), want: `{"data":null,"meta":{"status":"ok"}}`},
		"empty many": {data: ResourceCollection(), want: `{"data":[],"meta":{"status":"ok"}}`},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(Document{
				Data: test.data,
				Meta: Meta{"status": "ok"},
			})
			if err != nil {
				t.Fatalf("marshal document: %v", err)
			}
			if string(got) != test.want {
				t.Fatalf("unexpected JSON: got %s, want %s", got, test.want)
			}
		})
	}
}

func TestMarshalRelationshipDataShapes(t *testing.T) {
	t.Parallel()

	relationships := Relationships{
		"missing": {Data: NullRelationship()},
		"tags": {
			Data: ToMany(
				Identifier{Type: "tags", ID: "2"},
				Identifier{Type: "tags", LID: "new-tag"},
			),
		},
	}

	got, err := json.Marshal(relationships)
	if err != nil {
		t.Fatalf("marshal relationships: %v", err)
	}
	want := `{"missing":{"data":null},"tags":{"data":[{"type":"tags","id":"2"},{"type":"tags","lid":"new-tag"}]}}`
	if string(got) != want {
		t.Fatalf("unexpected JSON: got %s, want %s", got, want)
	}
}

func TestMarshalErrorDocument(t *testing.T) {
	t.Parallel()

	document := Document{
		Errors: []ErrorObject{{
			ID:     "validation-1",
			Status: "422",
			Code:   "invalid-attribute",
			Title:  "Invalid Attribute",
			Detail: "title must not be blank",
			Source: &ErrorSource{Pointer: "/data/attributes/title"},
			Links: Links{
				"about": URI("https://example.com/problems/invalid-attribute"),
				"type":  NullLink(),
			},
			Meta: Meta{"retryable": false},
		}},
	}

	got, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal error document: %v", err)
	}
	want := `{"errors":[{"id":"validation-1","links":{"about":"https://example.com/problems/invalid-attribute","type":null},"status":"422","code":"invalid-attribute","title":"Invalid Attribute","detail":"title must not be blank","source":{"pointer":"/data/attributes/title"},"meta":{"retryable":false}}]}`
	if string(got) != want {
		t.Fatalf("unexpected JSON: got %s, want %s", got, want)
	}
}
