package expression_test

import (
	"reflect"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/expression"
)

func FuzzRuntimeExpressionParse(f *testing.F) {
	for _, seed := range []string{
		"$url", "$method", "$statusCode", "$request.header.accept",
		"$request.body#/items/0", "$response.query.name", "$request.bad",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		first, firstErr := expression.Parse(raw)
		second, secondErr := expression.Parse(raw)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("nondeterministic errors: %v and %v", firstErr, secondErr)
		}
		if firstErr == nil && (first.String() != raw || !reflect.DeepEqual(first, second)) {
			t.Fatalf("nondeterministic expressions: %#v and %#v", first, second)
		}
	})
}

func FuzzRuntimeExpressionTemplateParse(f *testing.F) {
	for _, seed := range []string{
		"literal", "{$url}", "prefix/{$request.path.id}", "{", "{}", "literal}",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		template, err := expression.ParseTemplate(raw)
		if err != nil {
			return
		}
		parts := template.Parts()
		partsAgain := template.Parts()
		if !reflect.DeepEqual(parts, partsAgain) {
			t.Fatalf("nondeterministic template parts: %#v and %#v", parts, partsAgain)
		}
	})
}
