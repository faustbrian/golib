package diff_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/diff"
	"github.com/faustbrian/golib/pkg/openapi/parse"
)

func FuzzOperationDiffDeterminism(f *testing.F) {
	seeds := [][2]string{
		{
			`{"openapi":"3.2.0","info":{"title":"A","version":"1"},"paths":{}}`,
			`{"openapi":"3.2.0","info":{"title":"A","version":"1"},"paths":{"/pets":{"get":{}}}}`,
		},
		{
			`{"swagger":"2.0","info":{"title":"A","version":"1"},"paths":{"/~0~1":{"post":{}}}}`,
			`{"swagger":"2.0","info":{"title":"A","version":"1"},"paths":{}}`,
		},
		{`null`, `[`},
	}
	for _, seed := range seeds {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, leftRaw string, rightRaw string) {
		left, leftOK := fuzzDocument(leftRaw)
		right, rightOK := fuzzDocument(rightRaw)
		if !leftOK || !rightOK ||
			left.SpecificationVersion().Dialect() != right.SpecificationVersion().Dialect() {
			return
		}
		options := diff.DefaultOptions()
		options.MaxChanges = 1_000
		first, firstErr := diff.Operations(context.Background(), left, right, options)
		second, secondErr := diff.Operations(context.Background(), left, right, options)
		if !sameError(firstErr, secondErr) {
			t.Fatalf("nondeterministic errors: %v and %v", firstErr, secondErr)
		}
		if firstErr != nil {
			return
		}
		if !reflect.DeepEqual(first.Changes(), second.Changes()) {
			t.Fatalf("nondeterministic reports: %#v and %#v", first, second)
		}
		for _, change := range first.Changes() {
			if !strings.HasPrefix(change.Pointer(), "/paths/") &&
				!strings.HasPrefix(change.Pointer(), "/webhooks/") {
				t.Fatalf("change has invalid surface pointer %q", change.Pointer())
			}
		}
	})
}

func fuzzDocument(raw string) (openapi.Document, bool) {
	limits := parse.DefaultLimits()
	limits.MaxBytes = 64 * 1024
	limits.MaxTokens = 4_096
	limits.MaxDepth = 64
	limits.MaxObjectMembers = 1_024
	limits.MaxArrayItems = 1_024
	limits.MaxScalarBytes = 16 * 1024
	limits.MaxTotalValues = 2_048
	document, err := openapi.ParseJSON(
		context.Background(),
		strings.NewReader(raw),
		limits,
	)
	return document, err == nil
}

func sameError(left error, right error) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Error() == right.Error()
}
