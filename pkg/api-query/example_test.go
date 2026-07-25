package apiquery_test

import (
	"context"
	"fmt"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

func ExampleCompile() {
	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "orders-v1",
		Fields: []apiquery.FieldDefinition{
			{Name: "id", Type: apiquery.TypeString, Required: true},
			{Name: "status", Type: apiquery.TypeString, Default: true},
		},
	})
	if err != nil {
		fmt.Println("schema error")
		return
	}
	plan, err := apiquery.Compile(context.Background(), schema, apiquery.Request{},
		apiquery.CompileOptions{})
	if err != nil {
		fmt.Println("compile error")
		return
	}
	fmt.Println(plan.Resource(), plan.SchemaRevision(), plan.ResponseFields(), plan.ExecutionFields())
	// Output: orders orders-v1 [status] [id status]
}
