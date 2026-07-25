package xsd_test

import (
	"context"
	_ "embed"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/compile"
	"github.com/faustbrian/golib/pkg/xsd/validate"
)

//go:embed testdata/benchmark/schema.xsd
var benchmarkSchema []byte

//go:embed testdata/benchmark/valid.xml
var benchmarkInstance []byte

func BenchmarkParseSchema(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchmarkSchema)))
	for b.Loop() {
		if _, err := xsd.Parse(context.Background(), benchmarkSchema, xsd.ParseOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompileSchema(b *testing.B) {
	compiler, err := compile.New(compile.Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(benchmarkSchema)))
	for b.Loop() {
		if _, err := compiler.Compile(context.Background(), compile.Source{
			URI: "urn:benchmark:schema", Content: benchmarkSchema,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateInstance(b *testing.B) {
	validator := benchmarkValidator(b)
	result, err := validator.Validate(context.Background(), benchmarkInstance)
	if err != nil || !result.Valid {
		b.Fatalf("correctness check = %#v, %v", result, err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(benchmarkInstance)))
	b.ResetTimer()
	for b.Loop() {
		result, err := validator.Validate(context.Background(), benchmarkInstance)
		if err != nil || !result.Valid {
			b.Fatalf("Validate() = %#v, %v", result, err)
		}
	}
}

func BenchmarkMarshalSchema(b *testing.B) {
	document, err := xsd.Parse(context.Background(), benchmarkSchema, xsd.ParseOptions{})
	if err != nil {
		b.Fatal(err)
	}
	if _, err := xsd.Marshal(document); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := xsd.Marshal(document); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkValidator(tb testing.TB) *validate.Validator {
	tb.Helper()
	compiler, err := compile.New(compile.Options{})
	if err != nil {
		tb.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "urn:benchmark:schema", Content: benchmarkSchema,
	})
	if err != nil {
		tb.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		tb.Fatal(err)
	}
	return validator
}
