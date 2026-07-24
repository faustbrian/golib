package jsonapi

import (
	"errors"
	"testing"
)

func TestCreateRequestContextAllowsNewResourceWithoutIdentity(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"data":{"type":"articles","attributes":{"title":"New"},"relationships":{"author":{"data":{"type":"people","id":"9"}}}}}`)
	options := ValidationOptions{
		Context:      CreateRequest,
		ExpectedType: "articles",
	}
	document, err := UnmarshalWith(payload, options)
	if err != nil {
		t.Fatalf("unmarshal create request: %v", err)
	}
	got, err := MarshalWith(document, options)
	if err != nil {
		t.Fatalf("marshal create request: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("unexpected round trip: got %s, want %s", got, payload)
	}

	if _, err := Unmarshal(payload); err == nil {
		t.Fatal("generic document validation must not assume create-request identity rules")
	}
}

func TestValidateCreateRequestContext(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		document Document
		options  ValidationOptions
		path     string
		code     string
	}{
		"requires data": {
			document: Document{Meta: Meta{"request": true}},
			options:  ValidationOptions{Context: CreateRequest},
			path:     "/data",
			code:     "required",
		},
		"requires one resource": {
			document: Document{Data: ResourceCollection(ResourceObject{Type: "articles"})},
			options:  ValidationOptions{Context: CreateRequest},
			path:     "/data",
			code:     "shape",
		},
		"rejects errors": {
			document: Document{Errors: []ErrorObject{{Title: "bad"}}},
			options:  ValidationOptions{Context: CreateRequest},
			path:     "/errors",
			code:     "forbidden",
		},
		"requires relationship linkage": {
			document: Document{Data: ResourceData(ResourceObject{
				Type: "articles",
				Relationships: Relationships{
					"author": {Links: Links{"related": URI("/people/9")}},
				},
			})},
			options: ValidationOptions{Context: CreateRequest},
			path:    "/data/relationships/author/data",
			code:    "required",
		},
		"must target collection type": {
			document: Document{Data: ResourceData(ResourceObject{Type: "comments"})},
			options:  ValidationOptions{Context: CreateRequest, ExpectedType: "articles"},
			path:     "/data/type",
			code:     "endpoint-mismatch",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertContextViolation(t, test.document, test.options, test.path, test.code)
		})
	}
}

func TestValidateUpdateRequestContext(t *testing.T) {
	t.Parallel()

	valid := Document{Data: ResourceData(ResourceObject{
		Type:       "articles",
		ID:         "1",
		Attributes: Attributes{"title": "Updated"},
		Relationships: Relationships{
			"author": {Data: NullRelationship()},
		},
	})}
	options := ValidationOptions{
		Context:      UpdateRequest,
		ExpectedType: "articles",
		ExpectedID:   "1",
	}
	if err := valid.ValidateWith(options); err != nil {
		t.Fatalf("validate update request: %v", err)
	}

	tests := map[string]struct {
		document Document
		path     string
		code     string
	}{
		"requires id not lid": {
			document: Document{Data: ResourceData(ResourceObject{Type: "articles", LID: "local"})},
			path:     "/data/id",
			code:     "required",
		},
		"requires one resource": {
			document: Document{Data: NullData()},
			path:     "/data",
			code:     "shape",
		},
		"requires relationship linkage": {
			document: Document{Data: ResourceData(ResourceObject{
				Type: "articles",
				ID:   "1",
				Relationships: Relationships{
					"author": {Meta: Meta{"note": "missing data"}},
				},
			})},
			path: "/data/relationships/author/data",
			code: "required",
		},
		"type must match endpoint": {
			document: Document{Data: ResourceData(ResourceObject{Type: "comments", ID: "1"})},
			path:     "/data/type",
			code:     "endpoint-mismatch",
		},
		"id must match endpoint": {
			document: Document{Data: ResourceData(ResourceObject{Type: "articles", ID: "2"})},
			path:     "/data/id",
			code:     "endpoint-mismatch",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertContextViolation(t, test.document, options, test.path, test.code)
		})
	}
}

func TestValidateRelationshipRequestContexts(t *testing.T) {
	t.Parallel()

	valid := []struct {
		name     string
		document Document
		context  ValidationContext
	}{
		{"clear to one", Document{Data: NullData()}, ToOneRelationshipRequest},
		{"set to one", Document{Data: ResourceData(ResourceObject{Type: "people", ID: "9"})}, ToOneRelationshipRequest},
		{"clear to many", Document{Data: ResourceCollection()}, ToManyRelationshipRequest},
		{"set to many", Document{Data: ResourceCollection(ResourceObject{Type: "tags", ID: "2"})}, ToManyRelationshipRequest},
	}
	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.document.ValidateWith(ValidationOptions{Context: test.context}); err != nil {
				t.Fatalf("validate relationship request: %v", err)
			}
		})
	}

	invalid := []struct {
		name     string
		document Document
		context  ValidationContext
		path     string
		code     string
	}{
		{
			"to one rejects collection",
			Document{Data: ResourceCollection()},
			ToOneRelationshipRequest,
			"/data",
			"shape",
		},
		{
			"to many rejects single",
			Document{Data: ResourceData(ResourceObject{Type: "tags", ID: "2"})},
			ToManyRelationshipRequest,
			"/data",
			"shape",
		},
		{
			"identifier rejects attributes",
			Document{Data: ResourceData(ResourceObject{
				Type:       "people",
				ID:         "9",
				Attributes: Attributes{"name": "Jane"},
			})},
			ToOneRelationshipRequest,
			"/data/attributes",
			"forbidden",
		},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertContextViolation(
				t,
				test.document,
				ValidationOptions{Context: test.context},
				test.path,
				test.code,
			)
		})
	}
}

func TestResponseContextRequiresServerIdentity(t *testing.T) {
	t.Parallel()

	assertContextViolation(
		t,
		Document{Data: ResourceData(ResourceObject{Type: "articles", LID: "new"})},
		ValidationOptions{Context: Response},
		"/data/id",
		"required",
	)
}

func TestUpdateRequestCanMatchAnExplicitlyEmptyEndpointID(t *testing.T) {
	t.Parallel()

	options := ValidationOptions{
		Context:           UpdateRequest,
		ExpectedType:      "articles",
		ExpectedIDPresent: true,
	}
	matching := Document{Data: ResourceData(
		(ResourceObject{Type: "articles"}).WithID(""),
	)}
	if err := matching.ValidateWith(options); err != nil {
		t.Fatalf("validate matching empty endpoint id: %v", err)
	}

	mismatched := Document{Data: ResourceData(ResourceObject{
		Type: "articles",
		ID:   "1",
	})}
	assertContextViolation(
		t,
		mismatched,
		options,
		"/data/id",
		"endpoint-mismatch",
	)
}

func TestValidationRejectsAnUnknownContext(t *testing.T) {
	t.Parallel()

	assertContextViolation(
		t,
		Document{Meta: Meta{}},
		ValidationOptions{Context: ValidationContext(255)},
		"",
		"validation-context",
	)
}

func assertContextViolation(
	t *testing.T,
	document Document,
	options ValidationOptions,
	path string,
	code string,
) {
	t.Helper()

	err := document.ValidateWith(options)
	if err == nil {
		t.Fatal("expected validation error")
	}
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if !hasViolation(validationError, path, code) {
		t.Fatalf(
			"missing violation path %q code %q in %#v",
			path,
			code,
			validationError.Violations,
		)
	}
}
