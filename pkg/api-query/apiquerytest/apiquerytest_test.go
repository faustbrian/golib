package apiquerytest_test

import (
	"context"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
	"github.com/faustbrian/golib/pkg/api-query/apiquerytest"
)

func TestBuildersAndAssertions(t *testing.T) {
	t.Parallel()

	schema := apiquerytest.NewSchema("orders", "v1").
		Field(apiquery.FieldDefinition{Name: "id", Type: apiquery.TypeString, Required: true}).
		Field(apiquery.FieldDefinition{Name: "status", Type: apiquery.TypeString, Default: true}).
		Filter(apiquery.FilterDefinition{Name: "status", Type: apiquery.TypeString,
			Operators: []apiquery.Operator{apiquery.OpEqual}}).
		MustBuild(t)
	request := apiquerytest.NewRequest().Fields("status").
		Where("status", apiquery.OpEqual, apiquery.StringValue("paid")).Build()
	plan := apiquerytest.MustCompile(t, schema, request, apiquery.CompileOptions{})
	apiquerytest.AssertCanonicalEqual(t, plan, plan)

	_, err := apiquery.Compile(context.Background(), schema,
		apiquerytest.NewRequest().Fields("missing").Build(), apiquery.CompileOptions{})
	apiquerytest.AssertViolation(t, err, apiquery.CodeInvalidElement, "fields[0]")
}

func TestRequestBuilderReturnsDefensiveFilterSnapshot(t *testing.T) {
	t.Parallel()

	builder := apiquerytest.NewRequest().Where("status", apiquery.OpEqual, apiquery.StringValue("open"))
	first := builder.Build()
	first.Filter.Predicate.Values[0] = apiquery.StringValue("changed")
	second := builder.Build()
	if second.Filter.Predicate.Values[0].String() != "open" {
		t.Fatal("Build returned caller-owned filter storage")
	}
}

func TestCanonicalConformanceSuite(t *testing.T) {
	t.Parallel()

	schema := apiquerytest.OrderSchema(t)
	request := apiquerytest.NewRequest().Fields("status").Build()
	apiquerytest.RunCanonicalConformance(t, schema, apiquery.CompileOptions{}, map[string]func() (apiquery.Request, error){
		"http": func() (apiquery.Request, error) { return request, nil },
		"rpc":  func() (apiquery.Request, error) { return request, nil },
	})
}
