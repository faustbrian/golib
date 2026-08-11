// Package glue adapts AWS Glue Schema Registry without erasing its registry,
// schema-name, UUID-version-ID, lifecycle, or compatibility semantics.
package glue

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	awsglue "github.com/aws/aws-sdk-go-v2/service/glue"
	"github.com/aws/aws-sdk-go-v2/service/glue/types"
	"github.com/aws/smithy-go"
	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

const (
	// ProviderName is the stable capability and provider-ID namespace.
	ProviderName      = "aws-glue"
	maxGlueSchemaSize = 170_000
)

// API is the narrow AWS SDK v2 boundary used by Provider. Configure SDK retry,
// endpoint, region, and credentials explicitly when constructing its client;
// Provider adds one total context timeout and does not add a second retry loop.
type API interface {
	GetSchemaByDefinition(context.Context, *awsglue.GetSchemaByDefinitionInput, ...func(*awsglue.Options)) (*awsglue.GetSchemaByDefinitionOutput, error)
	RegisterSchemaVersion(context.Context, *awsglue.RegisterSchemaVersionInput, ...func(*awsglue.Options)) (*awsglue.RegisterSchemaVersionOutput, error)
	GetSchemaVersion(context.Context, *awsglue.GetSchemaVersionInput, ...func(*awsglue.Options)) (*awsglue.GetSchemaVersionOutput, error)
}

// Config binds one AWS region/account/registry scope to explicit schema format
// implementations.
type Config struct {
	API            API
	Scope          string
	RequestTimeout time.Duration
	MaxConcurrent  int
	Canonicalizers map[schemaregistry.Format]schemaregistry.Canonicalizer
}

// Provider is safe for concurrent use when the supplied AWS SDK client is.
type Provider struct {
	api            API
	scope          string
	timeout        time.Duration
	slots          chan struct{}
	canonicalizers map[schemaregistry.Format]schemaregistry.Canonicalizer
	capabilities   schemaregistry.Capabilities
}

// New validates all policy before AWS I/O.
func New(config Config) (*Provider, error) {
	if interfaceIsNil(config.API) || config.Scope == "" || config.RequestTimeout <= 0 || config.MaxConcurrent <= 0 || len(config.Canonicalizers) == 0 {
		return nil, fmt.Errorf("invalid AWS Glue provider config")
	}
	canonicalizers := make(map[schemaregistry.Format]schemaregistry.Canonicalizer, len(config.Canonicalizers))
	formats := make([]schemaregistry.Format, 0, len(config.Canonicalizers))
	for format, canonicalizer := range config.Canonicalizers {
		if !supportedFormat(format) {
			return nil, fmt.Errorf("invalid AWS Glue canonicalizer format %q", format)
		}
		if interfaceIsNil(canonicalizer) {
			return nil, fmt.Errorf("nil %s canonicalizer", format)
		}
		canonicalizers[format] = canonicalizer
		formats = append(formats, format)
	}
	slices.Sort(formats)
	return &Provider{
		api:            config.API,
		scope:          config.Scope,
		timeout:        config.RequestTimeout,
		slots:          make(chan struct{}, config.MaxConcurrent),
		canonicalizers: canonicalizers,
		capabilities: schemaregistry.Capabilities{
			Provider: ProviderName,
			Formats:  formats,
			Lookups: []schemaregistry.LookupKind{
				schemaregistry.LookupByProviderID,
				schemaregistry.LookupByVersion,
				schemaregistry.LookupLatest,
			},
			NumericVersions:             true,
			RegistrationCreationOutcome: false,
		},
	}, nil
}

// Capabilities returns an immutable snapshot of AWS Glue semantics.
func (provider *Provider) Capabilities() schemaregistry.Capabilities {
	capabilities := provider.capabilities
	capabilities.Formats = append([]schemaregistry.Format(nil), capabilities.Formats...)
	capabilities.Lookups = append([]schemaregistry.LookupKind(nil), capabilities.Lookups...)
	return capabilities
}

