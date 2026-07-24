package jsonapi

import (
	"errors"
	"testing"
)

func TestAtomicDocumentValidation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		document AtomicDocument
		path     string
		code     string
	}{
		"requires a top-level semantic member": {
			document: AtomicDocument{},
			path:     "",
			code:     "required",
		},
		"operations must not be empty": {
			document: AtomicDocument{Operations: []AtomicOperation{}},
			path:     "/atomic:operations",
			code:     "min-items",
		},
		"results must not be empty": {
			document: AtomicDocument{Results: []AtomicResult{}},
			path:     "/atomic:results",
			code:     "min-items",
		},
		"operations and results conflict": {
			document: AtomicDocument{
				Operations: []AtomicOperation{{
					Op:   AtomicAdd,
					Data: ResourceData(ResourceObject{Type: "articles"}),
				}},
				Results: []AtomicResult{{}},
			},
			path: "/atomic:results",
			code: "conflict",
		},
		"operations and errors conflict": {
			document: AtomicDocument{
				Operations: []AtomicOperation{{
					Op:   AtomicAdd,
					Data: ResourceData(ResourceObject{Type: "articles"}),
				}},
				Errors: []ErrorObject{{Title: "failed"}},
			},
			path: "/errors",
			code: "conflict",
		},
		"pagination links require collection data": {
			document: AtomicDocument{
				Meta:  Meta{},
				Links: Links{"next": URI("/operations?page%5Bafter%5D=one")},
			},
			path: "/links/next",
			code: "pagination-shape",
		},
		"extension declaration includes Atomic": {
			document: AtomicDocument{
				JSONAPI: &JSONAPI{Ext: []string{}},
				Meta:    Meta{},
			},
			path: "/jsonapi/ext",
			code: "missing-extension",
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
			if got := validationError.Violations[0]; got.Path != test.path || got.Code != test.code {
				t.Fatalf("unexpected violation: %#v", got)
			}
		})
	}
}

func TestAtomicDocumentAcceptsEachTopLevelForm(t *testing.T) {
	t.Parallel()

	documents := []AtomicDocument{
		{Operations: []AtomicOperation{{
			Op:   AtomicAdd,
			Data: ResourceData(ResourceObject{Type: "articles"}),
		}}},
		{Results: []AtomicResult{{}}},
		{Errors: []ErrorObject{{Title: "failed"}}},
		{Meta: Meta{}},
	}

	for _, document := range documents {
		if err := document.Validate(); err != nil {
			t.Fatalf("expected valid atomic document: %v", err)
		}
	}
}

