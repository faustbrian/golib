package schemaregistry

import (
	"context"
	"errors"
	"testing"
)

type basicProvider struct {
	capabilities  Capabilities
	register      func(context.Context, RegisterRequest) (RegisterResult, error)
	resolve       func(context.Context, Lookup) (ResolveResult, error)
	compatibility func(context.Context, CompatibilityRequest) (CompatibilityResult, error)
}

func (provider *basicProvider) Capabilities() Capabilities { return provider.capabilities }
func (provider *basicProvider) Register(ctx context.Context, request RegisterRequest) (RegisterResult, error) {
	return provider.register(ctx, request)
}
func (provider *basicProvider) Resolve(ctx context.Context, lookup Lookup) (ResolveResult, error) {
	return provider.resolve(ctx, lookup)
}
func (provider *basicProvider) CheckCompatibility(ctx context.Context, request CompatibilityRequest) (CompatibilityResult, error) {
	return provider.compatibility(ctx, request)
}

type adminProvider struct {
	*basicProvider
	list   func(context.Context, ListRequest) (ListPage, error)
	delete func(context.Context, DeleteRequest) (DeleteResult, error)
}

func (provider *adminProvider) List(ctx context.Context, request ListRequest) (ListPage, error) {
	return provider.list(ctx, request)
}
func (provider *adminProvider) Delete(ctx context.Context, request DeleteRequest) (DeleteResult, error) {
	return provider.delete(ctx, request)
}

func validClientLimits() Limits {
	return Limits{MaxSchemaBytes: 1024, MaxListResults: 2, MaxConcurrent: 1}
}

func TestClientConstructionLookupAndCapabilitiesBoundaries(t *testing.T) {
	t.Parallel()

	var nilProvider *basicProvider
	if _, err := NewClient(nilProvider, validClientLimits()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("NewClient(nil) error = %v", err)
	}
	provider := &basicProvider{capabilities: Capabilities{Provider: "test"}}
	for _, limits := range []Limits{{}, {MaxSchemaBytes: 1, MaxListResults: 1}, {MaxSchemaBytes: 1, MaxConcurrent: 1}} {
		if _, err := NewClient(provider, limits); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("NewClient(%+v) error = %v", limits, err)
		}
	}
	provider.capabilities.Provider = ""
	if _, err := NewClient(provider, validClientLimits()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("NewClient(empty provider) error = %v", err)
	}
	provider.capabilities = Capabilities{
		Provider: "test", Formats: []Format{FormatAvro},
		Lookups: []LookupKind{LookupByProviderID}, CompatibilityModes: []CompatibilityMode{CompatibilityBackward},
	}
	client, err := NewClient(provider, validClientLimits())
	if err != nil {
		t.Fatal(err)
	}
	capabilities := client.Capabilities()
	capabilities.Formats[0] = FormatProtobuf
	capabilities.Lookups[0] = LookupLatest
	capabilities.CompatibilityModes[0] = CompatibilityNone
	if client.Capabilities().Formats[0] != FormatAvro || client.Capabilities().Lookups[0] != LookupByProviderID ||
		client.Capabilities().CompatibilityModes[0] != CompatibilityBackward {
		t.Fatal("Capabilities() returned aliased slices")
	}
	fingerprint := Fingerprint{sum: [32]byte{1}}
	subject := Subject{Name: "subject"}
	version := Version{Number: 1}
	lookups := []Lookup{
		ByProviderID(ProviderID{Provider: "test", Value: "1"}), ByFingerprint(fingerprint),
		AtVersion(subject, version), Latest(subject),
	}
	if lookups[0].Kind() != LookupByProviderID || lookups[1].Fingerprint() != fingerprint ||
		lookups[2].Subject() != subject || lookups[2].Version() != version {
		t.Fatalf("lookup accessors = %+v", lookups)
	}
	for _, lookup := range []Lookup{
		ByProviderID(ProviderID{Provider: "other", Value: "1"}),
		ByProviderID(ProviderID{Provider: "test"}), ByFingerprint(Fingerprint{}),
		AtVersion(Subject{}, Version{}), Latest(Subject{}), {},
	} {
		if err := lookup.validate("test"); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("lookup.validate(%s) error = %v", lookup.Kind(), err)
		}
	}
}

