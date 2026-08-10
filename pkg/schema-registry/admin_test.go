package schemaregistry_test

import (
	"context"
	"errors"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

type administrativeProviderStub struct {
	providerStub
	list   func(context.Context, schemaregistry.ListRequest) (schemaregistry.ListPage, error)
	delete func(context.Context, schemaregistry.DeleteRequest) (schemaregistry.DeleteResult, error)
}

func (provider *administrativeProviderStub) List(
	ctx context.Context,
	request schemaregistry.ListRequest,
) (schemaregistry.ListPage, error) {
	return provider.list(ctx, request)
}

func (provider *administrativeProviderStub) Delete(
	ctx context.Context,
	request schemaregistry.DeleteRequest,
) (schemaregistry.DeleteResult, error) {
	return provider.delete(ctx, request)
}

func TestClientBoundsListingBeforeProviderIO(t *testing.T) {
	t.Parallel()

	called := false
	provider := &administrativeProviderStub{
		providerStub: providerStub{capabilities: schemaregistry.Capabilities{Provider: "test", BoundedListing: true}},
		list: func(context.Context, schemaregistry.ListRequest) (schemaregistry.ListPage, error) {
			called = true
			return schemaregistry.ListPage{}, nil
		},
	}
	client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{MaxSchemaBytes: 1024, MaxListResults: 10, MaxConcurrent: 2})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.List(context.Background(), schemaregistry.ListRequest{Limit: 11})
	if !errors.Is(err, schemaregistry.ErrLimitExceeded) {
		t.Fatalf("List() error = %v, want ErrLimitExceeded", err)
	}
	if called {
		t.Fatal("provider called for excessive list request")
	}
}

func TestClientRequiresExactDeletionPolicy(t *testing.T) {
	t.Parallel()

	called := false
	provider := &administrativeProviderStub{
		providerStub: providerStub{capabilities: schemaregistry.Capabilities{
			Provider:        "test",
			NumericVersions: true,
			SoftDelete:      true,
			HardDelete:      true,
		}},
		delete: func(context.Context, schemaregistry.DeleteRequest) (schemaregistry.DeleteResult, error) {
			called = true
			return schemaregistry.DeleteResult{Lifecycle: schemaregistry.LifecycleDeleted}, nil
		},
	}
	client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{MaxSchemaBytes: 1024, MaxListResults: 10, MaxConcurrent: 2})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.Delete(context.Background(), schemaregistry.DeleteRequest{
		Subject: schemaregistry.Subject{Name: "orders"},
		Version: schemaregistry.Version{Number: 2},
		Policy:  schemaregistry.DeletionPolicy{Mode: schemaregistry.DeleteHard},
	})
	if !errors.Is(err, schemaregistry.ErrConfirmationRequired) {
		t.Fatalf("Delete() error = %v, want ErrConfirmationRequired", err)
	}
	if called {
		t.Fatal("provider called without exact fingerprint confirmation")
	}

	schema := compileAvroString(t)
	_, err = client.Delete(context.Background(), schemaregistry.DeleteRequest{
		Subject: schemaregistry.Subject{Name: "orders"},
		Version: schemaregistry.Version{Number: 2, Opaque: "two"},
		Policy:  schemaregistry.DeletionPolicy{Mode: schemaregistry.DeleteHard, ExpectedFingerprint: schema.Fingerprint()},
	})
	if !errors.Is(err, schemaregistry.ErrInvalidRequest) || called {
		t.Fatalf("Delete(ambiguous version) = %v, called=%t", err, called)
	}
	_, err = client.Delete(context.Background(), schemaregistry.DeleteRequest{
		Subject: schemaregistry.Subject{Name: "orders"},
		Version: schemaregistry.Version{Opaque: "two"},
		Policy:  schemaregistry.DeletionPolicy{Mode: schemaregistry.DeleteHard, ExpectedFingerprint: schema.Fingerprint()},
	})
	if !errors.Is(err, schemaregistry.ErrUnsupportedOperation) || called {
		t.Fatalf("Delete(unsupported opaque version) = %v, called=%t", err, called)
	}

	result, err := client.Delete(context.Background(), schemaregistry.DeleteRequest{
		Subject: schemaregistry.Subject{Name: "orders"},
		Version: schemaregistry.Version{Number: 2},
		Policy: schemaregistry.DeletionPolicy{
			Mode:                schemaregistry.DeleteHard,
			ExpectedFingerprint: schema.Fingerprint(),
		},
	})
	if err != nil || result.Lifecycle != schemaregistry.LifecycleDeleted || !called {
		t.Fatalf("Delete(confirmed) = (%+v, %v), called=%t", result, err, called)
	}
}

func TestClientRejectsInvalidDeletionLifecycle(t *testing.T) {
	t.Parallel()

	schema := compileAvroString(t)
	provider := &administrativeProviderStub{
		providerStub: providerStub{capabilities: schemaregistry.Capabilities{Provider: "test", NumericVersions: true, SoftDelete: true}},
		delete: func(context.Context, schemaregistry.DeleteRequest) (schemaregistry.DeleteResult, error) {
			return schemaregistry.DeleteResult{}, nil
		},
	}
	client, err := schemaregistry.NewClient(provider, schemaregistry.Limits{MaxSchemaBytes: 1024, MaxListResults: 10, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Delete(context.Background(), schemaregistry.DeleteRequest{
		Subject: schemaregistry.Subject{Name: "orders"}, Version: schemaregistry.Version{Number: 1},
		Policy: schemaregistry.DeletionPolicy{Mode: schemaregistry.DeleteSoft, ExpectedFingerprint: schema.Fingerprint()},
	})
	if !errors.Is(err, schemaregistry.ErrInvalidRequest) {
		t.Fatalf("Delete() error = %v", err)
	}
}
