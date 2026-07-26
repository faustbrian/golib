package apiquerypgx_test

import (
	"context"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
	"github.com/faustbrian/golib/pkg/api-query/apiquerypgx"
)

func TestFilterOperatorCompilationMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr *apiquery.FilterExpr
		want string
		args []string
	}{
		{"not equal", leaf(apiquery.OpNotEqual, "a"), `"records"."value" <> $1`, []string{"a"}},
		{"less", leaf(apiquery.OpLess, "a"), `"records"."value" < $1`, []string{"a"}},
		{"less equal", leaf(apiquery.OpLessOrEqual, "a"), `"records"."value" <= $1`, []string{"a"}},
		{"greater", leaf(apiquery.OpGreater, "a"), `"records"."value" > $1`, []string{"a"}},
		{"greater equal", leaf(apiquery.OpGreaterOrEqual, "a"), `"records"."value" >= $1`, []string{"a"}},
		{"between", leaf(apiquery.OpBetween, "a", "z"), `"records"."value" BETWEEN $1 AND $2`, []string{"a", "z"}},
		{"in", leaf(apiquery.OpIn, "a", "b"), `"records"."value" IN ($1, $2)`, []string{"a", "b"}},
		{"not in", leaf(apiquery.OpNotIn, "a", "b"), `"records"."value" NOT IN ($1, $2)`, []string{"a", "b"}},
		{"is null", leaf(apiquery.OpIsNull), `"records"."value" IS NULL`, nil},
		{"contains", leaf(apiquery.OpContains, `a%b_c\d`), `"records"."value" LIKE $1 ESCAPE '\'`, []string{`%a\%b\_c\\d%`}},
		{"starts", leaf(apiquery.OpStartsWith, "a"), `"records"."value" LIKE $1 ESCAPE '\'`, []string{"a%"}},
		{"ends", leaf(apiquery.OpEndsWith, "z"), `"records"."value" LIKE $1 ESCAPE '\'`, []string{"%z"}},
		{"and", group(apiquery.LogicAnd, leaf(apiquery.OpGreater, "a"), leaf(apiquery.OpLess, "z")),
			`("records"."value" > $1) AND ("records"."value" < $2)`, []string{"a", "z"}},
		{"or", group(apiquery.LogicOr, leaf(apiquery.OpEqual, "a"), leaf(apiquery.OpNotEqual, "z")),
			`("records"."value" = $1) OR ("records"."value" <> $2)`, []string{"a", "z"}},
		{"not", group(apiquery.LogicNot, leaf(apiquery.OpEqual, "a")),
			`NOT ("records"."value" = $1)`, []string{"a"}},
	}
	compiler := operatorCompiler(t)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			parts, err := compiler.Compile(operatorPlan(t, test.expr))
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			if parts.Where != "("+test.want+")" {
				t.Fatalf("Where = %q, want %q", parts.Where, "("+test.want+")")
			}
			if len(parts.Arguments) != len(test.args) {
				t.Fatalf("Arguments = %#v", parts.Arguments)
			}
			for index, want := range test.args {
				if parts.Arguments[index].String() != want {
					t.Fatalf("argument %d = %q, want %q", index, parts.Arguments[index].String(), want)
				}
			}
		})
	}
}

func TestCompilerFailureAndOrderingMatrix(t *testing.T) {
	t.Parallel()

	plan := operatorPlan(t, leaf(apiquery.OpEqual, "a"))
	if _, err := (*apiquerypgx.Compiler)(nil).Compile(plan); err == nil {
		t.Fatal("nil compiler accepted")
	}
	compiler := operatorCompiler(t)
	if _, err := compiler.Compile(nil); err == nil {
		t.Fatal("nil plan accepted")
	}

	missingField, _ := apiquerypgx.NewCompiler(apiquerypgx.Mapping{Filters: map[string]string{"value": "records.value"}})
	if _, err := missingField.Compile(plan); err == nil {
		t.Fatal("missing field mapping accepted")
	}
	missingFilter, _ := apiquerypgx.NewCompiler(apiquerypgx.Mapping{Fields: map[string]string{"id": "records.id"}})
	if _, err := missingFilter.Compile(plan); err == nil {
		t.Fatal("missing filter mapping accepted")
	}
	if _, err := missingFilter.Compile(operatorPlan(t, group(apiquery.LogicAnd,
		leaf(apiquery.OpEqual, "a")))); err == nil {
		t.Fatal("nested missing filter mapping accepted")
	}

	sorted := sortedPlan(t)
	missingSort, _ := apiquerypgx.NewCompiler(apiquerypgx.Mapping{Fields: map[string]string{"id": "records.id"}})
	if _, err := missingSort.Compile(sorted); err == nil {
		t.Fatal("missing sort mapping accepted")
	}
	parts, err := operatorCompiler(t).Compile(sorted)
	if err != nil {
		t.Fatalf("Compile(sorted) error = %v", err)
	}
	if parts.OrderBy != `"records"."id" DESC NULLS LAST` {
		t.Fatalf("OrderBy = %q", parts.OrderBy)
	}
}

