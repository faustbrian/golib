package golib_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/faustbrian/golib/pkg/cloudevents"
	golib "github.com/faustbrian/golib/pkg/cloudevents/adapters/golib"
	"github.com/faustbrian/golib/pkg/correlation"
	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
	"github.com/faustbrian/golib/pkg/kafka"
	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryjsonschema "github.com/faustbrian/golib/pkg/schema-registry/formats/jsonschema"
	telemetrypropagation "github.com/faustbrian/golib/pkg/telemetry/propagation"
	"github.com/faustbrian/golib/pkg/tenancy"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

func TestMetadataAdapterRejectsEveryMalformedOrUntrustedBoundary(t *testing.T) {
	t.Parallel()

	if _, err := golib.AddCorrelation(cloudevents.Event{}, correlation.Values{}); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid event error = %v", err)
	}
	if _, err := golib.AddCorrelation(baseEvent(t), correlation.Values{CorrelationID: correlation.CorrelationID("bad\nvalue")}); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid correlation addition error = %v", err)
	}
	values := correlation.Values{CorrelationID: correlation.CorrelationID("same")}
	withSame, err := golib.AddCorrelation(baseEvent(t), values)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := golib.AddCorrelation(withSame, values); err != nil {
		t.Fatalf("idempotent correlation addition error = %v", err)
	}

	if extracted, err := golib.ExtractCorrelation(baseEvent(t), true, correlation.Policy{}); err != nil || extracted != (correlation.Values{}) {
		t.Fatalf("absent correlation = %#v, %v", extracted, err)
	}
	for _, name := range []string{"correlationid", "requestid", "causationid"} {
		event := eventWithExtensions(t, map[string]cloudevents.Attribute{name: cloudevents.NewBooleanAttribute(true)})
		if _, err := golib.ExtractCorrelation(event, true, correlation.Policy{}); !errors.Is(err, golib.ErrInvalidAdapterInput) {
			t.Fatalf("invalid %s kind error = %v", name, err)
		}
		invalid, _ := cloudevents.NewStringAttribute("bad.value")
		event = eventWithExtensions(t, map[string]cloudevents.Attribute{name: invalid})
		if _, err := golib.ExtractCorrelation(event, true, correlation.Policy{}); !errors.Is(err, golib.ErrInvalidAdapterInput) {
			t.Fatalf("invalid %s value error = %v", name, err)
		}
	}
	if _, err := golib.AddTenant(baseEvent(t), tenancy.TenantID{}); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid tenant addition error = %v", err)
	}
	if tenant, err := golib.ExtractTenant(baseEvent(t), true); !errors.Is(err, tenancy.ErrTenantMetadataMissing) || tenant.Valid() {
		t.Fatalf("absent tenant = %v, %v", tenant, err)
	}
	if _, err := golib.ExtractTenant(eventWithExtensions(t, map[string]cloudevents.Attribute{"tenantid": cloudevents.NewBooleanAttribute(true)}), true); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid tenant kind error = %v", err)
	}
	invalidTenant, _ := cloudevents.NewStringAttribute("bad?tenant")
	if _, err := golib.ExtractTenant(eventWithExtensions(t, map[string]cloudevents.Attribute{"tenantid": invalidTenant}), true); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid tenant value error = %v", err)
	}
}

func TestTelemetryAdapterCoversNilCollisionBaggageAndTrustedExtraction(t *testing.T) {
	t.Parallel()

	policy, _ := telemetrypropagation.New(telemetrypropagation.Config{
		BaggageEnabled: true, TrustedBaggageKeys: []string{"tenant"}, MaxHeaderBytes: 1024, MaxBaggageItems: 1,
	})
	var nilContext context.Context
	if _, _, err := golib.InjectTraceContext(nilContext, baseEvent(t), policy); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, _, err := golib.InjectTraceContext(context.Background(), baseEvent(t), nil); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("nil policy error = %v", err)
	}
	member, _ := baggage.NewMember("tenant", "a")
	bag, _ := baggage.New(member)
	ctx := baggage.ContextWithBaggage(traceContext(), bag)
	_, report, err := golib.InjectTraceContext(ctx, baseEvent(t), policy)
	if err != nil || len(report.Losses) != 1 {
		t.Fatalf("baggage report = %#v, %v", report, err)
	}
	existing, _ := cloudevents.NewTraceParentAttribute("00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01")
	if _, _, err := golib.InjectTraceContext(traceContext(), eventWithExtensions(t, map[string]cloudevents.Attribute{"traceparent": existing}), policy); !errors.Is(err, golib.ErrMetadataCollision) {
		t.Fatalf("trace collision error = %v", err)
	}
	if got := golib.ExtractTraceContext(nilContext, baseEvent(t), nil, true); got == nil {
		t.Fatal("nil trace extraction context")
	}
	event, _, err := golib.InjectTraceContext(traceContext(), baseEvent(t), policy)
	if err != nil {
		t.Fatal(err)
	}
	if got := trace.SpanContextFromContext(golib.ExtractTraceContext(context.Background(), event, policy, true)); !got.IsValid() {
		t.Fatalf("trusted trace context = %v", got)
	}
}

