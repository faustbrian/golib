package schemaregistry_test

import (
	"context"
	"errors"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryavro "github.com/faustbrian/golib/pkg/schema-registry/formats/avro"
)

func TestExplicitDualRegistrationCutoverFailoverAndRollback(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	canonicalizer := registryavro.New(4096)
	schema, err := schemaregistry.Compile(ctx, schemaregistry.Definition{
		Format:  schemaregistry.FormatAvro,
		Content: []byte(`{"type":"record","name":"Order","fields":[{"name":"id","type":"string"}]}`),
	}, canonicalizer)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	bundle, err := schemaregistry.NewBundle(
		schema,
		nil,
		schemaregistry.GraphLimits{MaxSchemas: 1, MaxDepth: 1, MaxReferences: 1},
		schemaregistry.Provenance{Source: "source-export", Revision: "release-2026-08-10"},
	)
	if err != nil {
		t.Fatalf("NewBundle(export) error = %v", err)
	}
	exported, err := bundle.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary(export) error = %v", err)
	}
	recovery, err := schemaregistry.LoadBundle(
		ctx,
		exported,
		map[schemaregistry.Format]schemaregistry.Canonicalizer{schemaregistry.FormatAvro: canonicalizer},
		schemaregistry.GraphLimits{MaxSchemas: 1, MaxDepth: 1, MaxReferences: 1},
		len(exported),
	)
	if err != nil || recovery.Provenance().Source != "source-export" {
		t.Fatalf("LoadBundle(recovery) = (%+v, %v)", recovery.Provenance(), err)
	}

	subject := schemaregistry.Subject{Name: "orders-value"}
	sourceID := schemaregistry.ProviderID{Provider: "source", Scope: "cluster-a", Value: "11"}
	targetID := schemaregistry.ProviderID{Provider: "target", Scope: "region-b", Value: "7f6b0af1"}
	targetUnavailable := false

	source := migrationClient(t, "source", func(_ context.Context, request schemaregistry.RegisterRequest) (schemaregistry.RegisterResult, error) {
		if request.Subject != subject || request.Schema.Fingerprint() != schema.Fingerprint() {
			t.Fatal("source registration lost subject or portable identity")
		}
		return schemaregistry.RegisterResult{
			Outcome: schemaregistry.RegistrationExisting,
			ID:      sourceID, Version: schemaregistry.Version{Number: 1},
		}, nil
	}, func(_ context.Context, lookup schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
		if lookup.ProviderID() != sourceID {
			return schemaregistry.ResolveResult{}, schemaregistry.ErrNotFound
		}
		return migrationResolution(schema, sourceID, subject, 1), nil
	})
	target := migrationClient(t, "target", func(_ context.Context, request schemaregistry.RegisterRequest) (schemaregistry.RegisterResult, error) {
		if request.Subject != subject || request.Schema.Fingerprint() != schema.Fingerprint() {
			t.Fatal("target registration lost subject or portable identity")
		}
		return schemaregistry.RegisterResult{
			Outcome: schemaregistry.RegistrationUnknown,
			ID:      targetID, Version: schemaregistry.Version{Number: 3},
		}, nil
	}, func(_ context.Context, lookup schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
		if targetUnavailable {
			return schemaregistry.ResolveResult{}, schemaregistry.ErrUnavailable
		}
		if lookup.ProviderID() != targetID {
			return schemaregistry.ResolveResult{}, schemaregistry.ErrNotFound
		}
		return migrationResolution(schema, targetID, subject, 3), nil
	})

	sourceRegistration, err := source.Register(ctx, schemaregistry.RegisterRequest{Subject: subject, Schema: schema})
	if err != nil || sourceRegistration.Outcome != schemaregistry.RegistrationExisting {
		t.Fatalf("source Register() = (%+v, %v)", sourceRegistration, err)
	}
	targetRegistration, err := target.Register(ctx, schemaregistry.RegisterRequest{Subject: subject, Schema: schema})
	if err != nil || targetRegistration.Outcome != schemaregistry.RegistrationUnknown {
		t.Fatalf("target Register() = (%+v, %v)", targetRegistration, err)
	}

	cutover, err := target.Resolve(ctx, schemaregistry.ByProviderID(targetRegistration.ID))
	if err != nil || cutover.Schema.Fingerprint() != schema.Fingerprint() {
		t.Fatalf("target verification before cutover = (%s, %v)", cutover.Schema.Fingerprint(), err)
	}
	if _, err := target.Resolve(ctx, schemaregistry.ByProviderID(sourceRegistration.ID)); !errors.Is(err, schemaregistry.ErrInvalidRequest) {
		t.Fatalf("target accepted source provider ID: %v", err)
	}

	targetUnavailable = true
	if _, err := target.Resolve(ctx, schemaregistry.ByProviderID(targetRegistration.ID)); !errors.Is(err, schemaregistry.ErrUnavailable) {
		t.Fatalf("target outage error = %v", err)
	}
	fallback, err := source.Resolve(ctx, schemaregistry.ByProviderID(sourceRegistration.ID))
	if err != nil || fallback.Schema.Fingerprint() != schema.Fingerprint() {
		t.Fatalf("explicit source failover = (%s, %v)", fallback.Schema.Fingerprint(), err)
	}
	offline, found := recovery.Resolve(schema.Fingerprint())
	if !found || offline.Fingerprint() != schema.Fingerprint() {
		t.Fatalf("offline disaster recovery = (%s, %t)", offline.Fingerprint(), found)
	}

	targetUnavailable = false
	rolledBack, err := source.Resolve(ctx, schemaregistry.ByProviderID(sourceRegistration.ID))
	if err != nil || rolledBack.ID != sourceID || rolledBack.Schema.Fingerprint() != cutover.Schema.Fingerprint() {
		t.Fatalf("rollback result = (%+v, %v)", rolledBack, err)
	}
}

func migrationClient(
	t *testing.T,
	name string,
	register func(context.Context, schemaregistry.RegisterRequest) (schemaregistry.RegisterResult, error),
	resolve func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error),
) *schemaregistry.Client {
	t.Helper()
	client, err := schemaregistry.NewClient(&providerStub{
		capabilities: schemaregistry.Capabilities{
			Provider: name, Formats: []schemaregistry.Format{schemaregistry.FormatAvro},
			Lookups: []schemaregistry.LookupKind{schemaregistry.LookupByProviderID}, NumericVersions: true,
		},
		register: register,
		resolve:  resolve,
	}, schemaregistry.Limits{MaxSchemaBytes: 4096, MaxListResults: 10, MaxConcurrent: 2})
	if err != nil {
		t.Fatalf("NewClient(%s) error = %v", name, err)
	}
	return client
}

func migrationResolution(
	schema schemaregistry.Schema,
	id schemaregistry.ProviderID,
	subject schemaregistry.Subject,
	version uint64,
) schemaregistry.ResolveResult {
	return schemaregistry.ResolveResult{
		Schema: schema, ID: id, Subject: subject,
		Version: schemaregistry.Version{Number: version}, Lifecycle: schemaregistry.LifecycleAvailable,
	}
}
