//go:build integration

package confluent_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryavro "github.com/faustbrian/golib/pkg/schema-registry/formats/avro"
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
			schemaregistry.FormatAvro:     canonicalizer,
			schemaregistry.FormatProtobuf: protobufCanonicalizer,
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

	configuration := independent.SetCompatibility(ctx, sr.SetCompatibility{Level: sr.CompatBackward}, subject)
	if len(configuration) != 1 || configuration[0].Err != nil {
		t.Fatalf("set independent compatibility = %+v", configuration)
	}
	compatible, err := provider.CheckCompatibility(ctx, schemaregistry.CompatibilityRequest{
		Subject:   schemaregistry.Subject{Name: subject},
		Candidate: schema,
		Mode:      schemaregistry.CompatibilityBackward,
	})
	if err != nil || !compatible.Supported || !compatible.Compatible {
		t.Fatalf("compatibility = %+v, %v", compatible, err)
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
