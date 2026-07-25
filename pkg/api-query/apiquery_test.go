package apiquery_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

func TestCompileBuildsImmutableBoundedPlan(t *testing.T) {
	t.Parallel()

	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders",
		Revision: "orders-v1",
		Fields: []apiquery.FieldDefinition{
			{Name: "id", Type: apiquery.TypeString, Required: true, Cost: 1},
			{Name: "status", Type: apiquery.TypeString, Default: true, Cost: 1},
			{Name: "secret", Type: apiquery.TypeString, Cost: 5},
		},
		Filters: []apiquery.FilterDefinition{{
			Name: "status", Type: apiquery.TypeString,
			Operators: []apiquery.Operator{apiquery.OpEqual}, Cost: 2,
		}},
		Sorts: []apiquery.SortDefinition{
			{Name: "created_at", Type: apiquery.TypeTime, Cost: 1},
			{Name: "id", Type: apiquery.TypeString, TieBreaker: true, Cost: 1},
		},
		DefaultSort: []apiquery.SortTerm{{Name: "created_at", Direction: apiquery.Descending}},
		Pagination:  apiquery.PaginationDefinition{Cursor: true, DefaultPageSize: 25},
		Bounds: apiquery.Bounds{MaxFields: 3, MaxSorts: 3, MaxFilterDepth: 3,
			MaxFilterNodes: 4, MaxValues: 4, MaxStringBytes: 64,
			MaxPageSize: 100, MaxCost: 20, MaxErrors: 8},
	})
	if err != nil {
		t.Fatalf("NewSchema() error = %v", err)
	}

	requestedFields := []string{"status"}
	request := apiquery.Request{
		Fields: apiquery.Present(requestedFields),
		Filter: &apiquery.FilterExpr{Predicate: &apiquery.Predicate{
			Name: "status", Operator: apiquery.OpEqual,
			Values: []apiquery.Value{apiquery.StringValue("paid")},
		}},
		Page: apiquery.PageRequest{Mode: apiquery.PageCursor, Size: 25},
	}

	plan, err := apiquery.Compile(context.Background(), schema, request, apiquery.CompileOptions{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	requestedFields[0] = "secret"
	request.Filter.Predicate.Values[0] = apiquery.StringValue("refunded")

	if got, want := plan.ResponseFields(), []string{"status"}; !equalStrings(got, want) {
		t.Fatalf("ResponseFields() = %v, want %v", got, want)
	}
	if got, want := plan.ExecutionFields(), []string{"id", "status"}; !equalStrings(got, want) {
		t.Fatalf("ExecutionFields() = %v, want %v", got, want)
	}
	if got := plan.Filter().Predicate.Values[0].String(); got != "paid" {
		t.Fatalf("compiled filter value = %q, want paid", got)
	}
	if got := plan.Sorts(); len(got) != 2 || got[1].Name != "id" {
		t.Fatalf("Sorts() = %#v, want stable id tie-breaker", got)
	}
	if plan.Cost() != 7 {
		t.Fatalf("Cost() = %d, want 7", plan.Cost())
	}

	first, err := plan.Canonical()
	if err != nil {
		t.Fatalf("Canonical() error = %v", err)
	}
	second, err := plan.Canonical()
	if err != nil {
		t.Fatalf("Canonical() second error = %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("Canonical() is not deterministic: %q != %q", first, second)
	}
}

func TestCompileRejectsUndeclaredAndUnauthorizedCapabilities(t *testing.T) {
	t.Parallel()

	schema := mustSchema(t)
	tests := []struct {
		name    string
		request apiquery.Request
		options apiquery.CompileOptions
		code    apiquery.ErrorCode
		path    string
	}{
		{
			name:    "unknown field",
			request: apiquery.Request{Fields: apiquery.Present([]string{"missing"})},
			code:    apiquery.CodeInvalidElement,
			path:    "fields[0]",
		},
		{
			name:    "duplicate field",
			request: apiquery.Request{Fields: apiquery.Present([]string{"status", "status"})},
			code:    apiquery.CodeConflict,
			path:    "fields[1]",
		},
		{
			name:    "unauthorized field hides capability detail",
			request: apiquery.Request{Fields: apiquery.Present([]string{"secret"})},
			options: apiquery.CompileOptions{Authorize: func(_ context.Context, c apiquery.Capability) bool {
				return c.Name != "secret"
			}},
			code: apiquery.CodeAuthorization,
			path: "fields[0]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := apiquery.Compile(context.Background(), schema, test.request, test.options)
			var violations *apiquery.Violations
			if !errors.As(err, &violations) {
				t.Fatalf("Compile() error = %v, want Violations", err)
			}
			if got := violations.Items(); len(got) != 1 || got[0].Code != test.code || got[0].Path != test.path {
				t.Fatalf("violations = %#v, want code %q path %q", got, test.code, test.path)
			}
			if test.code == apiquery.CodeAuthorization && violations.Error() == "secret" {
				t.Fatal("authorization error leaked inaccessible capability")
			}
		})
	}
}

func TestAbsentAndExplicitlyEmptyFieldsDiffer(t *testing.T) {
	t.Parallel()

	schema := mustSchema(t)
	absent, err := apiquery.Compile(context.Background(), schema, apiquery.Request{}, apiquery.CompileOptions{})
	if err != nil {
		t.Fatalf("Compile(absent) error = %v", err)
	}
	empty, err := apiquery.Compile(context.Background(), schema, apiquery.Request{
		Fields: apiquery.Present([]string{}),
	}, apiquery.CompileOptions{})
	if err != nil {
		t.Fatalf("Compile(empty) error = %v", err)
	}
	if got := absent.ResponseFields(); !equalStrings(got, []string{"status"}) {
		t.Fatalf("absent fields = %v, want defaults", got)
	}
	if got := empty.ResponseFields(); len(got) != 0 {
		t.Fatalf("explicit empty fields = %v, want empty", got)
	}
	if got := empty.ExecutionFields(); !equalStrings(got, []string{"id"}) {
		t.Fatalf("explicit empty execution fields = %v, want required id", got)
	}
}

func TestCompileAuthorizesImplicitCursorTieBreaker(t *testing.T) {
	t.Parallel()

	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "v1",
		Fields: []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString}},
		Sorts: []apiquery.SortDefinition{{
			Name: "id", Type: apiquery.TypeString, TieBreaker: true,
		}},
		Pagination: apiquery.PaginationDefinition{Cursor: true, DefaultPageSize: 10},
	})
	if err != nil {
		t.Fatalf("NewSchema() error = %v", err)
	}
	_, err = apiquery.Compile(context.Background(), schema, apiquery.Request{
		Page: apiquery.PageRequest{Mode: apiquery.PageCursor, Size: 10},
	}, apiquery.CompileOptions{Authorize: func(_ context.Context, capability apiquery.Capability) bool {
		return capability.Kind != apiquery.CapabilitySort
	}})
	var violations *apiquery.Violations
	if !errors.As(err, &violations) {
		t.Fatalf("Compile() error = %v, want Violations", err)
	}
	items := violations.Items()
	if len(items) != 1 || items[0].Code != apiquery.CodeAuthorization || items[0].Path != "sorts" {
		t.Fatalf("violations = %#v, want authorization rejection at sorts", items)
	}
}

func mustSchema(t *testing.T) *apiquery.Schema {
	t.Helper()

	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "v1",
		Fields: []apiquery.FieldDefinition{
			{Name: "id", Type: apiquery.TypeString, Required: true},
			{Name: "status", Type: apiquery.TypeString, Default: true},
			{Name: "secret", Type: apiquery.TypeString},
		},
		Bounds: apiquery.Bounds{MaxFields: 3, MaxSorts: 2, MaxFilterDepth: 3,
			MaxFilterNodes: 4, MaxValues: 4, MaxStringBytes: 64,
			MaxPageSize: 100, MaxCost: 20, MaxErrors: 8},
	})
	if err != nil {
		t.Fatalf("NewSchema() error = %v", err)
	}

	return schema
}

func equalStrings(left, right []string) bool {
	return slices.Equal(left, right)
}
