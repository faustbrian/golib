package reference_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	"github.com/faustbrian/golib/pkg/openrpc/reference"
)

func FuzzReferenceAndPointerParsing(f *testing.F) {
	for _, seed := range []string{
		"#/components/schemas/Value",
		"other.json#/$defs/~0escaped~1name",
		"https://example.com/schema.json",
		"#/%ZZ",
		"#/array/01",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		policy := reference.DefaultPolicy()
		policy.MaxLength = 1 << 10
		parsed, err := reference.Parse(input, policy)
		if err != nil {
			return
		}
		_, _ = parsed.ResolveAgainst("https://example.com/openrpc.json")
		if pointer, err := parsed.TargetPointer(reference.DefaultPointerPolicy()); err == nil {
			document, parseErr := jsonvalue.Parse(
				[]byte(`{"array":[0],"components":{"schemas":{"Value":true}}}`),
				jsonvalue.DefaultPolicy(),
			)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			_, _ = pointer.Evaluate(document, jsonvalue.DefaultPolicy())
		}
	})
}

func FuzzResolverInternalReferences(f *testing.F) {
	f.Add([]byte(`{"value":{"nested":true}}`), "#/value")
	f.Add([]byte(`[]`), "#/0")
	f.Add([]byte(`{"$ref":"#/cycle"}`), "#/$ref")
	f.Fuzz(func(t *testing.T, root []byte, rawReference string) {
		value, err := jsonvalue.Parse(root, jsonvalue.Policy{
			MaxBytes: 4 << 10, MaxDepth: 32, MaxTokens: 512,
		})
		if err != nil {
			return
		}
		resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
		if err != nil {
			t.Fatal(err)
		}
		_, _ = resolver.Resolve(
			context.Background(), value,
			"https://example.com/openrpc.json", rawReference,
		)
	})
}
