package glue

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	awsglue "github.com/aws/aws-sdk-go-v2/service/glue"
	"github.com/aws/aws-sdk-go-v2/service/glue/types"
	"github.com/aws/smithy-go"
	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

const internalVersionID = "123e4567-e89b-12d3-a456-426614174000"

type canonicalizerFunction func(context.Context, schemaregistry.Definition) ([]byte, error)

func (function canonicalizerFunction) Canonicalize(ctx context.Context, definition schemaregistry.Definition) ([]byte, error) {
	return function(ctx, definition)
}

type apiFunction struct {
	byDefinition func(context.Context, *awsglue.GetSchemaByDefinitionInput) (*awsglue.GetSchemaByDefinitionOutput, error)
	register     func(context.Context, *awsglue.RegisterSchemaVersionInput) (*awsglue.RegisterSchemaVersionOutput, error)
	version      func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error)
}

func (api *apiFunction) GetSchemaByDefinition(ctx context.Context, input *awsglue.GetSchemaByDefinitionInput, _ ...func(*awsglue.Options)) (*awsglue.GetSchemaByDefinitionOutput, error) {
	return api.byDefinition(ctx, input)
}

func (api *apiFunction) RegisterSchemaVersion(ctx context.Context, input *awsglue.RegisterSchemaVersionInput, _ ...func(*awsglue.Options)) (*awsglue.RegisterSchemaVersionOutput, error) {
	return api.register(ctx, input)
}

func (api *apiFunction) GetSchemaVersion(ctx context.Context, input *awsglue.GetSchemaVersionInput, _ ...func(*awsglue.Options)) (*awsglue.GetSchemaVersionOutput, error) {
	return api.version(ctx, input)
}

func noCallAPI(t *testing.T) *apiFunction {
	t.Helper()
	fail := func() { t.Fatal("unexpected API call") }
	return &apiFunction{
		byDefinition: func(context.Context, *awsglue.GetSchemaByDefinitionInput) (*awsglue.GetSchemaByDefinitionOutput, error) {
			fail()
			return nil, nil
		},
		register: func(context.Context, *awsglue.RegisterSchemaVersionInput) (*awsglue.RegisterSchemaVersionOutput, error) {
			fail()
			return nil, nil
		},
		version: func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
			fail()
			return nil, nil
		},
	}
}

