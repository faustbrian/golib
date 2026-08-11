//go:build integration

package confluent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryavro "github.com/faustbrian/golib/pkg/schema-registry/formats/avro"
	registryjsonschema "github.com/faustbrian/golib/pkg/schema-registry/formats/jsonschema"
	registryprotobuf "github.com/faustbrian/golib/pkg/schema-registry/formats/protobuf"
	"github.com/faustbrian/golib/pkg/schema-registry/providers/confluent"
	"github.com/twmb/franz-go/pkg/sr"
)

func TestProviderAgainstConfluentAndIndependentClient(t *testing.T) {
	endpoint := os.Getenv("SCHEMA_REGISTRY_CONFLUENT_INTEGRATION_ENDPOINT")
	if endpoint == "" {
		t.Fatal("SCHEMA_REGISTRY_CONFLUENT_INTEGRATION_ENDPOINT is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	subject := fmt.Sprintf("golib-schema-registry-%d-value", time.Now().UnixNano())
	independent, err := sr.NewClient(sr.URLs(endpoint), sr.HTTPClient(&http.Client{Timeout: 10 * time.Second}))
	if err != nil {
		t.Fatalf("construct independent client: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = independent.DeleteSubject(cleanupCtx, subject, sr.SoftDelete)
		_, _ = independent.DeleteSubject(cleanupCtx, subject, sr.HardDelete)
	})

	canonicalizer := registryavro.New(64 << 10)
	jsonSchemaCanonicalizer, err := registryjsonschema.New(registryjsonschema.Config{
		Dialect:             registryjsonschema.Draft202012,
		MaxSchemaBytes:      64 << 10,
		MaxTotalSchemaBytes: 64 << 10,
		MaxPayloadBytes:     64 << 10,
		MaxResources:        8,
	})
	if err != nil {
		t.Fatalf("construct JSON Schema canonicalizer: %v", err)
	}
	protobufCanonicalizer, err := registryprotobuf.New(registryprotobuf.Config{
		Filename:       "root.proto",
		Imports:        map[string]string{"shared.proto": protobufDependency},
		MaxSchemaBytes: 64 << 10,
		MaxImports:     8,
	})
	if err != nil {
		t.Fatalf("construct Protobuf canonicalizer: %v", err)
	}
	provider, err := confluent.New(confluent.Config{
		Endpoint:            endpoint,
		Scope:               "integration",
		Transport:           http.DefaultTransport,
		AllowHTTPForTesting: true,
		RequestTimeout:      10 * time.Second,
		MaxResponseBytes:    256 << 10,
		MaxAttempts:         3,
		MaxConcurrent:       4,
		RetryDelay:          10 * time.Millisecond,
		ReferenceLimits:     schemaregistry.GraphLimits{MaxSchemas: 16, MaxDepth: 8, MaxReferences: 32},
		Canonicalizers: map[schemaregistry.Format]schemaregistry.Canonicalizer{
			schemaregistry.FormatAvro:       canonicalizer,
			schemaregistry.FormatJSONSchema: jsonSchemaCanonicalizer,
			schemaregistry.FormatProtobuf:   protobufCanonicalizer,
		},
	})
	if err != nil {
		t.Fatalf("construct provider: %v", err)
	}

	definition := schemaregistry.Definition{
		Format:  schemaregistry.FormatAvro,
		Content: []byte(`{"type":"record","name":"Order","fields":[{"name":"id","type":"string"}]}`),
	}
	schema, err := schemaregistry.Compile(ctx, definition, canonicalizer)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	registered, err := provider.Register(ctx, schemaregistry.RegisterRequest{
		Subject: schemaregistry.Subject{Name: subject},
		Schema:  schema,
	})
	if err != nil {
		t.Fatalf("register schema: %v", err)
	}
	if registered.Outcome != schemaregistry.RegistrationUnknown || registered.ID.Provider != confluent.ProviderName {
		t.Fatalf("register result = %+v", registered)
	}
	id, err := strconv.Atoi(registered.ID.Value)
	if err != nil || id <= 0 {
		t.Fatalf("provider ID = %q, %v", registered.ID.Value, err)
	}

	independentSchema, err := independent.LookupSchema(ctx, subject, sr.Schema{Schema: string(definition.Content), Type: sr.TypeAvro})
	if err != nil {
		t.Fatalf("independent lookup: %v", err)
	}
	if independentSchema.ID != id || independentSchema.Version != 1 {
		t.Fatalf("independent identity = id %d version %d", independentSchema.ID, independentSchema.Version)
	}

	existing, err := provider.Register(ctx, schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Name: subject}, Schema: schema})
	if err != nil {
		t.Fatalf("register existing schema: %v", err)
	}
	if existing.Outcome != schemaregistry.RegistrationExisting || existing.ID != registered.ID || existing.Version.Number != 1 {
		t.Fatalf("existing result = %+v", existing)
	}
	resolved, err := provider.Resolve(ctx, schemaregistry.ByProviderID(registered.ID))
	if err != nil {
		t.Fatalf("resolve by ID: %v", err)
	}
	if resolved.Schema.Fingerprint() != schema.Fingerprint() || resolved.ID != registered.ID {
		t.Fatalf("resolved identity = %+v fingerprint %s", resolved.ID, resolved.Schema.Fingerprint())
	}

	verifyGlobalCompatibilityFallback(t, ctx, independent, provider, schema, subject)
	for _, mode := range []struct {
		independent sr.CompatibilityLevel
		portable    schemaregistry.CompatibilityMode
	}{
		{sr.CompatBackward, schemaregistry.CompatibilityBackward},
		{sr.CompatBackwardTransitive, schemaregistry.CompatibilityBackwardTransitive},
		{sr.CompatForward, schemaregistry.CompatibilityForward},
		{sr.CompatForwardTransitive, schemaregistry.CompatibilityForwardTransitive},
		{sr.CompatFull, schemaregistry.CompatibilityFull},
		{sr.CompatFullTransitive, schemaregistry.CompatibilityFullTransitive},
		{sr.CompatNone, schemaregistry.CompatibilityNone},
	} {
		configuration := independent.SetCompatibility(ctx, sr.SetCompatibility{Level: mode.independent}, subject)
		if len(configuration) != 1 || configuration[0].Err != nil {
			t.Fatalf("set independent compatibility %s = %+v", mode.portable, configuration)
		}
		compatible, compatibilityErr := provider.CheckCompatibility(ctx, schemaregistry.CompatibilityRequest{
			Subject: schemaregistry.Subject{Name: subject}, Candidate: schema, Mode: mode.portable,
		})
		if compatibilityErr != nil || !compatible.Supported || !compatible.Compatible {
			t.Fatalf("compatibility %s = %+v, %v", mode.portable, compatible, compatibilityErr)
		}
	}

	page, err := provider.List(ctx, schemaregistry.ListRequest{SubjectPrefix: subject, Limit: 1})
	if err != nil || len(page.Schemas) != 1 || page.Schemas[0].Subject.Name != subject {
		t.Fatalf("list = %+v, %v", page, err)
	}

	payload := []byte("payload")
	framer, err := confluent.NewClassicFramer("integration", len(payload))
	if err != nil {
		t.Fatalf("construct framer: %v", err)
	}
	ours, err := framer.Frame(ctx, registered.ID, payload)
	if err != nil {
		t.Fatalf("frame payload: %v", err)
	}
	var independentHeader sr.ConfluentHeader
	want, err := independentHeader.AppendEncode(nil, id, nil)
	if err != nil {
		t.Fatalf("independent frame: %v", err)
	}
	want = append(want, payload...)
	if !bytes.Equal(ours, want) {
		t.Fatalf("wire frame = %x, independent = %x", ours, want)
	}

	verifyProtobufReferences(t, ctx, independent, provider, protobufCanonicalizer, subject)
	verifyJSONSchemaCodecAndWire(t, ctx, independent, provider, jsonSchemaCanonicalizer, subject)
	for _, corpus := range []struct {
		filename      string
		format        schemaregistry.Format
		schemaType    sr.SchemaType
		canonicalizer schemaregistry.Canonicalizer
	}{
		{"testdata/compatibility-corpus.json", schemaregistry.FormatAvro, sr.TypeAvro, canonicalizer},
		{"testdata/jsonschema-compatibility-corpus.json", schemaregistry.FormatJSONSchema, sr.TypeJSON, jsonSchemaCanonicalizer},
		{"testdata/protobuf-compatibility-corpus.json", schemaregistry.FormatProtobuf, sr.TypeProtobuf, protobufCanonicalizer},
	} {
		verifyCompatibilityCorpus(t, ctx, independent, provider, corpus.filename, corpus.format, corpus.schemaType, corpus.canonicalizer, subject)
	}
}