func TestAtomicOperationValidation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		operation AtomicOperation
		path      string
		code      string
	}{
		"requires op": {
			operation: AtomicOperation{},
			path:      "/atomic:operations/0/op",
			code:      "required",
		},
		"rejects unknown op": {
			operation: AtomicOperation{Op: "replace", Data: NullData()},
			path:      "/atomic:operations/0/op",
			code:      "value",
		},
		"rejects ref and href together": {
			operation: AtomicOperation{
				Op:   AtomicRemove,
				Ref:  &AtomicReference{Type: "articles", ID: "1"},
				Href: "/articles/1",
			},
			path: "/atomic:operations/0/href",
			code: "conflict",
		},
		"at member is not a reference type": {
			operation: AtomicOperation{
				Op:  AtomicRemove,
				Ref: &AtomicReference{Type: "@articles", ID: "1"},
			},
			path: "/atomic:operations/0/ref/type",
			code: "member-name",
		},
		"reference requires type": {
			operation: AtomicOperation{
				Op:  AtomicRemove,
				Ref: &AtomicReference{ID: "1"},
			},
			path: "/atomic:operations/0/ref/type",
			code: "required",
		},
		"reference requires identity": {
			operation: AtomicOperation{
				Op:  AtomicRemove,
				Ref: &AtomicReference{Type: "articles"},
			},
			path: "/atomic:operations/0/ref/id",
			code: "required",
		},
		"reference identity is exclusive": {
			operation: AtomicOperation{
				Op: AtomicRemove,
				Ref: &AtomicReference{
					Type: "articles",
					ID:   "1",
					LID:  "local-1",
				},
			},
			path: "/atomic:operations/0/ref/lid",
			code: "conflict",
		},
		"relationship name must be valid": {
			operation: AtomicOperation{
				Op: AtomicUpdate,
				Ref: &AtomicReference{
					Type:         "articles",
					ID:           "1",
					Relationship: "-bad",
				},
				Data: NullData(),
			},
			path: "/atomic:operations/0/ref/relationship",
			code: "member-name",
		},
		"href must be a URI reference": {
			operation: AtomicOperation{
				Op:   AtomicRemove,
				Href: ":not-a-reference",
			},
			path: "/atomic:operations/0/href",
			code: "uri-reference",
		},
		"add requires data": {
			operation: AtomicOperation{Op: AtomicAdd},
			path:      "/atomic:operations/0/data",
			code:      "required",
		},
		"add resource must not use ref": {
			operation: AtomicOperation{
				Op:   AtomicAdd,
				Ref:  &AtomicReference{Type: "articles", ID: "1"},
				Data: ResourceData(ResourceObject{Type: "articles"}),
			},
			path: "/atomic:operations/0/ref",
			code: "forbidden",
		},
		"update requires data": {
			operation: AtomicOperation{Op: AtomicUpdate},
			path:      "/atomic:operations/0/data",
			code:      "required",
		},
		"remove resource requires target": {
			operation: AtomicOperation{Op: AtomicRemove},
			path:      "/atomic:operations/0/ref",
			code:      "required",
		},
		"relationship mutation requires data": {
			operation: AtomicOperation{
				Op: AtomicRemove,
				Ref: &AtomicReference{
					Type:         "articles",
					ID:           "1",
					Relationship: "comments",
				},
			},
			path: "/atomic:operations/0/data",
			code: "required",
		},
		"remove resource forbids data": {
			operation: AtomicOperation{
				Op:   AtomicRemove,
				Ref:  &AtomicReference{Type: "articles", ID: "1"},
				Data: ResourceData(ResourceObject{Type: "articles", ID: "1"}),
			},
			path: "/atomic:operations/0/data",
			code: "forbidden",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			document := AtomicDocument{Operations: []AtomicOperation{test.operation}}
			err := document.Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}
			var validationError *ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
			if got := validationError.Violations[0]; got.Path != test.path || got.Code != test.code {
				t.Fatalf("unexpected first violation: %#v", got)
			}
		})
	}
}