func TestAuditMetadataRejectsIncompleteMalformedAndCollidingInput(t *testing.T) {
	t.Parallel()

	if _, _, err := golib.AddAuditMetadata(baseEvent(t), audit.Record{}); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("zero audit record error = %v", err)
	}
	record := auditRecord(t)
	if _, _, err := golib.AddAuditMetadata(
		baseEvent(t), auditRecordWithTenant(t, "bad?tenant"),
	); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid outbound audit tenant error = %v", err)
	}
	collision, _ := cloudevents.NewStringAttribute("other")
	if _, _, err := golib.AddAuditMetadata(eventWithExtensions(t, map[string]cloudevents.Attribute{"auditid": collision}), record); !errors.Is(err, golib.ErrMetadataCollision) {
		t.Fatalf("audit collision error = %v", err)
	}
	if metadata, err := golib.ExtractAuditMetadata(baseEvent(t), true); err != nil || metadata != (golib.AuditMetadata{}) {
		t.Fatalf("absent audit metadata = %#v, %v", metadata, err)
	}
	for _, name := range []string{"auditid", "auditaction", "auditoutcome"} {
		if _, err := golib.ExtractAuditMetadata(eventWithExtensions(t, map[string]cloudevents.Attribute{name: cloudevents.NewBooleanAttribute(true)}), true); !errors.Is(err, golib.ErrInvalidAdapterInput) {
			t.Fatalf("invalid audit %s error = %v", name, err)
		}
	}
	auditID, _ := cloudevents.NewStringAttribute("audit-1")
	action, _ := cloudevents.NewStringAttribute("action")
	validTenant, _ := cloudevents.NewStringAttribute("tenant-a")
	for _, outcome := range []string{"bad", "0", "5"} {
		outcomeAttribute, _ := cloudevents.NewStringAttribute(outcome)
		event := eventWithExtensions(t, map[string]cloudevents.Attribute{
			"auditid": auditID, "auditaction": action, "auditoutcome": outcomeAttribute,
			"tenantid": validTenant,
		})
		if _, err := golib.ExtractAuditMetadata(event, true); !errors.Is(err, golib.ErrInvalidAdapterInput) {
			t.Fatalf("audit outcome %q error = %v", outcome, err)
		}
	}
	for name, extensions := range map[string]map[string]cloudevents.Attribute{
		"missing record":  {"auditaction": action, "auditoutcome": outcomeAttribute(t, "1"), "tenantid": validTenant},
		"missing action":  {"auditid": auditID, "auditoutcome": outcomeAttribute(t, "1"), "tenantid": validTenant},
		"missing outcome": {"auditid": auditID, "auditaction": action, "tenantid": validTenant},
	} {
		if _, err := golib.ExtractAuditMetadata(eventWithExtensions(t, extensions), true); !errors.Is(err, golib.ErrInvalidAdapterInput) {
			t.Fatalf("%s audit error = %v", name, err)
		}
	}
	for _, validOutcome := range []string{"1", "2", "3", "4"} {
		event := eventWithExtensions(t, map[string]cloudevents.Attribute{
			"auditid": auditID, "auditaction": action, "auditoutcome": outcomeAttribute(t, validOutcome),
			"tenantid": validTenant,
		})
		if _, err := golib.ExtractAuditMetadata(event, true); err != nil {
			t.Fatalf("valid audit outcome %s error = %v", validOutcome, err)
		}
	}
	outcome, _ := cloudevents.NewStringAttribute("1")
	badCorrelation, _ := cloudevents.NewStringAttribute("bad.value")
	if _, err := golib.ExtractAuditMetadata(eventWithExtensions(t, map[string]cloudevents.Attribute{
		"auditid": auditID, "auditaction": action, "auditoutcome": outcome, "correlationid": badCorrelation,
	}), true); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("audit correlation error = %v", err)
	}
	badTenant, _ := cloudevents.NewStringAttribute("bad?tenant")
	if _, err := golib.ExtractAuditMetadata(eventWithExtensions(t, map[string]cloudevents.Attribute{
		"auditid": auditID, "auditaction": action, "auditoutcome": outcome, "tenantid": badTenant,
	}), true); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("audit tenant error = %v", err)
	}
}

