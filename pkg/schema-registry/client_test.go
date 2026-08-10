package schemaregistry_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

type providerStub struct {
	capabilities  schemaregistry.Capabilities
	register      func(context.Context, schemaregistry.RegisterRequest) (schemaregistry.RegisterResult, error)
	resolve       func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error)
	compatibility func(
		context.Context,
		schemaregistry.CompatibilityRequest,
	) (schemaregistry.CompatibilityResult, error)
}

func (provider *providerStub) Capabilities() schemaregistry.Capabilities {
	return provider.capabilities
}

func (provider *providerStub) Register(
	ctx context.Context,
	request schemaregistry.RegisterRequest,
) (schemaregistry.RegisterResult, error) {
	return provider.register(ctx, request)
}

func (provider *providerStub) Resolve(
	ctx context.Context,
	lookup schemaregistry.Lookup,
) (schemaregistry.ResolveResult, error) {
	return provider.resolve(ctx, lookup)
}

func (provider *providerStub) CheckCompatibility(
	ctx context.Context,
	request schemaregistry.CompatibilityRequest,
) (schemaregistry.CompatibilityResult, error) {
	return provider.compatibility(ctx, request)
}

func TestClientRejectsUnsupportedLookupWithoutCallingProvider(t *testing.T) {
	t.Parallel()

	called := false
	provider := &providerStub{
		capabilities: schemaregistry.Capabilities{
			Provider: "fixed-version-registry",
			Lookups:  []schemaregistry.LookupKind{schemaregistry.LookupByProviderID},
		},
		resolve: func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			called = true
			return schemaregistry.ResolveResult{}, nil
		},
	}
	client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{
		MaxSchemaBytes: 1024,
		MaxListResults: 10,
		MaxConcurrent:  2,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.Resolve(context.Background(), schemaregistry.Latest(schemaregistry.Subject{
		Registry: "events",
		Name:     "orders-value",
	}))
	if !errors.Is(err, schemaregistry.ErrUnsupportedOperation) {
		t.Fatalf("Resolve() error = %v, want ErrUnsupportedOperation", err)
	}
	if called {
		t.Fatal("provider called for unsupported latest lookup")
	}
}

func TestClientRejectsMismatchedProviderResolution(t *testing.T) {
	t.Parallel()

	provider := &providerStub{
		capabilities: schemaregistry.Capabilities{Provider: "test", Lookups: []schemaregistry.LookupKind{schemaregistry.LookupByProviderID}},
		resolve: func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			return schemaregistry.ResolveResult{ID: schemaregistry.ProviderID{Provider: "test", Value: "other"}}, nil
		},
	}
	client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{MaxSchemaBytes: 1024, MaxListResults: 10, MaxConcurrent: 1})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Resolve(context.Background(), schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "test", Value: "wanted"}))
	if !errors.Is(err, schemaregistry.ErrResolutionMismatch) {
		t.Fatalf("Resolve() error = %v, want ErrResolutionMismatch", err)
	}
}