func TestAtomicReferenceLIDMustBeAssignedByPriorOperation(t *testing.T) {
	t.Parallel()

	invalid := AtomicDocument{Operations: []AtomicOperation{
		{
			Op:  AtomicRemove,
			Ref: &AtomicReference{Type: "articles", LID: "new-article"},
		},
		{
			Op: AtomicAdd,
			Data: ResourceData(ResourceObject{
				Type: "articles",
				LID:  "new-article",
			}),
		},
	}}
	assertAtomicViolation(t, invalid, "/atomic:operations/0/ref/lid", "unresolved-lid")

	invalidUpdate := AtomicDocument{Operations: []AtomicOperation{{
		Op: AtomicUpdate,
		Data: ResourceData(ResourceObject{
			Type: "articles",
			LID:  "missing-article",
		}),
	}}}
	assertAtomicViolation(
		t,
		invalidUpdate,
		"/atomic:operations/0/data/lid",
		"unresolved-lid",
	)

	invalidRelationship := AtomicDocument{Operations: []AtomicOperation{{
		Op:   AtomicAdd,
		Href: "/articles/1/relationships/comments",
		Data: ResourceCollection(ResourceObject{Type: "comments", LID: "missing-comment"}),
	}}}
	assertAtomicViolation(
		t,
		invalidRelationship,
		"/atomic:operations/0/data/0/lid",
		"unresolved-lid",
	)

	invalidNested := AtomicDocument{Operations: []AtomicOperation{
		{
			Op: AtomicAdd,
			Data: ResourceData(ResourceObject{
				Type: "articles",
				Relationships: Relationships{
					"author": {
						Data: ToOne(Identifier{Type: "people", LID: "future-person"}),
					},
				},
			}),
		},
		{
			Op:   AtomicAdd,
			Data: ResourceData(ResourceObject{Type: "people", LID: "future-person"}),
		},
	}}
	assertAtomicViolation(
		t,
		invalidNested,
		"/atomic:operations/0/data/relationships/author/data/lid",
		"unresolved-lid",
	)

	valid := AtomicDocument{Operations: []AtomicOperation{
		{
			Op: AtomicAdd,
			Data: ResourceData(ResourceObject{
				Type: "articles",
				LID:  "new-article",
			}),
		},
		{
			Op:  AtomicUpdate,
			Ref: &AtomicReference{Type: "articles", LID: "new-article"},
			Data: ResourceData(ResourceObject{
				Type: "articles",
				LID:  "new-article",
				Attributes: Attributes{
					"title": "Updated",
				},
			}),
		},
		{
			Op:   AtomicAdd,
			Data: ResourceData(ResourceObject{Type: "comments", LID: "new-comment"}),
		},
		{
			Op:   AtomicAdd,
			Href: "/articles/1/relationships/comments",
			Data: ResourceCollection(
				ResourceObject{Type: "comments", LID: "new-comment"},
			),
		},
	}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected prior local identity to resolve: %v", err)
	}
}

func assertAtomicViolation(t *testing.T, document AtomicDocument, path, code string) {
	t.Helper()

	err := document.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	for _, violation := range validationError.Violations {
		if violation.Path == path && violation.Code == code {
			return
		}
	}
	t.Fatalf("missing violation at %q with code %q: %#v", path, code, validationError.Violations)
}

func TestAtomicResourceReferenceMustMatchUpdateData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		document AtomicDocument
		path     string
	}{
		{
			document: AtomicDocument{Operations: []AtomicOperation{{
				Op:   AtomicUpdate,
				Ref:  &AtomicReference{Type: "articles", ID: "1"},
				Data: ResourceData(ResourceObject{Type: "comments", ID: "1"}),
			}}},
			path: "/atomic:operations/0/data/type",
		},
		{
			document: AtomicDocument{Operations: []AtomicOperation{{
				Op:   AtomicUpdate,
				Ref:  &AtomicReference{Type: "articles", ID: "1"},
				Data: ResourceData(ResourceObject{Type: "articles", ID: "2"}),
			}}},
			path: "/atomic:operations/0/data/id",
		},
		{
			document: AtomicDocument{Operations: []AtomicOperation{
				{
					Op: AtomicAdd,
					Data: ResourceData(ResourceObject{
						Type: "articles", LID: "first-article",
					}),
				},
				{
					Op: AtomicAdd,
					Data: ResourceData(ResourceObject{
						Type: "articles", LID: "second-article",
					}),
				},
				{
					Op:   AtomicUpdate,
					Ref:  &AtomicReference{Type: "articles", LID: "first-article"},
					Data: ResourceData(ResourceObject{Type: "articles", LID: "second-article"}),
				},
			}},
			path: "/atomic:operations/2/data/lid",
		},
	}
	for _, test := range tests {
		assertAtomicViolation(t, test.document, test.path, "target-mismatch")
	}

	valid := AtomicDocument{Operations: []AtomicOperation{{
		Op:   AtomicUpdate,
		Ref:  &AtomicReference{Type: "articles", ID: "1"},
		Data: ResourceData(ResourceObject{Type: "articles", ID: "1"}),
	}}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("matching ref and data were rejected: %v", err)
	}
}

