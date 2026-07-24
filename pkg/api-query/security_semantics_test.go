package apiquery_test

import (
	"context"
	"errors"
	"math"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

func TestMandatoryConstraintsAreImmutableAndSeparateFromClientFilters(t *testing.T) {
	t.Parallel()

	schema := mustSchema(t)
	constraints := []apiquery.Constraint{{
		Name: "tenant_id", Value: apiquery.StringValue("tenant-42"), Protected: true,
	}}
	plan, err := apiquery.Compile(context.Background(), schema, apiquery.Request{
		Filter: nil,
	}, apiquery.CompileOptions{MandatoryConstraints: constraints})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	constraints[0].Name = "attacker_override"
	constraints[0].Value = apiquery.StringValue("tenant-evil")

	got := plan.MandatoryConstraints()
	if len(got) != 1 || got[0].Name != "tenant_id" || got[0].Value.String() != "tenant-42" {
		t.Fatalf("MandatoryConstraints() = %#v, want immutable tenant constraint", got)
	}
	got[0].Name = "mutated"
	if plan.MandatoryConstraints()[0].Name != "tenant_id" {
		t.Fatal("MandatoryConstraints() returned plan-owned storage")
	}
}

func TestSchemaRejectsCostAndPaginationDefinitionsThatWeakenBounds(t *testing.T) {
	t.Parallel()

	tests := []apiquery.SchemaConfig{
		{Resource: "orders", Revision: "v1",
			Fields: []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString, Cost: -1}}},
		{Resource: "orders", Revision: "v1",
			Fields:     []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString}},
			Pagination: apiquery.PaginationDefinition{Cursor: true}},
		{Resource: "orders", Revision: "v1",
			Fields:     []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString}},
			Pagination: apiquery.PaginationDefinition{Offset: true, DefaultPageSize: 101, MaxOffset: 10},
			Bounds:     apiquery.Bounds{MaxPageSize: 100}},
	}
	for index, config := range tests {
		if _, err := apiquery.NewSchema(config); err == nil {
			t.Fatalf("NewSchema(case %d) accepted weakening definition", index)
		}
	}
}

func TestCompileStopsAtValueAndConstraintBounds(t *testing.T) {
	t.Parallel()

	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "v1",
		Fields: []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString}},
		Filters: []apiquery.FilterDefinition{{Name: "id", Type: apiquery.TypeString,
			Operators: []apiquery.Operator{apiquery.OpIn}}},
		Bounds: apiquery.Bounds{MaxValues: 2, MaxMembership: 2, MaxErrors: 2},
	})
	if err != nil {
		t.Fatalf("NewSchema() error = %v", err)
	}
	_, err = apiquery.Compile(context.Background(), schema, apiquery.Request{
		Filter: predicate("id", apiquery.OpIn, apiquery.StringValue("1"),
			apiquery.StringValue("2"), apiquery.StringValue("3")),
	}, apiquery.CompileOptions{MandatoryConstraints: []apiquery.Constraint{
		{Name: "one", Value: apiquery.StringValue("1")},
		{Name: "two", Value: apiquery.StringValue("2")},
		{Name: "three", Value: apiquery.StringValue("3")},
	}})
	var violations *apiquery.Violations
	if !errors.As(err, &violations) {
		t.Fatalf("Compile() error = %v, want Violations", err)
	}
	if len(violations.Items()) != 2 {
		t.Fatalf("violations = %#v, want bounded two", violations.Items())
	}
}

func TestNonFiniteFloatCannotEnterPlan(t *testing.T) {
	t.Parallel()

	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "v1",
		Fields: []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString}},
		Filters: []apiquery.FilterDefinition{{Name: "amount", Type: apiquery.TypeFloat,
			Operators: []apiquery.Operator{apiquery.OpEqual}}},
	})
	if err != nil {
		t.Fatalf("NewSchema() error = %v", err)
	}
	_, err = apiquery.Compile(context.Background(), schema, apiquery.Request{
		Filter: predicate("amount", apiquery.OpEqual, apiquery.FloatValue(math.Inf(1))),
	}, apiquery.CompileOptions{})
	if err == nil {
		t.Fatal("Compile() accepted non-finite float")
	}
}

func TestNestedIncludesAuthorizeEveryEdge(t *testing.T) {
	t.Parallel()

	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "v1",
		Fields: []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString}},
		Relationships: []apiquery.RelationshipDefinition{{
			Name: "customer", Resource: "customers", Cost: 2,
			Relationships: []apiquery.RelationshipDefinition{{
				Name: "address", Resource: "addresses", Cost: 3,
			}},
		}},
		Bounds: apiquery.Bounds{MaxIncludes: 3, MaxIncludeDepth: 2, MaxCost: 20},
	})
	if err != nil {
		t.Fatalf("NewSchema() error = %v", err)
	}

	var authorized []string
	plan, err := apiquery.Compile(context.Background(), schema, apiquery.Request{
		Includes: apiquery.Present([]string{"customer.address"}),
	}, apiquery.CompileOptions{Authorize: func(_ context.Context, capability apiquery.Capability) bool {
		if capability.Kind == apiquery.CapabilityRelationship {
			authorized = append(authorized, capability.Name)
		}
		return true
	}})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if !equalStrings(authorized, []string{"customer", "customer.address"}) {
		t.Fatalf("authorized edges = %v", authorized)
	}
	if got := plan.Includes(); !equalStrings(got, []string{"customer.address"}) {
		t.Fatalf("Includes() = %v", got)
	}
	if plan.Cost() != 6 {
		t.Fatalf("Cost() = %d, want base plus both relationship edges", plan.Cost())
	}
}

func TestRelationshipSchemaRejectsCycles(t *testing.T) {
	t.Parallel()

	_, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "v1",
		Fields: []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString}},
		Relationships: []apiquery.RelationshipDefinition{{
			Name: "customer", Resource: "customers",
			Relationships: []apiquery.RelationshipDefinition{{
				Name: "orders", Resource: "orders",
			}},
		}},
	})
	var violations *apiquery.Violations
	if !errors.As(err, &violations) {
		t.Fatalf("NewSchema() error = %v, want Violations", err)
	}
	items := violations.Items()
	if len(items) != 1 || items[0].Code != apiquery.CodeConflict {
		t.Fatalf("violations = %#v, want cycle conflict", items)
	}
}
