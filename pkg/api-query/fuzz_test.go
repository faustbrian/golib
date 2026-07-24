package apiquery_test

import (
	"context"
	"fmt"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

func FuzzCompileFilterExpression(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5})
	f.Add([]byte("\xff\x00duplicate-depth"))
	schema := fuzzSchema(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 512 {
			t.Skip()
		}
		expression := fuzzExpression(data)
		plan, err := apiquery.Compile(context.Background(), schema,
			apiquery.Request{Filter: expression}, apiquery.CompileOptions{})
		if err == nil {
			if _, canonicalErr := plan.Canonical(); canonicalErr != nil {
				t.Fatalf("accepted plan failed canonicalization: %v", canonicalErr)
			}
		}
	})
}

func fuzzSchema(t testing.TB) *apiquery.Schema {
	t.Helper()
	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "records", Revision: "v1",
		Fields: []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString, Required: true}},
		Filters: []apiquery.FilterDefinition{{Name: "value", Type: apiquery.TypeString,
			Operators: []apiquery.Operator{apiquery.OpEqual, apiquery.OpIn}}},
		AllowedLogic: []apiquery.Logic{apiquery.LogicAnd, apiquery.LogicOr, apiquery.LogicNot},
		Bounds: apiquery.Bounds{MaxFilterDepth: 8, MaxFilterNodes: 32, MaxValues: 32,
			MaxMembership: 8, MaxStringBytes: 64, MaxCanonicalBytes: 4096, MaxErrors: 16},
	})
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func fuzzExpression(data []byte) *apiquery.FilterExpr {
	if len(data) == 0 {
		return &apiquery.FilterExpr{}
	}
	leaves := make([]apiquery.FilterExpr, 0, len(data))
	for index, value := range data {
		name := "value"
		if value&1 != 0 {
			name = fmt.Sprintf("unknown_%d", value)
		}
		leaves = append(leaves, apiquery.FilterExpr{Predicate: &apiquery.Predicate{
			Name: name, Operator: apiquery.OpEqual,
			Values: []apiquery.Value{apiquery.StringValue(string([]byte{value}))},
		}})
		if index >= 63 {
			break
		}
	}
	expression := &leaves[0]
	for index := 1; index < len(leaves); index++ {
		expression = &apiquery.FilterExpr{Logic: apiquery.LogicAnd,
			Children: []apiquery.FilterExpr{*expression, leaves[index]}}
	}
	return expression
}
