package jsonapi

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCursorPaginationParsesProfileParameters(t *testing.T) {
	t.Parallel()

	pagination, err := NewCursorPagination(CursorPaginationConfig{
		DefaultSize: 25,
		MaxSize:     100,
		AllowRange:  true,
		ValidateCursor: func(cursor string) error {
			if cursor == "bad" {
				return errors.New("invalid cursor")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("construct pagination parser: %v", err)
	}

	page, err := pagination.Parse(ParameterFamily{
		"page[size]":   {"10"},
		"page[after]":  {"abc"},
		"page[before]": {"xyz"},
	})
	if err != nil {
		t.Fatalf("parse cursor pagination: %v", err)
	}
	if page.Size != 10 || !page.SizePresent || page.After != "abc" || page.Before != "xyz" || !page.Range {
		t.Fatalf("unexpected page request: %#v", page)
	}
}

func TestCursorPaginationUsesContextualDefaultSize(t *testing.T) {
	t.Parallel()

	pagination, err := NewCursorPagination(CursorPaginationConfig{
		DefaultSize: 25,
		MaxSize:     100,
		AllowRange:  true,
	})
	if err != nil {
		t.Fatalf("construct pagination parser: %v", err)
	}

	ordinary, err := pagination.Parse(nil)
	if err != nil {
		t.Fatalf("parse ordinary page: %v", err)
	}
	if ordinary.Size != 25 {
		t.Fatalf("unexpected default size: %d", ordinary.Size)
	}

	rangePage, err := pagination.Parse(ParameterFamily{
		"page[after]":  {"abc"},
		"page[before]": {"xyz"},
	})
	if err != nil {
		t.Fatalf("parse range page: %v", err)
	}
	if rangePage.Size != 100 {
		t.Fatalf("range default must use max size, got %d", rangePage.Size)
	}
}

func TestCursorPaginationCarriesAnAliasedPageMemberIntoErrors(t *testing.T) {
	t.Parallel()

	pagination, err := NewCursorPagination(CursorPaginationConfig{
		DefaultSize: 10,
		MaxSize:     20,
		PageMember:  "cursorPage",
	})
	if err != nil {
		t.Fatalf("construct aliased pagination: %v", err)
	}
	request, err := pagination.Parse(nil)
	if err != nil || request.PageMember != "cursorPage" {
		t.Fatalf("page member was not retained: %#v err=%v", request, err)
	}

	_, err = pagination.Parse(ParameterFamily{"page[size]": {"21"}})
	var pageError *CursorPaginationError
	if !errors.As(err, &pageError) {
		t.Fatalf("expected cursor pagination error, got %T: %v", err, err)
	}
	object := pageError.ErrorObject("too large", "too large")
	if _, exists := object.Meta["cursorPage"]; !exists {
		t.Fatalf("aliased max-size metadata missing: %#v", object.Meta)
	}
}

func TestCursorPaginationRejectsAnInvalidPageMemberAlias(t *testing.T) {
	t.Parallel()

	if _, err := NewCursorPagination(CursorPaginationConfig{
		DefaultSize: 1,
		PageMember:  "bad/name",
	}); err == nil {
		t.Fatal("expected invalid page member alias error")
	}

	object := (&CursorPaginationError{
		Code:       "max-size-exceeded",
		MaxSize:    10,
		PageMember: "bad/name",
	}).ErrorObject("too large", "too large")
	if _, exists := object.Meta["page"]; !exists {
		t.Fatalf("invalid manual alias must fall back safely: %#v", object.Meta)
	}
}

func TestCursorPaginationRejectsInvalidParameters(t *testing.T) {
	t.Parallel()

	pagination, err := NewCursorPagination(CursorPaginationConfig{
		DefaultSize: 10,
		MaxSize:     50,
		ValidateCursor: func(cursor string) error {
			if cursor == "invalid" {
				return errors.New("signature mismatch")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("construct pagination parser: %v", err)
	}

	tests := map[string]struct {
		family    ParameterFamily
		parameter string
		code      string
	}{
		"size is positive decimal": {
			family:    ParameterFamily{"page[size]": {"0"}},
			parameter: "page[size]",
			code:      "invalid-parameter",
		},
		"size is not empty": {
			family:    ParameterFamily{"page[size]": {""}},
			parameter: "page[size]",
			code:      "invalid-parameter",
		},
		"size contains only decimal digits": {
			family:    ParameterFamily{"page[size]": {"1x"}},
			parameter: "page[size]",
			code:      "invalid-parameter",
		},
		"size fits an integer": {
			family:    ParameterFamily{"page[size]": {"999999999999999999999999999"}},
			parameter: "page[size]",
			code:      "invalid-parameter",
		},
		"size occurs once": {
			family:    ParameterFamily{"page[size]": {"1", "2"}},
			parameter: "page[size]",
			code:      "multiple-values",
		},
		"size respects maximum": {
			family:    ParameterFamily{"page[size]": {"51"}},
			parameter: "page[size]",
			code:      "max-size-exceeded",
		},
		"cursor is validated": {
			family:    ParameterFamily{"page[after]": {"invalid"}},
			parameter: "page[after]",
			code:      "invalid-parameter",
		},
		"cursor occurs once": {
			family:    ParameterFamily{"page[before]": {"one", "two"}},
			parameter: "page[before]",
			code:      "multiple-values",
		},
		"range must be supported": {
			family: ParameterFamily{
				"page[after]":  {"abc"},
				"page[before]": {"xyz"},
			},
			parameter: "page[before]",
			code:      "range-not-supported",
		},
		"unknown page member": {
			family:    ParameterFamily{"page[number]": {"2"}},
			parameter: "page[number]",
			code:      "unknown-parameter",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := pagination.Parse(test.family)
			if err == nil {
				t.Fatal("expected cursor pagination error")
			}
			var pageError *CursorPaginationError
			if !errors.As(err, &pageError) {
				t.Fatalf("expected CursorPaginationError, got %T: %v", err, err)
			}
			if pageError.Parameter != test.parameter || pageError.Code != test.code || pageError.Status != 400 {
				t.Fatalf("unexpected error: %#v", pageError)
			}
		})
	}
}

func TestCursorPaginationRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	invalid := []CursorPaginationConfig{
		{DefaultSize: 0, MaxSize: 10},
		{DefaultSize: 10, MaxSize: -1},
		{DefaultSize: 11, MaxSize: 10},
		{DefaultSize: 10, AllowRange: true},
	}
	for _, config := range invalid {
		if _, err := NewCursorPagination(config); err == nil {
			t.Fatalf("expected invalid config error: %#v", config)
		}
	}
}

func TestCursorPaginationParseQueryReturnsPageErrorsBeforeSortValidation(t *testing.T) {
	t.Parallel()

	sortCalled := false
	pagination, err := NewCursorPagination(CursorPaginationConfig{
		DefaultSize: 10,
		MaxSize:     50,
		ValidateSort: func([]SortField) error {
			sortCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("construct pagination parser: %v", err)
	}
	_, err = pagination.ParseQuery(Query{
		Page: ParameterFamily{"page[size]": {"0"}},
	})
	var pageError *CursorPaginationError
	if !errors.As(err, &pageError) || pageError.Parameter != "page[size]" {
		t.Fatalf("unexpected page error: %T %#v", err, pageError)
	}
	if sortCalled {
		t.Fatal("sort validation ran after invalid page parameters")
	}
}

func TestCursorPaginationPreservesEmptyCursorPresence(t *testing.T) {
	t.Parallel()

	pagination, err := NewCursorPagination(CursorPaginationConfig{
		DefaultSize: 10,
		MaxSize:     20,
		AllowRange:  true,
		ValidateCursor: func(string) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("construct pagination parser: %v", err)
	}

	page, err := pagination.Parse(ParameterFamily{
		"page[after]":  {""},
		"page[before]": {""},
	})
	if err != nil {
		t.Fatalf("parse empty opaque cursors: %v", err)
	}
	if !page.AfterPresent || !page.BeforePresent || !page.Range {
		t.Fatalf("cursor presence was lost: %#v", page)
	}
}

func TestCursorPaginationErrorBuildsProfileErrorObject(t *testing.T) {
	t.Parallel()

	pagination, err := NewCursorPagination(CursorPaginationConfig{
		DefaultSize: 10,
		MaxSize:     50,
	})
	if err != nil {
		t.Fatalf("construct pagination parser: %v", err)
	}
	_, err = pagination.Parse(ParameterFamily{"page[size]": {"51"}})
	var pageError *CursorPaginationError
	if !errors.As(err, &pageError) {
		t.Fatalf("expected CursorPaginationError, got %T: %v", err, err)
	}

	object := pageError.ErrorObject("Page size too large", "The maximum is 50.")
	document := Document{Errors: []ErrorObject{object}}
	payload, err := Marshal(document)
	if err != nil {
		t.Fatalf("marshal error document: %v", err)
	}
	want := `{"errors":[{"links":{"type":"https://jsonapi.org/profiles/ethanresnick/cursor-pagination/max-size-exceeded"},"status":"400","code":"max-size-exceeded","title":"Page size too large","detail":"The maximum is 50.","source":{"parameter":"page[size]"},"meta":{"page":{"maxSize":50}}}]}`
	if string(payload) != want {
		t.Fatalf("unexpected error document:\n got: %s\nwant: %s", payload, want)
	}
}

func TestValidateCursorPaginationLinks(t *testing.T) {
	t.Parallel()

	if err := ValidateCursorPaginationLinks(Links{
		"prev": NullLink(),
		"next": URI("/articles?page%5Bafter%5D=abc"),
	}); err != nil {
		t.Fatalf("expected required pagination links: %v", err)
	}

	tests := []struct {
		links Links
		path  string
	}{
		{links: Links{"next": NullLink()}, path: "/links/prev"},
		{links: Links{"prev": NullLink()}, path: "/links/next"},
	}
	for _, test := range tests {
		err := ValidateCursorPaginationLinks(test.links)
		var validationError *ValidationError
		if !errors.As(err, &validationError) {
			t.Fatalf("expected ValidationError, got %T: %v", err, err)
		}
		if validationError.Violations[0].Path != test.path {
			t.Fatalf("unexpected violation: %#v", validationError.Violations[0])
		}
	}
}

func TestCursorProfileErrorTypeLinks(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"unsupported-sort":    CursorUnsupportedSortTypeURI,
		"range-not-supported": CursorRangeNotSupportedTypeURI,
	}
	for code, typeURI := range tests {
		object := (&CursorPaginationError{
			Status:    400,
			Parameter: "sort",
			Code:      code,
			Message:   code,
		}).ErrorObject(code, code)
		payload, err := object.Links["type"].MarshalJSON()
		if err != nil {
			t.Fatalf("marshal type link: %v", err)
		}
		if string(payload) != `"`+typeURI+`"` {
			t.Fatalf("unexpected type link for %s: %s", code, payload)
		}
	}
}

func TestCursorPaginationValidatesStableSort(t *testing.T) {
	t.Parallel()

	var received []SortField
	pagination, err := NewCursorPagination(CursorPaginationConfig{
		DefaultSize: 10,
		MaxSize:     50,
		ValidateSort: func(sortFields []SortField) error {
			received = append([]SortField(nil), sortFields...)
			if len(sortFields) == 1 && sortFields[0].Name == "unstable" {
				return errors.New("sort cannot produce a unique order")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("construct pagination parser: %v", err)
	}

	query := Query{
		Sort:        []SortField{{Name: "unstable"}},
		SortPresent: true,
		Page:        ParameterFamily{"page[size]": {"10"}},
	}
	_, err = pagination.ParseQuery(query)
	var pageError *CursorPaginationError
	if !errors.As(err, &pageError) || pageError.Code != "unsupported-sort" ||
		pageError.Parameter != "sort" {
		t.Fatalf("unexpected sort error: %T %#v", err, pageError)
	}
	if !reflect.DeepEqual(received, query.Sort) {
		t.Fatalf("validator received wrong sort: %#v", received)
	}

	query.Sort = []SortField{{Name: "created", Descending: true}, {Name: "id"}}
	if _, err := pagination.ParseQuery(query); err != nil {
		t.Fatalf("expected stable sort: %v", err)
	}
}

func TestCursorValidatorFailuresPreserveCausesWithoutDisclosingThem(t *testing.T) {
	t.Parallel()

	secret := errors.New("cursor payload contains tenant-secret")
	pagination, err := NewCursorPagination(CursorPaginationConfig{
		DefaultSize: 1,
		ValidateCursor: func(string) error {
			return secret
		},
	})
	if err != nil {
		t.Fatalf("construct cursor pagination: %v", err)
	}
	_, err = pagination.Parse(ParameterFamily{"page[after]": {"opaque-secret"}})
	var pageError *CursorPaginationError
	if !errors.As(err, &pageError) {
		t.Fatalf("expected CursorPaginationError, got %T: %v", err, err)
	}
	if !errors.Is(err, secret) {
		t.Fatal("validator cause was not preserved")
	}
	if strings.Contains(err.Error(), "tenant-secret") ||
		strings.Contains(err.Error(), "opaque-secret") {
		t.Fatalf("cursor details leaked through public error: %v", err)
	}
}