func TestClientPreservesExplicitRegistrationOutcome(t *testing.T) {
	t.Parallel()

	want := schemaregistry.RegisterResult{
		Outcome: schemaregistry.RegistrationExisting,
		ID: schemaregistry.ProviderID{
			Provider: "confluent",
			Scope:    "cluster-a",
			Value:    "42",
		},
		Version: schemaregistry.Version{Number: 3},
	}
	provider := &providerStub{
		capabilities: schemaregistry.Capabilities{
			Provider:        "confluent",
			Formats:         []schemaregistry.Format{schemaregistry.FormatAvro},
			NumericVersions: true,
		},
		register: func(_ context.Context, request schemaregistry.RegisterRequest) (schemaregistry.RegisterResult, error) {
			if request.Subject.Name != "orders-value" {
				t.Fatalf("Register() subject = %q", request.Subject.Name)
			}
			return want, nil
		},
	}
	client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{
		MaxSchemaBytes: 1024,
		MaxListResults: 10,
		MaxConcurrent:  2,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	schema := compileAvroString(t)
	got, err := client.Register(context.Background(), schemaregistry.RegisterRequest{
		Subject: schemaregistry.Subject{Registry: "events", Name: "orders-value"},
		Schema:  schema,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if got != want {
		t.Fatalf("Register() = %+v, want %+v", got, want)
	}
}

func TestClientClassifiesRegistrationFailures(t *testing.T) {
	t.Parallel()

	schema := compileAvroString(t)
	tests := []struct {
		err     error
		outcome schemaregistry.RegistrationOutcome
	}{
		{schemaregistry.ErrIncompatible, schemaregistry.RegistrationIncompatible},
		{schemaregistry.ErrRejected, schemaregistry.RegistrationRejected},
		{schemaregistry.ErrUnauthorized, schemaregistry.RegistrationUnauthorized},
		{schemaregistry.ErrUnavailable, schemaregistry.RegistrationUnavailable},
		{schemaregistry.ErrUnknownOutcome, schemaregistry.RegistrationUnknown},
	}
	for _, test := range tests {
		test := test
		t.Run(string(test.outcome), func(t *testing.T) {
			t.Parallel()
			provider := &providerStub{
				capabilities: schemaregistry.Capabilities{Provider: "test", Formats: []schemaregistry.Format{schemaregistry.FormatAvro}},
				register: func(context.Context, schemaregistry.RegisterRequest) (schemaregistry.RegisterResult, error) {
					return schemaregistry.RegisterResult{}, test.err
				},
			}
			client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{MaxSchemaBytes: 1024, MaxListResults: 10, MaxConcurrent: 2})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			result, err := client.Register(context.Background(), schemaregistry.RegisterRequest{
				Subject: schemaregistry.Subject{Name: "orders"}, Schema: schema,
			})
			if !errors.Is(err, test.err) || result.Outcome != test.outcome {
				t.Fatalf("Register() = (%+v, %v), want %s", result, err, test.outcome)
			}
		})
	}
}

func TestClientDoesNotPreserveSuccessfulOutcomeOnRegistrationFailure(t *testing.T) {
	t.Parallel()

	provider := &providerStub{
		capabilities: schemaregistry.Capabilities{Provider: "test", Formats: []schemaregistry.Format{schemaregistry.FormatAvro}},
		register: func(context.Context, schemaregistry.RegisterRequest) (schemaregistry.RegisterResult, error) {
			return schemaregistry.RegisterResult{Outcome: schemaregistry.RegistrationCreated}, schemaregistry.ErrUnavailable
		},
	}
	client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{MaxSchemaBytes: 1024, MaxListResults: 10, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Register(context.Background(), schemaregistry.RegisterRequest{
		Subject: schemaregistry.Subject{Name: "orders"}, Schema: compileAvroString(t),
	})
	if !errors.Is(err, schemaregistry.ErrUnavailable) || result.Outcome != schemaregistry.RegistrationUnavailable {
		t.Fatalf("Register() = (%+v, %v)", result, err)
	}
}

func TestClientRejectsOversizedSchemaBeforeRegistration(t *testing.T) {
	t.Parallel()

	called := false
	provider := &providerStub{
		capabilities: schemaregistry.Capabilities{
			Provider: "glue",
			Formats:  []schemaregistry.Format{schemaregistry.FormatAvro},
		},
		register: func(context.Context, schemaregistry.RegisterRequest) (schemaregistry.RegisterResult, error) {
			called = true
			return schemaregistry.RegisterResult{}, nil
		},
	}
	client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{
		MaxSchemaBytes: 4,
		MaxListResults: 10,
		MaxConcurrent:  2,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.Register(context.Background(), schemaregistry.RegisterRequest{
		Subject: schemaregistry.Subject{Registry: "events", Name: "orders"},
		Schema:  compileAvroString(t),
	})
	if !errors.Is(err, schemaregistry.ErrLimitExceeded) {
		t.Fatalf("Register() error = %v, want ErrLimitExceeded", err)
	}
	if called {
		t.Fatal("provider called for oversized schema")
	}
}

func TestClientCoalescesConcurrentIdempotentRegistration(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	provider := &providerStub{
		capabilities: schemaregistry.Capabilities{Provider: "test", Formats: []schemaregistry.Format{schemaregistry.FormatAvro}},
		register: func(context.Context, schemaregistry.RegisterRequest) (schemaregistry.RegisterResult, error) {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-release
			return schemaregistry.RegisterResult{Outcome: schemaregistry.RegistrationExisting, ID: schemaregistry.ProviderID{Provider: "test", Value: "1"}}, nil
		},
	}
	client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{
		MaxSchemaBytes: 1024, MaxListResults: 10, MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	request := schemaregistry.RegisterRequest{
		Subject: schemaregistry.Subject{Name: "orders"}, Schema: compileAvroString(t),
	}
	first := make(chan error, 1)
	go func() { _, registerErr := client.Register(context.Background(), request); first <- registerErr }()
	<-started
	second := make(chan error, 1)
	go func() { _, registerErr := client.Register(context.Background(), request); second <- registerErr }()
	time.Sleep(10 * time.Millisecond)
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second Register() error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("provider registration calls = %d, want 1", calls.Load())
	}
}

func compileAvroString(t *testing.T) schemaregistry.Schema {
	t.Helper()
	schema, err := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`"string"`)},
		canonicalizerFunc(func(context.Context, schemaregistry.Definition) ([]byte, error) {
			return []byte(`"string"`), nil
		}),
	)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return schema
}
