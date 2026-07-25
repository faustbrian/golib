// Package apiquerytest provides builders, fixtures, assertions, and
// conformance suites for API query consumers and adapters.
package apiquerytest

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

// SchemaBuilder incrementally assembles explicit schema declarations for tests.
type SchemaBuilder struct{ config apiquery.SchemaConfig }

// NewSchema starts a schema builder.
func NewSchema(resource, revision string) *SchemaBuilder {
	return &SchemaBuilder{config: apiquery.SchemaConfig{Resource: resource, Revision: revision}}
}

// Field appends one field declaration.
func (b *SchemaBuilder) Field(field apiquery.FieldDefinition) *SchemaBuilder {
	b.config.Fields = append(b.config.Fields, field)
	return b
}

// Filter appends one filter declaration.
func (b *SchemaBuilder) Filter(filter apiquery.FilterDefinition) *SchemaBuilder {
	b.config.Filters = append(b.config.Filters, filter)
	return b
}

// Sort appends one sort declaration.
func (b *SchemaBuilder) Sort(definition apiquery.SortDefinition) *SchemaBuilder {
	b.config.Sorts = append(b.config.Sorts, definition)
	return b
}

// Relationship appends one relationship declaration.
func (b *SchemaBuilder) Relationship(definition apiquery.RelationshipDefinition) *SchemaBuilder {
	b.config.Relationships = append(b.config.Relationships, definition)
	return b
}

// Pagination configures explicit page capabilities.
func (b *SchemaBuilder) Pagination(definition apiquery.PaginationDefinition) *SchemaBuilder {
	b.config.Pagination = definition
	return b
}

// Bounds configures hard test schema bounds.
func (b *SchemaBuilder) Bounds(bounds apiquery.Bounds) *SchemaBuilder {
	b.config.Bounds = bounds
	return b
}

// MustBuild constructs the schema or fails the current test.
func (b *SchemaBuilder) MustBuild(t testing.TB) *apiquery.Schema {
	t.Helper()
	schema, err := apiquery.NewSchema(b.config)
	if err != nil {
		t.Fatalf("apiquerytest schema: %v", err)
	}
	return schema
}

// RequestBuilder incrementally assembles transport-neutral requests for tests.
type RequestBuilder struct{ request apiquery.Request }

// NewRequest starts an absent-component request builder.
func NewRequest() *RequestBuilder { return &RequestBuilder{} }

// Fields supplies an explicit, possibly empty field selection.
func (b *RequestBuilder) Fields(fields ...string) *RequestBuilder {
	b.request.Fields = apiquery.Present(append([]string(nil), fields...))
	return b
}

// Includes supplies explicit, possibly empty relationship paths.
func (b *RequestBuilder) Includes(paths ...string) *RequestBuilder {
	b.request.Includes = apiquery.Present(append([]string(nil), paths...))
	return b
}

// Where supplies one typed predicate.
func (b *RequestBuilder) Where(name string, operator apiquery.Operator, values ...apiquery.Value) *RequestBuilder {
	b.request.Filter = &apiquery.FilterExpr{Predicate: &apiquery.Predicate{
		Name: name, Operator: operator, Values: append([]apiquery.Value(nil), values...),
	}}
	return b
}

// Sorts supplies explicit ordered sort terms.
func (b *RequestBuilder) Sorts(sorts ...apiquery.SortTerm) *RequestBuilder {
	b.request.Sorts = apiquery.Present(append([]apiquery.SortTerm(nil), sorts...))
	return b
}

// Page supplies explicit page state.
func (b *RequestBuilder) Page(page apiquery.PageRequest) *RequestBuilder {
	b.request.Page = page
	return b
}

// Build returns a defensive request snapshot.
func (b *RequestBuilder) Build() apiquery.Request {
	request := b.request
	if fields, present := request.Fields.Value(); present {
		request.Fields = apiquery.Present(append([]string(nil), fields...))
	}
	if includes, present := request.Includes.Value(); present {
		request.Includes = apiquery.Present(append([]string(nil), includes...))
	}
	if sorts, present := request.Sorts.Value(); present {
		request.Sorts = apiquery.Present(append([]apiquery.SortTerm(nil), sorts...))
	}
	request.Filter = cloneFilter(request.Filter)
	return request
}

func cloneFilter(filter *apiquery.FilterExpr) *apiquery.FilterExpr {
	if filter == nil {
		return nil
	}
	result := &apiquery.FilterExpr{Logic: filter.Logic}
	if filter.Predicate != nil {
		result.Predicate = &apiquery.Predicate{Name: filter.Predicate.Name,
			Operator: filter.Predicate.Operator,
			Values:   append([]apiquery.Value(nil), filter.Predicate.Values...)}
	}
	result.Children = make([]apiquery.FilterExpr, len(filter.Children))
	for index := range filter.Children {
		result.Children[index] = *cloneFilter(&filter.Children[index])
	}
	return result
}

// MustCompile compiles a plan or fails the current test.
func MustCompile(t testing.TB, schema *apiquery.Schema, request apiquery.Request, options apiquery.CompileOptions) *apiquery.Plan {
	t.Helper()
	plan, err := apiquery.Compile(context.Background(), schema, request, options)
	if err != nil {
		t.Fatalf("apiquerytest compile: %v", err)
	}
	return plan
}

// AssertViolation requires one exact code and path among structured failures.
func AssertViolation(t testing.TB, err error, code apiquery.ErrorCode, path string) {
	t.Helper()
	var violations *apiquery.Violations
	if !errors.As(err, &violations) {
		t.Fatalf("error = %v, want apiquery violations", err)
	}
	for _, violation := range violations.Items() {
		if violation.Code == code && violation.Path == path {
			return
		}
	}
	t.Fatalf("violations = %#v, want code %q path %q", violations.Items(), code, path)
}

// AssertCanonicalEqual requires byte-identical canonical plans.
func AssertCanonicalEqual(t testing.TB, left, right *apiquery.Plan) {
	t.Helper()
	leftCanonical, leftErr := left.Canonical()
	rightCanonical, rightErr := right.Canonical()
	if leftErr != nil || rightErr != nil || !bytes.Equal(leftCanonical, rightCanonical) {
		t.Fatalf("canonical plans differ: left=%q (%v), right=%q (%v)",
			leftCanonical, leftErr, rightCanonical, rightErr)
	}
}

// RunCanonicalConformance runs named request decoders against one schema and
// requires every successful decoder to compile to the same canonical plan.
func RunCanonicalConformance(t *testing.T, schema *apiquery.Schema, options apiquery.CompileOptions, decoders map[string]func() (apiquery.Request, error)) {
	t.Helper()
	names := make([]string, 0, len(decoders))
	for name := range decoders {
		names = append(names, name)
	}
	sort.Strings(names)
	var baseline *apiquery.Plan
	for _, name := range names {
		request, err := decoders[name]()
		if err != nil {
			t.Fatalf("decoder %s: %v", name, err)
		}
		plan := MustCompile(t, schema, request, options)
		if baseline == nil {
			baseline = plan
			continue
		}
		AssertCanonicalEqual(t, baseline, plan)
	}
}

// OrderSchema returns a small immutable fixture used by examples and adapter
// conformance tests.
func OrderSchema(t testing.TB) *apiquery.Schema {
	t.Helper()
	return NewSchema("orders", "v1").
		Field(apiquery.FieldDefinition{Name: "id", Type: apiquery.TypeString, Required: true}).
		Field(apiquery.FieldDefinition{Name: "status", Type: apiquery.TypeString, Default: true}).
		MustBuild(t)
}
