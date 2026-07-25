package jsonapi

import (
	"errors"
	"testing"
)

func TestValidateJSONAPIExtensionAndProfileURIs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		object JSONAPI
		path   string
		code   string
	}{
		"extension must be absolute URI": {
			object: JSONAPI{Ext: []string{"/relative-extension"}},
			path:   "/jsonapi/ext/0",
			code:   "uri",
		},
		"profile must be absolute URI": {
			object: JSONAPI{Profile: []string{"profile"}},
			path:   "/jsonapi/profile/0",
			code:   "uri",
		},
		"extensions must be unique": {
			object: JSONAPI{Ext: []string{atomicExtension, atomicExtension}},
			path:   "/jsonapi/ext/1",
			code:   "duplicate-uri",
		},
		"profiles must be unique": {
			object: JSONAPI{Profile: []string{cursorProfile, cursorProfile}},
			path:   "/jsonapi/profile/1",
			code:   "duplicate-uri",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := (Document{
				JSONAPI: &test.object,
				Data:    NullData(),
			}).Validate()
			assertValidationViolation(t, err, test.path, test.code)
		})
	}
}

func TestValidateRequiresConsistentLocalIdentity(t *testing.T) {
	t.Parallel()

	document := Document{Data: ResourceData(ResourceObject{
		Type: "articles",
		ID:   "1",
		LID:  "article-one",
		Relationships: Relationships{
			"self": {
				Data: ToOne(Identifier{
					Type: "articles",
					ID:   "1",
					LID:  "different-local-id",
				}),
			},
		},
	})}

	err := document.Validate()
	assertValidationViolation(
		t,
		err,
		"/data/relationships/self/data/lid",
		"local-identity",
	)
}

func TestValidateRejectsDuplicateCanonicalResourceObjects(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		document Document
		path     string
	}{
		"duplicate in primary collection": {
			document: Document{Data: ResourceCollection(
				ResourceObject{Type: "articles", ID: "1"},
				ResourceObject{Type: "articles", ID: "1"},
			)},
			path: "/data/1",
		},
		"included duplicates primary resource": {
			document: Document{
				Data: ResourceData(ResourceObject{
					Type: "articles",
					ID:   "1",
					Relationships: Relationships{
						"self": {Data: ToOne(Identifier{Type: "articles", ID: "1"})},
					},
				}),
				Included: []ResourceObject{{Type: "articles", ID: "1"}},
			},
			path: "/included/0",
		},
		"id and lid aliases identify one resource": {
			document: Document{Data: ResourceCollection(
				ResourceObject{Type: "articles", ID: "1", LID: "local-article"},
				ResourceObject{Type: "articles", LID: "local-article"},
			)},
			path: "/data/1",
		},
		"duplicate representation contributes a new alias": {
			document: Document{Data: ResourceCollection(
				ResourceObject{Type: "articles", ID: "1"},
				ResourceObject{Type: "articles", ID: "1", LID: "local-article"},
				ResourceObject{Type: "articles", LID: "local-article"},
			)},
			path: "/data/2",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := test.document.Validate()
			assertValidationViolation(t, err, test.path, "duplicate-resource")
		})
	}
}

func assertValidationViolation(t *testing.T, err error, path, code string) {
	t.Helper()

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
