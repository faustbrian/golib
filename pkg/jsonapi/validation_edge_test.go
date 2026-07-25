package jsonapi

import (
	"errors"
	"testing"
)

func TestValidateRejectsEdgeDocumentShapes(t *testing.T) {
	t.Parallel()

	validResource := ResourceObject{Type: "articles", ID: "1"}
	tests := []struct {
		document Document
		path     string
		code     string
	}{
		{
			document: Document{Data: &PrimaryData{kind: primaryDataOne}},
			path:     "/data", code: "required",
		},
		{
			document: Document{Data: &PrimaryData{kind: primaryDataKind(99)}},
			path:     "/data", code: "shape",
		},
		{
			document: Document{Data: ResourceData(ResourceObject{
				Type: "articles", ID: "1",
				Attributes: Attributes{"bad/name": true},
			})},
			path: "/data/attributes/bad~1name", code: "member-name",
		},
		{
			document: Document{Data: ResourceData(ResourceObject{
				Type: "articles", ID: "1",
				Relationships: Relationships{"id": {Meta: Meta{}}},
			})},
			path: "/data/relationships/id", code: "reserved-field",
		},
		{
			document: Document{Data: ResourceData(ResourceObject{
				Type: "articles", ID: "1",
				Relationships: Relationships{"bad/name": {Meta: Meta{}}},
			})},
			path: "/data/relationships/bad~1name", code: "member-name",
		},
		{
			document: Document{Data: ResourceData(ResourceObject{
				Type: "articles", ID: "1",
				Relationships: Relationships{"author": {
					Data: &RelationshipData{kind: relationshipDataOne},
				}},
			})},
			path: "/data/relationships/author/data", code: "required",
		},
		{
			document: Document{Data: ResourceData(ResourceObject{
				Type: "articles", ID: "1",
				Relationships: Relationships{"author": {
					Data: &RelationshipData{kind: relationshipDataKind(99)},
				}},
			})},
			path: "/data/relationships/author/data", code: "shape",
		},
		{
			document: Document{Data: ResourceData(ResourceObject{
				Type: "articles", ID: "1",
				Relationships: Relationships{"author": {
					Data: ToOne(Identifier{ID: "9"}),
				}},
			})},
			path: "/data/relationships/author/data/type", code: "required",
		},
		{
			document: Document{Data: ResourceData(ResourceObject{
				Type: "articles", ID: "1",
				Relationships: Relationships{"author": {
					Data: ToOne(Identifier{Type: "bad/type", ID: "9"}),
				}},
			})},
			path: "/data/relationships/author/data/type", code: "member-name",
		},
		{
			document: Document{
				Data:  ResourceData(validResource),
				Links: Links{"bad/name": URI("/articles/1")},
			},
			path: "/links/bad~1name", code: "member-name",
		},
		{
			document: Document{Errors: []ErrorObject{{
				Source: &ErrorSource{Pointer: "/data/~"},
			}}},
			path: "/errors/0/source/pointer", code: "json-pointer",
		},
	}
	for _, test := range tests {
		err := test.document.Validate()
		var validationError *ValidationError
		if !errors.As(err, &validationError) ||
			!hasViolation(validationError, test.path, test.code) {
			t.Fatalf("missing %s at %s: %T %#v", test.code, test.path, err, validationError)
		}
	}
}

func TestAtMembersDoNotSatisfyRequiredObjectContent(t *testing.T) {
	t.Parallel()

	assertContextViolation(
		t,
		Document{AdditionalMembers: Members{"@context": "https://example.com"}},
		ValidationOptions{},
		"",
		"required",
	)

	document := Document{Data: ResourceData(ResourceObject{
		Type: "articles",
		ID:   "1",
		Relationships: Relationships{"author": {
			AdditionalMembers: Members{"@annotation": true},
		}},
	})}
	assertContextViolation(
		t,
		document,
		ValidationOptions{},
		"/data/relationships/author",
		"required",
	)
}

