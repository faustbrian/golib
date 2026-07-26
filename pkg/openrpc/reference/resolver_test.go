package reference_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	"github.com/faustbrian/golib/pkg/openrpc/reference"
)

type recordingStore struct {
	documents map[string][]byte
	loads     []string
}

func (store *recordingStore) Load(_ context.Context, documentURI string, _ int) ([]byte, error) {
	store.loads = append(store.loads, documentURI)
	document, ok := store.documents[documentURI]
	if !ok {
		return nil, errors.New("missing test document")
	}
	return append([]byte(nil), document...), nil
}

func TestResolverFollowsAliasesAndLoadsEachDocumentOnce(t *testing.T) {
	t.Parallel()

	root := parseValue(t, `{"entry":{"$ref":"shared.json#/alias"}}`)
	store := &recordingStore{documents: map[string][]byte{
		"https://example.com/api/shared.json": []byte(`{
			"alias":{"$ref":"#/target"},
			"target":{"answer":42}
		}`),
	}}
	policy := reference.DefaultResolvePolicy()
	policy.AllowExternal = true
	policy.AllowedSchemes = []string{"https"}
	policy.AllowedHosts = []string{"example.com"}
	resolver, err := reference.NewResolver(store, policy)
	if err != nil {
		t.Fatal(err)
	}

	target, err := resolver.Resolve(
		context.Background(),
		root,
		"https://example.com/api/openrpc.json",
		"#/entry",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(target.Value().Bytes()); got != `{"answer":42}` {
		t.Fatalf("resolved value = %s", got)
	}
	if target.DocumentURI() != "https://example.com/api/shared.json" {
		t.Fatalf("document URI = %q", target.DocumentURI())
	}
	if len(store.loads) != 1 || store.loads[0] != target.DocumentURI() {
		t.Fatalf("loads = %#v", store.loads)
	}
}

func TestResolverResolveManySharesFetchedDocumentsAcrossReferences(t *testing.T) {
	t.Parallel()

	root := parseValue(t, `{
		"first":{"$ref":"shared.json#/first"},
		"second":{"$ref":"shared.json#/second"}
	}`)
	store := &recordingStore{documents: map[string][]byte{
		"https://example.com/api/shared.json": []byte(`{
			"first":{"value":1},
			"second":{"value":2}
		}`),
	}}
	policy := reference.DefaultResolvePolicy()
	policy.AllowExternal = true
	policy.AllowedSchemes = []string{"https"}
	policy.AllowedHosts = []string{"example.com"}
	resolver, err := reference.NewResolver(store, policy)
	if err != nil {
		t.Fatal(err)
	}

	targets, err := resolver.ResolveMany(
		context.Background(),
		root,
		"https://example.com/api/openrpc.json",
		[]string{"#/first", "#/second"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || string(targets[0].Value().Bytes()) != `{"value":1}` ||
		string(targets[1].Value().Bytes()) != `{"value":2}` {
		t.Fatalf("targets = %#v", targets)
	}
	if len(store.loads) != 1 {
		t.Fatalf("loads = %#v", store.loads)
	}
}

func TestResolverBoundsAggregateReferenceFanout(t *testing.T) {
	t.Parallel()

	root := parseValue(t, `{
		"first":{"$ref":"#/firstValue"},
		"firstValue":1,
		"second":{"$ref":"#/secondValue"},
		"secondValue":2
	}`)
	policy := reference.DefaultResolvePolicy()
	policy.MaxReferences = 3
	resolver, err := reference.NewResolver(nil, policy)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.ResolveMany(
		context.Background(), root, "https://example.com/openrpc.json",
		[]string{"#/first", "#/second"},
	)
	if !errors.Is(err, reference.ErrResolveLimit) {
		t.Fatalf("aggregate alias fan-out error = %v", err)
	}

	policy.MaxReferences = 2
	resolver, err = reference.NewResolver(nil, policy)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.ResolveMany(
		context.Background(), root, "https://example.com/openrpc.json",
		[]string{"#", "#", "#"},
	)
	if !errors.Is(err, reference.ErrResolveLimit) {
		t.Fatalf("input fan-out error = %v", err)
	}
	_, err = resolver.ResolveMany(
		context.Background(), root, "https://example.com/openrpc.json",
		[]string{"#", "#"},
	)
	if err != nil {
		t.Fatalf("exact input fan-out failed: %v", err)
	}
	_, err = resolver.ResolveMany(
		context.Background(), root, "https://example.com/openrpc.json",
		[]string{"#/first"},
	)
	if err != nil {
		t.Fatalf("exact alias fan-out failed: %v", err)
	}
}

func TestResolverDisablesExternalAccessByDefault(t *testing.T) {
	t.Parallel()

	store := &recordingStore{documents: map[string][]byte{}}
	resolver, err := reference.NewResolver(store, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(
		context.Background(),
		parseValue(t, `{}`),
		"https://example.com/openrpc.json",
		"https://other.example/schema.json",
	)
	if !errors.Is(err, reference.ErrExternalDisabled) {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(store.loads) != 0 {
		t.Fatalf("external store was called: %#v", store.loads)
	}
}

func TestResolverRejectsCyclesAndBounds(t *testing.T) {
	t.Parallel()

	root := parseValue(t, `{"a":{"$ref":"#/b"},"b":{"$ref":"#/a"}}`)
	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), root, "https://example.com/openrpc.json", "#/a"); !errors.Is(err, reference.ErrReferenceCycle) {
		t.Fatalf("cycle error = %v", err)
	}

	policy := reference.DefaultResolvePolicy()
	policy.MaxDepth = 1
	resolver, err = reference.NewResolver(nil, policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), root, "https://example.com/openrpc.json", "#/a"); !errors.Is(err, reference.ErrResolveLimit) {
		t.Fatalf("depth error = %v", err)
	}
}

func TestResolverEnforcesExternalSchemeHostBytesAndCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		ref    string
		change func(*reference.ResolvePolicy)
		want   error
	}{
		{name: "scheme", ref: "http://example.com/schema.json", want: reference.ErrSchemeNotAllowed},
		{name: "host", ref: "https://other.example/schema.json", want: reference.ErrHostNotAllowed},
		{name: "bytes", ref: "https://example.com/schema.json", change: func(policy *reference.ResolvePolicy) { policy.MaxFetchedBytes = 1 }, want: reference.ErrResolveLimit},
	}
	for _, test := range tests {
		policy := reference.DefaultResolvePolicy()
		policy.AllowExternal = true
		policy.AllowedSchemes = []string{"https"}
		policy.AllowedHosts = []string{"example.com"}
		if test.change != nil {
			test.change(&policy)
		}
		store := &recordingStore{documents: map[string][]byte{
			"https://example.com/schema.json": []byte(`{"value":true}`),
		}}
		resolver, err := reference.NewResolver(store, policy)
		if err != nil {
			t.Fatal(err)
		}
		_, err = resolver.Resolve(context.Background(), parseValue(t, `{}`), "https://example.com/openrpc.json", test.ref)
		if !errors.Is(err, test.want) {
			t.Errorf("%s error = %v", test.name, err)
		}
	}

	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = resolver.Resolve(ctx, parseValue(t, `{}`), "https://example.com/openrpc.json", "#")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestResolverRejectsInvalidPolicy(t *testing.T) {
	t.Parallel()

	if _, err := reference.NewResolver(nil, reference.ResolvePolicy{}); !errors.Is(err, reference.ErrResolvePolicy) {
		t.Fatalf("NewResolver error = %v", err)
	}
}

func TestResolverRejectsInvalidUTF8Base(t *testing.T) {
	t.Parallel()

	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	invalid := string([]byte{'h', 't', 't', 'p', 's', ':', '/', '/', 0xff})
	_, err = resolver.Resolve(context.Background(), parseValue(t, `{}`), invalid, "#")
	if !errors.Is(err, reference.ErrInvalidBase) {
		t.Fatalf("Resolve error = %v", err)
	}
}

func parseValue(t *testing.T, input string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Parse([]byte(input), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return value
}
