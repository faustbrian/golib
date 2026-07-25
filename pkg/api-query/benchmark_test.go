package apiquery_test

import (
	"context"
	"fmt"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

func BenchmarkCompile(b *testing.B) {
	schema := benchmarkSchema(b, 32)
	request := benchmarkRequest(16)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := apiquery.Compile(context.Background(), schema, request,
			apiquery.CompileOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanonical(b *testing.B) {
	schema := benchmarkSchema(b, 32)
	plan, err := apiquery.Compile(context.Background(), schema, benchmarkRequest(16),
		apiquery.CompileOptions{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := plan.Canonical(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompileDeepFilter(b *testing.B) {
	schema := benchmarkSchema(b, 64)
	request := benchmarkRequest(48)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := apiquery.Compile(context.Background(), schema, request,
			apiquery.CompileOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNewLargeSchema(b *testing.B) {
	config := benchmarkSchemaConfig(1_000, 1)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := apiquery.NewSchema(config); err != nil {
			b.Fatal(err)
		}
	}
}

func TestAllocationBudgets(t *testing.T) {
	schema := benchmarkSchema(t, 32)
	request := benchmarkRequest(16)
	plan, err := apiquery.Compile(context.Background(), schema, request, apiquery.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	largeConfig := benchmarkSchemaConfig(1_000, 1)
	tests := []struct {
		name string
		max  float64
		run  func()
	}{
		{"compile", 180, func() { _, _ = apiquery.Compile(context.Background(), schema, request, apiquery.CompileOptions{}) }},
		{"canonical", 80, func() { _, _ = plan.Canonical() }},
		{"large schema", 3000, func() { _, _ = apiquery.NewSchema(largeConfig) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allocations := testing.AllocsPerRun(100, test.run)
			if allocations > test.max {
				t.Fatalf("allocations = %.0f, budget = %.0f", allocations, test.max)
			}
		})
	}
}

func benchmarkSchema(t testing.TB, filterNodes int) *apiquery.Schema {
	t.Helper()
	schema, err := apiquery.NewSchema(benchmarkSchemaConfig(64, filterNodes))
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func benchmarkSchemaConfig(fieldCount, filterNodes int) apiquery.SchemaConfig {
	fields := make([]apiquery.FieldDefinition, fieldCount)
	for index := range fields {
		fields[index] = apiquery.FieldDefinition{Name: fmt.Sprintf("field_%04d", index),
			Type: apiquery.TypeString, Default: index < 8, Required: index == 0}
	}
	filters := make([]apiquery.FilterDefinition, filterNodes)
	for index := range filters {
		filters[index] = apiquery.FilterDefinition{Name: fmt.Sprintf("filter_%04d", index),
			Type: apiquery.TypeString, Operators: []apiquery.Operator{apiquery.OpEqual}}
	}
	return apiquery.SchemaConfig{
		Resource: "records", Revision: "v1", Fields: fields,
		Filters:      filters,
		AllowedLogic: []apiquery.Logic{apiquery.LogicAnd},
		Bounds: apiquery.Bounds{MaxFields: fieldCount, MaxFilterDepth: filterNodes + 1,
			MaxFilterNodes: filterNodes, MaxValues: filterNodes, MaxStringBytes: 64,
			MaxCanonicalBytes: 1 << 20, MaxErrors: 32, MaxCost: fieldCount + filterNodes + 1},
	}
}

func benchmarkRequest(nodes int) apiquery.Request {
	children := make([]apiquery.FilterExpr, nodes)
	for index := range children {
		children[index] = apiquery.FilterExpr{Predicate: &apiquery.Predicate{Name: fmt.Sprintf("filter_%04d", index),
			Operator: apiquery.OpEqual,
			Values:   []apiquery.Value{apiquery.StringValue(fmt.Sprintf("value-%d", index))}}}
	}
	return apiquery.Request{Filter: &apiquery.FilterExpr{Logic: apiquery.LogicAnd, Children: children}}
}