func TestValidateAllowsLIDAsAResourceFieldName(t *testing.T) {
	t.Parallel()

	document := Document{Data: ResourceData(ResourceObject{
		Type:       "articles",
		ID:         "1",
		Attributes: Attributes{"lid": "attribute"},
	})}
	if err := document.ValidateWith(ValidationOptions{Context: Response}); err != nil {
		t.Fatalf("lid attribute name must be accepted: %v", err)
	}

	document = Document{Data: ResourceData(ResourceObject{
		Type: "articles",
		ID:   "1",
		Relationships: Relationships{
			"lid": {Data: ToOne(Identifier{Type: "people", ID: "2"})},
		},
	})}
	if err := document.ValidateWith(ValidationOptions{Context: Response}); err != nil {
		t.Fatalf("lid relationship name must be accepted: %v", err)
	}
}

func TestRelationshipLinksMustContainAQualifyingLink(t *testing.T) {
	t.Parallel()

	tests := map[string]Links{
		"empty links":      {},
		"pagination alone": {"next": URI("/articles/1/comments?page%5Bafter%5D=one")},
	}
	for name, links := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			document := Document{Data: ResourceData(ResourceObject{
				Type: "articles",
				ID:   "1",
				Relationships: Relationships{
					"comments": {Links: links},
				},
			})}
			err := document.ValidateWith(ValidationOptions{Context: Response})
			var validationError *ValidationError
			if !errors.As(err, &validationError) ||
				!hasViolation(validationError, "/data/relationships/comments", "required") {
				t.Fatalf("expected qualifying-link violation, got %#v", validationError)
			}
		})
	}

	for name, links := range map[string]Links{
		"self":    {"self": NullLink()},
		"related": {"related": URI("/articles/1/comments")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			document := Document{Data: ResourceData(ResourceObject{
				Type: "articles",
				ID:   "1",
				Relationships: Relationships{
					"comments": {Links: links},
				},
			})}
			if err := document.ValidateWith(ValidationOptions{Context: Response}); err != nil {
				t.Fatalf("qualifying relationship link rejected: %v", err)
			}
		})
	}
}

func TestPaginationLinksRequireCollectionData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		document Document
		path     string
	}{
		{
			document: Document{
				Data:  ResourceData(ResourceObject{Type: "articles", ID: "1"}),
				Links: Links{"next": URI("/articles?page%5Bafter%5D=one")},
			},
			path: "/links/next",
		},
		{
			document: Document{
				Meta:  Meta{},
				Links: Links{"prev": NullLink()},
			},
			path: "/links/prev",
		},
		{
			document: Document{Data: ResourceData(ResourceObject{
				Type: "articles",
				ID:   "1",
				Relationships: Relationships{
					"author": {
						Data:  ToOne(Identifier{Type: "people", ID: "2"}),
						Links: Links{"next": URI("/people?page%5Bafter%5D=one")},
					},
				},
			})},
			path: "/data/relationships/author/links/next",
		},
	}
	for _, test := range tests {
		err := test.document.ValidateWith(ValidationOptions{Context: Response})
		var validationError *ValidationError
		if !errors.As(err, &validationError) ||
			!hasViolation(validationError, test.path, "pagination-shape") {
			t.Fatalf("pagination link at %s was accepted: %v", test.path, err)
		}
	}

	valid := Document{
		Data:  ResourceCollection(ResourceObject{Type: "articles", ID: "1"}),
		Links: Links{"next": NullLink()},
	}
	if err := valid.ValidateWith(ValidationOptions{Context: Response}); err != nil {
		t.Fatalf("collection pagination link was rejected: %v", err)
	}
	valid = Document{Data: ResourceData(ResourceObject{
		Type: "articles",
		ID:   "1",
		Relationships: Relationships{
			"comments": {
				Data:  ToMany(Identifier{Type: "comments", ID: "2"}),
				Links: Links{"next": NullLink()},
			},
		},
	})}
	if err := valid.ValidateWith(ValidationOptions{Context: Response}); err != nil {
		t.Fatalf("to-many pagination link was rejected: %v", err)
	}
}

