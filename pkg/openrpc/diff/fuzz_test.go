package diff_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/diff"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	openrpcparse "github.com/faustbrian/golib/pkg/openrpc/parse"
)

func FuzzSemanticDiffIsDeterministic(f *testing.F) {
	document := []byte(`{"openrpc":"1.4.1","info":{"title":"Fuzz","version":"1"},"methods":[]}`)
	f.Add(document, document)
	f.Fuzz(func(t *testing.T, beforeInput []byte, afterInput []byte) {
		options := openrpcparse.DefaultOptions()
		options.JSON = jsonvalue.Policy{MaxBytes: 1 << 20, MaxDepth: 64, MaxTokens: 100_000}
		before, err := openrpcparse.Decode(beforeInput, options)
		if err != nil {
			return
		}
		after, err := openrpcparse.Decode(afterInput, options)
		if err != nil {
			return
		}
		first := diff.Compare(context.Background(), before.Document(), after.Document(), diff.DefaultOptions())
		second := diff.Compare(context.Background(), before.Document(), after.Document(), diff.DefaultOptions())
		if first.Err() != nil || second.Err() != nil || first.Truncated() != second.Truncated() ||
			!reflect.DeepEqual(first.Changes(), second.Changes()) {
			t.Fatal("semantic diff was not deterministic")
		}
	})
}