func verifyGlobalCompatibilityFallback(
	t *testing.T,
	ctx context.Context,
	independent *sr.Client,
	provider *confluent.Provider,
	schema schemaregistry.Schema,
	prefix string,
) {
	t.Helper()
	subject := prefix + "-global-policy"
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = independent.DeleteSubject(cleanupCtx, subject, sr.SoftDelete)
		_, _ = independent.DeleteSubject(cleanupCtx, subject, sr.HardDelete)
	})
	if _, err := independent.CreateSchema(ctx, subject, sr.Schema{
		Schema: string(schema.Definition().Content), Type: sr.TypeAvro,
	}); err != nil {
		t.Fatalf("register global-policy fixture: %v", err)
	}
	result, err := provider.CheckCompatibility(ctx, schemaregistry.CompatibilityRequest{
		Subject:   schemaregistry.Subject{Name: subject},
		Candidate: schema,
		Mode:      schemaregistry.CompatibilityBackward,
	})
	if err != nil || !result.Supported || !result.Compatible {
		t.Fatalf("global compatibility fallback = (%+v, %v)", result, err)
	}
}

func verifyJSONSchemaCodecAndWire(
	t *testing.T,
	ctx context.Context,
	independent *sr.Client,
	provider *confluent.Provider,
	adapter *registryjsonschema.Adapter,
	prefix string,
) {
	t.Helper()
	subject := prefix + "-json-wire"
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = independent.DeleteSubject(cleanupCtx, subject, sr.SoftDelete)
		_, _ = independent.DeleteSubject(cleanupCtx, subject, sr.HardDelete)
	})

	definition := schemaregistry.Definition{
		Format:  schemaregistry.FormatJSONSchema,
		Content: []byte("{\"type\":\"object\",\"additionalProperties\":false,\"required\":[\"id\"],\"properties\":{\"id\":{\"type\":\"integer\"}}}"),
	}
	schema, err := schemaregistry.Compile(ctx, definition, adapter)
	if err != nil {
		t.Fatalf("compile JSON Schema wire fixture: %v", err)
	}
	registered, err := provider.Register(ctx, schemaregistry.RegisterRequest{
		Subject: schemaregistry.Subject{Name: subject},
		Schema:  schema,
	})
	if err != nil {
		t.Fatalf("register JSON Schema wire fixture: %v", err)
	}
	id, err := strconv.Atoi(registered.ID.Value)
	if err != nil || id <= 0 {
		t.Fatalf("JSON Schema provider ID = %q, %v", registered.ID.Value, err)
	}
	lookup, err := independent.LookupSchema(ctx, subject, sr.Schema{Schema: string(definition.Content), Type: sr.TypeJSON})
	if err != nil || lookup.ID != id {
		t.Fatalf("independent JSON Schema identity = (%+v, %v), want ID %d", lookup, err, id)
	}

	value := map[string]any{"id": 7}
	payload, err := adapter.Encode(ctx, schema, value)
	if err != nil {
		t.Fatalf("encode JSON Schema payload: %v", err)
	}
	independentPayload, err := json.Marshal(value)
	if err != nil || !bytes.Equal(payload, independentPayload) {
		t.Fatalf("JSON payload = (%s, %v), independent = %s", payload, err, independentPayload)
	}
	framer, err := confluent.NewClassicFramer("integration", len(payload))
	if err != nil {
		t.Fatalf("construct JSON Schema framer: %v", err)
	}
	ours, err := framer.Frame(ctx, registered.ID, payload)
	if err != nil {
		t.Fatalf("frame JSON Schema payload: %v", err)
	}
	var header sr.ConfluentHeader
	want, err := header.AppendEncode(nil, id, nil)
	if err != nil {
		t.Fatalf("independent JSON Schema frame: %v", err)
	}
	want = append(want, independentPayload...)
	if !bytes.Equal(ours, want) {
		t.Fatalf("JSON Schema wire frame = %x, independent = %x", ours, want)
	}
}

