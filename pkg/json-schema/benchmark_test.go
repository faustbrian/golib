package jsonschema_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

var benchmarkSchema = []byte(`{
	"type":"object",
	"required":["id","tags"],
	"properties":{
		"id":{"type":"integer","minimum":1},
		"tags":{"type":"array","items":{"type":"string"},"uniqueItems":true}
	},
	"additionalProperties":false
}`)

var benchmarkInstance = []byte(`{"id":42,"tags":["one","two","three"]}`)

func BenchmarkCompile(b *testing.B) {
	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := compiler.Compile(context.Background(), benchmarkSchema); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidate(b *testing.B) {
	schema := compileBenchmarkSchema(b, benchmarkSchema)
	b.ReportAllocs()
	b.SetBytes(int64(len(benchmarkInstance)))
	for b.Loop() {
		result, err := schema.Validate(context.Background(), benchmarkInstance)
		if err != nil || !result.Valid {
			b.Fatalf("result %#v: %v", result, err)
		}
	}
}

func BenchmarkValidateReference(b *testing.B) {
	schema := compileBenchmarkSchema(b, []byte(`{
		"$defs":{"identifier":{"type":"integer","minimum":1}},
		"properties":{"id":{"$ref":"#/$defs/identifier"}}
	}`))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := schema.Validate(context.Background(), []byte(`{"id":42}`)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateUniqueItemsScaling(b *testing.B) {
	schema := compileBenchmarkSchema(b, []byte(`{"type":"array","uniqueItems":true}`))
	for _, size := range []int{10, 100, 500} {
		instance := []byte("[" + integerSequence(size) + "]")
		b.Run(fmt.Sprintf("items_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(instance)))
			for b.Loop() {
				if _, err := schema.Validate(context.Background(), instance); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func compileBenchmarkSchema(tb testing.TB, raw []byte) *jsonschema.Schema {
	tb.Helper()
	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		tb.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), raw)
	if err != nil {
		tb.Fatal(err)
	}
	return schema
}

func integerSequence(size int) string {
	values := make([]string, size)
	for index := range values {
		values[index] = fmt.Sprint(index)
	}
	return strings.Join(values, ",")
}