func internalProvider(t *testing.T, api API) *Provider {
	t.Helper()
	provider, err := New(Config{
		API: api, Scope: "scope", RequestTimeout: time.Second, MaxConcurrent: 1,
		Canonicalizers: map[schemaregistry.Format]schemaregistry.Canonicalizer{
			schemaregistry.FormatAvro: canonicalizerFunction(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
				return definition.Content, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func internalSchema(t *testing.T, content string, references ...schemaregistry.Reference) schemaregistry.Schema {
	t.Helper()
	schema, err := schemaregistry.Compile(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatAvro, Content: []byte(content), References: references,
	}, canonicalizerFunction(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
		return definition.Content, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestConfigurationMappingsErrorsAndFramingBoundaries(t *testing.T) {
	t.Parallel()

	api := noCallAPI(t)
	invalid := []Config{
		{},
		{API: api, RequestTimeout: time.Second, MaxConcurrent: 1, Canonicalizers: map[schemaregistry.Format]schemaregistry.Canonicalizer{schemaregistry.FormatAvro: canonicalizerFunction(func(context.Context, schemaregistry.Definition) ([]byte, error) { return nil, nil })}},
		{API: api, Scope: "scope", MaxConcurrent: 1, Canonicalizers: map[schemaregistry.Format]schemaregistry.Canonicalizer{schemaregistry.FormatAvro: canonicalizerFunction(func(context.Context, schemaregistry.Definition) ([]byte, error) { return nil, nil })}},
		{API: api, Scope: "scope", RequestTimeout: time.Second, Canonicalizers: map[schemaregistry.Format]schemaregistry.Canonicalizer{schemaregistry.FormatAvro: canonicalizerFunction(func(context.Context, schemaregistry.Definition) ([]byte, error) { return nil, nil })}},
		{API: api, Scope: "scope", RequestTimeout: time.Second, MaxConcurrent: 1, Canonicalizers: map[schemaregistry.Format]schemaregistry.Canonicalizer{"yaml": canonicalizerFunction(func(context.Context, schemaregistry.Definition) ([]byte, error) { return nil, nil })}},
	}
	for _, config := range invalid {
		if _, err := New(config); err == nil {
			t.Fatal("New(invalid) error = nil")
		}
	}
	var nilCanonicalizer *canonicalizerFunction
	config := Config{API: api, Scope: "scope", RequestTimeout: time.Second, MaxConcurrent: 1, Canonicalizers: map[schemaregistry.Format]schemaregistry.Canonicalizer{schemaregistry.FormatAvro: nilCanonicalizer}}
	if _, err := New(config); err == nil {
		t.Fatal("New(nil canonicalizer) error = nil")
	}
	if interfaceIsNil(1) || !interfaceIsNil(nilCanonicalizer) {
		t.Fatal("interfaceIsNil mismatch")
	}
	for format, want := range map[types.DataFormat]schemaregistry.Format{
		types.DataFormatAvro: schemaregistry.FormatAvro, types.DataFormatJson: schemaregistry.FormatJSONSchema, types.DataFormatProtobuf: schemaregistry.FormatProtobuf,
	} {
		got, err := schemaFormat(format)
		if err != nil || got != want {
			t.Fatalf("schemaFormat(%s) = (%s, %v)", format, got, err)
		}
	}
	if _, err := schemaFormat("YAML"); !errors.Is(err, schemaregistry.ErrInvalidSchema) {
		t.Fatalf("schemaFormat(invalid) error = %v", err)
	}
	for status, want := range map[types.SchemaVersionStatus]schemaregistry.LifecycleState{
		types.SchemaVersionStatusAvailable: schemaregistry.LifecycleAvailable,
		types.SchemaVersionStatusPending:   schemaregistry.LifecyclePending,
		types.SchemaVersionStatusDeleting:  schemaregistry.LifecycleDeleting,
		types.SchemaVersionStatusFailure:   schemaregistry.LifecycleFailed,
		"UNKNOWN":                          schemaregistry.LifecycleUnknown,
	} {
		if got := lifecycle(status); got != want {
			t.Fatalf("lifecycle(%s) = %s", status, got)
		}
	}
	if classifyError(nil) != nil || !errors.Is(classifyError(context.Canceled), context.Canceled) || !errors.Is(classifyError(context.DeadlineExceeded), context.DeadlineExceeded) {
		t.Fatal("classifyError context mismatch")
	}
	for code, want := range map[string]error{
		"EntityNotFoundException":              schemaregistry.ErrNotFound,
		"AccessDeniedException":                schemaregistry.ErrUnauthorized,
		"InvalidInputException":                schemaregistry.ErrRejected,
		"ResourceNumberLimitExceededException": schemaregistry.ErrRejected,
		"ConcurrentModificationException":      schemaregistry.ErrUnavailable,
		"ThrottlingException":                  schemaregistry.ErrUnavailable,
		"InternalServiceException":             schemaregistry.ErrUnavailable,
		"OperationTimeoutException":            schemaregistry.ErrUnavailable,
		"UnknownException":                     schemaregistry.ErrUnavailable,
	} {
		if err := classifyError(&smithy.GenericAPIError{Code: code}); !errors.Is(err, want) {
			t.Fatalf("classifyError(%s) = %v", code, err)
		}
	}
	if !errors.Is(classifyError(errors.New("network")), schemaregistry.ErrUnavailable) {
		t.Fatal("classifyError(generic) mismatch")
	}

	for _, cfg := range []struct {
		scope string
		size  int
	}{{"", 1}, {"scope", 0}} {
		if _, err := NewUncompressedFramer(cfg.scope, cfg.size); err == nil {
			t.Fatal("NewUncompressedFramer(invalid) error = nil")
		}
	}
	framer, err := NewUncompressedFramer("scope", 1)
	if err != nil {
		t.Fatalf("NewUncompressedFramer() error = %v", err)
	}
	id := schemaregistry.ProviderID{Provider: ProviderName, Scope: "scope", Value: internalVersionID}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := framer.Frame(canceled, id, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Frame(canceled) error = %v", err)
	}
	if _, _, err := framer.Unframe(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Unframe(canceled) error = %v", err)
	}
	for _, test := range []struct {
		id      schemaregistry.ProviderID
		payload []byte
	}{
		{schemaregistry.ProviderID{}, nil},
		{schemaregistry.ProviderID{Provider: "other", Scope: "scope", Value: internalVersionID}, nil},
		{schemaregistry.ProviderID{Provider: ProviderName, Scope: "other", Value: internalVersionID}, nil},
		{id, []byte{1, 2}},
		{schemaregistry.ProviderID{Provider: ProviderName, Scope: "scope", Value: "bad"}, nil},
	} {
		if _, err := framer.Frame(context.Background(), test.id, test.payload); !errors.Is(err, ErrInvalidFrame) {
			t.Fatalf("Frame(%+v) error = %v", test, err)
		}
	}
	validFrame, err := framer.Frame(context.Background(), id, nil)
	if err != nil {
		t.Fatal(err)
	}
	exactFrame, err := framer.Frame(context.Background(), id, []byte{1})
	if err != nil {
		t.Fatal(err)
	}
	decodedID, payload, err := framer.Unframe(context.Background(), exactFrame)
	if err != nil || decodedID != id || !bytes.Equal(payload, []byte{1}) {
		t.Fatalf("Unframe(exact payload) = (%+v, %v, %v)", decodedID, payload, err)
	}
	decodedID, payload, err = framer.Unframe(context.Background(), validFrame)
	if err != nil || decodedID != id || len(payload) != 0 {
		t.Fatalf("Unframe(empty payload) = (%+v, %v, %v)", decodedID, payload, err)
	}
	for _, frame := range [][]byte{nil, {2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, append(append([]byte(nil), validFrame...), 1, 2)} {
		if _, _, err := framer.Unframe(context.Background(), frame); !errors.Is(err, ErrInvalidFrame) {
			t.Fatalf("Unframe(%v) error = %v", frame, err)
		}
	}
	unknownCompression := append([]byte(nil), validFrame...)
	unknownCompression[1] = 1
	if _, _, err := framer.Unframe(context.Background(), unknownCompression); !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("Unframe(compression) error = %v", err)
	}
	if _, err := decodeUUID("123e4567-e89b-12d3-a456-42661417400z"); !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("decodeUUID(hex) error = %v", err)
	}
}

func TestRegisterBoundaries(t *testing.T) {
	t.Parallel()

	schema := internalSchema(t, "string")
	provider := internalProvider(t, noCallAPI(t))
	for _, subject := range []schemaregistry.Subject{{}, {Registry: "r"}, {Name: "s"}} {
		if _, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: subject}); !errors.Is(err, schemaregistry.ErrInvalidRequest) {
			t.Fatalf("Register(subject %+v) error = %v", subject, err)
		}
	}
	if _, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Registry: "r", Name: "s"}}); !errors.Is(err, schemaregistry.ErrLimitExceeded) {
		t.Fatalf("Register(empty schema) error = %v", err)
	}
	large := internalSchema(t, strings.Repeat("x", maxGlueSchemaSize+1))
	if _, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Registry: "r", Name: "s"}, Schema: large}); !errors.Is(err, schemaregistry.ErrLimitExceeded) {
		t.Fatalf("Register(large schema) error = %v", err)
	}
	withReference := internalSchema(t, "string", schemaregistry.Reference{Name: "x", Subject: "s", Version: 1, Fingerprint: schema.Fingerprint()})
	if _, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Registry: "r", Name: "s"}, Schema: withReference}); !errors.Is(err, schemaregistry.ErrUnsupportedOperation) {
		t.Fatalf("Register(reference) error = %v", err)
	}
	provider.slots <- struct{}{}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Register(canceled, schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Registry: "r", Name: "s"}, Schema: schema}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Register(saturated) error = %v", err)
	}
	<-provider.slots

	existingID := internalVersionID
	provider = internalProvider(t, &apiFunction{
		byDefinition: func(context.Context, *awsglue.GetSchemaByDefinitionInput) (*awsglue.GetSchemaByDefinitionOutput, error) {
			return &awsglue.GetSchemaByDefinitionOutput{SchemaVersionId: &existingID}, nil
		},
	})
	result, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Registry: "r", Name: "s"}, Schema: schema})
	if err != nil || result.Outcome != schemaregistry.RegistrationExisting || result.ID.Value != existingID {
		t.Fatalf("Register(existing) = (%+v, %v)", result, err)
	}
	exact := internalSchema(t, strings.Repeat("x", maxGlueSchemaSize))
	result, err = provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Registry: "r", Name: "s"}, Schema: exact})
	if err != nil || result.Outcome != schemaregistry.RegistrationExisting {
		t.Fatalf("Register(exact maximum) = (%+v, %v)", result, err)
	}
	provider = internalProvider(t, &apiFunction{byDefinition: func(context.Context, *awsglue.GetSchemaByDefinitionInput) (*awsglue.GetSchemaByDefinitionOutput, error) {
		return nil, nil
	}})
	if _, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Registry: "r", Name: "s"}, Schema: schema}); !errors.Is(err, schemaregistry.ErrInvalidSchema) {
		t.Fatalf("Register(incomplete existing) error = %v", err)
	}
	badID := "not-a-uuid"
	provider = internalProvider(t, &apiFunction{byDefinition: func(context.Context, *awsglue.GetSchemaByDefinitionInput) (*awsglue.GetSchemaByDefinitionOutput, error) {
		return &awsglue.GetSchemaByDefinitionOutput{SchemaVersionId: &badID}, nil
	}})
	if _, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Registry: "r", Name: "s"}, Schema: schema}); !errors.Is(err, schemaregistry.ErrInvalidSchema) {
		t.Fatalf("Register(invalid existing ID) error = %v", err)
	}
	provider = internalProvider(t, &apiFunction{byDefinition: func(context.Context, *awsglue.GetSchemaByDefinitionInput) (*awsglue.GetSchemaByDefinitionOutput, error) {
		return nil, &smithy.GenericAPIError{Code: "AccessDeniedException"}
	}})
	if _, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Registry: "r", Name: "s"}, Schema: schema}); !errors.Is(err, schemaregistry.ErrUnauthorized) {
		t.Fatalf("Register(lookup error) error = %v", err)
	}

	notFound := func(context.Context, *awsglue.GetSchemaByDefinitionInput) (*awsglue.GetSchemaByDefinitionOutput, error) {
		return nil, &smithy.GenericAPIError{Code: "EntityNotFoundException"}
	}
	for _, test := range []struct {
		name     string
		response *awsglue.RegisterSchemaVersionOutput
		err      error
		want     error
		outcome  schemaregistry.RegistrationOutcome
	}{
		{"unavailable", nil, &smithy.GenericAPIError{Code: "ThrottlingException"}, schemaregistry.ErrUnknownOutcome, schemaregistry.RegistrationUnknown},
		{"rejected", nil, &smithy.GenericAPIError{Code: "InvalidInputException"}, schemaregistry.ErrRejected, ""},
		{"nil response", nil, nil, schemaregistry.ErrInvalidSchema, ""},
		{"missing ID", &awsglue.RegisterSchemaVersionOutput{}, nil, schemaregistry.ErrInvalidSchema, ""},
		{"invalid ID", &awsglue.RegisterSchemaVersionOutput{SchemaVersionId: pointer("not-a-uuid")}, nil, schemaregistry.ErrInvalidSchema, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := internalProvider(t, &apiFunction{
				byDefinition: notFound,
				register: func(context.Context, *awsglue.RegisterSchemaVersionInput) (*awsglue.RegisterSchemaVersionOutput, error) {
					return test.response, test.err
				},
			})
			result, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Registry: "r", Name: "s"}, Schema: schema})
			if !errors.Is(err, test.want) || result.Outcome != test.outcome {
				t.Fatalf("Register() = (%+v, %v), want %v", result, err, test.want)
			}
		})
	}
	for _, nonpositive := range []int64{-1, 0} {
		provider = internalProvider(t, &apiFunction{
			byDefinition: notFound,
			register: func(context.Context, *awsglue.RegisterSchemaVersionInput) (*awsglue.RegisterSchemaVersionOutput, error) {
				return &awsglue.RegisterSchemaVersionOutput{SchemaVersionId: &existingID, VersionNumber: &nonpositive}, nil
			},
		})
		result, err = provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Registry: "r", Name: "s"}, Schema: schema})
		if !errors.Is(err, schemaregistry.ErrInvalidSchema) {
			t.Fatalf("Register(nonpositive version %d) = (%+v, %v)", nonpositive, result, err)
		}
	}
	versionOne := int64(1)
	provider = internalProvider(t, &apiFunction{
		byDefinition: notFound,
		register: func(context.Context, *awsglue.RegisterSchemaVersionInput) (*awsglue.RegisterSchemaVersionOutput, error) {
			return &awsglue.RegisterSchemaVersionOutput{SchemaVersionId: &existingID, VersionNumber: &versionOne}, nil
		},
	})
	result, err = provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Registry: "r", Name: "s"}, Schema: schema})
	if err != nil || result.Version.Number != 1 {
		t.Fatalf("Register(version one) = (%+v, %v)", result, err)
	}
}

