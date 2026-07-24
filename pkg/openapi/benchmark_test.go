package openapi_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/compose"
	"github.com/faustbrian/golib/pkg/openapi/convert"
	"github.com/faustbrian/golib/pkg/openapi/diff"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/serialize"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func BenchmarkParseJSON(b *testing.B) {
	raw := benchmarkDescription(100)
	limits := parse.DefaultLimits()
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for range b.N {
		if _, err := openapi.ParseJSON(context.Background(), bytes.NewReader(raw), limits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseJSONScaling(b *testing.B) {
	for _, paths := range []int{1, 100, 1_000} {
		raw := benchmarkDescription(paths)
		b.Run(fmt.Sprintf("paths_%d", paths), func(b *testing.B) {
			limits := parse.DefaultLimits()
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			for range b.N {
				if _, err := openapi.ParseJSON(
					context.Background(), bytes.NewReader(raw), limits,
				); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkParseInvalidJSON(b *testing.B) {
	raw := benchmarkDescription(100)
	raw = raw[:len(raw)-1]
	limits := parse.DefaultLimits()
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	for range b.N {
		if _, err := openapi.ParseJSON(
			context.Background(), bytes.NewReader(raw), limits,
		); !errors.Is(err, parse.ErrInvalidJSON) {
			b.Fatalf("invalid document error = %v", err)
		}
	}
}

func BenchmarkParseRejectedDepth(b *testing.B) {
	limits := parse.DefaultLimits()
	raw := []byte(strings.Repeat("[", limits.MaxDepth+1) + "0" +
		strings.Repeat("]", limits.MaxDepth+1))
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	for range b.N {
		if _, err := openapi.ParseJSON(
			context.Background(), bytes.NewReader(raw), limits,
		); !errors.Is(err, parse.ErrLimitExceeded) {
			b.Fatalf("depth-limit error = %v", err)
		}
	}
}

func BenchmarkValidateDocument(b *testing.B) {
	document := benchmarkParsedDocument(b, benchmarkDescription(100))
	validator := validate.NewValidator()
	if report, err := validator.Document(context.Background(), document); err != nil {
		b.Fatal(err)
	} else if !report.Valid() {
		b.Fatal("representative document is invalid")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		report, err := validator.Document(context.Background(), document)
		if err != nil {
			b.Fatal(err)
		}
		if !report.Valid() {
			b.Fatal("representative document is invalid")
		}
	}
}

func BenchmarkValidateDocumentCold(b *testing.B) {
	document := benchmarkParsedDocument(b, benchmarkDescription(100))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		report, err := validate.NewValidator().Document(
			context.Background(), document,
		)
		if err != nil {
			b.Fatal(err)
		}
		if !report.Valid() {
			b.Fatal("representative document is invalid")
		}
	}
}

func BenchmarkValidateSchemaHeavyDocument(b *testing.B) {
	document := benchmarkParsedDocument(b, benchmarkSchemaHeavyDescription(250))
	validator := validate.NewValidator()
	if report, err := validator.Document(
		context.Background(), document,
	); err != nil || !report.Valid() {
		b.Fatalf("schema-heavy baseline = %v, %#v", err, report.Diagnostics())
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		report, err := validator.Document(context.Background(), document)
		if err != nil || !report.Valid() {
			b.Fatalf("schema-heavy validation = %v, %#v", err, report.Diagnostics())
		}
	}
}

func BenchmarkSerializeJSON(b *testing.B) {
	document := benchmarkParsedDocument(b, benchmarkDescription(100))
	options := serialize.DefaultOptions()
	options.Mode = serialize.Canonical
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := serialize.JSON(context.Background(), io.Discard, document, options); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolveInternalReference(b *testing.B) {
	document := benchmarkParsedDocument(b, benchmarkDescription(100))
	resource := reference.Resource{Root: document.Raw()}
	limits := reference.DefaultLimits()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		target, err := reference.Resolve(
			context.Background(), resource,
			"#/components/schemas/Pet", nil, limits,
		)
		if err != nil {
			b.Fatal(err)
		}
		if target.Value.Kind() == 0 {
			b.Fatal("resolved an invalid value")
		}
	}
}

func BenchmarkResolveFileResource(b *testing.B) {
	root := b.TempDir()
	resourcePath := filepath.Join(root, "schema.json")
	raw := []byte(`{"type":"object","properties":{"id":{"type":"string"}}}`)
	if err := os.WriteFile(resourcePath, raw, 0o600); err != nil {
		b.Fatal(err)
	}
	options := reference.DefaultFileResolverOptions()
	options.AllowedRoots = []string{root}
	options.MaxDocuments = b.N
	resolver, err := reference.NewFileResolver(options)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if closeErr := resolver.Close(); closeErr != nil {
			b.Errorf("close file resolver: %v", closeErr)
		}
	})
	identifier := (&url.URL{Scheme: "file", Path: resourcePath}).String()
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for range b.N {
		resource, resolveErr := resolver.Resolve(context.Background(), identifier)
		if resolveErr != nil {
			b.Fatal(resolveErr)
		}
		if resource.Root.Kind() == 0 {
			b.Fatal("resolved an invalid resource")
		}
	}
}

func BenchmarkResolveHTTPResource(b *testing.B) {
	raw := []byte(`{"type":"object","properties":{"id":{"type":"string"}}}`)
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(raw)
	}))
	b.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		b.Fatal(err)
	}
	port, err := strconv.Atoi(serverURL.Port())
	if err != nil {
		b.Fatal(err)
	}
	options := reference.DefaultHTTPResolverOptions()
	options.AllowedSchemes = []string{"http"}
	options.AllowedHosts = []string{serverURL.Hostname()}
	options.AllowedPorts = []int{port}
	options.AllowedCIDRs = []string{"127.0.0.0/8", "::1/128"}
	options.MaxDocuments = b.N
	resolver, err := reference.NewHTTPResolver(options)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(resolver.CloseIdleConnections)
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for range b.N {
		resource, resolveErr := resolver.Resolve(context.Background(), server.URL)
		if resolveErr != nil {
			b.Fatal(resolveErr)
		}
		if resource.Root.Kind() == 0 {
			b.Fatal("resolved an invalid resource")
		}
	}
}

