package apiquerypgx_test

import (
	"context"
	"strings"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
	"github.com/faustbrian/golib/pkg/api-query/apiquerypgx"
)

func TestCompilerKeepsIdentifiersAllowlistedAndValuesParameterized(t *testing.T) {
	t.Parallel()

	plan := databasePlan(t, "paid' OR true --")
	compiler, err := apiquerypgx.NewCompiler(apiquerypgx.Mapping{
		Fields:      map[string]string{"id": "orders.id", "status": "orders.status"},
		Filters:     map[string]string{"status": "orders.status"},
		Sorts:       map[string]string{"id": "orders.id"},
		Constraints: map[string]string{"tenant_id": "orders.tenant_id"},
	})
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	parts, err := compiler.Compile(plan)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if strings.Contains(parts.Where, "paid") || strings.Contains(parts.Where, "OR true") {
		t.Fatalf("Where exposed client value: %q", parts.Where)
	}
	wantWhere := `("orders"."tenant_id" = $1) AND ("orders"."status" = $2)`
	if parts.Where != wantWhere {
		t.Fatalf("Where = %q, want %q", parts.Where, wantWhere)
	}
	if parts.Projection != `"orders"."id", "orders"."status"` {
		t.Fatalf("Projection = %q", parts.Projection)
	}
	if parts.OrderBy != `"orders"."id" ASC` {
		t.Fatalf("OrderBy = %q", parts.OrderBy)
	}
	if len(parts.Arguments) != 2 || parts.Arguments[0].String() != "tenant-42" ||
		parts.Arguments[1].String() != "paid' OR true --" {
		t.Fatalf("Arguments = %#v", parts.Arguments)
	}
}

func TestCompilerCannotDropMandatoryConstraint(t *testing.T) {
	t.Parallel()

	plan := databasePlan(t, "paid")
	compiler, err := apiquerypgx.NewCompiler(apiquerypgx.Mapping{
		Fields:  map[string]string{"id": "orders.id", "status": "orders.status"},
		Filters: map[string]string{"status": "orders.status"},
		Sorts:   map[string]string{"id": "orders.id"},
	})
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	if _, err := compiler.Compile(plan); err == nil {
		t.Fatal("Compile() accepted a plan without mandatory constraint mapping")
	}
}

func TestCompilerRejectsUnsafeMappedIdentifier(t *testing.T) {
	t.Parallel()

	_, err := apiquerypgx.NewCompiler(apiquerypgx.Mapping{
		Fields: map[string]string{"id": `orders.id; DROP TABLE orders`},
	})
	if err == nil {
		t.Fatal("NewCompiler() accepted unsafe identifier")
	}
}

func databasePlan(t *testing.T, status string) *apiquery.Plan {
	t.Helper()
	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "v1",
		Fields: []apiquery.FieldDefinition{
			{Name: "id", Type: apiquery.TypeString, Required: true},
			{Name: "status", Type: apiquery.TypeString, Default: true},
		},
		Filters: []apiquery.FilterDefinition{{Name: "status", Type: apiquery.TypeString,
			Operators: []apiquery.Operator{apiquery.OpEqual}, AllowEmpty: true}},
		Sorts:       []apiquery.SortDefinition{{Name: "id", Type: apiquery.TypeString, TieBreaker: true}},
		DefaultSort: []apiquery.SortTerm{{Name: "id", Direction: apiquery.Ascending}},
	})
	if err != nil {
		t.Fatalf("NewSchema() error = %v", err)
	}
	plan, err := apiquery.Compile(context.Background(), schema, apiquery.Request{
		Filter: &apiquery.FilterExpr{Predicate: &apiquery.Predicate{
			Name: "status", Operator: apiquery.OpEqual,
			Values: []apiquery.Value{apiquery.StringValue(status)},
		}},
	}, apiquery.CompileOptions{MandatoryConstraints: []apiquery.Constraint{{
		Name: "tenant_id", Value: apiquery.StringValue("tenant-42"), Protected: true,
	}}})
	if err != nil {
		t.Fatalf("apiquery.Compile() error = %v", err)
	}
	return plan
}