func TestResolveBoundaries(t *testing.T) {
	t.Parallel()

	provider := internalProvider(t, noCallAPI(t))
	invalid := []schemaregistry.Lookup{
		schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "other", Scope: "scope", Value: internalVersionID}),
		schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: ProviderName, Scope: "scope", Value: "bad"}),
		schemaregistry.AtVersion(schemaregistry.Subject{}, schemaregistry.Version{Number: 1}),
		schemaregistry.AtVersion(schemaregistry.Subject{Registry: "r"}, schemaregistry.Version{Number: 1}),
		schemaregistry.AtVersion(schemaregistry.Subject{Name: "s"}, schemaregistry.Version{Number: 1}),
		schemaregistry.AtVersion(schemaregistry.Subject{Registry: "r", Name: "s"}, schemaregistry.Version{}),
		schemaregistry.AtVersion(schemaregistry.Subject{Registry: "r", Name: "s"}, schemaregistry.Version{Opaque: "v"}),
		schemaregistry.AtVersion(schemaregistry.Subject{Registry: "r", Name: "s"}, schemaregistry.Version{Number: ^uint64(0)}),
		{},
	}
	for _, lookup := range invalid {
		if _, err := provider.Resolve(context.Background(), lookup); err == nil {
			t.Fatalf("Resolve(%+v) error = nil", lookup)
		}
	}
	provider.slots <- struct{}{}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Resolve(canceled, schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: ProviderName, Scope: "scope", Value: internalVersionID})); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve(saturated) error = %v", err)
	}
	<-provider.slots

	lookup := schemaregistry.AtVersion(schemaregistry.Subject{Registry: "r", Name: "s"}, schemaregistry.Version{Number: 2})
	provider = internalProvider(t, &apiFunction{version: func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
		return nil, &smithy.GenericAPIError{Code: "EntityNotFoundException"}
	}})
	if _, err := provider.Resolve(context.Background(), lookup); !errors.Is(err, schemaregistry.ErrNotFound) {
		t.Fatalf("Resolve(API error) error = %v", err)
	}
	maximumVersionLookup := schemaregistry.AtVersion(schemaregistry.Subject{Registry: "r", Name: "s"}, schemaregistry.Version{Number: uint64(math.MaxInt64)})
	provider = internalProvider(t, &apiFunction{version: func(_ context.Context, input *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
		if input.SchemaVersionNumber == nil || input.SchemaVersionNumber.VersionNumber == nil || *input.SchemaVersionNumber.VersionNumber != math.MaxInt64 {
			t.Fatalf("maximum version input = %+v", input)
		}
		return nil, &smithy.GenericAPIError{Code: "EntityNotFoundException"}
	}})
	if _, err := provider.Resolve(context.Background(), maximumVersionLookup); !errors.Is(err, schemaregistry.ErrNotFound) {
		t.Fatalf("Resolve(maximum version) error = %v", err)
	}
	definition := "string"
	badID := "bad"
	for _, test := range []struct {
		name     string
		response *awsglue.GetSchemaVersionOutput
		want     error
	}{
		{"nil", nil, schemaregistry.ErrInvalidSchema},
		{"missing definition", &awsglue.GetSchemaVersionOutput{SchemaVersionId: &badID}, schemaregistry.ErrInvalidSchema},
		{"bad ID", &awsglue.GetSchemaVersionOutput{SchemaVersionId: &badID, SchemaDefinition: &definition}, schemaregistry.ErrInvalidSchema},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := internalProvider(t, &apiFunction{version: func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
				return test.response, nil
			}})
			if _, err := provider.Resolve(context.Background(), lookup); !errors.Is(err, test.want) {
				t.Fatalf("Resolve() error = %v, want %v", err, test.want)
			}
		})
	}
	for _, versionNumber := range []*int64{nil, func() *int64 { value := int64(0); return &value }(), func() *int64 { value := int64(3); return &value }()} {
		provider := internalProvider(t, &apiFunction{version: func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
			return &awsglue.GetSchemaVersionOutput{
				SchemaVersionId: pointer(internalVersionID), SchemaDefinition: &definition,
				DataFormat: types.DataFormatAvro, VersionNumber: versionNumber,
			}, nil
		}})
		if _, err := provider.Resolve(context.Background(), lookup); !errors.Is(err, schemaregistry.ErrInvalidSchema) {
			t.Fatalf("Resolve(mismatched version) error = %v", err)
		}
	}
	versionTwo := int64(2)
	large := strings.Repeat("x", maxGlueSchemaSize+1)
	provider = internalProvider(t, &apiFunction{version: func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
		return &awsglue.GetSchemaVersionOutput{SchemaVersionId: pointer(internalVersionID), SchemaDefinition: &large, VersionNumber: &versionTwo}, nil
	}})
	if _, err := provider.Resolve(context.Background(), lookup); !errors.Is(err, schemaregistry.ErrLimitExceeded) {
		t.Fatalf("Resolve(large) error = %v", err)
	}
	exactDefinition := strings.Repeat("x", maxGlueSchemaSize)
	provider = internalProvider(t, &apiFunction{version: func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
		return &awsglue.GetSchemaVersionOutput{SchemaVersionId: pointer(internalVersionID), SchemaDefinition: &exactDefinition, DataFormat: types.DataFormatAvro, VersionNumber: &versionTwo}, nil
	}})
	result, err := provider.Resolve(context.Background(), lookup)
	if err != nil || len(result.Schema.Definition().Content) != maxGlueSchemaSize {
		t.Fatalf("Resolve(exact maximum) = (%+v, %v)", result, err)
	}
	provider = internalProvider(t, &apiFunction{version: func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
		return &awsglue.GetSchemaVersionOutput{SchemaVersionId: pointer(internalVersionID), SchemaDefinition: &definition, DataFormat: types.DataFormatAvro}, nil
	}})
	result, err = provider.Resolve(context.Background(), schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: ProviderName, Scope: "scope", Value: internalVersionID}))
	if err != nil || result.Version.Number != 0 || result.ID.Value != internalVersionID {
		t.Fatalf("Resolve(provider ID without version) = (%+v, %v)", result, err)
	}
	versionOne := int64(1)
	provider = internalProvider(t, &apiFunction{version: func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
		return &awsglue.GetSchemaVersionOutput{SchemaVersionId: pointer(internalVersionID), SchemaDefinition: &definition, DataFormat: types.DataFormatAvro, VersionNumber: &versionOne}, nil
	}})
	result, err = provider.Resolve(context.Background(), schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: ProviderName, Scope: "scope", Value: internalVersionID}))
	if err != nil || result.Version.Number != 1 {
		t.Fatalf("Resolve(provider ID version one) = (%+v, %v)", result, err)
	}
	mismatchedVersionID := "11112233-4455-6677-8899-aabbccddeeff"
	provider = internalProvider(t, &apiFunction{version: func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
		return &awsglue.GetSchemaVersionOutput{SchemaVersionId: &mismatchedVersionID, SchemaDefinition: &definition, DataFormat: types.DataFormatAvro, VersionNumber: &versionOne}, nil
	}})
	if _, err := provider.Resolve(context.Background(), schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: ProviderName, Scope: "scope", Value: internalVersionID})); !errors.Is(err, schemaregistry.ErrInvalidSchema) {
		t.Fatalf("Resolve(mismatched provider ID) error = %v, want ErrInvalidSchema", err)
	}
	provider = internalProvider(t, &apiFunction{version: func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
		return &awsglue.GetSchemaVersionOutput{SchemaVersionId: pointer(internalVersionID), SchemaDefinition: &definition, DataFormat: "YAML", VersionNumber: &versionTwo}, nil
	}})
	if _, err := provider.Resolve(context.Background(), lookup); !errors.Is(err, schemaregistry.ErrInvalidSchema) {
		t.Fatalf("Resolve(format) error = %v", err)
	}
	provider = internalProvider(t, &apiFunction{version: func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
		return &awsglue.GetSchemaVersionOutput{SchemaVersionId: pointer(internalVersionID), SchemaDefinition: &definition, DataFormat: types.DataFormatJson, VersionNumber: &versionTwo}, nil
	}})
	if _, err := provider.Resolve(context.Background(), lookup); !errors.Is(err, schemaregistry.ErrUnsupportedFormat) {
		t.Fatalf("Resolve(canonicalizer) error = %v", err)
	}
	canonicalError := errors.New("canonical")
	provider = internalProvider(t, &apiFunction{version: func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
		return &awsglue.GetSchemaVersionOutput{SchemaVersionId: pointer(internalVersionID), SchemaDefinition: &definition, DataFormat: types.DataFormatAvro, VersionNumber: &versionTwo}, nil
	}})
	provider.canonicalizers[schemaregistry.FormatAvro] = canonicalizerFunction(func(context.Context, schemaregistry.Definition) ([]byte, error) { return nil, canonicalError })
	if _, err := provider.Resolve(context.Background(), lookup); !errors.Is(err, canonicalError) {
		t.Fatalf("Resolve(canonical) error = %v", err)
	}

	version := int64(4)
	provider = internalProvider(t, &apiFunction{version: func(_ context.Context, input *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
		if input.SchemaVersionNumber == nil || !input.SchemaVersionNumber.LatestVersion {
			t.Fatalf("latest input = %+v", input)
		}
		return &awsglue.GetSchemaVersionOutput{SchemaVersionId: pointer(internalVersionID), SchemaDefinition: &definition, DataFormat: types.DataFormatAvro, Status: types.SchemaVersionStatusPending, VersionNumber: &version}, nil
	}})
	result, err = provider.Resolve(context.Background(), schemaregistry.Latest(schemaregistry.Subject{Registry: "r", Name: "s"}))
	if err != nil || result.Version.Number != 4 || result.Subject.Name != "s" || result.Lifecycle != schemaregistry.LifecyclePending {
		t.Fatalf("Resolve(latest) = (%+v, %v)", result, err)
	}
	for _, mode := range []schemaregistry.CompatibilityMode{
		schemaregistry.CompatibilityBackward,
		schemaregistry.CompatibilityBackwardTransitive,
		schemaregistry.CompatibilityForward,
		schemaregistry.CompatibilityForwardTransitive,
		schemaregistry.CompatibilityFull,
		schemaregistry.CompatibilityFullTransitive,
		schemaregistry.CompatibilityNone,
	} {
		compatibility, compatibilityErr := provider.CheckCompatibility(context.Background(), schemaregistry.CompatibilityRequest{Mode: mode})
		if compatibilityErr != nil || compatibility.Supported {
			t.Fatalf("CheckCompatibility(%s) = (%+v, %v)", mode, compatibility, compatibilityErr)
		}
	}
	for _, mode := range []string{"NONE", "DISABLED", "BACKWARD", "BACKWARD_ALL", "FORWARD", "FORWARD_ALL", "FULL", "FULL_ALL"} {
		compatibility, compatibilityErr := provider.CheckCompatibility(context.Background(), schemaregistry.CompatibilityRequest{
			Mode: schemaregistry.CompatibilityProviderSpecific, ProviderMode: mode,
		})
		if compatibilityErr != nil || compatibility.Supported {
			t.Fatalf("CheckCompatibility(AWS %s) = (%+v, %v)", mode, compatibility, compatibilityErr)
		}
	}
}

func pointer(value string) *string { return &value }