func BenchmarkBundleComponents(b *testing.B) {
	raw := bytes.ReplaceAll(
		benchmarkDescription(100),
		[]byte(`"$ref":"#/components/schemas/Pet"`),
		[]byte(`"$ref":"models.json#/components/schemas/Pet"`),
	)
	document := benchmarkParsedDocument(b, raw)
	base := reference.Resource{
		RetrievalURI: "https://api.example.test/openapi.json",
		Root:         document.Raw(),
	}
	externalValue, err := parse.JSON(
		context.Background(),
		strings.NewReader(`{"components":{"schemas":{"Pet":{"type":"object"}}}}`),
		parse.DefaultLimits(),
	)
	if err != nil {
		b.Fatal(err)
	}
	resolver := reference.ResolverFunc(func(
		context.Context,
		string,
	) (reference.Resource, error) {
		return reference.Resource{
			RetrievalURI: "https://api.example.test/models.json",
			Root:         externalValue,
		}, nil
	})
	options := reference.DefaultBundleOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := reference.BundleComponents(
			context.Background(), base, resolver, options,
		)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Entries()) != 100 {
			b.Fatal("unexpected bundled reference count")
		}
	}
}

func BenchmarkDereferenceObjects(b *testing.B) {
	var responses strings.Builder
	for index := range 100 {
		if index > 0 {
			responses.WriteByte(',')
		}
		fmt.Fprintf(
			&responses,
			`"%d":{"$ref":"#/components/responses/Shared"}`,
			200+index,
		)
	}
	raw := fmt.Sprintf(`{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"responses":{%s}}}},
		"components":{"responses":{"Shared":{"description":"OK"}}}
	}`, responses.String())
	document := benchmarkParsedDocument(b, []byte(raw))
	base := reference.Resource{Root: document.Raw()}
	options := reference.DefaultDereferenceOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := reference.DereferenceObjects(
			context.Background(), base, nil, options,
		)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Entries()) != 100 {
			b.Fatal("unexpected dereferenced reference count")
		}
	}
}

