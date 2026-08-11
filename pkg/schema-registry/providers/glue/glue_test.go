package glue_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	awsglue "github.com/aws/aws-sdk-go-v2/service/glue"
	"github.com/aws/aws-sdk-go-v2/service/glue/types"
	"github.com/aws/smithy-go"
	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryglue "github.com/faustbrian/golib/pkg/schema-registry/providers/glue"
)

const schemaVersionID = "123e4567-e89b-12d3-a456-426614174000"

type canonicalizerFunc func(context.Context, schemaregistry.Definition) ([]byte, error)

func (fn canonicalizerFunc) Canonicalize(ctx context.Context, definition schemaregistry.Definition) ([]byte, error) {
	return fn(ctx, definition)
}

type apiStub struct {
	getByDefinition func(context.Context, *awsglue.GetSchemaByDefinitionInput) (*awsglue.GetSchemaByDefinitionOutput, error)
	register        func(context.Context, *awsglue.RegisterSchemaVersionInput) (*awsglue.RegisterSchemaVersionOutput, error)
	getVersion      func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error)
}

func (api *apiStub) GetSchemaByDefinition(ctx context.Context, input *awsglue.GetSchemaByDefinitionInput, _ ...func(*awsglue.Options)) (*awsglue.GetSchemaByDefinitionOutput, error) {
	return api.getByDefinition(ctx, input)
}

func (api *apiStub) RegisterSchemaVersion(ctx context.Context, input *awsglue.RegisterSchemaVersionInput, _ ...func(*awsglue.Options)) (*awsglue.RegisterSchemaVersionOutput, error) {
	return api.register(ctx, input)
}

func (api *apiStub) GetSchemaVersion(ctx context.Context, input *awsglue.GetSchemaVersionInput, _ ...func(*awsglue.Options)) (*awsglue.GetSchemaVersionOutput, error) {
	return api.getVersion(ctx, input)
}

func TestProviderPreservesGlueIdentityLifecycleAndUnknownCreationOutcome(t *testing.T) {
	t.Parallel()

	api := &apiStub{
		getByDefinition: func(_ context.Context, input *awsglue.GetSchemaByDefinitionInput) (*awsglue.GetSchemaByDefinitionOutput, error) {
			if value(input.SchemaId.RegistryName) != "events" || value(input.SchemaId.SchemaName) != "orders" {
				t.Fatalf("GetSchemaByDefinition schema ID = %+v", input.SchemaId)
			}
			return nil, &smithy.GenericAPIError{Code: "EntityNotFoundException", Message: "missing"}
		},
		register: func(_ context.Context, input *awsglue.RegisterSchemaVersionInput) (*awsglue.RegisterSchemaVersionOutput, error) {
			if value(input.SchemaDefinition) != `"string"` {
				t.Fatalf("RegisterSchemaVersion definition = %q", value(input.SchemaDefinition))
			}
			id := schemaVersionID
			version := int64(3)
			return &awsglue.RegisterSchemaVersionOutput{
				SchemaVersionId: &id,
				VersionNumber:   &version,
				Status:          types.SchemaVersionStatusPending,
			}, nil
		},
	}
	provider := newProvider(t, api)
	capabilities := provider.Capabilities()
	if capabilities.Provider != registryglue.ProviderName || capabilities.RegistrationCreationOutcome || len(capabilities.CompatibilityModes) != 0 {
		t.Fatalf("Capabilities() = %+v", capabilities)
	}

	result, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{
		Subject: schemaregistry.Subject{Registry: "events", Name: "orders"},
		Schema:  compileAvro(t),
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if result.Outcome != schemaregistry.RegistrationUnknown || result.ID.Value != schemaVersionID || result.Version.Number != 3 {
		t.Fatalf("Register() = %+v", result)
	}
}

func TestProviderResolvesGlueUUIDWithoutConfusingItForPortableIdentity(t *testing.T) {
	t.Parallel()

	requestedID := strings.ToUpper(schemaVersionID)
	api := &apiStub{
		getVersion: func(_ context.Context, input *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
			if value(input.SchemaVersionId) != requestedID {
				t.Fatalf("GetSchemaVersion ID = %q", value(input.SchemaVersionId))
			}
			definition := `"string"`
			id := schemaVersionID
			version := int64(2)
			return &awsglue.GetSchemaVersionOutput{
				DataFormat:       types.DataFormatAvro,
				SchemaDefinition: &definition,
				SchemaVersionId:  &id,
				Status:           types.SchemaVersionStatusAvailable,
				VersionNumber:    &version,
			}, nil
		},
	}
	provider := newProvider(t, api)
	result, err := provider.Resolve(context.Background(), schemaregistry.ByProviderID(schemaregistry.ProviderID{
		Provider: registryglue.ProviderName,
		Scope:    "eu-north-1:123456789012:events",
		Value:    requestedID,
	}))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if result.ID.Value != schemaVersionID || result.Schema.Fingerprint().String() == schemaVersionID || result.Lifecycle != schemaregistry.LifecycleAvailable {
		t.Fatalf("Resolve() = %+v", result)
	}
}

func TestUncompressedFramerMatchesAWSGlueHeader(t *testing.T) {
	t.Parallel()

	framer, err := registryglue.NewUncompressedFramer("eu-north-1:123456789012:events", 16)
	if err != nil {
		t.Fatalf("NewUncompressedFramer() error = %v", err)
	}
	framed, err := framer.Frame(context.Background(), schemaregistry.ProviderID{
		Provider: registryglue.ProviderName,
		Scope:    "eu-north-1:123456789012:events",
		Value:    schemaVersionID,
	}, []byte{0xaa})
	if err != nil {
		t.Fatalf("Frame() error = %v", err)
	}
	wantHeader := []byte{3, 0, 0x12, 0x3e, 0x45, 0x67, 0xe8, 0x9b, 0x12, 0xd3, 0xa4, 0x56, 0x42, 0x66, 0x14, 0x17, 0x40, 0x00}
	if string(framed[:18]) != string(wantHeader) || framed[18] != 0xaa {
		t.Fatalf("Frame() = %v", framed)
	}
	id, payload, err := framer.Unframe(context.Background(), framed)
	if err != nil || id.Value != schemaVersionID || len(payload) != 1 || payload[0] != 0xaa {
		t.Fatalf("Unframe() = (%+v, %v, %v)", id, payload, err)
	}
	framed[18] = 0
	if payload[0] != 0xaa {
		t.Fatal("Unframe() payload aliases frame")
	}
	_, _, err = framer.Unframe(context.Background(), append([]byte{3, 5}, framed[2:]...))
	if !errors.Is(err, registryglue.ErrCompressionUnsupported) {
		t.Fatalf("Unframe(compressed) error = %v, want ErrCompressionUnsupported", err)
	}
}

func newProvider(t *testing.T, api registryglue.API) *registryglue.Provider {
	t.Helper()
	provider, err := registryglue.New(registryglue.Config{
		API:            api,
		Scope:          "eu-north-1:123456789012:events",
		RequestTimeout: time.Second,
		MaxConcurrent:  2,
		Canonicalizers: map[schemaregistry.Format]schemaregistry.Canonicalizer{
			schemaregistry.FormatAvro: canonicalizerFunc(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
				return definition.Content, nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return provider
}

func compileAvro(t *testing.T) schemaregistry.Schema {
	t.Helper()
	schema, err := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`"string"`)},
		canonicalizerFunc(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
			return definition.Content, nil
		}),
	)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return schema
}

func value(pointer *string) string {
	if pointer == nil {
		return ""
	}
	return *pointer
}
