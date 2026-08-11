package schemaregistry

import (
	"context"
	"encoding/json"
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
		Format: FormatAvro, Content: []byte("xx"), Metadata: map[string]string{"k": "v"},
		References: []Reference{{Name: "r", Fingerprint: referenced.Fingerprint()}},
	}
	validCompile := CompileLimits{MaxSchemaBytes: 2, MaxCanonicalBytes: 2, MaxReferences: 1, MaxMetadata: 1}
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

func TestLogicalMutationBoundaryContracts(t *testing.T) {
	canonicalizer := canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) {
		return definition.Content, nil
	})
	base := internalSchema(t, FormatAvro, "x", nil)
	limits := GraphLimits{MaxSchemas: 1, MaxDepth: 1, MaxReferences: 1}
	validProvenance := Provenance{Source: "test", Revision: "1"}

	for _, provenance := range []Provenance{
		{Revision: "1"},
		{Source: "test"},
	} {
		if _, err := NewBundle(base, nil, limits, provenance); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("NewBundle(provenance %+v) error = %v", provenance, err)
		}
	}
	for name, bundle := range map[string]Bundle{
		"missing root":  {schemas: map[Fingerprint]Schema{base.Fingerprint(): base}},
		"empty schemas": {root: base.Fingerprint(), schemas: map[Fingerprint]Schema{}},
	} {
		if _, err := bundle.MarshalBinary(); !errors.Is(err, ErrInvalidSchema) {
			t.Fatalf("MarshalBinary(%s) error = %v", name, err)
		}
	}
	validBundle, err := NewBundle(base, nil, limits, validProvenance)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := validBundle.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var wire bundleWire
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	invalidWires := map[string]bundleWire{
		"version":  wire,
		"empty":    wire,
		"too many": wire,
	}
	versionWire := invalidWires["version"]
	versionWire.Version = 2
	invalidWires["version"] = versionWire
	emptyWire := invalidWires["empty"]
	emptyWire.Schemas = nil
	invalidWires["empty"] = emptyWire
	tooManyWire := invalidWires["too many"]
	tooManyWire.Schemas = append(append([]bundleSchemaWire(nil), wire.Schemas...), wire.Schemas[0])
	invalidWires["too many"] = tooManyWire
	for name, invalid := range invalidWires {
		invalidEncoded, marshalErr := json.Marshal(invalid)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, loadErr := LoadBundle(context.Background(), invalidEncoded, map[Format]Canonicalizer{FormatAvro: canonicalizer}, limits, len(invalidEncoded)); !errors.Is(loadErr, ErrInvalidSchema) {
			t.Fatalf("LoadBundle(%s) error = %v", name, loadErr)
		}
	}

	clock := &manualClock{now: time.Unix(100, 0)}
	lookup := ByProviderID(ProviderID{Provider: "test", Value: "1"})
	cache, err := NewResolveCache(resolverFunction(func(_ context.Context, lookup Lookup) (ResolveResult, error) {
		return validCachedResult(base, lookup.ProviderID()), nil
	}), validCacheConfig(clock))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Resolve(context.Background(), lookup, FailClosed); err != nil {
		t.Fatal(err)
	}
	clock.advance(1500 * time.Millisecond)
	stale, err := cache.Resolve(context.Background(), lookup, CacheOnly)
	if err != nil || stale.State != CacheStale {
		t.Fatalf("Resolve(stale cache only) = (%+v, %v)", stale, err)
	}
	clock.advance(1500 * time.Millisecond)
	if _, err := cache.Resolve(context.Background(), lookup, CacheOnly); !errors.Is(err, ErrOfflineMiss) {
		t.Fatalf("Resolve(expired cache only) error = %v", err)
	}

	subject := Subject{Name: "subject"}
	version := Version{Number: 1}
	validResult := ResolveResult{
		Schema: base, ID: ProviderID{Provider: "test", Value: "1"},
		Subject: subject, Version: version, Lifecycle: LifecycleAvailable,
	}
	for name, test := range map[string]struct {
		lookup Lookup
		result ResolveResult
	}{
		"version subject": {AtVersion(subject, version), withSubject(validResult, Subject{Name: "other"})},
		"version number":  {AtVersion(subject, version), withVersion(validResult, Version{Number: 2})},
		"latest subject":  {Latest(subject), withSubject(validResult, Subject{Name: "other"})},
		"latest version":  {Latest(subject), withVersion(validResult, Version{})},
	} {
		if err := validateResolution(test.lookup, test.result); !errors.Is(err, ErrResolutionMismatch) {
			t.Fatalf("validateResolution(%s) error = %v", name, err)
		}
	}

	provider := &basicProvider{capabilities: Capabilities{
		Provider: "test", Formats: []Format{FormatAvro},
		CompatibilityModes: []CompatibilityMode{CompatibilityBackward},
	}}
	provider.compatibility = func(context.Context, CompatibilityRequest) (CompatibilityResult, error) {
		t.Fatal("provider called for incomplete compatibility request")
		return CompatibilityResult{}, nil
	}
	client, err := NewClient(provider, validClientLimits())
	if err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string]CompatibilityRequest{
		"subject": {Candidate: base, Mode: CompatibilityBackward},
		"content": {Subject: subject, Mode: CompatibilityBackward},
	} {
		if _, err := client.CheckCompatibility(context.Background(), request); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("CheckCompatibility(missing %s) error = %v", name, err)
		}
	}

	var nilAdmin *adminProvider
	invalidAdminClient := &Client{
		provider:     nilAdmin,
		capabilities: Capabilities{Provider: "test", BoundedListing: true, SoftDelete: true},
		limits:       validClientLimits(), slots: make(chan struct{}, 1),
	}
	if _, err := invalidAdminClient.List(context.Background(), ListRequest{Limit: 1}); !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("List(typed nil provider) error = %v", err)
	}
	if _, err := invalidAdminClient.Delete(context.Background(), DeleteRequest{
		Subject: subject, Version: version,
		Policy: DeletionPolicy{Mode: DeleteSoft, ExpectedFingerprint: base.Fingerprint()},
	}); !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("Delete(typed nil provider) error = %v", err)
	}
	for name, lookup := range map[string]Lookup{
		"subject": AtVersion(Subject{}, version),
		"version": AtVersion(subject, Version{}),
	} {
		if err := lookup.validate("test"); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("lookup.validate(missing %s) error = %v", name, err)
		}
	}

	codec := &codecFunctions{
		encode: func(context.Context, Schema, any) ([]byte, error) { return nil, nil },
		decode: func(context.Context, Schema, []byte, any) error { return nil },
	}
	for name, id := range map[string]ProviderID{
		"provider": {Value: "1"},
		"value":    {Provider: "test"},
	} {
		framer := &framerFunctions{unframe: func(context.Context, []byte) (ProviderID, []byte, error) {
			return id, nil, nil
		}}
		integration, err := NewCodecIntegration(codec, framer, CodecLimits{MaxPayloadBytes: 1, MaxFrameBytes: 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := integration.Parse(context.Background(), nil); !errors.Is(err, ErrInvalidSchema) {
			t.Fatalf("Parse(missing %s) error = %v", name, err)
		}
	}

	coordinate := ReferenceCoordinate{Subject: subject, Version: version}
	_, err = BuildReferenceGraph(context.Background(), []ReferenceCoordinate{coordinate}, referenceResolverFunction(
		func(context.Context, ReferenceCoordinate) (ReferenceDocument, error) {
			return ReferenceDocument{}, ErrNotFound
		},
	), limits)
	if !errors.Is(err, ErrReferenceMissing) {
		t.Fatalf("BuildReferenceGraph(not found) error = %v", err)
	}

	wrongPrefix := "sha257:" + base.Fingerprint().String()[len("sha256:"):]
	if _, err := ParseFingerprint(wrongPrefix); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("ParseFingerprint(wrong prefix) error = %v", err)
	}
	invalidHex := "sha256:" + string(make([]byte, 64))
	if _, err := ParseFingerprint(invalidHex); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("ParseFingerprint(invalid hex) error = %v", err)
	}
}

func withSubject(result ResolveResult, subject Subject) ResolveResult {
	result.Subject = subject
	return result
}

func withVersion(result ResolveResult, version Version) ResolveResult {
	result.Version = version
	return result
}