func BenchmarkDereferenceCycle(b *testing.B) {
	document := benchmarkParsedDocument(b, []byte(`{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{},"components":{"responses":{
		"One":{"$ref":"#/components/responses/Two"},
		"Two":{"$ref":"#/components/responses/One"}}}}`))
	base := reference.Resource{Root: document.Raw()}
	options := reference.DefaultDereferenceOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := reference.DereferenceObjects(
			context.Background(), base, nil, options,
		); !errors.Is(err, reference.ErrDereferenceCycle) {
			b.Fatalf("cycle error = %v", err)
		}
	}
}

func BenchmarkOperationDiff(b *testing.B) {
	left := benchmarkParsedDocument(b, benchmarkDescription(100))
	right := benchmarkParsedDocument(b, benchmarkDescription(101))
	options := diff.DefaultOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		report, err := diff.Operations(context.Background(), left, right, options)
		if err != nil {
			b.Fatal(err)
		}
		if len(report.Changes()) != 1 {
			b.Fatal("unexpected operation diff")
		}
	}
}

func BenchmarkFilterOperations(b *testing.B) {
	document := benchmarkParsedDocument(b, benchmarkDescription(100))
	options := compose.DefaultFilterOptions()
	keep := func(compose.Operation) (bool, error) { return true, nil }
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := compose.FilterOperations(
			context.Background(), document, keep, options,
		)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Removed()) != 0 {
			b.Fatal("keep-all filter removed operations")
		}
	}
}

func BenchmarkMergeDocuments(b *testing.B) {
	first := benchmarkParsedDocument(b, benchmarkDescriptionRange(0, 50))
	second := benchmarkParsedDocument(b, benchmarkDescriptionRange(50, 50))
	options := compose.DefaultMergeOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := compose.Merge(
			context.Background(), []openapi.Document{first, second}, options,
		)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Contributions()) != 50 {
			b.Fatal("unexpected merge contribution count")
		}
	}
}

func BenchmarkConvertOpenAPI31To32(b *testing.B) {
	raw := bytes.Replace(
		benchmarkDescription(100),
		[]byte(`"openapi":"3.2.0"`),
		[]byte(`"openapi":"3.1.2"`),
		1,
	)
	document := benchmarkParsedDocument(b, raw)
	target, err := openapi.ParseVersion("3.2.0")
	if err != nil {
		b.Fatal(err)
	}
	options := convert.DefaultOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := convert.To(context.Background(), document, target, options)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Diagnostics()) != 1 {
			b.Fatal("unexpected conversion diagnostics")
		}
	}
}

func BenchmarkConvertOpenAPI30To31(b *testing.B) {
	raw := bytes.Replace(
		benchmarkDescription(100),
		[]byte(`"openapi":"3.2.0"`),
		[]byte(`"openapi":"3.0.4"`),
		1,
	)
	raw = bytes.ReplaceAll(
		raw,
		[]byte(`"type":"string"`),
		[]byte(`"type":"string","nullable":true`),
	)
	document := benchmarkParsedDocument(b, raw)
	target, err := openapi.ParseVersion("3.1.2")
	if err != nil {
		b.Fatal(err)
	}
	options := convert.DefaultOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := convert.To(context.Background(), document, target, options)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Diagnostics()) != 0 {
			b.Fatal("unexpected conversion diagnostics")
		}
	}
}

func BenchmarkConvertSwagger20ToOpenAPI30(b *testing.B) {
	document := benchmarkParsedDocument(b, benchmarkSwaggerDescription(100))
	target, err := openapi.ParseVersion("3.0.4")
	if err != nil {
		b.Fatal(err)
	}
	options := convert.DefaultOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := convert.To(context.Background(), document, target, options)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Diagnostics()) != 0 {
			b.Fatal("unexpected conversion diagnostics")
		}
	}
}

