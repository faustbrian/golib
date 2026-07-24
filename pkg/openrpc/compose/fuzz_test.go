package compose_test

import (
	"bytes"
	"context"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/compose"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	openrpcparse "github.com/faustbrian/golib/pkg/openrpc/parse"
)

func FuzzCompositionIsDeterministicAndPanicFree(f *testing.F) {
	f.Add(
		[]byte(`{"openrpc":"1.4.1","info":{"title":"Fuzz","version":"1"},"methods":[],"components":{"schemas":{"Old":true}}}`),
		[]byte(`{"info":{"title":"Patched"}}`), "New",
	)
	f.Fuzz(func(t *testing.T, input []byte, patch []byte, newName string) {
		parseOptions := openrpcparse.DefaultOptions()
		parseOptions.JSON = jsonvalue.Policy{MaxBytes: 1 << 20, MaxDepth: 64, MaxTokens: 100_000}
		parsed, err := openrpcparse.Decode(input, parseOptions)
		if err != nil {
			return
		}
		document := parsed.Document()
		predicate := compose.MethodPredicateFunc(func(_ context.Context, method openrpc.Method) (bool, error) {
			return len(method.Name())%2 == 0, nil
		})
		first, firstErr := compose.FilterMethods(context.Background(), document, predicate, compose.DefaultFilterOptions())
		second, secondErr := compose.FilterMethods(context.Background(), document, predicate, compose.DefaultFilterOptions())
		assertSameComposition(t, first, firstErr, second, secondErr)

		mergeOptions := compose.DefaultMergeOptions()
		mergeOptions.Conflict = compose.KeepLast
		first, firstErr = compose.Merge(context.Background(), []openrpc.Document{document, document}, mergeOptions)
		second, secondErr = compose.Merge(context.Background(), []openrpc.Document{document, document}, mergeOptions)
		assertSameComposition(t, first, firstErr, second, secondErr)

		overlay, overlayErr := compose.NewOverlay(patch, parseOptions.JSON)
		if overlayErr == nil {
			first, firstErr = compose.ApplyOverlays(context.Background(), document, []compose.Overlay{overlay}, compose.DefaultOverlayOptions())
			second, secondErr = compose.ApplyOverlays(context.Background(), document, []compose.Overlay{overlay}, compose.DefaultOverlayOptions())
			assertSameComposition(t, first, firstErr, second, secondErr)
		}

		renames := map[compose.ComponentKind]map[string]string{
			compose.SchemaComponents: {"Old": newName},
		}
		first, firstErr = compose.RenameComponents(context.Background(), document, renames, compose.DefaultRenameOptions())
		second, secondErr = compose.RenameComponents(context.Background(), document, renames, compose.DefaultRenameOptions())
		assertSameComposition(t, first, firstErr, second, secondErr)
	})
}

func assertSameComposition(
	t *testing.T,
	first openrpc.Document,
	firstErr error,
	second openrpc.Document,
	secondErr error,
) {
	t.Helper()
	if (firstErr == nil) != (secondErr == nil) ||
		firstErr != nil && firstErr.Error() != secondErr.Error() {
		t.Fatalf("composition errors differ: %v and %v", firstErr, secondErr)
	}
	if firstErr != nil {
		return
	}
	firstJSON, err := openrpc.MarshalCanonical(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := openrpc.MarshalCanonical(second)
	if err != nil || !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("composition output differs: %v", err)
	}
}