func TestSchemaValidatorsRejectEveryConfigurationResolutionAndPayloadFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	compiler, _ := jsonschema.NewCompiler()
	compiled, _ := compiler.Compile(ctx, []byte(`{"type":"object"}`))
	for _, validator := range []golib.JSONSchemaValidator{{}, {URI: "expected", Schema: compiled}} {
		if err := validator.Validate(ctx, "other", "application/json", []byte(`{}`)); !errors.Is(err, golib.ErrSchemaMapping) {
			t.Fatalf("direct mapping error = %v", err)
		}
	}
	direct := golib.JSONSchemaValidator{URI: "schema", Schema: compiled}
	if err := direct.Validate(ctx, "schema", "text/plain", []byte(`{}`)); !errors.Is(err, golib.ErrSchemaMapping) {
		t.Fatalf("direct content type error = %v", err)
	}
	if err := direct.Validate(ctx, "schema", "application/json", []byte("{")); err == nil {
		t.Fatal("direct parse error = nil")
	}

	adapter, _ := registryjsonschema.New(registryjsonschema.Config{MaxSchemaBytes: 1024, MaxTotalSchemaBytes: 2048, MaxPayloadBytes: 1024, MaxResources: 1})
	validSchema := registrySchema(t, adapter, `{"type":"object","required":["id"]}`)
	subject := schemaregistry.Subject{Name: "subject"}
	lookup := schemaregistry.Latest(subject)
	if err := (golib.RegistryJSONSchemaValidator{}).Validate(ctx, "schema", "application/json", []byte(`{}`)); !errors.Is(err, golib.ErrSchemaMapping) {
		t.Fatalf("zero-value registry config error = %v", err)
	}
	if _, err := golib.NewRegistryJSONSchemaValidator(golib.RegistryJSONSchemaConfig{
		SchemaLookups: map[string]schemaregistry.Lookup{"schema": lookup}, Adapter: adapter,
		AvailabilityPolicy: schemaregistry.FailClosed, Timeout: time.Second,
	}); !errors.Is(err, golib.ErrSchemaMapping) {
		t.Fatalf("nil-cache registry config error = %v", err)
	}
	validResult := schemaregistry.ResolveResult{
		Schema: validSchema, ID: schemaregistry.ProviderID{Provider: "test", Value: "subject-v1"},
		Subject: subject, Version: schemaregistry.Version{Number: 1}, Lifecycle: schemaregistry.LifecycleAvailable,
	}
	resolver := &resolverStub{result: validResult}
	newRegistry := func(mappings map[string]schemaregistry.Lookup) golib.RegistryJSONSchemaValidator {
		return newSchemaHardeningValidator(t, golib.RegistryJSONSchemaConfig{
			Cache: newSchemaHardeningCache(t, resolver), SchemaLookups: mappings, Adapter: adapter,
			AvailabilityPolicy: schemaregistry.FailClosed, Timeout: time.Second,
		})
	}
	registry := newRegistry(map[string]schemaregistry.Lookup{"schema": lookup})
	if err := registry.Validate(ctx, "schema", "text/plain", []byte(`{}`)); !errors.Is(err, golib.ErrSchemaMapping) {
		t.Fatalf("registry content type error = %v", err)
	}
	if err := registry.Validate(ctx, "other", "application/json", []byte(`{}`)); !errors.Is(err, golib.ErrSchemaMapping) {
		t.Fatalf("registry lookup error = %v", err)
	}
	resolver.err = errors.New("resolve")
	registry = newRegistry(map[string]schemaregistry.Lookup{"schema": lookup})
	if err := registry.Validate(ctx, "schema", "application/json", []byte(`{}`)); !errors.Is(err, resolver.err) {
		t.Fatalf("registry resolver error = %v", err)
	}
	resolver.err = nil
	resolver.result = schemaregistry.ResolveResult{}
	registry = newRegistry(map[string]schemaregistry.Lookup{"schema": lookup})
	if err := registry.Validate(ctx, "schema", "application/json", []byte(`{}`)); !errors.Is(err, schemaregistry.ErrResolutionMismatch) {
		t.Fatalf("registry identity error = %v", err)
	}
	resolver.result = validResult
	registry = newRegistry(map[string]schemaregistry.Lookup{"schema": lookup})
	if err := registry.Validate(ctx, "schema", "application/json", []byte(`{}`)); !errors.Is(err, golib.ErrSchemaViolation) {
		t.Fatalf("registry violation error = %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := registry.Validate(cancelled, "schema", "application/json", []byte(`{"id":1}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("registry cancellation error = %v", err)
	}
}