func TestMemberNameAndJSONPointerGrammarBoundaries(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"a", "@context", "two words", "ümlaut", "\u0080"} {
		if !validMemberName(name) {
			t.Fatalf("valid member name rejected: %q", name)
		}
	}
	for _, name := range []string{"", "@", "-start", "end-", "bad/name", "\u007f"} {
		if validMemberName(name) {
			t.Fatalf("invalid member name accepted: %q", name)
		}
	}

	for _, pointer := range []string{"", "/data", "/a~0b/~1"} {
		if !validJSONPointer(pointer) {
			t.Fatalf("valid JSON Pointer rejected: %q", pointer)
		}
	}
	for _, pointer := range []string{"data", "/data/~", "/data/~2"} {
		if validJSONPointer(pointer) {
			t.Fatalf("invalid JSON Pointer accepted: %q", pointer)
		}
	}
}

func TestAtMemberNamesAreNotSemanticNames(t *testing.T) {
	t.Parallel()

	document := Document{Data: ResourceData(ResourceObject{Type: "@articles", ID: "1"})}
	err := document.Validate()
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/data/type", "member-name") {
		t.Fatalf("@ resource type was accepted: %v", err)
	}

	document = Document{Data: ResourceData(ResourceObject{
		Type: "articles",
		ID:   "1",
		Relationships: Relationships{
			"author": {Data: ToOne(Identifier{Type: "@people", ID: "2"})},
		},
	})}
	err = document.Validate()
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/data/relationships/author/data/type", "member-name") {
		t.Fatalf("@ identifier type was accepted: %v", err)
	}

	document = Document{
		Data: ResourceData(ResourceObject{
			Type:       "articles",
			ID:         "1",
			Attributes: Attributes{"@annotation": true},
			Relationships: Relationships{
				"@annotation": {},
			},
		}),
		Links: Links{"@context": URI("not a valid URI reference ")},
	}
	if err := document.Validate(); err != nil {
		t.Fatalf("constructed @ members were interpreted: %v", err)
	}

	document = Document{Data: ResourceData(ResourceObject{
		Type:          "articles",
		ID:            "1",
		Attributes:    Attributes{"@": true},
		Relationships: Relationships{"@": {}},
	}), Links: Links{"@": URI("/ignored")}}
	err = document.Validate()
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/data/attributes/@", "member-name") ||
		!hasViolation(validationError, "/data/relationships/@", "member-name") ||
		!hasViolation(validationError, "/links/@", "member-name") {
		t.Fatalf("invalid @ members were accepted: %#v", validationError)
	}

	document = Document{
		Data: ResourceData(ResourceObject{
			Type: "articles",
			ID:   "1",
			Relationships: Relationships{
				"@annotation": {
					Data: ToOne(Identifier{Type: "people", ID: "2"}),
				},
			},
		}),
		Included: []ResourceObject{{Type: "people", ID: "2"}},
	}
	err = document.Validate()
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/included/0", "full-linkage") {
		t.Fatalf("@ relationship was used for compound linkage: %#v", validationError)
	}
}

func TestValidateRelationshipEndpointIdentifiersRejectResourceMembers(t *testing.T) {
	t.Parallel()

	document := Document{Data: ResourceData(ResourceObject{
		Type: "people", ID: "9",
		Relationships: Relationships{"team": {Data: NullRelationship()}},
		Links:         Links{"self": URI("/people/9")},
	})}
	err := document.ValidateWith(ValidationOptions{Context: ToOneRelationshipRequest})
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/data/relationships", "forbidden") ||
		!hasViolation(validationError, "/data/links", "forbidden") {
		t.Fatalf("missing identifier-only violations: %#v", validationError)
	}

	missing := Document{Meta: Meta{"request": true}}
	if err := missing.ValidateWith(ValidationOptions{
		Context: ToOneRelationshipRequest,
	}); err == nil {
		t.Fatal("relationship request without data was accepted")
	}

	localOnly := Document{Data: ResourceData(ResourceObject{
		Type: "people", LID: "local-person",
	})}
	err = localOnly.ValidateWith(ValidationOptions{Context: ToOneRelationshipRequest})
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/data/id", "required") {
		t.Fatalf("relationship identifier without server ID was accepted: %#v", validationError)
	}
}