func TestIdentityValidationHelperBoundaries(t *testing.T) {
	t.Parallel()

	for _, state := range []LifecycleState{
		LifecyclePending, LifecycleAvailable, LifecycleDeleting,
		LifecycleDeleted, LifecycleFailed, LifecycleUnknown,
	} {
		if !state.valid() {
			t.Fatalf("LifecycleState(%q).valid() = false", state)
		}
	}
	if LifecycleState("invalid").valid() {
		t.Fatal("invalid lifecycle accepted")
	}
	client := &Client{capabilities: Capabilities{Provider: "test", OpaqueVersions: true}}
	if err := client.validateVersionCapability(Version{Opaque: "one"}); err != nil {
		t.Fatalf("validateVersionCapability(opaque) error = %v", err)
	}
	if err := client.validateVersionCapability(Version{Number: 1}); !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("validateVersionCapability(numeric) error = %v", err)
	}
	if err := client.validateRegistrationResult(RegisterResult{
		Outcome: RegistrationUnknown, ID: ProviderID{Provider: "test", Value: "1"}, Version: Version{Opaque: "one"},
	}); err != nil {
		t.Fatalf("validateRegistrationResult(opaque) error = %v", err)
	}
	if err := client.validateRegistrationResult(RegisterResult{
		Outcome: RegistrationExisting, ID: ProviderID{Provider: "test", Value: "1"}, Version: Version{Number: 1},
	}); !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("validateRegistrationResult(unsupported numeric) error = %v", err)
	}
}