func TestAtomicOperationValidationAcceptsNormativeShapes(t *testing.T) {
	t.Parallel()

	operations := []AtomicOperation{
		{
			Op:   AtomicAdd,
			Href: "/articles",
			Data: ResourceData(ResourceObject{Type: "articles"}),
		},
		{
			Op: AtomicUpdate,
			Data: ResourceData(ResourceObject{
				Type:       "articles",
				ID:         "1",
				Attributes: Attributes{"title": "Updated"},
			}),
		},
		{
			Op:  AtomicRemove,
			Ref: &AtomicReference{Type: "articles", ID: "1"},
		},
		{
			Op: AtomicAdd,
			Ref: &AtomicReference{
				Type:         "articles",
				ID:           "1",
				Relationship: "comments",
			},
			Data: ResourceCollection(ResourceObject{Type: "comments", ID: "2"}),
		},
		{
			Op: AtomicUpdate,
			Ref: &AtomicReference{
				Type:         "articles",
				ID:           "1",
				Relationship: "author",
			},
			Data: NullData(),
		},
	}

	if err := (AtomicDocument{Operations: operations}).Validate(); err != nil {
		t.Fatalf("expected valid operations: %v", err)
	}
}

func TestAtomicRelationshipIdentifiersPreserveEmptyIdentityPresence(t *testing.T) {
	t.Parallel()

	for name, resource := range map[string]ResourceObject{
		"id":  (ResourceObject{Type: "comments"}).WithID(""),
		"lid": (ResourceObject{Type: "comments"}).WithLID(""),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			operations := []AtomicOperation{{
				Op: AtomicAdd,
				Ref: &AtomicReference{
					Type:         "articles",
					ID:           "1",
					Relationship: "comments",
				},
				Data: ResourceCollection(resource),
			}}
			if name == "lid" {
				operations = append([]AtomicOperation{{
					Op:   AtomicAdd,
					Data: ResourceData((ResourceObject{Type: "comments"}).WithLID("")),
				}}, operations...)
			}
			document := AtomicDocument{Operations: operations}
			if err := document.Validate(); err != nil {
				t.Fatalf("empty %s identity was rejected: %v", name, err)
			}
		})
	}
}

