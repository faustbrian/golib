package schemaregistry_test

import (
	"context"
	"errors"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

func TestClientRejectsAmbiguousOrUnsupportedVersionsBeforeProviderIO(t *testing.T) {
	t.Parallel()

	called := false
	provider := &providerStub{
		capabilities: schemaregistry.Capabilities{
			Provider: "test", Lookups: []schemaregistry.LookupKind{schemaregistry.LookupByVersion},
			NumericVersions: true,
		},
		resolve: func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			called = true
			return schemaregistry.ResolveResult{}, nil
		},
	}
	client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{MaxSchemaBytes: 1024, MaxListResults: 10, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	subject := schemaregistry.Subject{Name: "orders"}
	for _, version := range []schemaregistry.Version{
		{Number: 1, Opaque: "one"},
		{Opaque: "one"},
	} {
		if _, err := client.Resolve(context.Background(), schemaregistry.AtVersion(subject, version)); !errors.Is(err, schemaregistry.ErrInvalidRequest) && !errors.Is(err, schemaregistry.ErrUnsupportedOperation) {
			t.Fatalf("Resolve(%+v) error = %v", version, err)
		}
	}
	if called {
		t.Fatal("provider called for invalid or unsupported version identity")
	}
}

func TestClientRejectsInvalidSuccessfulRegistrationResults(t *testing.T) {
	t.Parallel()

	schema := compileAvroString(t)
	tests := []schemaregistry.RegisterResult{
		{},
		{Outcome: schemaregistry.RegistrationRejected, ID: schemaregistry.ProviderID{Provider: "test", Value: "1"}},
		{Outcome: schemaregistry.RegistrationExisting, ID: schemaregistry.ProviderID{Provider: "other", Value: "1"}},
		{Outcome: schemaregistry.RegistrationExisting, ID: schemaregistry.ProviderID{Provider: "test"}},
		{Outcome: schemaregistry.RegistrationCreated, ID: schemaregistry.ProviderID{Provider: "test", Value: "1"}},
		{Outcome: schemaregistry.RegistrationExisting, ID: schemaregistry.ProviderID{Provider: "test", Value: "1"}, Version: schemaregistry.Version{Number: 1, Opaque: "one"}},
	}
	for _, returned := range tests {
		returned := returned
		t.Run(string(returned.Outcome)+returned.ID.Provider+returned.ID.Value+returned.Version.Opaque, func(t *testing.T) {
			t.Parallel()
			provider := &providerStub{
				capabilities: schemaregistry.Capabilities{Provider: "test", Formats: []schemaregistry.Format{schemaregistry.FormatAvro}, NumericVersions: true},
				register: func(context.Context, schemaregistry.RegisterRequest) (schemaregistry.RegisterResult, error) {
					return returned, nil
				},
			}
			client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{MaxSchemaBytes: 1024, MaxListResults: 10, MaxConcurrent: 1})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Name: "orders"}, Schema: schema}); !errors.Is(err, schemaregistry.ErrInvalidRequest) {
				t.Fatalf("Register() error = %v", err)
			}
		})
	}
}

