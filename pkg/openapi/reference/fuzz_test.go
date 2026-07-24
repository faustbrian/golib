package reference_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func FuzzPointerCanonicalRoundTrip(f *testing.F) {
	for _, seed := range []string{"", "/a", "/a~1b/m~0n/0", "/", "/bad~2escape"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		pointer, err := reference.ParsePointer(raw)
		if err != nil {
			return
		}
		reparsed, err := reference.ParsePointer(pointer.String())
		if err != nil {
			t.Fatalf("canonical pointer %q failed: %v", pointer.String(), err)
		}
		if !reflect.DeepEqual(pointer.Tokens(), reparsed.Tokens()) {
			t.Fatalf("token drift: %#v and %#v", pointer.Tokens(), reparsed.Tokens())
		}
	})
}

func FuzzFragmentClassificationIsStable(f *testing.F) {
	for _, seed := range []string{"", "/components/schemas/Pet", "anchor", "%2Fescaped", "%ff"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		first, firstErr := reference.ParseFragment(raw)
		second, secondErr := reference.ParseFragment(raw)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("nondeterministic errors: %v and %v", firstErr, secondErr)
		}
		if firstErr == nil && (first.Kind() != second.Kind() ||
			first.Pointer().String() != second.Pointer().String() ||
			first.Anchor() != second.Anchor()) {
			t.Fatalf("nondeterministic fragments: %#v and %#v", first, second)
		}
	})
}

func FuzzBundleInternalDocumentsIsIdentity(f *testing.F) {
	for _, seed := range []string{
		`{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"object"},"Alias":{"$ref":"#/components/schemas/Pet"}}}}`,
		`{"openapi":"3.1.2","paths":{}}`,
		`{"swagger":"2.0","paths":{},"definitions":{"Pet":{"type":"string"}}}`,
		`null`,
		`[`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
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
		if err != nil {
			return
		}
		occurrences, err := reference.Scan(
			context.Background(), document.Raw(), reference.DefaultLimits(),
		)
		if err != nil {
			return
		}
		for _, occurrence := range occurrences {
			if !strings.HasPrefix(occurrence.Raw(), "#") {
				return
			}
		}
		result, err := reference.BundleComponents(
			context.Background(),
			reference.Resource{Root: document.Raw()},
			nil,
			reference.DefaultBundleOptions(),
		)
		if err != nil {
			return
		}
		before, _ := document.Raw().MarshalJSON()
		after, _ := result.Document().Raw().MarshalJSON()
		if !bytes.Equal(after, before) {
			t.Fatalf("internal-only bundle changed semantics:\n%s\n%s", before, after)
		}
		if len(result.Entries()) != 0 {
			t.Fatalf("internal-only bundle entries = %#v", result.Entries())
		}
	})
}

func FuzzDereferenceObjectsIsDeterministic(f *testing.F) {
	for _, seed := range []string{
		`{"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{},"components":{"responses":{
		"Target":{"description":"OK"},
		"Alias":{"$ref":"#/components/responses/Target"}}}}`,
		`{"openapi":"3.2.0","paths":{},"components":{"schemas":{
		"Node":{"$ref":"#/components/schemas/Node"}}}}`,
		`{"swagger":"2.0","info":{"title":"API","version":"1"},"paths":{}}`,
		`null`,
		`[`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
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
		if err != nil {
			return
		}
		options := reference.DefaultDereferenceOptions()
		options.MaxReferences = 1_024
		options.MaxNodes = 4_096
		options.MaxDepth = 64
		base := reference.Resource{Root: document.Raw()}
		first, firstErr := reference.DereferenceObjects(
			context.Background(), base, nil, options,
		)
		second, secondErr := reference.DereferenceObjects(
			context.Background(), base, nil, options,
		)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("nondeterministic errors: %v and %v", firstErr, secondErr)
		}
		if firstErr != nil {
			if firstErr.Error() != secondErr.Error() {
				t.Fatalf("nondeterministic errors: %v and %v", firstErr, secondErr)
			}
			return
		}
		firstJSON, _ := first.Document().Raw().MarshalJSON()
		secondJSON, _ := second.Document().Raw().MarshalJSON()
		if !bytes.Equal(firstJSON, secondJSON) ||
			!reflect.DeepEqual(first.Entries(), second.Entries()) {
			t.Fatalf("nondeterministic dereference results")
		}
	})
}