func TestAtomicValidationCoversOperationDataShapes(t *testing.T) {
	t.Parallel()

	valid := []AtomicOperation{
		{
			Op: AtomicRemove,
			Ref: &AtomicReference{
				Type: "articles", ID: "1", Relationship: "tags",
			},
			Data: ResourceCollection(ResourceObject{Type: "tags", ID: "2"}),
		},
		{
			Op:   AtomicAdd,
			Href: "/articles/1/relationships/tags",
			Data: ResourceCollection(ResourceObject{Type: "tags", ID: "2"}),
		},
		{
			Op:   AtomicUpdate,
			Href: "/articles/1/relationships/author",
			Data: NullData(),
		},
		{
			Op:   AtomicUpdate,
			Href: "/articles/1/relationships/tags",
			Data: ResourceCollection(ResourceObject{Type: "tags", ID: "2"}),
		},
		{
			Op:   AtomicRemove,
			Href: "/articles/1/relationships/tags",
			Data: ResourceCollection(ResourceObject{Type: "tags", ID: "2"}),
		},
		{
			Op: AtomicUpdate,
			Ref: &AtomicReference{
				Type: "articles", ID: "1", Relationship: "author",
			},
			Data: ResourceData(ResourceObject{Type: "people", ID: "9"}),
		},
	}
	for _, operation := range valid {
		if err := (AtomicDocument{Operations: []AtomicOperation{operation}}).Validate(); err != nil {
			t.Fatalf("valid operation rejected: %#v: %v", operation, err)
		}
	}

	identifierWithResourceMembers := ResourceObject{
		Type:          "people",
		ID:            "9",
		Attributes:    Attributes{"name": "Jane"},
		Relationships: Relationships{"team": {Data: NullRelationship()}},
		Links:         Links{"self": URI("/people/9")},
	}
	tests := []struct {
		operation AtomicOperation
		path      string
		code      string
	}{
		{
			operation: AtomicOperation{
				Op:   AtomicUpdate,
				Ref:  &AtomicReference{Type: "articles", ID: "1"},
				Data: ResourceCollection(ResourceObject{Type: "articles", ID: "2"}),
			},
			path: "/atomic:operations/0/data",
			code: "shape",
		},
		{
			operation: AtomicOperation{
				Op:   AtomicRemove,
				Href: "/articles/1",
				Data: ResourceData(ResourceObject{Type: "articles", ID: "1"}),
			},
			path: "/atomic:operations/0/data",
			code: "shape",
		},
		{
			operation: AtomicOperation{
				Op:   AtomicAdd,
				Data: ResourceCollection(ResourceObject{Type: "tags", ID: "2"}),
			},
			path: "/atomic:operations/0/ref",
			code: "required",
		},
		{
			operation: AtomicOperation{
				Op:   AtomicRemove,
				Ref:  &AtomicReference{Type: "articles", ID: "1"},
				Data: NullData(),
			},
			path: "/atomic:operations/0/data",
			code: "forbidden",
		},
		{
			operation: AtomicOperation{
				Op: AtomicAdd,
				Ref: &AtomicReference{
					Type: "articles", ID: "1", Relationship: "tags",
				},
				Data: ResourceData(ResourceObject{Type: "tags", ID: "2"}),
			},
			path: "/atomic:operations/0/data",
			code: "shape",
		},
		{
			operation: AtomicOperation{
				Op: AtomicUpdate,
				Ref: &AtomicReference{
					Type: "bad:name", ID: "1", Relationship: "author",
				},
				Data: ResourceData(identifierWithResourceMembers),
			},
			path: "/atomic:operations/0/ref/type",
			code: "member-name",
		},
		{
			operation: AtomicOperation{
				Op: AtomicUpdate,
				Ref: &AtomicReference{
					Type: "articles", ID: "1", Relationship: "author",
				},
				Data: ResourceData(identifierWithResourceMembers),
			},
			path: "/atomic:operations/0/data/attributes",
			code: "forbidden",
		},
		{
			operation: AtomicOperation{
				Op: AtomicUpdate,
				Ref: &AtomicReference{
					Type: "articles", ID: "1", Relationship: "author",
				},
				Data: &PrimaryData{kind: primaryDataKind(99)},
			},
			path: "/atomic:operations/0/data",
			code: "shape",
		},
	}
	for _, test := range tests {
		document := AtomicDocument{Operations: []AtomicOperation{test.operation}}
		err := document.Validate()
		var validationError *ValidationError
		if !errors.As(err, &validationError) ||
			!hasViolation(validationError, test.path, test.code) {
			t.Fatalf("unexpected validation for %#v: %T %#v", test.operation, err, validationError)
		}
	}

	err := (AtomicDocument{Operations: []AtomicOperation{{
		Op: AtomicUpdate,
		Ref: &AtomicReference{
			Type: "articles", ID: "1", Relationship: "author",
		},
		Data: ResourceData(identifierWithResourceMembers),
	}}}).Validate()
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/atomic:operations/0/data/relationships", "forbidden") ||
		!hasViolation(validationError, "/atomic:operations/0/data/links", "forbidden") {
		t.Fatalf("missing identifier-only violations: %#v", validationError)
	}
}

