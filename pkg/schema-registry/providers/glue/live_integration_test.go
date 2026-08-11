//go:build liveintegration

package glue_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	awsglue "github.com/aws/aws-sdk-go-v2/service/glue"
	"github.com/aws/aws-sdk-go-v2/service/glue/types"
	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryavro "github.com/faustbrian/golib/pkg/schema-registry/formats/avro"
	registryglue "github.com/faustbrian/golib/pkg/schema-registry/providers/glue"
)

var errLiveIntegrationWouldRegister = errors.New("live integration test refused to register a new schema version")

type readOnlyGlue struct{ client *awsglue.Client }

func (client readOnlyGlue) GetSchemaByDefinition(ctx context.Context, input *awsglue.GetSchemaByDefinitionInput, options ...func(*awsglue.Options)) (*awsglue.GetSchemaByDefinitionOutput, error) {
	return client.client.GetSchemaByDefinition(ctx, input, options...)
}

func (readOnlyGlue) RegisterSchemaVersion(context.Context, *awsglue.RegisterSchemaVersionInput, ...func(*awsglue.Options)) (*awsglue.RegisterSchemaVersionOutput, error) {
	return nil, errLiveIntegrationWouldRegister
}

func (client readOnlyGlue) GetSchemaVersion(ctx context.Context, input *awsglue.GetSchemaVersionInput, options ...func(*awsglue.Options)) (*awsglue.GetSchemaVersionOutput, error) {
	return client.client.GetSchemaVersion(ctx, input, options...)
}

func TestProviderAgainstLiveAWSGlueService(t *testing.T) {
	region := requiredEnvironment(t, "SCHEMA_REGISTRY_GLUE_INTEGRATION_REGION")
	registry := requiredEnvironment(t, "SCHEMA_REGISTRY_GLUE_INTEGRATION_REGISTRY")
	name := requiredEnvironment(t, "SCHEMA_REGISTRY_GLUE_INTEGRATION_SCHEMA")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		t.Fatalf("load AWS configuration: %v", err)
	}
	client := awsglue.NewFromConfig(awsConfig)
	subject := schemaregistry.Subject{Registry: registry, Name: name}
	schemaID := &types.SchemaId{RegistryName: &registry, SchemaName: &name}
	latest := true
	official, err := client.GetSchemaVersion(ctx, &awsglue.GetSchemaVersionInput{
		SchemaId:            schemaID,
		SchemaVersionNumber: &types.SchemaVersionNumber{LatestVersion: latest},
	})
	if err != nil {
		t.Fatalf("resolve with official AWS SDK: %v", err)
	}
	if official.DataFormat != types.DataFormatAvro || official.SchemaDefinition == nil || official.SchemaVersionId == nil || official.VersionNumber == nil {
		t.Fatalf("integration schema must be a complete AVRO schema: %+v", official)
	}

	provider, err := registryglue.New(registryglue.Config{
		API: readOnlyGlue{client: client}, Scope: region + ":" + registry,
		RequestTimeout: 10 * time.Second, MaxConcurrent: 4,
		Canonicalizers: map[schemaregistry.Format]schemaregistry.Canonicalizer{
			schemaregistry.FormatAvro: registryavro.New(170_000),
		},
	})
	if err != nil {
		t.Fatalf("construct provider: %v", err)
	}

	resolved, err := provider.Resolve(ctx, schemaregistry.Latest(subject))
	if err != nil {
		t.Fatalf("resolve latest through adapter: %v", err)
	}
	if resolved.ID.Value != *official.SchemaVersionId || resolved.Version.Number != uint64(*official.VersionNumber) {
		t.Fatalf("adapter identity = %+v version %+v, official ID = %s version = %d", resolved.ID, resolved.Version, *official.SchemaVersionId, *official.VersionNumber)
	}
	byID, err := provider.Resolve(ctx, schemaregistry.ByProviderID(resolved.ID))
	if err != nil {
		t.Fatalf("resolve by ID through adapter: %v", err)
	}
	if byID.Schema.Fingerprint() != resolved.Schema.Fingerprint() || byID.ID != resolved.ID {
		t.Fatalf("resolve by ID = %+v fingerprint %s", byID.ID, byID.Schema.Fingerprint())
	}
	registered, err := provider.Register(ctx, schemaregistry.RegisterRequest{Subject: subject, Schema: resolved.Schema})
	if err != nil {
		if errors.Is(err, errLiveIntegrationWouldRegister) {
			t.Fatal("AWS did not find its latest schema definition; no registration was attempted")
		}
		t.Fatalf("idempotent registration lookup: %v", err)
	}
	if registered.Outcome != schemaregistry.RegistrationExisting || registered.ID != resolved.ID {
		t.Fatalf("idempotent registration = %+v", registered)
	}
}

func requiredEnvironment(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	return value
}