func TestClientOperationBoundaries(t *testing.T) {
	t.Parallel()

	schema := internalSchema(t, FormatAvro, `"string"`, nil)
	subject := Subject{Name: "subject"}
	version := Version{Number: 1}
	id := ProviderID{Provider: "test", Value: "1"}
	providerError := errors.New("provider failure")
	provider := &basicProvider{
		capabilities: Capabilities{
			Provider: "test", Formats: []Format{FormatAvro},
			Lookups:            []LookupKind{LookupByProviderID, LookupByFingerprint, LookupByVersion, LookupLatest},
			CompatibilityModes: []CompatibilityMode{CompatibilityBackward},
			NumericVersions:    true, RegistrationCreationOutcome: true,
		},
		register: func(context.Context, RegisterRequest) (RegisterResult, error) {
			return RegisterResult{Outcome: RegistrationCreated, ID: id}, nil
		},
		resolve: func(_ context.Context, lookup Lookup) (ResolveResult, error) {
			result := ResolveResult{Schema: schema, ID: id, Lifecycle: LifecycleAvailable}
			switch lookup.Kind() {
			case LookupByProviderID:
				result.ID = lookup.ProviderID()
			case LookupByFingerprint:
			case LookupByVersion:
				result.Subject, result.Version = lookup.Subject(), lookup.Version()
			case LookupLatest:
				result.Subject, result.Version = lookup.Subject(), version
			default:
				return ResolveResult{}, providerError
			}
			return result, nil
		},
		compatibility: func(context.Context, CompatibilityRequest) (CompatibilityResult, error) {
			return CompatibilityResult{Supported: true, Compatible: true}, nil
		},
	}
	client, err := NewClient(provider, validClientLimits())
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Register(canceled, RegisterRequest{Subject: subject, Schema: schema}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Register(canceled) error = %v", err)
	}
	if _, err := client.Register(context.Background(), RegisterRequest{Schema: schema}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Register(empty subject) error = %v", err)
	}
	jsonSchema := internalSchema(t, FormatJSONSchema, `true`, nil)
	if _, err := client.Register(context.Background(), RegisterRequest{Subject: subject, Schema: jsonSchema}); !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("Register(unsupported format) error = %v", err)
	}
	key := registrationKey{subject: subject, fingerprint: schema.Fingerprint()}
	client.registrations[key] = &registrationFlight{done: make(chan struct{})}
	if _, err := client.Register(&delayedCanceledContext{}, RegisterRequest{Subject: subject, Schema: schema}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Register(canceled waiter) error = %v", err)
	}
	delete(client.registrations, key)
	client.slots <- struct{}{}
	if _, err := client.Register(&delayedCanceledContext{}, RegisterRequest{Subject: subject, Schema: schema}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Register(blocked) error = %v", err)
	}
	<-client.slots
	for _, lookup := range []Lookup{
		ByProviderID(ProviderID{Provider: "test", Value: "1"}), ByFingerprint(schema.Fingerprint()),
		AtVersion(subject, version), Latest(subject),
	} {
		if _, err := client.Resolve(context.Background(), lookup); err != nil {
			t.Fatalf("Resolve(%s) error = %v", lookup.Kind(), err)
		}
	}
	if _, err := client.Resolve(canceled, ByProviderID(ProviderID{Provider: "test", Value: "1"})); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve(canceled) error = %v", err)
	}
	if _, err := client.Resolve(context.Background(), ByProviderID(ProviderID{Provider: "other", Value: "1"})); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Resolve(invalid identity) error = %v", err)
	}
	client.slots <- struct{}{}
	if _, err := client.Resolve(&delayedCanceledContext{}, ByProviderID(ProviderID{Provider: "test", Value: "1"})); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve(blocked) error = %v", err)
	}
	<-client.slots
	compatible, err := client.CheckCompatibility(context.Background(), CompatibilityRequest{
		Subject: subject, Candidate: schema, Mode: CompatibilityBackward,
	})
	if err != nil || !compatible.Compatible {
		t.Fatalf("CheckCompatibility() = (%+v, %v)", compatible, err)
	}
	if _, err := client.CheckCompatibility(canceled, CompatibilityRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("CheckCompatibility(canceled) error = %v", err)
	}
	unsupported, err := client.CheckCompatibility(context.Background(), CompatibilityRequest{Mode: CompatibilityFull})
	if err != nil || unsupported.Supported {
		t.Fatalf("CheckCompatibility(unsupported) = (%+v, %v)", unsupported, err)
	}
	if _, err := client.CheckCompatibility(context.Background(), CompatibilityRequest{Mode: CompatibilityBackward}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("CheckCompatibility(invalid) error = %v", err)
	}
	client.slots <- struct{}{}
	if _, err := client.CheckCompatibility(&delayedCanceledContext{}, CompatibilityRequest{Subject: subject, Candidate: schema, Mode: CompatibilityBackward}); !errors.Is(err, context.Canceled) {
		t.Fatalf("CheckCompatibility(blocked) error = %v", err)
	}
	<-client.slots
	provider.resolve = func(context.Context, Lookup) (ResolveResult, error) { return ResolveResult{}, providerError }
	if _, err := client.Resolve(context.Background(), ByProviderID(ProviderID{Provider: "test", Value: "1"})); !errors.Is(err, providerError) {
		t.Fatalf("Resolve(provider error) error = %v", err)
	}
}