func TestAtomicContextValidation(t *testing.T) {
	t.Parallel()

	validOperation := AtomicOperation{
		Op:   AtomicAdd,
		Data: ResourceData(ResourceObject{Type: "articles"}),
	}
	tests := map[string]struct {
		document AtomicDocument
		options  AtomicValidationOptions
		path     string
		code     string
	}{
		"request requires operations": {
			document: AtomicDocument{Meta: Meta{}},
			options:  AtomicValidationOptions{Context: AtomicRequestContext},
			path:     "/atomic:operations",
			code:     "required",
		},
		"request forbids results": {
			document: AtomicDocument{Results: []AtomicResult{{}}},
			options:  AtomicValidationOptions{Context: AtomicRequestContext},
			path:     "/atomic:results",
			code:     "forbidden",
		},
		"request forbids errors": {
			document: AtomicDocument{Errors: []ErrorObject{{Title: "failed"}}},
			options:  AtomicValidationOptions{Context: AtomicRequestContext},
			path:     "/errors",
			code:     "forbidden",
		},
		"response forbids operations": {
			document: AtomicDocument{Operations: []AtomicOperation{validOperation}},
			options:  AtomicValidationOptions{Context: AtomicResponseContext},
			path:     "/atomic:operations",
			code:     "forbidden",
		},
		"response result count must match": {
			document: AtomicDocument{Results: []AtomicResult{{}}},
			options: AtomicValidationOptions{
				Context:             AtomicResponseContext,
				ExpectedResultCount: 2,
			},
			path: "/atomic:results",
			code: "result-count",
		},
		"response operation count must match": {
			document: AtomicDocument{Results: []AtomicResult{{}}},
			options: AtomicValidationOptions{
				Context: AtomicResponseContext,
				ExpectedOperations: []AtomicOperation{
					{Op: AtomicRemove, Href: "/articles/1"},
					{Op: AtomicRemove, Href: "/articles/2"},
				},
			},
			path: "/atomic:results",
			code: "result-count",
		},
		"successful response requires results": {
			document: AtomicDocument{Meta: Meta{}},
			options: AtomicValidationOptions{
				Context:             AtomicResponseContext,
				ExpectedResultCount: 2,
			},
			path: "/atomic:results",
			code: "required",
		},
		"rejects unknown context": {
			document: AtomicDocument{Meta: Meta{}},
			options: AtomicValidationOptions{
				Context: AtomicValidationContext(255),
			},
			path: "",
			code: "validation-context",
		},
		"rejects negative expected result count": {
			document: AtomicDocument{Meta: Meta{}},
			options: AtomicValidationOptions{
				ExpectedResultCount: -1,
			},
			path: "/atomic:results",
			code: "result-count",
		},
		"rejects conflicting expected result options": {
			document: AtomicDocument{Results: []AtomicResult{{}}},
			options: AtomicValidationOptions{
				Context:             AtomicResponseContext,
				ExpectedResultCount: 2,
				ExpectedOperations: []AtomicOperation{
					{Op: AtomicRemove, Href: "/articles/1"},
				},
			},
			path: "/atomic:results",
			code: "result-count",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertAtomicViolationWith(t, test.document, test.options, test.path, test.code)
		})
	}
}

