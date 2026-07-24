package apiquerypgx

import (
	"errors"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

func TestCompileFilterFailsClosedForImpossiblePlanState(t *testing.T) {
	t.Parallel()

	compiler := &Compiler{mapping: Mapping{Filters: map[string]string{"value": "records.value"}}}
	arguments := []apiquery.Value{}
	invalidOperator := &apiquery.FilterExpr{Predicate: &apiquery.Predicate{Name: "value",
		Operator: apiquery.Operator("raw"), Values: []apiquery.Value{apiquery.StringValue("x")}}}
	if _, err := compiler.compileFilter(invalidOperator, &arguments); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid operator error = %v", err)
	}
	invalidLogic := &apiquery.FilterExpr{Logic: apiquery.Logic("raw"), Children: []apiquery.FilterExpr{
		{Predicate: &apiquery.Predicate{Name: "value", Operator: apiquery.OpEqual,
			Values: []apiquery.Value{apiquery.StringValue("x")}}},
	}}
	if _, err := compiler.compileFilter(invalidLogic, &arguments); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid logic error = %v", err)
	}
}
