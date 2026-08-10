package schemaregistry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAAAAClientOperationsAreCanceledOrBounded(t *testing.T) {
	schema := internalSchema(t, FormatAvro, "string", nil)
	subject := Subject{Name: "s"}
	version := Version{Number: 1}
	id := ProviderID{Provider: "test", Value: "1"}
	provider := &adminProvider{
		basicProvider: &basicProvider{
			capabilities: Capabilities{
				Provider: "test", Formats: []Format{FormatAvro},
				Lookups: []LookupKind{LookupByProviderID}, CompatibilityModes: []CompatibilityMode{CompatibilityBackward},
				NumericVersions: true, BoundedListing: true, SoftDelete: true,
			},
			register: func(context.Context, RegisterRequest) (RegisterResult, error) {
				return RegisterResult{Outcome: RegistrationExisting, ID: id}, nil
			},
			resolve: func(context.Context, Lookup) (ResolveResult, error) {
				return ResolveResult{ID: id, Schema: schema, Lifecycle: LifecycleAvailable}, nil
			},
			compatibility: func(context.Context, CompatibilityRequest) (CompatibilityResult, error) {
				return CompatibilityResult{Supported: true, Compatible: true}, nil
			},
		},
		list: func(context.Context, ListRequest) (ListPage, error) {
			return ListPage{Schemas: []SchemaDescriptor{{ID: id}}}, nil
		},
		delete: func(context.Context, DeleteRequest) (DeleteResult, error) {
			return DeleteResult{Lifecycle: LifecycleDeleting}, nil
		},
	}
	client, err := NewClient(provider, Limits{MaxSchemaBytes: 1024, MaxListResults: 2, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Register(canceled, RegisterRequest{Subject: subject, Schema: schema}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Register(canceled) error = %v", err)
	}
	if _, err := client.Resolve(canceled, ByProviderID(id)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve(canceled) error = %v", err)
	}
	if _, err := client.CheckCompatibility(canceled, CompatibilityRequest{Subject: subject, Candidate: schema, Mode: CompatibilityBackward}); !errors.Is(err, context.Canceled) {
		t.Fatalf("CheckCompatibility(canceled) error = %v", err)
	}
	if _, err := client.List(canceled, ListRequest{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("List(canceled) error = %v", err)
	}
	if _, err := client.Delete(canceled, DeleteRequest{Subject: subject, Version: version, Policy: DeletionPolicy{Mode: DeleteSoft, ExpectedFingerprint: schema.Fingerprint()}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete(canceled) error = %v", err)
	}

	ctx, cancelBound := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancelBound()
	if result, err := client.Register(ctx, RegisterRequest{Subject: subject, Schema: schema}); err != nil || result.ID != id {
		t.Fatalf("Register(valid) = (%+v, %v)", result, err)
	}
	if result, err := client.Resolve(ctx, ByProviderID(id)); err != nil || result.ID != id {
		t.Fatalf("Resolve(valid) = (%+v, %v)", result, err)
	}
	if result, err := client.CheckCompatibility(ctx, CompatibilityRequest{Subject: subject, Candidate: schema, Mode: CompatibilityBackward}); err != nil || !result.Supported || !result.Compatible {
		t.Fatalf("CheckCompatibility(valid) = (%+v, %v)", result, err)
	}
	if result, err := client.List(ctx, ListRequest{Limit: 1}); err != nil || len(result.Schemas) != 1 {
		t.Fatalf("List(valid) = (%+v, %v)", result, err)
	}
	if result, err := client.Delete(ctx, DeleteRequest{Subject: subject, Version: version, Policy: DeletionPolicy{Mode: DeleteSoft, ExpectedFingerprint: schema.Fingerprint()}}); err != nil || result.Lifecycle != LifecycleDeleting {
		t.Fatalf("Delete(valid) = (%+v, %v)", result, err)
	}
}
