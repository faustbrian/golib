package compose_test

import (
	"context"
	"errors"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/compose"
)

func TestFilterMethodsPreservesDocumentAndAllowsEmptyVisibility(t *testing.T) {
	t.Parallel()

	document := testDocument(t, "public", "secret")
	filtered, err := compose.FilterMethods(
		context.Background(),
		document,
		compose.MethodPredicateFunc(func(_ context.Context, method openrpc.Method) (bool, error) {
			return method.Name() == "missing", nil
		}),
		compose.DefaultFilterOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Methods()) != 0 {
		t.Fatalf("methods = %#v", filtered.Methods())
	}
	if filtered.Version() != document.Version() || filtered.Info().Title() != document.Info().Title() {
		t.Fatal("filter did not preserve required root metadata")
	}
}

func TestFilterMethodsHandlesReferencesByExplicitPolicy(t *testing.T) {
	t.Parallel()

	document := testDocument(t, "public")
	reference, err := openrpc.NewReference("#/components/methods/missing")
	if err != nil {
		t.Fatal(err)
	}
	info := document.Info()
	document, err = openrpc.NewDocument(openrpc.DocumentInput{
		Version: document.Version(), Info: &info,
		Methods: append(document.Methods(), openrpc.MethodReference(reference)),
	})
	if err != nil {
		t.Fatal(err)
	}
	predicate := compose.MethodPredicateFunc(func(context.Context, openrpc.Method) (bool, error) { return true, nil })
	keep, err := compose.FilterMethods(context.Background(), document, predicate, compose.DefaultFilterOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(keep.Methods()) != 2 {
		t.Fatalf("kept methods = %#v", keep.Methods())
	}
	options := compose.DefaultFilterOptions()
	options.KeepReferences = false
	drop, err := compose.FilterMethods(context.Background(), document, predicate, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(drop.Methods()) != 1 {
		t.Fatalf("dropped methods = %#v", drop.Methods())
	}
}

func TestFilterMethodsBoundsCancellationAndPolicyFailures(t *testing.T) {
	t.Parallel()

	document := testDocument(t, "one", "two")
	if _, err := compose.FilterMethods(context.Background(), document, nil, compose.DefaultFilterOptions()); !errors.Is(err, compose.ErrInvalidFilter) {
		t.Fatalf("nil predicate error = %v", err)
	}
	options := compose.DefaultFilterOptions()
	options.MaxMethods = 1
	predicate := compose.MethodPredicateFunc(func(context.Context, openrpc.Method) (bool, error) { return true, nil })
	if _, err := compose.FilterMethods(context.Background(), document, predicate, options); !errors.Is(err, compose.ErrFilterLimit) {
		t.Fatalf("limit error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := compose.FilterMethods(ctx, document, predicate, compose.DefaultFilterOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	failing := compose.MethodPredicateFunc(func(context.Context, openrpc.Method) (bool, error) {
		return false, errors.New("policy secret")
	})
	if _, err := compose.FilterMethods(context.Background(), document, failing, compose.DefaultFilterOptions()); !errors.Is(err, compose.ErrFilterPolicy) || err.Error() != compose.ErrFilterPolicy.Error() {
		t.Fatalf("policy error = %v", err)
	}
}

func testDocument(t *testing.T, names ...string) openrpc.Document {
	t.Helper()
	version, err := openrpc.ParseVersion("1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := openrpc.NewInfo(openrpc.InfoInput{Title: "Filter", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	methods := make([]openrpc.MethodOrReference, len(names))
	for index, name := range names {
		method, methodErr := openrpc.NewMethod(openrpc.MethodInput{Name: name, Params: []openrpc.ContentDescriptorOrReference{}})
		if methodErr != nil {
			t.Fatal(methodErr)
		}
		methods[index] = openrpc.MethodValue(method)
	}
	document, err := openrpc.NewDocument(openrpc.DocumentInput{Version: version, Info: &info, Methods: methods})
	if err != nil {
		t.Fatal(err)
	}
	return document
}
