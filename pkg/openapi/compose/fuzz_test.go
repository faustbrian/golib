package compose_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/compose"
	"github.com/faustbrian/golib/pkg/openapi/parse"
)

func FuzzFilterOperationsKeepAllIsIdentity(f *testing.F) {
	for _, seed := range []string{
		`{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}}`,
		`{"openapi":"3.1.2","info":{"title":"API","version":"1"},"paths":{"/pets":{"get":{}}}}`,
		`{"openapi":"3.0.4","info":{"title":"API","version":"1"},"paths":{}}`,
		`{"swagger":"2.0","info":{"title":"API","version":"1"},"paths":{"/pets":{"post":{}}}}`,
		`null`,
		`[`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		document, ok := fuzzComposeDocument(raw)
		if !ok {
			return
		}
		options := compose.DefaultFilterOptions()
		options.MaxOperations = 2_048
		options.MaxDepth = 128
		result, err := compose.FilterOperations(
			context.Background(), document,
			func(compose.Operation) (bool, error) { return true, nil },
			options,
		)
		if err != nil {
			t.Fatal(err)
		}
		before, err := document.Raw().MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		after, err := result.Document().Raw().MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			t.Fatalf("keep-all changed semantics:\n%s\n%s", before, after)
		}
		if len(result.Removed()) != 0 {
			t.Fatalf("keep-all removals = %#v", result.Removed())
		}
		if result.Document().SpecificationVersion() != document.SpecificationVersion() {
			t.Fatalf(
				"version changed from %s to %s",
				document.SpecificationVersion(),
				result.Document().SpecificationVersion(),
			)
		}
	})
}

func FuzzMergeIdenticalDocumentsIsIdentity(f *testing.F) {
	for _, seed := range []string{
		`{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":true}}}`,
		`{"openapi":"3.1.2","paths":{"/pets":{"get":{}}}}`,
		`{"openapi":"3.0.4","paths":{}}`,
		`{"swagger":"2.0","paths":{},"definitions":{"Pet":{"type":"string"}}}`,
		`null`,
		`[`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		document, ok := fuzzComposeDocument(raw)
		if !ok {
			return
		}
		options := compose.DefaultMergeOptions()
		options.MaxDocuments = 2
		options.MaxEntries = 4_096
		options.MaxDepth = 128
		options.MaxValueNodes = 16_384
		result, err := compose.Merge(
			context.Background(), []openapi.Document{document, document}, options,
		)
		if err != nil {
			t.Fatal(err)
		}
		before, err := document.Raw().MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		after, err := result.Document().Raw().MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			t.Fatalf("identical merge changed semantics:\n%s\n%s", before, after)
		}
		if len(result.Contributions()) != 0 {
			t.Fatalf("identical merge contributions = %#v", result.Contributions())
		}
	})
}

func fuzzComposeDocument(raw string) (openapi.Document, bool) {
	limits := parse.DefaultLimits()
	limits.MaxBytes = 64 * 1024
	limits.MaxTokens = 4_096
	limits.MaxDepth = 64
	limits.MaxObjectMembers = 1_024
	limits.MaxArrayItems = 1_024
	limits.MaxScalarBytes = 16 * 1024
	limits.MaxTotalValues = 2_048
	document, err := openapi.ParseJSON(
		context.Background(), strings.NewReader(raw), limits,
	)
	return document, err == nil
}
