package reference

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestResolverCoversInputErrorsCancellationAndLoadFailures(t *testing.T) {
	t.Parallel()

	root := referenceValue(t, `{"value":true,"bad":{"$ref":"\n"},"alias":{"$ref":"#"}}`)
	resolver, err := NewResolver(nil, DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (*Resolver)(nil).ResolveMany(context.Background(), root, "https://example.com/root", nil); !errors.Is(err, ErrResolvePolicy) {
		t.Fatalf("nil resolver error = %v", err)
	}
	if _, err := resolver.ResolveMany(explicitNilContext(), root, "https://example.com/root", nil); !errors.Is(err, ErrResolvePolicy) {
		t.Fatalf("nil resolve context error = %v", err)
	}
	if _, err := resolver.ResolveMany(context.Background(), root, "relative", nil); !errors.Is(err, ErrInvalidBase) {
		t.Fatalf("invalid base error = %v", err)
	}
	if _, err := resolver.ResolveMany(context.Background(), root, "https://example.com/root", []string{"%zz"}); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("invalid input error = %v", err)
	}
	if _, err := resolver.Resolve(context.Background(), root, "https://example.com/root", "#/missing"); !errors.Is(err, ErrPointerTarget) {
		t.Fatalf("missing target error = %v", err)
	}
	if _, err := resolver.Resolve(context.Background(), root, "https://example.com/root", "#plain"); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("plain fragment error = %v", err)
	}
	if _, err := resolver.Resolve(context.Background(), root, "https://example.com/root", "#/bad"); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("invalid alias error = %v", err)
	}
	invalidAlias := referenceValue(t, `{"bad":{"$ref":1}}`)
	if _, err := resolver.Resolve(context.Background(), invalidAlias, "https://example.com/root", "#/bad"); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("non-string alias error = %v", err)
	}
	ctx := &referenceCountingContext{Context: context.Background(), remaining: 1}
	if _, err := resolver.Resolve(ctx, root, "https://example.com/root", "#/value"); !errors.Is(err, context.Canceled) {
		t.Fatalf("resolve loop cancellation error = %v", err)
	}
	shortPolicy := DefaultResolvePolicy()
	shortPolicy.Reference.MaxLength = 2
	shortResolver, err := NewResolver(nil, shortPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shortResolver.Resolve(context.Background(), root, "https://example.com/root", "#"); !errors.Is(err, ErrReferenceLimit) {
		t.Fatalf("resolved input length error = %v", err)
	}
	current, err := Parse("https://example.com/root#/alias", DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	state := &resolveState{
		documents: map[string]jsonvalue.Value{"https://example.com/root": root},
		visited:   make(map[string]struct{}),
	}
	if _, err := shortResolver.resolve(context.Background(), current, state); !errors.Is(err, ErrReferenceLimit) {
		t.Fatalf("resolved alias length error = %v", err)
	}

	policy := externalResolvePolicy()
	stores := []struct {
		name  string
		store Store
		want  error
	}{
		{name: "missing store", store: nil, want: ErrLoadFailed},
		{name: "store limit", store: StoreFunc(func(context.Context, string, int) ([]byte, error) { return nil, ErrStoreLimit }), want: ErrResolveLimit},
		{name: "store failure", store: StoreFunc(func(context.Context, string, int) ([]byte, error) { return nil, errors.New("secret") }), want: ErrLoadFailed},
		{name: "oversized", store: StoreFunc(func(_ context.Context, _ string, maximum int) ([]byte, error) { return make([]byte, maximum+1), nil }), want: ErrResolveLimit},
		{name: "invalid JSON", store: StoreFunc(func(context.Context, string, int) ([]byte, error) { return []byte("{"), nil }), want: ErrInvalidDocument},
	}
	for _, test := range stores {
		resolver, err := NewResolver(test.store, policy)
		if err != nil {
			t.Fatal(err)
		}
		_, err = resolver.Resolve(context.Background(), root, "https://example.com/root", "child.json")
		if !errors.Is(err, test.want) {
			t.Errorf("%s error = %v, want %v", test.name, err, test.want)
		}
	}

	resolver, err = NewResolver(StoreFunc(func(ctx context.Context, _ string, _ int) ([]byte, error) {
		if cancelling, ok := ctx.(*referenceCountingContext); ok {
			cancelling.remaining = -1
		}
		return nil, errors.New("canceled")
	}), policy)
	if err != nil {
		t.Fatal(err)
	}
	ctx = &referenceCountingContext{Context: context.Background(), remaining: 10}
	if _, err := resolver.Resolve(ctx, root, "https://example.com/root", "child.json"); !errors.Is(err, context.Canceled) {
		t.Fatalf("load cancellation error = %v", err)
	}
}

func TestLoadCoversDirectPolicyBranches(t *testing.T) {
	t.Parallel()

	policy := externalResolvePolicy()
	resolver, err := NewResolver(StoreFunc(func(context.Context, string, int) ([]byte, error) { return []byte(`{}`), nil }), policy)
	if err != nil {
		t.Fatal(err)
	}
	state := &resolveState{}
	for _, test := range []struct {
		uri   string
		state resolveState
		want  error
	}{
		{uri: "://", want: ErrInvalidReference},
		{uri: "http://example.com/a", want: ErrSchemeNotAllowed},
		{uri: "https://other.example/a", want: ErrHostNotAllowed},
		{uri: "https://example.com/a", state: resolveState{fetchedDocs: policy.MaxDocuments}, want: ErrResolveLimit},
		{uri: "https://example.com/a", state: resolveState{fetchedBytes: policy.MaxFetchedBytes}, want: ErrResolveLimit},
	} {
		state = &test.state
		if _, err := resolver.load(context.Background(), test.uri, state); !errors.Is(err, test.want) {
			t.Errorf("load(%q) error = %v, want %v", test.uri, err, test.want)
		}
	}
	loadCalls := 0
	exactResolver, err := NewResolver(StoreFunc(func(_ context.Context, _ string, maximum int) ([]byte, error) {
		loadCalls++
		if maximum == 0 {
			return nil, nil
		}
		return []byte(`{}`), nil
	}), policy)
	if err != nil {
		t.Fatal(err)
	}
	zeroRemaining := &resolveState{fetchedBytes: policy.MaxFetchedBytes}
	if _, err := exactResolver.load(context.Background(), "https://example.com/a", zeroRemaining); !errors.Is(err, ErrResolveLimit) || loadCalls != 0 {
		t.Fatalf("zero remaining result = calls %d, error %v", loadCalls, err)
	}
	exactResolver.policy.MaxFetchedBytes = 2
	exactState := &resolveState{}
	if _, err := exactResolver.load(context.Background(), "https://example.com/a", exactState); err != nil {
		t.Fatalf("exact fetched byte limit failed: %v", err)
	}
	if exactState.fetchedBytes != 2 || exactState.fetchedDocs != 1 {
		t.Fatalf("fetched counters = bytes %d, docs %d", exactState.fetchedBytes, exactState.fetchedDocs)
	}
}

func TestResourcesCoversQueueValidationAndNestedErrors(t *testing.T) {
	t.Parallel()

	root := referenceValue(t, `{}`)
	resolver, err := NewResolver(nil, DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (*Resolver)(nil).Resources(context.Background(), root, "https://example.com/root", nil); !errors.Is(err, ErrResolvePolicy) {
		t.Fatalf("nil resources resolver error = %v", err)
	}
	if _, err := resolver.Resources(explicitNilContext(), root, "https://example.com/root", nil); !errors.Is(err, ErrResolvePolicy) {
		t.Fatalf("nil resources context error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolver.Resources(canceled, root, "https://example.com/root", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("initial resources cancellation error = %v", err)
	}
	if _, err := resolver.Resources(context.Background(), root, "relative", nil); !errors.Is(err, ErrInvalidBase) {
		t.Fatalf("resources base error = %v", err)
	}
	if _, err := resolver.Resources(context.Background(), root, "https://example.com/root", []string{"%"}); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("resource queue reference error = %v", err)
	}
	inputLimitPolicy := DefaultResolvePolicy()
	inputLimitPolicy.MaxReferences = 1
	inputLimitResolver, err := NewResolver(nil, inputLimitPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inputLimitResolver.Resources(context.Background(), root, "https://example.com/root", []string{"#", "#"}); !errors.Is(err, ErrResolveLimit) {
		t.Fatalf("resource input fan-out error = %v", err)
	}
	if _, err := inputLimitResolver.Resources(context.Background(), root, "https://example.com/root", []string{"#"}); err != nil {
		t.Fatalf("exact resource input fan-out failed: %v", err)
	}
	if resources, err := resolver.Resources(context.Background(), root, "https://example.com/root", []string{"#"}); err != nil || len(resources) != 0 {
		t.Fatalf("root resource result = %v, %v", resources, err)
	}
	policy := externalResolvePolicy()
	policy.MaxDepth = 1
	store := StoreFunc(func(_ context.Context, uri string, _ int) ([]byte, error) {
		switch uri {
		case "https://example.com/child.json":
			return []byte(`{"$ref":"grandchild.json"}`), nil
		default:
			return []byte(`{}`), nil
		}
	})
	resolver, err = NewResolver(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resources(context.Background(), root, "https://example.com/root", []string{"child.json"}); !errors.Is(err, ErrResolveLimit) {
		t.Fatalf("resource depth error = %v", err)
	}
	queueCtx := &referenceCountingContext{Context: context.Background(), remaining: 2}
	if _, err := resolver.Resources(queueCtx, root, "https://example.com/root", []string{"child.json"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("resource queue cancellation error = %v", err)
	}
	invalidDocumentPolicy := externalResolvePolicy()
	invalidDocumentPolicy.MaxDepth = 1
	invalidDocumentResolver, err := NewResolver(StoreFunc(func(context.Context, string, int) ([]byte, error) {
		return []byte(`{"a":{"b":true}}`), nil
	}), invalidDocumentPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invalidDocumentResolver.Resources(context.Background(), root, "https://example.com/root", []string{"child.json"}); !errors.Is(err, ErrResolveLimit) {
		t.Fatalf("loaded resource depth error = %v", err)
	}
	invalidReferenceResolver, err := NewResolver(StoreFunc(func(context.Context, string, int) ([]byte, error) {
		return []byte(`{"$id":"\n"}`), nil
	}), externalResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invalidReferenceResolver.Resources(context.Background(), root, "https://example.com/root", []string{"child.json"}); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("invalid loaded resource error = %v", err)
	}
	if _, _, err := resourceReferences(jsonvalue.Value{}, "https://example.com/root", 1, 1); err == nil {
		t.Fatal("zero resource value succeeded")
	}
	var references []locatedReference
	var aliases []string
	if err := walkResourceReferences(map[string]any{"$id": "\n"}, "https://example.com/root", 0, 2, 1, &references, &aliases); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("invalid nested identifier error = %v", err)
	}
	if _, _, err := resourceReferences(referenceValue(t, `{"$id":"\n"}`), "https://example.com/root", 2, 1); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("resource walker error = %v", err)
	}
	ordered := referenceValue(t, `{"z":{"$id":"z/","$ref":"b"},"b":{"$ref":"z"},"a":{"$ref":"a"}}`)
	references, _, err = resourceReferences(ordered, "https://example.com/root", 8, 3)
	if err != nil || len(references) != 3 || references[0].ref != "a" || references[1].ref != "z" || references[2].base != "https://example.com/z/" {
		t.Fatalf("ordered resource references = %#v, %v", references, err)
	}
	if err := walkResourceReferences(map[string]any{"a": map[string]any{}}, "https://example.com/root", 0, 0, 4, &references, &aliases); !errors.Is(err, ErrResolveLimit) {
		t.Fatalf("object depth error = %v", err)
	}
	if err := walkResourceReferences([]any{map[string]any{}}, "https://example.com/root", 0, 0, 4, &references, &aliases); !errors.Is(err, ErrResolveLimit) {
		t.Fatalf("array depth error = %v", err)
	}
	if _, err := resolveResourceReference("relative", "child"); !errors.Is(err, ErrInvalidBase) {
		t.Fatalf("resource base error = %v", err)
	}
	if _, err := resolveResourceReference("https://example.com/root", "\n"); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("resource reference error = %v", err)
	}
	if _, err := resolveResourceReference("https://example.com/root", "%"); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("resource parse error = %v", err)
	}
}

func TestTransformCoversInvalidReferencesDepthAndInternalHelpers(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(nil, DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Dereference(context.Background(), resolver, referenceValue(t, `{}`), "relative", DefaultTransformPolicy()); !errors.Is(err, ErrInvalidBase) {
		t.Fatalf("transform base error = %v", err)
	}
	if _, err := Dereference(context.Background(), resolver, jsonvalue.Value{}, "https://example.com/root", DefaultTransformPolicy()); !errors.Is(err, ErrTransformInput) {
		t.Fatalf("zero transform input error = %v", err)
	}
	for _, source := range []string{`{"$ref":1}`, `{"$ref":""}`} {
		if _, err := Dereference(context.Background(), resolver, referenceValue(t, source), "https://example.com/root", DefaultTransformPolicy()); !errors.Is(err, ErrInvalidReference) {
			t.Errorf("transform %s error = %v", source, err)
		}
	}
	policy := DefaultTransformPolicy()
	policy.MaxDepth = 1
	if _, err := Dereference(context.Background(), resolver, referenceValue(t, `{"a":{"b":true}}`), "https://example.com/root", policy); !errors.Is(err, ErrTransformLimit) {
		t.Fatalf("transform depth error = %v", err)
	}
	if result, err := Dereference(context.Background(), resolver, referenceValue(t, `[1,2]`), "https://example.com/root", DefaultTransformPolicy()); err != nil || string(result.Bytes()) != `[1,2]` {
		t.Fatalf("array transform = %s, %v", result.Bytes(), err)
	}
	if _, err := Dereference(context.Background(), resolver, referenceValue(t, `[[true]]`), "https://example.com/root", policy); !errors.Is(err, ErrTransformLimit) {
		t.Fatalf("array transform depth error = %v", err)
	}
	tokenPolicy := DefaultTransformPolicy()
	tokenPolicy.MaxOutputTokens = 1
	if _, err := Dereference(context.Background(), resolver, referenceValue(t, `{}`), "https://example.com/root", tokenPolicy); !errors.Is(err, ErrTransformLimit) {
		t.Fatalf("transform token error = %v", err)
	}
	ctx := &referenceCountingContext{Context: context.Background(), remaining: 2}
	if _, err := Dereference(ctx, resolver, referenceValue(t, `{"a":true}`), "https://example.com/root", DefaultTransformPolicy()); !errors.Is(err, context.Canceled) {
		t.Fatalf("transform walk cancellation error = %v", err)
	}
	if _, err := decodeTransformValue([]byte(`{`)); err == nil {
		t.Fatal("malformed transform JSON succeeded")
	}
	walk := transformWalk{
		ctx:      context.Background(),
		resolver: resolver,
		policy:   DefaultTransformPolicy(),
		state:    &resolveState{documents: map[string]jsonvalue.Value{"https://example.com/root": referenceValue(t, `{"loop":{"$ref":"#/loop"}}`)}},
		active:   make(map[string]struct{}),
	}
	if _, err := walk.reference("\n", "https://example.com/root", 0, nil); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("transform reference parse error = %v", err)
	}
	shortPolicy := DefaultResolvePolicy()
	shortPolicy.Reference.MaxLength = 2
	shortResolver, err := NewResolver(nil, shortPolicy)
	if err != nil {
		t.Fatal(err)
	}
	walk.resolver = shortResolver
	if _, err := walk.reference("#", "https://example.com/root", 0, nil); !errors.Is(err, ErrReferenceLimit) {
		t.Fatalf("transform resolved reference error = %v", err)
	}
	walk.resolver = resolver
	if _, err := walk.reference("#/loop", "https://example.com/root", 0, nil); !errors.Is(err, ErrDereferenceCycle) {
		t.Fatalf("transform resolver cycle error = %v", err)
	}
	if _, err := walk.reference("#/missing", "https://example.com/root", 0, nil); !errors.Is(err, ErrPointerTarget) {
		t.Fatalf("transform resolver error = %v", err)
	}
	existing := &TransformError{Pointer: "#", Err: ErrTransformLimit}
	if !errors.Is(transformError(nil, existing), existing) {
		t.Fatal("transformError wrapped an existing transform error")
	}
}

func TestTransformMutationBoundaries(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(nil, DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	root := referenceValue(t, `{}`)
	policy := DefaultTransformPolicy()
	policy.MaxOutputBytes = len(root.Bytes())
	if _, err := Dereference(context.Background(), resolver, root, "https://example.com/root", policy); err != nil {
		t.Fatalf("exact output byte limit failed: %v", err)
	}
	policy.MaxOutputBytes--
	if _, err := Dereference(context.Background(), resolver, root, "https://example.com/root", policy); !errors.Is(err, ErrTransformLimit) {
		t.Fatalf("exceeded output byte limit error = %v", err)
	}

	walk := transformWalk{
		ctx:      context.Background(),
		resolver: resolver,
		policy: TransformPolicy{
			MaxDepth:        1,
			MaxReferences:   1,
			MaxOutputBytes:  100,
			MaxOutputTokens: 100,
		},
		state: &resolveState{documents: map[string]jsonvalue.Value{
			"https://example.com/root": referenceValue(t, `{"target":{"child":true}}`),
		}},
		active: make(map[string]struct{}),
	}
	for name, value := range map[string]any{
		"object": map[string]any{"a": map[string]any{"b": true}},
		"array":  []any{[]any{true}},
	} {
		if _, err := walk.value(value, "https://example.com/root", 0, nil); !errors.Is(err, ErrTransformLimit) {
			t.Errorf("%s depth error = %v", name, err)
		}
	}
	walk.references = 0
	if _, err := walk.reference("#/target/child", "https://example.com/root", 0, nil); err != nil {
		t.Fatalf("reference at exact count failed: %v", err)
	}
	if _, err := walk.reference("#/target/child", "https://example.com/root", 0, nil); !errors.Is(err, ErrTransformLimit) {
		t.Fatalf("reference above exact count error = %v", err)
	}
	walk.references = 0
	if _, err := walk.reference("#/target", "https://example.com/root", 0, nil); !errors.Is(err, ErrTransformLimit) {
		t.Fatalf("referenced target depth error = %v", err)
	}

	valid := DefaultTransformPolicy()
	for name, mutate := range map[string]func(*TransformPolicy){
		"depth":      func(policy *TransformPolicy) { policy.MaxDepth = 0 },
		"references": func(policy *TransformPolicy) { policy.MaxReferences = 0 },
		"bytes":      func(policy *TransformPolicy) { policy.MaxOutputBytes = 0 },
		"tokens":     func(policy *TransformPolicy) { policy.MaxOutputTokens = 0 },
	} {
		candidate := valid
		mutate(&candidate)
		if validTransformPolicy(candidate) {
			t.Errorf("policy with zero %s is valid", name)
		}
	}
	if transformPointer(nil) != "#" || transformPointer([]string{"a"}) != "#/a" {
		t.Fatal("transform pointer root boundary failed")
	}
}

func TestReferencePolicyMutationBoundaries(t *testing.T) {
	t.Parallel()

	if Internal != 1 || ExternalRelative != 2 || ExternalAbsolute != 3 {
		t.Fatal("reference kind values changed")
	}
	store, err := NewMemoryStore(map[string][]byte{"https://example.com/a": []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), "https://example.com/a", 0); !errors.Is(err, ErrStorePolicy) {
		t.Fatalf("memory store zero byte limit error = %v", err)
	}
	filesystem, err := NewFSStore(fstest.MapFS{"a": {Data: []byte(`{}`)}}, "https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := filesystem.Load(context.Background(), "https://example.com/a", 0); !errors.Is(err, ErrStorePolicy) {
		t.Fatalf("filesystem store zero byte limit error = %v", err)
	}

	root := referenceValue(t, `{"value":true,"alias":{"$ref":"#/value"}}`)
	resolvePolicy := DefaultResolvePolicy()
	resolvePolicy.MaxDepth = 1
	resolver, err := NewResolver(nil, resolvePolicy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), root, "https://example.com/root", "#/value"); err != nil {
		t.Fatalf("resolution at exact depth failed: %v", err)
	}
	if _, err := resolver.Resolve(context.Background(), root, "https://example.com/root", "#/alias"); !errors.Is(err, ErrResolveLimit) {
		t.Fatalf("resolution above exact depth error = %v", err)
	}

	valid := DefaultResolvePolicy()
	for name, mutate := range map[string]func(*ResolvePolicy){
		"depth":          func(policy *ResolvePolicy) { policy.MaxDepth = 0 },
		"documents":      func(policy *ResolvePolicy) { policy.MaxDocuments = 0 },
		"fetched bytes":  func(policy *ResolvePolicy) { policy.MaxFetchedBytes = 0 },
		"references":     func(policy *ResolvePolicy) { policy.MaxReferences = 0 },
		"reference":      func(policy *ResolvePolicy) { policy.Reference.MaxLength = 0 },
		"pointer length": func(policy *ResolvePolicy) { policy.Pointer.MaxLength = 0 },
		"pointer tokens": func(policy *ResolvePolicy) { policy.Pointer.MaxTokens = 0 },
		"pointer digits": func(policy *ResolvePolicy) { policy.Pointer.MaxIndexDigits = 0 },
		"JSON bytes":     func(policy *ResolvePolicy) { policy.JSON.MaxBytes = 0 },
		"JSON depth":     func(policy *ResolvePolicy) { policy.JSON.MaxDepth = 0 },
		"JSON tokens":    func(policy *ResolvePolicy) { policy.JSON.MaxTokens = 0 },
	} {
		candidate := valid
		mutate(&candidate)
		if validResolvePolicy(candidate) {
			t.Errorf("policy with zero %s is valid", name)
		}
	}
}

func externalResolvePolicy() ResolvePolicy {
	policy := DefaultResolvePolicy()
	policy.AllowExternal = true
	policy.AllowedSchemes = []string{"https"}
	policy.AllowedHosts = []string{"example.com"}
	return policy
}

func explicitNilContext() context.Context { return nil }
