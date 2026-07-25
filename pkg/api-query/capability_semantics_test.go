package apiquery_test

import (
	"context"
	"errors"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

func TestCapabilitySemanticMatrix(t *testing.T) {
	t.Parallel()

	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "v2",
		Fields: []apiquery.FieldDefinition{
			{Name: "id", Type: apiquery.TypeString, Required: true},
			{Name: "legacy", Type: apiquery.TypeString, Deprecated: true},
		},
		Filters: []apiquery.FilterDefinition{{
			Name: "status", Type: apiquery.TypeString,
			Operators: []apiquery.Operator{apiquery.OpEqual, apiquery.OpIn},
		}, {
			Name: "deleted_at", Type: apiquery.TypeTime, Nullable: true,
			Operators: []apiquery.Operator{apiquery.OpIsNull},
		}},
		Sorts: []apiquery.SortDefinition{{
			Name: "id", Type: apiquery.TypeString, TieBreaker: true,
		}},
		AllowedLogic: []apiquery.Logic{apiquery.LogicAnd, apiquery.LogicOr},
		Pagination: apiquery.PaginationDefinition{
			Cursor: true, Offset: false, DefaultPageSize: 25, MaxOffset: 0,
		},
		Bounds: apiquery.Bounds{MaxFilterDepth: 2, MaxFilterNodes: 3,
			MaxValues: 3, MaxMembership: 2, MaxStringBytes: 8,
			MaxPageSize: 50, MaxCost: 20, MaxErrors: 8},
	})
	if err != nil {
		t.Fatalf("NewSchema() error = %v", err)
	}

	tests := []struct {
		name    string
		request apiquery.Request
		code    apiquery.ErrorCode
		path    string
	}{
		{
			name:    "schema revision mismatch",
			request: apiquery.Request{SchemaRevision: apiquery.Present("v1")},
			code:    apiquery.CodeVersionMismatch, path: "schema_revision",
		},
		{
			name:    "deprecated field",
			request: apiquery.Request{Fields: apiquery.Present([]string{"legacy"})},
			code:    apiquery.CodeUnsupported, path: "fields[0]",
		},
		{
			name:    "empty equality value",
			request: apiquery.Request{Filter: predicate("status", apiquery.OpEqual, apiquery.StringValue(""))},
			code:    apiquery.CodeInvalidElement, path: "filter",
		},
		{
			name: "membership bound",
			request: apiquery.Request{Filter: predicate("status", apiquery.OpIn,
				apiquery.StringValue("a"), apiquery.StringValue("b"), apiquery.StringValue("c"))},
			code: apiquery.CodeLimitExceeded, path: "filter",
		},
		{
			name:    "offset disabled",
			request: apiquery.Request{Page: apiquery.PageRequest{Mode: apiquery.PageOffset, Size: 10}},
			code:    apiquery.CodeUnsupported, path: "page.mode",
		},
		{
			name: "unsafe null order",
			request: apiquery.Request{Sorts: apiquery.Present([]apiquery.SortTerm{{
				Name: "id", Direction: apiquery.Ascending,
				Nulls: apiquery.NullOrder("last; drop table orders"),
			}})},
			code: apiquery.CodeInvalidElement, path: "sorts[0]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := apiquery.Compile(context.Background(), schema, test.request, apiquery.CompileOptions{})
			var violations *apiquery.Violations
			if !errors.As(err, &violations) {
				t.Fatalf("Compile() error = %v, want Violations", err)
			}
			items := violations.Items()
			if len(items) == 0 || items[0].Code != test.code || items[0].Path != test.path {
				t.Fatalf("violations = %#v, want first code %q path %q", items, test.code, test.path)
			}
		})
	}
}

func TestSchemaRejectsTypeIncompatibleOperator(t *testing.T) {
	t.Parallel()

	_, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "v1",
		Fields: []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString}},
		Filters: []apiquery.FilterDefinition{{
			Name: "count", Type: apiquery.TypeInt,
			Operators: []apiquery.Operator{apiquery.OpContains},
		}},
	})
	var violations *apiquery.Violations
	if !errors.As(err, &violations) {
		t.Fatalf("NewSchema() error = %v, want violations", err)
	}
	items := violations.Items()
	if len(items) == 0 || items[0].Code != apiquery.CodeUnsupported {
		t.Fatalf("NewSchema() error = %v, want unsupported operator violation", err)
	}
}

func TestPageStateConflictMatrix(t *testing.T) {
	t.Parallel()

	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "v1",
		Fields: []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString}},
		Sorts:  []apiquery.SortDefinition{{Name: "id", Type: apiquery.TypeString, TieBreaker: true}},
		Pagination: apiquery.PaginationDefinition{Cursor: true, Offset: true,
			DefaultPageSize: 10, MaxOffset: 100},
		Bounds: apiquery.Bounds{MaxCursorBytes: 4, MaxPageSize: 20},
	})
	if err != nil {
		t.Fatalf("NewSchema() error = %v", err)
	}
	tests := []apiquery.PageRequest{
		{Mode: apiquery.PageNone, After: "x"},
		{Mode: apiquery.PageCursor, After: "12345"},
		{Mode: apiquery.PageCursor, Offset: 1},
		{Mode: apiquery.PageOffset, After: "x"},
		{Mode: apiquery.PageOffset, Offset: 101},
	}
	for _, page := range tests {
		if _, err := apiquery.Compile(context.Background(), schema,
			apiquery.Request{Page: page}, apiquery.CompileOptions{}); err == nil {
			t.Fatalf("Compile(page %#v) accepted conflicting or excessive state", page)
		}
	}
}

func TestFilterDuplicateAndDeprecationMatrix(t *testing.T) {
	t.Parallel()

	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "v1",
		Fields: []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString}},
		Filters: []apiquery.FilterDefinition{
			{Name: "status", Type: apiquery.TypeString,
				Operators: []apiquery.Operator{apiquery.OpEqual, apiquery.OpIn}},
			{Name: "legacy", Type: apiquery.TypeString, Deprecated: true,
				Operators: []apiquery.Operator{apiquery.OpEqual}},
		},
	})
	if err != nil {
		t.Fatalf("NewSchema() error = %v", err)
	}
	tests := []struct {
		name   string
		filter *apiquery.FilterExpr
		path   string
	}{
		{name: "duplicate leaf", path: "filter.children[1]", filter: &apiquery.FilterExpr{
			Logic: apiquery.LogicAnd, Children: []apiquery.FilterExpr{
				*predicate("status", apiquery.OpEqual, apiquery.StringValue("paid")),
				*predicate("status", apiquery.OpEqual, apiquery.StringValue("open")),
			},
		}},
		{name: "duplicate membership value", path: "filter", filter: predicate("status", apiquery.OpIn, apiquery.StringValue("paid"), apiquery.StringValue("paid"))},
		{name: "deprecated filter", path: "filter", filter: predicate("legacy", apiquery.OpEqual, apiquery.StringValue("x"))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := apiquery.Compile(context.Background(), schema,
				apiquery.Request{Filter: test.filter}, apiquery.CompileOptions{})
			var violations *apiquery.Violations
			if !errors.As(err, &violations) || violations.Items()[0].Path != test.path {
				t.Fatalf("Compile() error = %v, want violation at %s", err, test.path)
			}
		})
	}
}

func predicate(name string, operator apiquery.Operator, values ...apiquery.Value) *apiquery.FilterExpr {
	return &apiquery.FilterExpr{Predicate: &apiquery.Predicate{
		Name: name, Operator: operator, Values: values,
	}}
}