// Register performs exact-definition lookup before idempotent version registration.
func (provider *Provider) Register(ctx context.Context, request schemaregistry.RegisterRequest) (schemaregistry.RegisterResult, error) {
	if request.Subject.Registry == "" || request.Subject.Name == "" {
		return schemaregistry.RegisterResult{}, fmt.Errorf("%w: AWS Glue registry and schema name", schemaregistry.ErrInvalidRequest)
	}
	definition := request.Schema.Definition()
	if len(definition.Content) == 0 || len(definition.Content) > maxGlueSchemaSize {
		return schemaregistry.RegisterResult{}, fmt.Errorf("%w: AWS Glue schema bytes", schemaregistry.ErrLimitExceeded)
	}
	if len(definition.References) != 0 {
		return schemaregistry.RegisterResult{}, fmt.Errorf("%w: AWS Glue references", schemaregistry.ErrUnsupportedOperation)
	}
	ctx, cancel := context.WithTimeout(ctx, provider.timeout)
	defer cancel()
	if err := provider.acquire(ctx); err != nil {
		return schemaregistry.RegisterResult{}, err
	}
	defer provider.release()
	schemaID := schemaID(request.Subject)
	content := string(definition.Content)
	existing, err := provider.api.GetSchemaByDefinition(ctx, &awsglue.GetSchemaByDefinitionInput{
		SchemaDefinition: &content,
		SchemaId:         schemaID,
	})
	if err == nil {
		if existing == nil || existing.SchemaVersionId == nil || !validUUID(*existing.SchemaVersionId) {
			return schemaregistry.RegisterResult{}, fmt.Errorf("%w: missing AWS Glue schema version ID", schemaregistry.ErrInvalidSchema)
		}
		return schemaregistry.RegisterResult{
			Outcome: schemaregistry.RegistrationExisting,
			ID:      provider.id(*existing.SchemaVersionId),
		}, nil
	}
	if !errors.Is(classifyError(err), schemaregistry.ErrNotFound) {
		return schemaregistry.RegisterResult{}, classifyError(err)
	}
	response, err := provider.api.RegisterSchemaVersion(ctx, &awsglue.RegisterSchemaVersionInput{
		SchemaDefinition: &content,
		SchemaId:         schemaID,
	})
	if err != nil {
		classified := classifyError(err)
		if errors.Is(classified, schemaregistry.ErrUnavailable) {
			return schemaregistry.RegisterResult{Outcome: schemaregistry.RegistrationUnknown}, fmt.Errorf("%w: %v", schemaregistry.ErrUnknownOutcome, classified)
		}
		return schemaregistry.RegisterResult{}, classified
	}
	if response == nil || response.SchemaVersionId == nil || !validUUID(*response.SchemaVersionId) {
		return schemaregistry.RegisterResult{}, fmt.Errorf("%w: missing AWS Glue schema version ID", schemaregistry.ErrInvalidSchema)
	}
	result := schemaregistry.RegisterResult{
		// AWS returns the existing UUID for duplicates and cannot prove whether a
		// concurrent request created this version.
		Outcome: schemaregistry.RegistrationUnknown,
		ID:      provider.id(*response.SchemaVersionId),
	}
	if response.VersionNumber != nil {
		if *response.VersionNumber < 1 {
			return schemaregistry.RegisterResult{}, fmt.Errorf("%w: AWS Glue version identity", schemaregistry.ErrInvalidSchema)
		}
		result.Version = schemaregistry.Version{Number: uint64(*response.VersionNumber)}
	}
	return result, nil
}

// Resolve loads one schema version by an advertised AWS Glue selector.
func (provider *Provider) Resolve(ctx context.Context, lookup schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
	input := &awsglue.GetSchemaVersionInput{}
	result := schemaregistry.ResolveResult{}
	requiresVersion := false
	switch lookup.Kind() {
	case schemaregistry.LookupByProviderID:
		id := lookup.ProviderID()
		if id.Provider != ProviderName || id.Scope != provider.scope || !validUUID(id.Value) {
			return result, fmt.Errorf("%w: AWS Glue schema version ID", schemaregistry.ErrInvalidRequest)
		}
		input.SchemaVersionId = &id.Value
		result.ID = id
	case schemaregistry.LookupByVersion, schemaregistry.LookupLatest:
		requiresVersion = true
		subject := lookup.Subject()
		if subject.Registry == "" || subject.Name == "" {
			return result, fmt.Errorf("%w: AWS Glue subject", schemaregistry.ErrInvalidRequest)
		}
		input.SchemaId = schemaID(subject)
		input.SchemaVersionNumber = &types.SchemaVersionNumber{}
		if lookup.Kind() == schemaregistry.LookupLatest {
			input.SchemaVersionNumber.LatestVersion = true
		} else {
			version := lookup.Version()
			if version.Number == 0 || version.Opaque != "" || version.Number > uint64(^uint64(0)>>1) {
				return result, fmt.Errorf("%w: AWS Glue version", schemaregistry.ErrInvalidRequest)
			}
			number := int64(version.Number)
			input.SchemaVersionNumber.VersionNumber = &number
		}
		result.Subject = subject
	default:
		return result, fmt.Errorf("%w: AWS Glue lookup", schemaregistry.ErrUnsupportedOperation)
	}
	ctx, cancel := context.WithTimeout(ctx, provider.timeout)
	defer cancel()
	if err := provider.acquire(ctx); err != nil {
		return schemaregistry.ResolveResult{}, err
	}
	defer provider.release()
	response, err := provider.api.GetSchemaVersion(ctx, input)
	if err != nil {
		return schemaregistry.ResolveResult{}, classifyError(err)
	}
	if response == nil || response.SchemaDefinition == nil || response.SchemaVersionId == nil || !validUUID(*response.SchemaVersionId) {
		return schemaregistry.ResolveResult{}, fmt.Errorf("%w: incomplete AWS Glue schema version", schemaregistry.ErrInvalidSchema)
	}
	if lookup.Kind() == schemaregistry.LookupByProviderID {
		requestedUUID, _ := decodeUUID(lookup.ProviderID().Value)
		responseUUID, _ := decodeUUID(*response.SchemaVersionId)
		if responseUUID != requestedUUID {
			return schemaregistry.ResolveResult{}, fmt.Errorf("%w: AWS Glue schema version identity", schemaregistry.ErrInvalidSchema)
		}
	}
	if response.VersionNumber != nil && *response.VersionNumber < 1 {
		return schemaregistry.ResolveResult{}, fmt.Errorf("%w: AWS Glue version identity", schemaregistry.ErrInvalidSchema)
	}
	if requiresVersion {
		if response.VersionNumber == nil ||
			(lookup.Kind() == schemaregistry.LookupByVersion && uint64(*response.VersionNumber) != lookup.Version().Number) {
			return schemaregistry.ResolveResult{}, fmt.Errorf("%w: AWS Glue version identity", schemaregistry.ErrInvalidSchema)
		}
	}
	if len(*response.SchemaDefinition) > maxGlueSchemaSize {
		return schemaregistry.ResolveResult{}, fmt.Errorf("%w: AWS Glue schema bytes", schemaregistry.ErrLimitExceeded)
	}
	format, err := schemaFormat(response.DataFormat)
	if err != nil {
		return schemaregistry.ResolveResult{}, err
	}
	canonicalizer := provider.canonicalizers[format]
	if interfaceIsNil(canonicalizer) {
		return schemaregistry.ResolveResult{}, fmt.Errorf("%w: %s", schemaregistry.ErrUnsupportedFormat, format)
	}
	schema, err := schemaregistry.Compile(ctx, schemaregistry.Definition{Format: format, Content: []byte(*response.SchemaDefinition)}, canonicalizer)
	if err != nil {
		return schemaregistry.ResolveResult{}, err
	}
	result.Schema = schema
	result.ID = provider.id(*response.SchemaVersionId)
	result.Lifecycle = lifecycle(response.Status)
	if response.VersionNumber != nil {
		result.Version = schemaregistry.Version{Number: uint64(*response.VersionNumber)}
	}
	return result, nil
}