func TestKafkaAdapterPropagatesBindingDecodeFailuresAndAcceptsEqualKeys(t *testing.T) {
	t.Parallel()

	if _, err := golib.EncodeKafka(baseEvent(t), cloudevents.BatchMode, golib.KafkaTransport{}); !errors.Is(err, cloudevents.ErrUnsupportedMode) {
		t.Fatalf("binding encode error = %v", err)
	}
	partition, _ := cloudevents.NewPartitionKeyAttribute("key")
	event := eventWithExtensions(t, map[string]cloudevents.Attribute{"partitionkey": partition})
	if _, err := golib.EncodeKafka(event, cloudevents.BinaryMode, golib.KafkaTransport{Key: []byte("key")}); err != nil {
		t.Fatalf("equal key error = %v", err)
	}
	if _, _, err := golib.DecodeKafka(kafka.ConsumedRecord{}, cloudevents.Limits{}); !errors.Is(err, cloudevents.ErrLimitExceeded) {
		t.Fatalf("binding decode error = %v", err)
	}
}

func TestKafkaAdapterRejectsOversizedMetadataBeforeCopyingIt(t *testing.T) {
	record := kafka.ConsumedRecord{Key: make([]byte, 1<<20)}
	limits := cloudevents.DefaultLimits()
	limits.MaxKafkaKeyBytes = 1

	allocations := testing.AllocsPerRun(100, func() {
		_, _, err := golib.DecodeKafka(record, limits)
		if !errors.Is(err, cloudevents.ErrLimitExceeded) {
			panic("oversized Kafka key was not rejected")
		}
	})
	if allocations != 0 {
		t.Fatalf("oversized Kafka key allocations = %v, want 0", allocations)
	}

	record = kafka.ConsumedRecord{Headers: make([]kafka.Header, 2)}
	limits = cloudevents.DefaultLimits()
	limits.MaxKafkaHeaders = 1
	allocations = testing.AllocsPerRun(100, func() {
		_, _, err := golib.DecodeKafka(record, limits)
		if !errors.Is(err, cloudevents.ErrLimitExceeded) {
			panic("oversized Kafka header count was not rejected")
		}
	})
	if allocations != 0 {
		t.Fatalf("oversized Kafka header allocations = %v, want 0", allocations)
	}

	exact := kafka.ConsumedRecord{Headers: []kafka.Header{
		{Key: "ce_specversion", Value: []byte("1.0")},
		{Key: "ce_id", Value: []byte("1")},
		{Key: "ce_source", Value: []byte("/")},
		{Key: "ce_type", Value: []byte("x")},
	}}
	limits = cloudevents.DefaultLimits()
	limits.MaxKafkaHeaders = len(exact.Headers)
	if _, _, err := golib.DecodeKafka(exact, limits); err != nil {
		t.Fatalf("exact Kafka header count error = %v", err)
	}
}

func traceContext() context.Context {
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:  trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}, TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithRemoteSpanContext(context.Background(), spanContext)
}

func eventWithExtensions(t *testing.T, extensions map[string]cloudevents.Attribute) cloudevents.Event {
	t.Helper()
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: "event-1", Source: "/source", Type: "example.created",
		DataContentType: "application/octet-stream", Extensions: extensions,
	}, cloudevents.NewBinaryData([]byte("body")))
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func auditRecord(t *testing.T) audit.Record {
	return auditRecordWithTenant(t, "")
}

func auditRecordWithTenant(t *testing.T, tenant string) audit.Record {
	t.Helper()
	now := time.Now()
	builder, _ := audit.NewBuilder(audit.BuilderConfig{Clock: func() time.Time { return now }, IDGenerator: func() (string, error) { return "audit-1", nil }})
	record, err := builder.Build(audit.RecordInput{
		OccurredAt: now, Action: "action", Outcome: audit.OutcomeSucceeded,
		Actor:   audit.ActorInput{Kind: audit.ActorSystem, ID: "system"},
		Subject: audit.SubjectInput{Type: "subject", ID: "1"}, Changes: audit.ChangeSetInput{NoChange: true},
		Context: audit.ContextInput{TenantID: tenant},
		Policy:  audit.PolicyMetadata{PolicyID: "policy", Version: "v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func registrySchema(t *testing.T, adapter *registryjsonschema.Adapter, raw string) schemaregistry.Schema {
	t.Helper()
	schema, err := schemaregistry.Compile(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatJSONSchema, Content: []byte(raw),
	}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func outcomeAttribute(t *testing.T, value string) cloudevents.Attribute {
	t.Helper()
	attribute, err := cloudevents.NewStringAttribute(value)
	if err != nil {
		t.Fatal(err)
	}
	return attribute
}
