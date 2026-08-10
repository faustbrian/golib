package schemaregistry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMutationBoundaryContracts(t *testing.T) {
	canonicalizer := canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) {
		return definition.Content, nil
	})
	base := internalSchema(t, FormatAvro, "x", nil)
	provenance := Provenance{Source: "test", Revision: "1"}
	validGraph := GraphLimits{MaxSchemas: 1, MaxDepth: 1, MaxReferences: 1}
	for _, limits := range []GraphLimits{
		{MaxDepth: 1, MaxReferences: 1},
		{MaxSchemas: 1, MaxReferences: 1},
		{MaxSchemas: 1, MaxDepth: 1},
	} {
		if _, err := NewBundle(base, nil, limits, provenance); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("NewBundle(%+v) error = %v", limits, err)
		}
	}
	bundle, err := NewBundle(base, nil, validGraph, provenance)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := bundle.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBundle(context.Background(), encoded, map[Format]Canonicalizer{FormatAvro: canonicalizer}, validGraph, len(encoded)); err != nil {
		t.Fatalf("LoadBundle(exact byte limit) error = %v", err)
	}

	clock := &manualClock{now: time.Unix(100, 0)}
	resolver := resolverFunction(func(_ context.Context, lookup Lookup) (ResolveResult, error) {
		return ResolveResult{Schema: base, ID: lookup.ProviderID(), Lifecycle: LifecycleAvailable}, nil
	})
	validCache := validCacheConfig(clock)
	for _, mutate := range []func(*ResolveCacheConfig){
		func(config *ResolveCacheConfig) { config.MaxEntries = 0 },
		func(config *ResolveCacheConfig) { config.MaxConcurrent = 0 },
		func(config *ResolveCacheConfig) { config.FreshFor = 0 },
		func(config *ResolveCacheConfig) { config.NegativeFor = 0 },
	} {
		config := validCache
		mutate(&config)
		if _, err := NewResolveCache(resolver, config); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("NewResolveCache(%+v) error = %v", config, err)
		}
	}
	zeroStale := validCache
	zeroStale.StaleFor = 0
	if _, err := NewResolveCache(resolver, zeroStale); err != nil {
		t.Fatalf("NewResolveCache(zero stale) error = %v", err)
	}
	lruConfig := validCache
	lruConfig.MaxEntries = 2
	cache, err := NewResolveCache(resolver, lruConfig)
	if err != nil {
		t.Fatal(err)
	}
	first := ByProviderID(ProviderID{Provider: "test", Value: "1"})
	second := ByProviderID(ProviderID{Provider: "test", Value: "2"})
	third := ByProviderID(ProviderID{Provider: "test", Value: "3"})
	for _, lookup := range []Lookup{first, second, first, third} {
		if _, err := cache.Resolve(context.Background(), lookup, FailClosed); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cache.Resolve(context.Background(), first, CacheOnly); err != nil {
		t.Fatalf("recent entry evicted: %v", err)
	}
	if _, err := cache.Resolve(context.Background(), second, CacheOnly); !errors.Is(err, ErrOfflineMiss) {
		t.Fatalf("old entry retained: %v", err)
	}

	provider := &adminProvider{basicProvider: &basicProvider{capabilities: Capabilities{
		Provider: "test", Formats: []Format{FormatAvro},
		CompatibilityModes: []CompatibilityMode{CompatibilityBackward}, BoundedListing: true,
	}}}
	for _, limits := range []Limits{{MaxListResults: 1, MaxConcurrent: 1}, {MaxSchemaBytes: 1, MaxConcurrent: 1}, {MaxSchemaBytes: 1, MaxListResults: 1}} {
		if _, err := NewClient(provider, limits); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("NewClient(%+v) error = %v", limits, err)
		}
	}
	provider.register = func(context.Context, RegisterRequest) (RegisterResult, error) {
		return RegisterResult{Outcome: RegistrationExisting, ID: ProviderID{Provider: "test", Value: "1"}}, nil
	}
	provider.list = func(context.Context, ListRequest) (ListPage, error) {
		return ListPage{Schemas: make([]SchemaDescriptor, 2)}, nil
	}
	provider.compatibility = func(context.Context, CompatibilityRequest) (CompatibilityResult, error) {
		return CompatibilityResult{Supported: true, Compatible: true}, nil
	}
	client, err := NewClient(provider, Limits{MaxSchemaBytes: 1, MaxListResults: 2, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Register(context.Background(), RegisterRequest{Subject: Subject{Name: "s"}, Schema: base}); err != nil {
		t.Fatalf("Register(exact schema limit) error = %v", err)
	}
	if result, err := client.CheckCompatibility(context.Background(), CompatibilityRequest{
		Subject: Subject{Name: "s"}, Candidate: base, Mode: CompatibilityBackward,
	}); err != nil || !result.Supported || !result.Compatible {
		t.Fatalf("CheckCompatibility(exact schema limit) = (%+v, %v)", result, err)
	}
	if page, err := client.List(context.Background(), ListRequest{Limit: 2}); err != nil || len(page.Schemas) != 2 {
		t.Fatalf("List(exact limits) = (%+v, %v)", page, err)
	}

	codec := &codecFunctions{
		encode: func(context.Context, Schema, any) ([]byte, error) { return []byte("x"), nil },
		decode: func(context.Context, Schema, []byte, any) error { return nil },
	}
	framer := &framerFunctions{
		frame: func(_ context.Context, _ ProviderID, payload []byte) ([]byte, error) {
			return append([]byte(nil), payload...), nil
		},
		unframe: func(_ context.Context, frame []byte) (ProviderID, []byte, error) {
			return ProviderID{Provider: "test", Value: "1"}, append([]byte(nil), frame...), nil
		},
	}
	for _, limits := range []CodecLimits{{MaxFrameBytes: 1}, {MaxPayloadBytes: 1}} {
		if _, err := NewCodecIntegration(codec, framer, limits); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("NewCodecIntegration(%+v) error = %v", limits, err)
		}
	}
	integration, err := NewCodecIntegration(codec, framer, CodecLimits{MaxPayloadBytes: 1, MaxFrameBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	id := ProviderID{Provider: "test", Value: "1"}
	if _, err := integration.Encode(context.Background(), base, id, nil); err != nil {
		t.Fatalf("Encode(exact limits) error = %v", err)
	}
	if _, err := integration.Parse(context.Background(), []byte("x")); err != nil {
		t.Fatalf("Parse(exact limits) error = %v", err)
	}
	if err := integration.Decode(context.Background(), base, WireMessage{ID: id, Payload: []byte("x")}, nil); err != nil {
		t.Fatalf("Decode(exact limits) error = %v", err)
	}

	coordinate := ReferenceCoordinate{Subject: Subject{Name: "s"}, Version: Version{Number: 1}}
	referenceResolver := referenceResolverFunction(func(context.Context, ReferenceCoordinate) (ReferenceDocument, error) {
		return ReferenceDocument{Coordinate: coordinate}, nil
	})
	for _, limits := range []GraphLimits{{MaxDepth: 1, MaxReferences: 1}, {MaxSchemas: 1, MaxReferences: 1}, {MaxSchemas: 1, MaxDepth: 1}} {
		if _, err := BuildReferenceGraph(context.Background(), []ReferenceCoordinate{coordinate}, referenceResolver, limits); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("BuildReferenceGraph(%+v) error = %v", limits, err)
		}
	}
	if _, err := BuildReferenceGraph(context.Background(), []ReferenceCoordinate{coordinate}, referenceResolver, validGraph); err != nil {
		t.Fatalf("BuildReferenceGraph(exact limits) error = %v", err)
	}

	referenced := internalSchema(t, FormatAvro, "r", nil)
	definition := Definition{
		Format: FormatAvro, Content: []byte("x"), Metadata: map[string]string{"k": "v"},
		References: []Reference{{Name: "r", Fingerprint: referenced.Fingerprint()}},
	}
	validCompile := CompileLimits{MaxSchemaBytes: 1, MaxCanonicalBytes: 1, MaxReferences: 1, MaxMetadata: 1}
	for _, mutate := range []func(*CompileLimits){
		func(limits *CompileLimits) { limits.MaxSchemaBytes = 0 },
		func(limits *CompileLimits) { limits.MaxCanonicalBytes = 0 },
		func(limits *CompileLimits) { limits.MaxReferences = 0 },
		func(limits *CompileLimits) { limits.MaxMetadata = 0 },
	} {
		limits := validCompile
		mutate(&limits)
		if _, err := CompileWithLimits(context.Background(), definition, canonicalizer, limits); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("CompileWithLimits(%+v) error = %v", limits, err)
		}
	}
	if _, err := CompileWithLimits(context.Background(), definition, canonicalizer, validCompile); err != nil {
		t.Fatalf("CompileWithLimits(exact limits) error = %v", err)
	}
}
