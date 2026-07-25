package jsonapi

import (
	"errors"
	"testing"
)

func TestValidateCursorPageAcceptsConformantRangePage(t *testing.T) {
	t.Parallel()

	truncated := true
	meta, err := (CursorPageMeta{RangeTruncated: &truncated}).Meta()
	if err != nil {
		t.Fatalf("build page metadata: %v", err)
	}
	page := CursorPage{
		Request: CursorPageRequest{
			Size:          2,
			After:         "start",
			AfterPresent:  true,
			Before:        "end",
			BeforePresent: true,
			Range:         true,
		},
		Links: Links{
			"prev": URI("/articles?page%5Bbefore%5D=one"),
			"next": URI("/articles?page%5Bafter%5D=two"),
		},
		Meta:    meta,
		Items:   []Meta{CursorItemMeta("one"), CursorItemMeta("two")},
		HasMore: true,
	}
	if err := page.Validate(); err != nil {
		t.Fatalf("expected conformant page: %v", err)
	}
}

func TestValidateCursorPageRejectsResultContractViolations(t *testing.T) {
	t.Parallel()

	base := func() CursorPage {
		return CursorPage{
			Request: CursorPageRequest{Size: 2},
			Links: Links{
				"prev": NullLink(),
				"next": NullLink(),
			},
			Items: []Meta{nil, nil},
		}
	}
	tests := map[string]struct {
		mutate func(*CursorPage)
		path   string
		code   string
	}{
		"used page size must be positive": {
			mutate: func(page *CursorPage) {
				page.Request.Size = 0
				page.Items = nil
			},
			path: "/data",
			code: "page-size",
		},
		"page exceeds used size": {
			mutate: func(page *CursorPage) { page.Items = append(page.Items, nil) },
			path:   "/data",
			code:   "page-size",
		},
		"short page claims more items": {
			mutate: func(page *CursorPage) {
				page.Items = page.Items[:1]
				page.HasMore = true
			},
			path: "/data",
			code: "page-size",
		},
		"truncated range requires metadata": {
			mutate: func(page *CursorPage) {
				page.Request.Range = true
				page.HasMore = true
			},
			path: "/meta/page/rangeTruncated",
			code: "required",
		},
		"ordinary page forbids range truncation": {
			mutate: func(page *CursorPage) {
				value := true
				page.Meta, _ = (CursorPageMeta{RangeTruncated: &value}).Meta()
			},
			path: "/meta/page/rangeTruncated",
			code: "forbidden",
		},
		"untruncated range forbids truncated metadata": {
			mutate: func(page *CursorPage) {
				page.Request = CursorPageRequest{
					Size:          2,
					AfterPresent:  true,
					BeforePresent: true,
					Range:         true,
				}
				value := true
				page.Meta, _ = (CursorPageMeta{RangeTruncated: &value}).Meta()
			},
			path: "/meta/page/rangeTruncated",
			code: "inconsistent",
		},
		"known absent next page requires null": {
			mutate: func(page *CursorPage) {
				page.Links["next"] = URI("/articles?page%5Bafter%5D=two")
			},
			path: "/links/next",
			code: "link-state",
		},
		"known next page requires a URI": {
			mutate: func(page *CursorPage) {
				page.HasNext = true
			},
			path: "/links/next",
			code: "link-state",
		},
		"known absent previous page requires null": {
			mutate: func(page *CursorPage) {
				page.Links["prev"] = URI("/articles?page%5Bbefore%5D=one")
			},
			path: "/links/prev",
			code: "link-state",
		},
		"known previous page requires a URI": {
			mutate: func(page *CursorPage) {
				page.HasPrevious = true
			},
			path: "/links/prev",
			code: "link-state",
		},
		"initial page cannot have previous results": {
			mutate: func(page *CursorPage) {
				page.HasPrevious = true
				page.Links["prev"] = URI("/articles?page%5Bbefore%5D=one")
			},
			path: "/data",
			code: "page-start",
		},
		"item cursor must be a string": {
			mutate: func(page *CursorPage) {
				page.Items[1] = Meta{"page": map[string]any{"cursor": 42}}
			},
			path: "/data/1/meta/page/cursor",
			code: "type",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			page := base()
			test.mutate(&page)
			err := page.Validate()
			var validationError *ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
			if !hasViolation(validationError, test.path, test.code) {
				t.Fatalf("missing %s at %s: %#v", test.code, test.path, validationError.Violations)
			}
		})
	}
}