func BenchmarkConvertOpenAPI31To30(b *testing.B) {
	raw := bytes.Replace(
		benchmarkDescription(100),
		[]byte(`"openapi":"3.2.0"`),
		[]byte(`"openapi":"3.1.2"`),
		1,
	)
	raw = bytes.ReplaceAll(
		raw,
		[]byte(`"type":"string"`),
		[]byte(`"type":["string","null"]`),
	)
	document := benchmarkParsedDocument(b, raw)
	target, err := openapi.ParseVersion("3.0.4")
	if err != nil {
		b.Fatal(err)
	}
	options := convert.DefaultOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := convert.To(context.Background(), document, target, options)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Diagnostics()) != 0 {
			b.Fatal("unexpected conversion diagnostics")
		}
	}
}

func BenchmarkConvertOpenAPI32To31(b *testing.B) {
	document := benchmarkParsedDocument(b, benchmarkDescription(100))
	target, err := openapi.ParseVersion("3.1.2")
	if err != nil {
		b.Fatal(err)
	}
	options := convert.DefaultOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := convert.To(context.Background(), document, target, options)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Diagnostics()) != 0 {
			b.Fatal("unexpected conversion diagnostics")
		}
	}
}

func BenchmarkConvertOpenAPI32ToSwagger20(b *testing.B) {
	document := benchmarkParsedDocument(b, benchmarkDescription(100))
	target, err := openapi.ParseVersion("2.0")
	if err != nil {
		b.Fatal(err)
	}
	options := convert.DefaultOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := convert.To(context.Background(), document, target, options)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Diagnostics()) != 1 {
			b.Fatal("unexpected conversion diagnostics")
		}
	}
}

func benchmarkSwaggerDescription(paths int) []byte {
	var builder strings.Builder
	builder.WriteString(`{"swagger":"2.0","info":{"title":"Bench",`)
	builder.WriteString(`"version":"1"},"host":"api.example.test",`)
	builder.WriteString(`"basePath":"/v1","schemes":["https"],`)
	builder.WriteString(`"produces":["application/json"],"paths":{`)
	for index := range paths {
		if index > 0 {
			builder.WriteByte(',')
		}
		fmt.Fprintf(&builder, `"/items/%d":{"get":{"parameters":[`+
			`{"name":"tags","in":"query","type":"array",`+
			`"items":{"type":"string"},"collectionFormat":"multi"}],`+
			`"responses":{"200":{"description":"OK","schema":`+
			`{"$ref":"#/definitions/Item"}}}}}`, index)
	}
	builder.WriteString(`},"definitions":{"Item":{"type":"object",`)
	builder.WriteString(`"properties":{"id":{"type":"integer"}}}}}`)
	return []byte(builder.String())
}

func benchmarkDescription(paths int) []byte {
	return benchmarkDescriptionRange(0, paths)
}

func benchmarkDescriptionRange(start int, paths int) []byte {
	var output strings.Builder
	output.WriteString(`{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{`)
	for index := range paths {
		if index > 0 {
			output.WriteByte(',')
		}
		pathIndex := start + index
		fmt.Fprintf(
			&output,
			`"/items/%d":{"get":{"operationId":"getItem%d","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Pet"}}}}}}}`,
			pathIndex,
			pathIndex,
		)
	}
	output.WriteString(`},"components":{"schemas":{"Pet":{"type":"object","properties":{"id":{"type":"integer"},"name":{"type":"string"}}}}}}`)
	return []byte(output.String())
}

func benchmarkSchemaHeavyDescription(properties int) []byte {
	var output strings.Builder
	output.WriteString(`{"openapi":"3.2.0","info":{"title":"API",`)
	output.WriteString(`"version":"1"},"paths":{},"components":{"schemas":{`)
	output.WriteString(`"Payload":{"type":"object","properties":{`)
	for index := range properties {
		if index > 0 {
			output.WriteByte(',')
		}
		fmt.Fprintf(
			&output,
			`"field%d":{"type":["string","null"],"minLength":1,`+
				`"maxLength":128,"pattern":"^[a-z]+$"}`,
			index,
		)
	}
	output.WriteString(`}}}}}`)
	return []byte(output.String())
}

func benchmarkParsedDocument(tb testing.TB, raw []byte) openapi.Document {
	tb.Helper()
	document, err := openapi.ParseJSON(
		context.Background(), bytes.NewReader(raw), parse.DefaultLimits(),
	)
	if err != nil {
		tb.Fatal(err)
	}
	return document
}
