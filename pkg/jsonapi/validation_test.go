package jsonapi

import (
	"errors"
	"testing"
)

func TestValidateRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()

	validResource := ResourceObject{Type: "articles", ID: "1"}
	tests := map[string]struct {
		document Document
		path     string
		code     string
	}{
		"missing required top-level member": {
			document: Document{JSONAPI: &JSONAPI{Version: "1.1"}},
			path:     "",
			code:     "required",
		},
		"data and errors coexist": {
			document: Document{
				Data:   ResourceData(validResource),
				Errors: []ErrorObject{{Title: "failure"}},
			},
			path: "/errors",
			code: "conflict",
		},
		"included without data": {
			document: Document{
				Included: []ResourceObject{validResource},
				Meta:     Meta{"status": "invalid"},
			},
			path: "/included",
			code: "requires-data",
		},
		"resource misses type": {
			document: Document{Data: ResourceData(ResourceObject{ID: "1"})},
			path:     "/data/type",
			code:     "required",
		},
		"resource misses id": {
			document: Document{Data: ResourceData(ResourceObject{Type: "articles"})},
			path:     "/data/id",
			code:     "required",
		},
		"resource type is not a valid member name": {
			document: Document{Data: ResourceData(ResourceObject{Type: "bad/type", ID: "1"})},
			path:     "/data/type",
			code:     "member-name",
		},
		"attribute uses reserved field name": {
			document: Document{Data: ResourceData(ResourceObject{
				Type:       "articles",
				ID:         "1",
				Attributes: Attributes{"id": "shadow"},
			})},
			path: "/data/attributes/id",
			code: "reserved-field",
		},
		"attribute conflicts with relationship": {
			document: Document{Data: ResourceData(ResourceObject{
				Type:       "articles",
				ID:         "1",
				Attributes: Attributes{"author": "Jane"},
				Relationships: Relationships{
					"author": {Meta: Meta{"loaded": true}},
				},
			})},
			path: "/data/relationships/author",
			code: "duplicate-field",
		},
		"relationship is empty": {
			document: Document{Data: ResourceData(ResourceObject{
				Type: "articles",
				ID:   "1",
				Relationships: Relationships{
					"author": {},
				},
			})},
			path: "/data/relationships/author",
			code: "required",
		},
		"relationship identifier misses id and lid": {
			document: Document{Data: ResourceData(ResourceObject{
				Type: "articles",
				ID:   "1",
				Relationships: Relationships{
					"author": {Data: ToOne(Identifier{Type: "people"})},
				},
			})},
			path: "/data/relationships/author/data/id",
			code: "required",
		},
		"invalid link URL": {
			document: Document{
				Data:  ResourceData(validResource),
				Links: Links{"self": URI(":not-a-uri")},
			},
			path: "/links/self",
			code: "uri-reference",
		},
		"error object is empty": {
			document: Document{Errors: []ErrorObject{{}}},
			path:     "/errors/0",
			code:     "required",
		},
		"error pointer is not a JSON pointer": {
			document: Document{Errors: []ErrorObject{{
				Source: &ErrorSource{Pointer: "data/attributes/title"},
			}}},
			path: "/errors/0/source/pointer",
			code: "json-pointer",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := test.document.Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}

			var validationError *ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
			if !hasViolation(validationError, test.path, test.code) {
				t.Fatalf(
					"missing violation path %q code %q in %#v",
					test.path,
					test.code,
					validationError.Violations,
				)
			}
		})
	}
}

func TestValidateWithAllowsSparseFieldsetFullLinkageException(t *testing.T) {
	t.Parallel()

	document := Document{
		Data: ResourceData(ResourceObject{Type: "articles", ID: "1"}),
		Included: []ResourceObject{
			{Type: "people", ID: "9"},
		},
	}

	if err := document.Validate(); err == nil {
		t.Fatal("expected ordinary validation to enforce full linkage")
	}
	if err := document.ValidateWith(ValidationOptions{
		SparseFieldsetsOmittedLinkage: true,
	}); err != nil {
		t.Fatalf("expected sparse fieldset exception to validate: %v", err)
	}
}

func TestValidateAcceptsValidCompoundDocument(t *testing.T) {
	t.Parallel()

	document := Document{
		Data: ResourceData(ResourceObject{
			Type:       "articles",
			ID:         "1",
			Attributes: Attributes{"title": "JSON:API"},
			Relationships: Relationships{
				"author": {Data: ToOne(Identifier{Type: "people", ID: "9"})},
			},
		}),
		Included: []ResourceObject{{
			Type:       "people",
			ID:         "9",
			Attributes: Attributes{"name": "Jane"},
		}},
	}

	if err := document.Validate(); err != nil {
		t.Fatalf("validate document: %v", err)
	}
}

func TestValidateLinksIncludedResourceThroughItsLocalIdentity(t *testing.T) {
	t.Parallel()

	document := Document{
		Data: ResourceData(ResourceObject{
			Type: "articles",
			ID:   "1",
			Relationships: Relationships{
				"author": {
					Data: ToOne(Identifier{Type: "people", LID: "local-person"}),
				},
				"editor": {
					Data: ToOne(Identifier{Type: "people", LID: "local-person"}),
				},
			},
		}),
		Included: []ResourceObject{{
			Type: "people",
			ID:   "9",
			LID:  "local-person",
		}},
	}

	if err := document.Validate(); err != nil {
		t.Fatalf("local identity linkage was rejected: %v", err)
	}
}

func TestValidateRejectsBrokenCompoundDocuments(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		included []ResourceObject
		path     string
		code     string
	}{
		"included resource has no linkage": {
			included: []ResourceObject{{Type: "people", ID: "10"}},
			path:     "/included/0",
			code:     "full-linkage",
		},
		"included resource duplicates another": {
			included: []ResourceObject{
				{Type: "people", ID: "9"},
				{Type: "people", ID: "9"},
			},
			path: "/included/1",
			code: "duplicate-resource",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			document := Document{
				Data: ResourceData(ResourceObject{
					Type: "articles",
					ID:   "1",
					Relationships: Relationships{
						"author": {
							Data: ToOne(Identifier{Type: "people", ID: "9"}),
						},
					},
				}),
				Included: test.included,
			}

			err := document.Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}
			var validationError *ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
			if !hasViolation(validationError, test.path, test.code) {
				t.Fatalf(
					"missing violation path %q code %q in %#v",
					test.path,
					test.code,
					validationError.Violations,
				)
			}
		})
	}
}

func hasViolation(validationError *ValidationError, path, code string) bool {
	for _, violation := range validationError.Violations {
		if violation.Path == path && violation.Code == code {
			return true
		}
	}

	return false
}