func TestIdentifierValidationMatrix(t *testing.T) {
	t.Parallel()

	for _, identifier := range []string{"", ".id", "a.b.c.d", "1table.id", "table.bad-name"} {
		if _, err := apiquerypgx.NewCompiler(apiquerypgx.Mapping{Fields: map[string]string{"id": identifier}}); err == nil {
			t.Fatalf("NewCompiler() accepted %q", identifier)
		}
	}
	if _, err := apiquerypgx.NewCompiler(apiquerypgx.Mapping{Fields: map[string]string{"": "records.id"}}); err == nil {
		t.Fatal("NewCompiler() accepted empty capability name")
	}
}

func operatorCompiler(t *testing.T) *apiquerypgx.Compiler {
	t.Helper()
	compiler, err := apiquerypgx.NewCompiler(apiquerypgx.Mapping{
		Fields: map[string]string{"id": "records.id"}, Filters: map[string]string{"value": "records.value"},
		Sorts: map[string]string{"id": "records.id"},
	})
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	return compiler
}

func operatorPlan(t *testing.T, expression *apiquery.FilterExpr) *apiquery.Plan {
	t.Helper()
	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{Resource: "records", Revision: "v1",
		Fields: []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString, Required: true}},
		Filters: []apiquery.FilterDefinition{{Name: "value", Type: apiquery.TypeString, Nullable: true, AllowEmpty: true,
			Operators: []apiquery.Operator{apiquery.OpEqual, apiquery.OpNotEqual, apiquery.OpLess, apiquery.OpLessOrEqual,
				apiquery.OpGreater, apiquery.OpGreaterOrEqual, apiquery.OpIn, apiquery.OpNotIn, apiquery.OpBetween,
				apiquery.OpIsNull, apiquery.OpContains, apiquery.OpStartsWith, apiquery.OpEndsWith}}},
		AllowedLogic: []apiquery.Logic{apiquery.LogicAnd, apiquery.LogicOr, apiquery.LogicNot},
	})
	if err != nil {
		t.Fatalf("NewSchema() error = %v", err)
	}
	plan, err := apiquery.Compile(context.Background(), schema, apiquery.Request{Filter: expression}, apiquery.CompileOptions{})
	if err != nil {
		t.Fatalf("apiquery.Compile() error = %v", err)
	}
	return plan
}

func sortedPlan(t *testing.T) *apiquery.Plan {
	t.Helper()
	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{Resource: "records", Revision: "v1",
		Fields: []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString, Required: true}},
		Sorts:  []apiquery.SortDefinition{{Name: "id", Type: apiquery.TypeString, Nulls: apiquery.NullsLast}},
	})
	if err != nil {
		t.Fatalf("NewSchema() error = %v", err)
	}
	plan, err := apiquery.Compile(context.Background(), schema, apiquery.Request{
		Sorts: apiquery.Present([]apiquery.SortTerm{{Name: "id", Direction: apiquery.Descending}}),
	}, apiquery.CompileOptions{})
	if err != nil {
		t.Fatalf("apiquery.Compile() error = %v", err)
	}
	return plan
}

func leaf(operator apiquery.Operator, values ...string) *apiquery.FilterExpr {
	typed := make([]apiquery.Value, len(values))
	for index, value := range values {
		typed[index] = apiquery.StringValue(value)
	}
	return &apiquery.FilterExpr{Predicate: &apiquery.Predicate{Name: "value", Operator: operator, Values: typed}}
}

func group(logic apiquery.Logic, children ...*apiquery.FilterExpr) *apiquery.FilterExpr {
	values := make([]apiquery.FilterExpr, len(children))
	for index, child := range children {
		values[index] = *child
	}
	return &apiquery.FilterExpr{Logic: logic, Children: values}
}