func TestAtomicResponseValidationUsesRequestOperationSemantics(t *testing.T) {
	t.Parallel()

	resource := ResourceData(ResourceObject{Type: "articles", ID: "1"})
	collection := ResourceCollection(ResourceObject{Type: "articles", ID: "1"})
	assertAtomicViolationWith(
		t,
		AtomicDocument{Results: []AtomicResult{{Data: collection}}},
		AtomicValidationOptions{Context: AtomicResponseContext},
		"/atomic:results/0/data",
		"shape",
	)
	tests := map[string]struct {
		operation AtomicOperation
		result    AtomicResult
		path      string
		code      string
	}{
		"remove result data is forbidden": {
			operation: AtomicOperation{Op: AtomicRemove, Href: "/articles/1"},
			result:    AtomicResult{Data: resource},
			path:      "/atomic:results/0/data",
			code:      "forbidden",
		},
		"relationship result data is forbidden": {
			operation: AtomicOperation{
				Op: AtomicUpdate,
				Ref: &AtomicReference{
					Type: "articles", ID: "1", Relationship: "author",
				},
				Data: ResourceData(ResourceObject{Type: "people", ID: "2"}),
			},
			result: AtomicResult{Data: resource},
			path:   "/atomic:results/0/data",
			code:   "forbidden",
		},
		"resource result must be singular": {
			operation: AtomicOperation{
				Op:   AtomicUpdate,
				Data: ResourceData(ResourceObject{Type: "articles", ID: "1"}),
			},
			result: AtomicResult{Data: collection},
			path:   "/atomic:results/0/data",
			code:   "shape",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertAtomicViolationWith(
				t,
				AtomicDocument{Results: []AtomicResult{test.result}},
				AtomicValidationOptions{
					Context:            AtomicResponseContext,
					ExpectedOperations: []AtomicOperation{test.operation},
				},
				test.path,
				test.code,
			)
		})
	}

	valid := AtomicDocument{Results: []AtomicResult{{Data: resource}, {}}}
	err := valid.ValidateWith(AtomicValidationOptions{
		Context: AtomicResponseContext,
		ExpectedOperations: []AtomicOperation{
			{Op: AtomicAdd, Data: ResourceData(ResourceObject{Type: "articles"})},
			{
				Op: AtomicAdd,
				Ref: &AtomicReference{
					Type: "articles", ID: "1", Relationship: "comments",
				},
				Data: ResourceCollection(ResourceObject{Type: "comments", ID: "2"}),
			},
		},
	})
	if err != nil {
		t.Fatalf("valid operation-aware response rejected: %v", err)
	}
}

func TestAtomicResultRequiresDataForServerGeneratedCreate(t *testing.T) {
	t.Parallel()

	operation := AtomicOperation{
		Op:   AtomicAdd,
		Data: ResourceData(ResourceObject{Type: "articles"}),
	}
	assertAtomicViolationWith(
		t,
		AtomicDocument{Results: []AtomicResult{{}}},
		AtomicValidationOptions{
			Context:            AtomicResponseContext,
			ExpectedOperations: []AtomicOperation{operation},
		},
		"/atomic:results/0/data",
		"required",
	)

	clientGenerated := operation
	clientGenerated.Data = ResourceData(ResourceObject{Type: "articles", ID: "client-id"})
	if err := (AtomicDocument{Results: []AtomicResult{{}}}).ValidateWith(
		AtomicValidationOptions{
			Context:            AtomicResponseContext,
			ExpectedOperations: []AtomicOperation{clientGenerated},
		},
	); err != nil {
		t.Fatalf("client-generated create result may omit unchanged data: %v", err)
	}
}

func TestAtomicContextValidationAcceptsMatchingExchange(t *testing.T) {
	t.Parallel()

	request := AtomicDocument{Operations: []AtomicOperation{{
		Op:   AtomicAdd,
		Data: ResourceData(ResourceObject{Type: "articles"}),
	}}}
	if err := request.ValidateWith(AtomicValidationOptions{Context: AtomicRequestContext}); err != nil {
		t.Fatalf("expected valid request: %v", err)
	}

	response := AtomicDocument{Results: []AtomicResult{{
		Data: ResourceData(ResourceObject{Type: "articles", ID: "1"}),
	}}}
	if err := response.ValidateWith(AtomicValidationOptions{
		Context:             AtomicResponseContext,
		ExpectedResultCount: len(request.Operations),
		ExpectedOperations:  request.Operations,
	}); err != nil {
		t.Fatalf("expected valid response: %v", err)
	}
}

func assertAtomicViolationWith(
	t *testing.T,
	document AtomicDocument,
	options AtomicValidationOptions,
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
	for _, violation := range validationError.Violations {
		if violation.Path == path && violation.Code == code {
			return
		}
	}
	t.Fatalf("missing violation at %q with code %q: %#v", path, code, validationError.Violations)
}
