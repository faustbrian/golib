package golib_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/faustbrian/golib/pkg/cloudevents"
	golib "github.com/faustbrian/golib/pkg/cloudevents/adapters/golib"
	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryjsonschema "github.com/faustbrian/golib/pkg/schema-registry/formats/jsonschema"
)

func TestAuditMetadataAdapterSelectsSafeFieldsAndRequiresTrust(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 4, 5, 6, 0, time.UTC)
	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Clock: func() time.Time { return now }, IDGenerator: func() (string, error) { return "audit-1", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := builder.Build(audit.RecordInput{
		OccurredAt: now, Action: "order.create", Outcome: audit.OutcomeSucceeded,
		Actor:   audit.ActorInput{Kind: audit.ActorService, ID: "orders"},
		Subject: audit.SubjectInput{Type: "order", ID: "A-123"},
		Context: audit.ContextInput{TenantID: "tenant-a", CorrelationID: "correlation-1", CausationID: "cause-1"},
		Changes: audit.ChangeSetInput{NoChange: true}, Policy: audit.PolicyMetadata{PolicyID: "audit", Version: "v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	event, report, err := golib.AddAuditMetadata(baseEvent(t), record)
	if err != nil {
		t.Fatalf("add audit metadata = %#v, %v", report, err)
	}
	wantLosses := []string{
		"audit.occurred_at", "audit.recorded_at", "audit.reason_code", "audit.description",
		"audit.actor", "audit.subject", "audit.context.request_id", "audit.context.trace_id",
		"audit.context.idempotency_id", "audit.context.source_service", "audit.context.source_version",
		"audit.context.environment", "audit.context.network_origin", "audit.context.user_agent",
		"audit.changes", "audit.policy", "audit.integrity", "audit.attributes", "audit.redaction_applied",
	}
	if len(report.Losses) != len(wantLosses) {
		t.Fatalf("audit losses = %#v, want fields %#v", report.Losses, wantLosses)
	}
	for index, want := range wantLosses {
		if report.Losses[index].Field != want {
			t.Fatalf("audit loss %d = %#v, want %q", index, report.Losses[index], want)
		}
	}
	if _, err := golib.ExtractAuditMetadata(event, false); !errors.Is(err, golib.ErrUntrustedMetadata) {
		t.Fatalf("untrusted audit metadata error = %v", err)
	}
	metadata, err := golib.ExtractAuditMetadata(event, true)
	if err != nil || metadata.RecordID != record.ID() || metadata.Action != record.Action() ||
		metadata.Outcome != record.Outcome() || metadata.Tenant.Value() != "tenant-a" ||
		metadata.Correlation.CorrelationID.String() != "correlation-1" ||
		metadata.Correlation.CausationID.String() != "cause-1" {
		t.Fatalf("audit metadata = %#v, %v", metadata, err)
	}
	for _, name := range []string{"correlationid", "causationid", "tenantid"} {
		if _, present := event.Extension(name); !present {
			t.Fatalf("audit context extension %s is absent", name)
		}
	}
}

func TestAuditMetadataRoundTripAllowsAbsentTenant(t *testing.T) {
	t.Parallel()

	record := auditRecord(t)
	event, _, err := golib.AddAuditMetadata(baseEvent(t), record)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := golib.ExtractAuditMetadata(event, true)
	if err != nil {
		t.Fatalf("extract tenantless audit metadata: %v", err)
	}
	if metadata.RecordID != record.ID() || metadata.Action != record.Action() ||
		metadata.Outcome != record.Outcome() || metadata.Tenant.Value() != "" {
		t.Fatalf("tenantless audit metadata = %#v", metadata)
	}
}

func TestSchemaAdaptersAreExplicitAndRegistryLookupIsCallerMapped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(ctx, []byte(`{"type":"object","required":["id"]}`))
	if err != nil {
		t.Fatal(err)
	}
	direct := golib.JSONSchemaValidator{URI: "https://schemas.example/order.json", Schema: compiled}
	for name, validator := range map[string]golib.JSONSchemaValidator{
		"empty URI":  {Schema: compiled},
		"nil schema": {URI: "https://schemas.example/order.json"},
	} {
		if err := validator.Validate(ctx, "https://schemas.example/order.json", "application/json", []byte(`{}`)); !errors.Is(err, golib.ErrSchemaMapping) {
			t.Fatalf("%s mapping error = %v", name, err)
		}
	}
	valid := schemaEvent(t, `{"id":1}`)
	if err := cloudevents.ValidateSchema(ctx, valid, direct); err != nil {
		t.Fatal(err)
	}
	invalid := schemaEvent(t, `{}`)
	if err := cloudevents.ValidateSchema(ctx, invalid, direct); !errors.Is(err, golib.ErrSchemaViolation) {
		t.Fatalf("direct schema violation error = %v", err)
	}

	canonicalizer, err := registryjsonschema.New(registryjsonschema.Config{
		MaxSchemaBytes: 1024, MaxTotalSchemaBytes: 2048, MaxPayloadBytes: 1024, MaxResources: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	registrySchema, err := schemaregistry.Compile(ctx, schemaregistry.Definition{
		Format: schemaregistry.FormatJSONSchema, Content: []byte(`{"type":"object","required":["id"]}`),
	}, canonicalizer)
	if err != nil {
		t.Fatal(err)
	}
	subject := schemaregistry.Subject{Name: "orders"}
	resolver := resolverStub{result: schemaregistry.ResolveResult{
		Schema: registrySchema, ID: schemaregistry.ProviderID{Provider: "test", Value: "orders-v1"},
		Subject: subject, Version: schemaregistry.Version{Number: 1}, Lifecycle: schemaregistry.LifecycleAvailable,
	}}
	registry := newSchemaHardeningValidator(t, golib.RegistryJSONSchemaConfig{
		Cache: newSchemaHardeningCache(t, &resolver),
		SchemaLookups: map[string]schemaregistry.Lookup{
			"https://schemas.example/order.json": schemaregistry.Latest(subject),
		},
		Adapter: canonicalizer, AvailabilityPolicy: schemaregistry.FailClosed, Timeout: time.Second,
	})
	if err := cloudevents.ValidateSchema(ctx, valid, registry); err != nil || resolver.calls != 1 {
		t.Fatalf("registry validation = calls %d, %v", resolver.calls, err)
	}
}

func schemaEvent(t *testing.T, payload string) cloudevents.Event {
	t.Helper()
	data, err := cloudevents.NewJSONData([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: "schema-event", Source: "/source", Type: "order.created",
		DataContentType: "application/json", DataSchema: "https://schemas.example/order.json",
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

type resolverStub struct {
	result schemaregistry.ResolveResult
	err    error
	calls  int
}

func (resolver *resolverStub) Resolve(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
	resolver.calls++
	return resolver.result, resolver.err
}