func TestClientAdministrativeBoundaries(t *testing.T) {
	t.Parallel()

	schema := internalSchema(t, FormatAvro, `"string"`, nil)
	subject := Subject{Name: "subject"}
	version := Version{Number: 1}
	providerError := errors.New("provider failure")
	base := &basicProvider{capabilities: Capabilities{Provider: "test", NumericVersions: true, BoundedListing: true, SoftDelete: true, HardDelete: true}}
	basicClient, err := NewClient(base, validClientLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := basicClient.List(context.Background(), ListRequest{Limit: 1}); !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("List(no contract) error = %v", err)
	}
	if _, err := basicClient.Delete(context.Background(), DeleteRequest{
		Subject: subject, Version: version, Policy: DeletionPolicy{Mode: DeleteSoft, ExpectedFingerprint: schema.Fingerprint()},
	}); !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("Delete(no contract) error = %v", err)
	}
	basicClient.capabilities.BoundedListing = false
	if _, err := basicClient.List(context.Background(), ListRequest{Limit: 1}); !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("List(not advertised) error = %v", err)
	}
	provider := &adminProvider{
		basicProvider: base,
		list: func(context.Context, ListRequest) (ListPage, error) {
			return ListPage{Schemas: []SchemaDescriptor{{Subject: subject}}}, nil
		},
		delete: func(context.Context, DeleteRequest) (DeleteResult, error) {
			return DeleteResult{Lifecycle: LifecycleDeleted}, nil
		},
	}
	client, err := NewClient(provider, validClientLimits())
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.List(canceled, ListRequest{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("List(canceled) error = %v", err)
	}
	if _, err := client.List(context.Background(), ListRequest{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("List(invalid) error = %v", err)
	}
	client.slots <- struct{}{}
	if _, err := client.List(&delayedCanceledContext{}, ListRequest{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("List(blocked) error = %v", err)
	}
	<-client.slots
	page, err := client.List(context.Background(), ListRequest{Limit: 1})
	if err != nil || len(page.Schemas) != 1 {
		t.Fatalf("List() = (%+v, %v)", page, err)
	}
	page.Schemas[0].Subject.Name = "mutated"
	provider.list = func(context.Context, ListRequest) (ListPage, error) { return ListPage{}, providerError }
	if _, err := client.List(context.Background(), ListRequest{Limit: 1}); !errors.Is(err, providerError) {
		t.Fatalf("List(provider error) error = %v", err)
	}
	provider.list = func(context.Context, ListRequest) (ListPage, error) {
		return ListPage{Schemas: make([]SchemaDescriptor, 2)}, nil
	}
	if _, err := client.List(context.Background(), ListRequest{Limit: 1}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("List(oversized response) error = %v", err)
	}
	if _, err := client.Delete(canceled, DeleteRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete(canceled) error = %v", err)
	}
	if _, err := client.Delete(context.Background(), DeleteRequest{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Delete(invalid target) error = %v", err)
	}
	request := DeleteRequest{Subject: subject, Version: version, Policy: DeletionPolicy{ExpectedFingerprint: schema.Fingerprint()}}
	if _, err := client.Delete(context.Background(), request); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Delete(invalid mode) error = %v", err)
	}
	request.Policy.Mode = DeleteSoft
	client.capabilities.SoftDelete = false
	if _, err := client.Delete(context.Background(), request); !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("Delete(unsupported soft) error = %v", err)
	}
	client.capabilities.SoftDelete = true
	request.Policy.Mode = DeleteHard
	client.capabilities.HardDelete = false
	if _, err := client.Delete(context.Background(), request); !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("Delete(unsupported hard) error = %v", err)
	}
	client.capabilities.HardDelete = true
	for _, mode := range []DeletionMode{DeleteSoft, DeleteHard} {
		request.Policy.Mode = mode
		if _, err := client.Delete(context.Background(), request); err != nil {
			t.Fatalf("Delete(%s) error = %v", mode, err)
		}
	}
	client.slots <- struct{}{}
	if _, err := client.Delete(&delayedCanceledContext{}, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete(blocked) error = %v", err)
	}
	<-client.slots
	provider.delete = func(context.Context, DeleteRequest) (DeleteResult, error) { return DeleteResult{}, providerError }
	if _, err := client.Delete(context.Background(), request); !errors.Is(err, providerError) {
		t.Fatalf("Delete(provider error) error = %v", err)
	}
}
