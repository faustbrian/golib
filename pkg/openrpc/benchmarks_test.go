package openrpc_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/diff"
	"github.com/faustbrian/golib/pkg/openrpc/discovery"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	"github.com/faustbrian/golib/pkg/openrpc/parse"
	"github.com/faustbrian/golib/pkg/openrpc/reference"
	"github.com/faustbrian/golib/pkg/openrpc/validate"
)

func BenchmarkParseCompleteDocument(b *testing.B) {
	input, _ := benchmarkDocument(b)
	options := parse.DefaultOptions()
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for range b.N {
		if _, err := parse.Decode(input, options); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateCompleteDocument(b *testing.B) {
	_, document := benchmarkDocument(b)
	options := validate.DefaultOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if report := validate.Document(context.Background(), document, options); !report.Valid() {
			b.Fatal(report.Diagnostics())
		}
	}
}

func BenchmarkSerializeCompleteDocument(b *testing.B) {
	_, document := benchmarkDocument(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := openrpc.MarshalCanonical(document); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolveReference(b *testing.B) {
	root, resolver := benchmarkResolver(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := resolver.Resolve(
			context.Background(), root, "https://example.com/openrpc.json",
			"schemas.json#/Value",
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBundleResources(b *testing.B) {
	root, resolver := benchmarkResolver(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := reference.Bundle(
			context.Background(), resolver, root,
			"https://example.com/openrpc.json",
		); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkResolver(tb testing.TB) (jsonvalue.Value, *reference.Resolver) {
	tb.Helper()
	root, err := jsonvalue.Parse(
		[]byte(`{"schema":{"$ref":"schemas.json#/Value"}}`),
		jsonvalue.DefaultPolicy(),
	)
	if err != nil {
		tb.Fatal(err)
	}
	store, err := reference.NewMemoryStore(map[string][]byte{
		"https://example.com/schemas.json": []byte(`{"Value":{"type":"string"}}`),
	})
	if err != nil {
		tb.Fatal(err)
	}
	policy := reference.DefaultResolvePolicy()
	policy.AllowExternal = true
	policy.AllowedSchemes = []string{"https"}
	policy.AllowedHosts = []string{"example.com"}
	resolver, err := reference.NewResolver(store, policy)
	if err != nil {
		tb.Fatal(err)
	}
	return root, resolver
}

func BenchmarkDiffCompleteDocument(b *testing.B) {
	_, document := benchmarkDocument(b)
	options := diff.DefaultOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if report := diff.Compare(context.Background(), document, document, options); report.Err() != nil {
			b.Fatal(report.Err())
		}
	}
}

func BenchmarkDiscoveryCompleteDocument(b *testing.B) {
	_, document := benchmarkDocument(b)
	service, err := discovery.NewService(discovery.Static(document), nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := service.Discover(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLargeSchemaCompile(b *testing.B) {
	var properties strings.Builder
	for index := range 500 {
		if index != 0 {
			properties.WriteByte(',')
		}
		fmt.Fprintf(&properties, "%q:{\"type\":\"string\"}", fmt.Sprintf("field%d", index))
	}
	schema, err := jsonschema.Parse(
		[]byte(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{`+properties.String()+`}}`),
		jsonvalue.DefaultPolicy(),
	)
	if err != nil {
		b.Fatal(err)
	}
	options := jsonschema.DefaultValidationOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := jsonschema.Compile(schema, options); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHostileBoundedParse(b *testing.B) {
	input := []byte(strings.Repeat("[", 128) + "0" + strings.Repeat("]", 128))
	policy := jsonvalue.Policy{MaxBytes: len(input), MaxDepth: 16, MaxTokens: 512}
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for range b.N {
		if _, err := jsonvalue.Parse(input, policy); err == nil {
			b.Fatal("hostile input unexpectedly accepted")
		}
	}
}

func benchmarkDocument(tb testing.TB) ([]byte, openrpc.Document) {
	tb.Helper()
	input, err := os.ReadFile("parse/testdata/complete-openrpc.json")
	if err != nil {
		tb.Fatal(err)
	}
	parsed, err := parse.Decode(input, parse.DefaultOptions())
	if err != nil {
		tb.Fatal(err)
	}
	return input, parsed.Document()
}