func TestValidateCursorPageOnlyRequiresKnownDirectionLinkState(t *testing.T) {
	t.Parallel()

	tests := map[string]CursorPage{
		"after request may use speculative prev": {
			Request: CursorPageRequest{Size: 1, AfterPresent: true},
			Links: Links{
				"prev": URI("/articles?page%5Bbefore%5D=one"),
				"next": NullLink(),
			},
			Items: []Meta{nil},
		},
		"before request may use speculative next": {
			Request: CursorPageRequest{Size: 1, BeforePresent: true},
			Links: Links{
				"prev": NullLink(),
				"next": URI("/articles?page%5Bafter%5D=one"),
			},
			Items: []Meta{nil},
		},
	}
	for name, page := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := page.Validate(); err != nil {
				t.Fatalf("speculative opposite-direction link rejected: %v", err)
			}
		})
	}
}

func TestValidateCursorPageUsesAnAliasedMetadataMember(t *testing.T) {
	t.Parallel()

	total := int64(1)
	meta, err := (CursorPageMeta{Total: &total}).MetaAs("cursorPage")
	if err != nil {
		t.Fatalf("build aliased metadata: %v", err)
	}
	itemMeta, err := CursorItemMetaAs("cursorPage", "one")
	if err != nil {
		t.Fatalf("build aliased item metadata: %v", err)
	}
	page := CursorPage{
		Request: CursorPageRequest{Size: 1, PageMember: "cursorPage"},
		Links:   Links{"prev": NullLink(), "next": NullLink()},
		Meta:    meta,
		Items:   []Meta{itemMeta},
	}
	if err := page.Validate(); err != nil {
		t.Fatalf("aliased page metadata rejected: %v", err)
	}
}

func TestValidateCursorPageRejectsAnInvalidMetadataAlias(t *testing.T) {
	t.Parallel()

	page := CursorPage{
		Request: CursorPageRequest{Size: 1, PageMember: "bad/name"},
		Links:   Links{"prev": NullLink(), "next": NullLink()},
		Items:   []Meta{nil},
	}
	err := page.Validate()
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/meta", "member-name") {
		t.Fatalf("missing invalid alias violation: %#v", validationError)
	}
}

func TestValidateCursorPageAtUsesNestedRelationshipPaths(t *testing.T) {
	t.Parallel()

	page := CursorPage{
		Request: CursorPageRequest{Size: 1},
		Links:   Links{"next": NullLink()},
		Items:   []Meta{nil},
	}
	err := page.ValidateAt("/data/relationships/comments")
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if !hasViolation(validationError, "/data/relationships/comments/links/prev", "required") {
		t.Fatalf("unexpected nested violations: %#v", validationError.Violations)
	}

	page.Links = Links{"prev": NullLink()}
	err = page.ValidateAt("/data/relationships/comments")
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/data/relationships/comments/links/next", "required") {
		t.Fatalf("missing nested next-link violation: %#v", validationError)
	}
}

func TestAppendCursorMetaErrorHandlesNonValidationErrors(t *testing.T) {
	t.Parallel()

	validator := documentValidator{}
	validator.appendCursorMetaError(errors.New("profile validator failed"), "/meta")
	if len(validator.violations) != 1 ||
		validator.violations[0].Path != "/meta" ||
		validator.violations[0].Code != "invalid" {
		t.Fatalf("unexpected generic metadata violation: %#v", validator.violations)
	}
}