func verifyCompatibilityCorpus(
	t *testing.T,
	ctx context.Context,
	independent *sr.Client,
	provider *confluent.Provider,
	filename string,
	format schemaregistry.Format,
	schemaType sr.SchemaType,
	canonicalizer schemaregistry.Canonicalizer,
	prefix string,
) {
	t.Helper()
	encoded, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Name       string   `json:"name"`
			Mode       string   `json:"mode"`
			History    []string `json:"history"`
			Candidate  string   `json:"candidate"`
			Compatible bool     `json:"compatible"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(encoded, &corpus); err != nil {
		t.Fatal(err)
	}
	modes := map[string]struct {
		independent sr.CompatibilityLevel
		portable    schemaregistry.CompatibilityMode
	}{
		"BACKWARD":            {sr.CompatBackward, schemaregistry.CompatibilityBackward},
		"BACKWARD_TRANSITIVE": {sr.CompatBackwardTransitive, schemaregistry.CompatibilityBackwardTransitive},
		"FORWARD":             {sr.CompatForward, schemaregistry.CompatibilityForward},
		"FORWARD_TRANSITIVE":  {sr.CompatForwardTransitive, schemaregistry.CompatibilityForwardTransitive},
		"FULL":                {sr.CompatFull, schemaregistry.CompatibilityFull},
		"FULL_TRANSITIVE":     {sr.CompatFullTransitive, schemaregistry.CompatibilityFullTransitive},
		"NONE":                {sr.CompatNone, schemaregistry.CompatibilityNone},
	}
	for index, test := range corpus.Cases {
		mode, found := modes[test.Mode]
		if !found || test.Name == "" || len(test.History) == 0 || test.Candidate == "" {
			t.Fatalf("invalid compatibility corpus case %+v", test)
		}
		subject := fmt.Sprintf("%s-%s-compat-%d", prefix, format, index)
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cleanupCancel()
			_, _ = independent.DeleteSubject(cleanupCtx, subject, sr.SoftDelete)
			_, _ = independent.DeleteSubject(cleanupCtx, subject, sr.HardDelete)
		})
		configured := independent.SetCompatibility(ctx, sr.SetCompatibility{Level: sr.CompatNone}, subject)
		if len(configured) != 1 || configured[0].Err != nil {
			t.Fatalf("configure NONE for %s = %+v", test.Name, configured)
		}
		for _, history := range test.History {
			if _, err := independent.CreateSchema(ctx, subject, sr.Schema{Schema: history, Type: schemaType}); err != nil {
				t.Fatalf("register history for %s: %v", test.Name, err)
			}
		}
		configured = independent.SetCompatibility(ctx, sr.SetCompatibility{Level: mode.independent}, subject)
		if len(configured) != 1 || configured[0].Err != nil {
			t.Fatalf("configure %s = %+v", test.Name, configured)
		}
		candidate, err := schemaregistry.Compile(ctx, schemaregistry.Definition{
			Format: format, Content: []byte(test.Candidate),
		}, canonicalizer)
		if err != nil {
			t.Fatalf("compile candidate %s: %v", test.Name, err)
		}
		result, err := provider.CheckCompatibility(ctx, schemaregistry.CompatibilityRequest{
			Subject: schemaregistry.Subject{Name: subject}, Candidate: candidate, Mode: mode.portable,
		})
		if err != nil || !result.Supported || result.Compatible != test.Compatible {
			t.Fatalf("compatibility %s = (%+v, %v), want %t", test.Name, result, err, test.Compatible)
		}
	}
}

const (
	protobufDependency = `syntax = "proto3"; package integration; message Shared { string id = 1; }`
	protobufRoot       = `syntax = "proto3"; package integration; import "shared.proto"; message Envelope { Shared value = 1; }`
)

func verifyProtobufReferences(
	t *testing.T,
	ctx context.Context,
	independent *sr.Client,
	provider *confluent.Provider,
	canonicalizer schemaregistry.Canonicalizer,
	prefix string,
) {
	t.Helper()
	dependencySubject := prefix + "-dependency"
	rootSubject := prefix + "-root"
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, subject := range []string{rootSubject, dependencySubject} {
			_, _ = independent.DeleteSubject(cleanupCtx, subject, sr.SoftDelete)
			_, _ = independent.DeleteSubject(cleanupCtx, subject, sr.HardDelete)
		}
	})

	dependency, err := independent.CreateSchema(ctx, dependencySubject, sr.Schema{Schema: protobufDependency, Type: sr.TypeProtobuf})
	if err != nil {
		t.Fatalf("register dependency with independent client: %v", err)
	}
	root, err := independent.CreateSchema(ctx, rootSubject, sr.Schema{
		Schema: protobufRoot,
		Type:   sr.TypeProtobuf,
		References: []sr.SchemaReference{{
			Name:    "shared.proto",
			Subject: dependencySubject,
			Version: dependency.Version,
		}},
	})
	if err != nil {
		t.Fatalf("register root with independent client: %v", err)
	}
	resolved, err := provider.Resolve(ctx, schemaregistry.AtVersion(
		schemaregistry.Subject{Name: rootSubject}, schemaregistry.Version{Number: uint64(root.Version)},
	))
	if err != nil {
		t.Fatalf("resolve referenced Protobuf schema: %v", err)
	}
	references := resolved.Schema.Definition().References
	if len(references) != 1 || references[0].Name != "shared.proto" || references[0].Fingerprint == (schemaregistry.Fingerprint{}) {
		t.Fatalf("resolved references = %+v", references)
	}

	compiled, err := schemaregistry.Compile(ctx, schemaregistry.Definition{
		Format:  schemaregistry.FormatProtobuf,
		Content: []byte(protobufRoot),
		References: []schemaregistry.Reference{{
			Name:        "shared.proto",
			Subject:     dependencySubject,
			Version:     uint64(dependency.Version),
			Fingerprint: references[0].Fingerprint,
		}},
	}, canonicalizer)
	if err != nil || compiled.Fingerprint() != resolved.Schema.Fingerprint() {
		t.Fatalf("portable referenced fingerprint = %s, resolved = %s, %v", compiled.Fingerprint(), resolved.Schema.Fingerprint(), err)
	}

	payload := []byte("protobuf")
	framer, err := confluent.NewProtobufFramer("integration", len(payload), 4)
	if err != nil {
		t.Fatalf("construct Protobuf framer: %v", err)
	}
	id := schemaregistry.ProviderID{Provider: confluent.ProviderName, Scope: "integration", Value: strconv.Itoa(root.ID)}
	ours, err := framer.FrameMessage(ctx, id, []int{0}, payload)
	if err != nil {
		t.Fatalf("frame Protobuf payload: %v", err)
	}
	var independentHeader sr.ConfluentHeader
	want, err := independentHeader.AppendEncode(nil, root.ID, []int{0})
	if err != nil {
		t.Fatalf("independent Protobuf frame: %v", err)
	}
	want = append(want, payload...)
	if !bytes.Equal(ours, want) {
		t.Fatalf("Protobuf wire frame = %x, independent = %x", ours, want)
	}
}