func TestClientRejectsIncompleteSuccessfulResolution(t *testing.T) {
	t.Parallel()

	lookup := schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "test", Value: "1"})
	provider := &providerStub{
		capabilities: schemaregistry.Capabilities{Provider: "test", Lookups: []schemaregistry.LookupKind{schemaregistry.LookupByProviderID}},
		resolve: func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			return schemaregistry.ResolveResult{ID: lookup.ProviderID(), Lifecycle: schemaregistry.LifecycleAvailable}, nil
		},
	}
	client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{MaxSchemaBytes: 1024, MaxListResults: 10, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(context.Background(), lookup); !errors.Is(err, schemaregistry.ErrResolutionMismatch) {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestClientRejectsResolutionFromAnotherProvider(t *testing.T) {
	t.Parallel()

	schema := compileAvroString(t)
	subject := schemaregistry.Subject{Name: "orders"}
	version := schemaregistry.Version{Number: 1}
	provider := &providerStub{
		capabilities: schemaregistry.Capabilities{
			Provider: "test", Lookups: []schemaregistry.LookupKind{schemaregistry.LookupByVersion}, NumericVersions: true,
		},
		resolve: func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			return schemaregistry.ResolveResult{
				Schema: schema, ID: schemaregistry.ProviderID{Provider: "other", Value: "1"},
				Subject: subject, Version: version, Lifecycle: schemaregistry.LifecycleAvailable,
			}, nil
		},
	}
	client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{MaxSchemaBytes: 1024, MaxListResults: 10, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(context.Background(), schemaregistry.AtVersion(subject, version)); !errors.Is(err, schemaregistry.ErrResolutionMismatch) {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestClientBoundsAndValidatesCompatibilityContracts(t *testing.T) {
	t.Parallel()

	schema := compileAvroString(t)
	called := false
	provider := &providerStub{
		capabilities: schemaregistry.Capabilities{
			Provider: "test", Formats: []schemaregistry.Format{schemaregistry.FormatAvro},
			CompatibilityModes: []schemaregistry.CompatibilityMode{schemaregistry.CompatibilityBackward, schemaregistry.CompatibilityProviderSpecific},
		},
		compatibility: func(context.Context, schemaregistry.CompatibilityRequest) (schemaregistry.CompatibilityResult, error) {
			called = true
			return schemaregistry.CompatibilityResult{Supported: false, Compatible: true}, nil
		},
	}
	client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{MaxSchemaBytes: 4, MaxListResults: 10, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	requests := []schemaregistry.CompatibilityRequest{
		{Subject: schemaregistry.Subject{Name: "orders"}, Candidate: schema, Mode: schemaregistry.CompatibilityBackward},
		{Subject: schemaregistry.Subject{Name: "orders"}, Candidate: schema, Mode: schemaregistry.CompatibilityProviderSpecific},
		{Subject: schemaregistry.Subject{Name: "orders"}, Candidate: schema, Mode: schemaregistry.CompatibilityBackward, ProviderMode: "custom"},
	}
	for _, request := range requests {
		called = false
		if _, err := client.CheckCompatibility(context.Background(), request); !errors.Is(err, schemaregistry.ErrLimitExceeded) && !errors.Is(err, schemaregistry.ErrInvalidRequest) {
			t.Fatalf("CheckCompatibility(%+v) error = %v", request, err)
		}
		if called {
			t.Fatal("provider called for invalid compatibility request")
		}
	}

	client, err = schemaregistry.NewClient(provider, schemaregistry.Limits{MaxSchemaBytes: 1024, MaxListResults: 10, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	jsonSchema, err := schemaregistry.Compile(
		context.Background(), schemaregistry.Definition{Format: schemaregistry.FormatJSONSchema, Content: []byte("true")},
		canonicalizerFunc(func(context.Context, schemaregistry.Definition) ([]byte, error) { return []byte("true"), nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CheckCompatibility(context.Background(), schemaregistry.CompatibilityRequest{
		Subject: schemaregistry.Subject{Name: "orders"}, Candidate: jsonSchema, Mode: schemaregistry.CompatibilityBackward,
	}); !errors.Is(err, schemaregistry.ErrUnsupportedOperation) {
		t.Fatalf("CheckCompatibility(unsupported format) error = %v", err)
	}
	for _, request := range []schemaregistry.CompatibilityRequest{
		{Subject: schemaregistry.Subject{Name: "orders"}, Candidate: schema, Mode: schemaregistry.CompatibilityProviderSpecific},
		{Subject: schemaregistry.Subject{Name: "orders"}, Candidate: schema, Mode: schemaregistry.CompatibilityBackward, ProviderMode: "custom"},
	} {
		called = false
		if _, err := client.CheckCompatibility(context.Background(), request); !errors.Is(err, schemaregistry.ErrInvalidRequest) {
			t.Fatalf("CheckCompatibility(provider mode %+v) error = %v", request, err)
		}
		if called {
			t.Fatal("provider called for invalid provider mode")
		}
	}
	_, err = client.CheckCompatibility(context.Background(), schemaregistry.CompatibilityRequest{
		Subject: schemaregistry.Subject{Name: "orders"}, Candidate: schema, Mode: schemaregistry.CompatibilityBackward,
	})
	if !errors.Is(err, schemaregistry.ErrInvalidRequest) {
		t.Fatalf("CheckCompatibility(contradictory result) error = %v", err)
	}
}