// CheckCompatibility is explicit unsupported because Glue applies configured
// compatibility while registering and exposes no candidate dry-run with the
// same semantics.
func (provider *Provider) CheckCompatibility(context.Context, schemaregistry.CompatibilityRequest) (schemaregistry.CompatibilityResult, error) {
	return schemaregistry.CompatibilityResult{Supported: false}, nil
}

func (provider *Provider) id(value string) schemaregistry.ProviderID {
	return schemaregistry.ProviderID{Provider: ProviderName, Scope: provider.scope, Value: value}
}

func (provider *Provider) acquire(ctx context.Context) error {
	select {
	case provider.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (provider *Provider) release() { <-provider.slots }

func schemaID(subject schemaregistry.Subject) *types.SchemaId {
	registry := subject.Registry
	name := subject.Name
	return &types.SchemaId{RegistryName: &registry, SchemaName: &name}
}

func schemaFormat(format types.DataFormat) (schemaregistry.Format, error) {
	switch format {
	case types.DataFormatAvro:
		return schemaregistry.FormatAvro, nil
	case types.DataFormatJson:
		return schemaregistry.FormatJSONSchema, nil
	case types.DataFormatProtobuf:
		return schemaregistry.FormatProtobuf, nil
	default:
		return "", fmt.Errorf("%w: AWS Glue data format %q", schemaregistry.ErrInvalidSchema, format)
	}
}

func supportedFormat(format schemaregistry.Format) bool {
	switch format {
	case schemaregistry.FormatAvro, schemaregistry.FormatJSONSchema, schemaregistry.FormatProtobuf:
		return true
	default:
		return false
	}
}

func lifecycle(status types.SchemaVersionStatus) schemaregistry.LifecycleState {
	switch status {
	case types.SchemaVersionStatusAvailable:
		return schemaregistry.LifecycleAvailable
	case types.SchemaVersionStatusPending:
		return schemaregistry.LifecyclePending
	case types.SchemaVersionStatusDeleting:
		return schemaregistry.LifecycleDeleting
	case types.SchemaVersionStatusFailure:
		return schemaregistry.LifecycleFailed
	default:
		return schemaregistry.LifecycleUnknown
	}
}

func classifyError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch apiError.ErrorCode() {
		case "EntityNotFoundException":
			return schemaregistry.ErrNotFound
		case "AccessDeniedException":
			return schemaregistry.ErrUnauthorized
		case "InvalidInputException", "ResourceNumberLimitExceededException":
			return schemaregistry.ErrRejected
		case "ConcurrentModificationException", "ThrottlingException", "InternalServiceException", "OperationTimeoutException":
			return schemaregistry.ErrUnavailable
		}
	}
	return fmt.Errorf("%w: AWS Glue API", schemaregistry.ErrUnavailable)
}

func interfaceIsNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