func TestValidateDetectsLocalIDReuseAcrossResourceIDs(t *testing.T) {
	t.Parallel()

	document := Document{Data: ResourceCollection(
		ResourceObject{Type: "articles", ID: "1", LID: "same"},
		ResourceObject{Type: "articles", ID: "2", LID: "same"},
	)}
	err := document.Validate()
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/data/1/lid", "local-identity") {
		t.Fatalf("missing local-ID reuse violation: %#v", validationError)
	}
}

func TestValidateAcceptsToManyCompoundLinkage(t *testing.T) {
	t.Parallel()

	document := Document{
		Data: ResourceData(ResourceObject{
			Type: "articles", ID: "1",
			Relationships: Relationships{"tags": {Data: ToMany(
				Identifier{Type: "tags", ID: "2"},
				Identifier{Type: "tags", ID: "3"},
			)}},
		}),
		Included: []ResourceObject{
			{Type: "tags", ID: "2"},
			{Type: "tags", ID: "3"},
		},
	}
	if err := document.Validate(); err != nil {
		t.Fatalf("valid to-many compound linkage rejected: %v", err)
	}
}

func TestValidateIdentityTraversalIgnoresRelationshipsWithoutLinkage(t *testing.T) {
	t.Parallel()

	document := Document{Data: ResourceData(ResourceObject{
		Type: "articles", ID: "1",
		Relationships: Relationships{
			"author": {Data: ToOne(Identifier{Type: "people", ID: "9"})},
			"editor": {Data: NullRelationship()},
		},
	}), Included: []ResourceObject{{Type: "people", ID: "9"}}}
	if err := document.Validate(); err != nil {
		t.Fatalf("metadata-only relationship rejected: %v", err)
	}
}

func TestLanguageTagFastFailureForms(t *testing.T) {
	t.Parallel()

	for _, tag := range []string{"", "-en", "en-", "en--US"} {
		if validLanguageTag(tag) {
			t.Fatalf("invalid language tag accepted: %q", tag)
		}
	}
}

func TestErrorStatusMustBeAValidHTTPStatusCode(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"", "99", "099", "600", "abc"} {
		document := Document{Errors: []ErrorObject{
			(ErrorObject{Title: "failure"}).WithStatus(status),
		}}
		err := document.Validate()
		var validationError *ValidationError
		if !errors.As(err, &validationError) ||
			!hasViolation(validationError, "/errors/0/status", "http-status") {
			t.Fatalf("invalid status %q was accepted: %v", status, err)
		}
	}
	if err := (Document{Errors: []ErrorObject{{
		Title:  "failure",
		Status: "471",
	}}}).Validate(); err != nil {
		t.Fatalf("valid unregistered HTTP status was rejected: %v", err)
	}
}

func TestLinksRejectCoreMembersFromTheWrongObjectScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		document Document
		path     string
	}{
		{
			document: Document{Meta: Meta{}, Links: Links{"about": URI("/about")}},
			path:     "/links/about",
		},
		{
			document: Document{Data: ResourceData(ResourceObject{
				Type: "articles", ID: "1", Links: Links{"related": URI("/related")},
			})},
			path: "/data/links/related",
		},
		{
			document: Document{Data: ResourceData(ResourceObject{
				Type: "articles", ID: "1", Relationships: Relationships{
					"author": {Links: Links{
						"self":        URI("/relationship"),
						"describedby": URI("/schema"),
					}},
				},
			})},
			path: "/data/relationships/author/links/describedby",
		},
		{
			document: Document{Errors: []ErrorObject{{
				Links: Links{"self": URI("/error")},
			}}},
			path: "/errors/0/links/self",
		},
	}
	for _, test := range tests {
		err := test.document.Validate()
		var validationError *ValidationError
		if !errors.As(err, &validationError) ||
			!hasViolation(validationError, test.path, "link-scope") {
			t.Fatalf("wrong-scope link at %s was accepted: %v", test.path, err)
		}
	}
}
