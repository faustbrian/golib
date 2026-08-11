// Package confluent implements the Confluent Schema Registry REST semantics
// without treating its global integer IDs as portable schema identity.
package confluent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

const (
	// ProviderName is the stable capability and provider-ID namespace.
	ProviderName = "confluent"
	mediaType    = "application/vnd.schemaregistry.v1+json"
)

// ErrInvalidResponse marks a malformed or identity-inconsistent registry response.
var ErrInvalidResponse = errors.New("confluent schema registry: invalid response")

// CredentialProvider returns an Authorization header value for the configured
// endpoint only. It must not include schema contents in errors.
type CredentialProvider interface {
	Authorization(context.Context) (string, error)
}

// Config defines endpoint policy, one total request budget, response bounds,
// and explicit format implementations.
type Config struct {
	Endpoint            string
	Scope               string
	Transport           http.RoundTripper
	Credentials         CredentialProvider
	AllowHTTPForTesting bool
	RequestTimeout      time.Duration
	MaxResponseBytes    int
	MaxAttempts         int
	MaxConcurrent       int
	RetryDelay          time.Duration
	ReferenceLimits     schemaregistry.GraphLimits
	Canonicalizers      map[schemaregistry.Format]schemaregistry.Canonicalizer
}

// Provider is safe for concurrent use when its supplied dependencies are.
type Provider struct {
	endpoint        *url.URL
	scope           string
	transport       http.RoundTripper
	credentials     CredentialProvider
	timeout         time.Duration
	maxResponse     int
	maxAttempts     int
	retryDelay      time.Duration
	slots           chan struct{}
	referenceLimits schemaregistry.GraphLimits
	canonicalizers  map[schemaregistry.Format]schemaregistry.Canonicalizer
	capabilities    schemaregistry.Capabilities
}

// New validates endpoint and resource policy before any I/O.
func New(config Config) (*Provider, error) {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, fmt.Errorf("invalid Confluent endpoint")
	}
	if endpoint.Scheme != "https" && (!config.AllowHTTPForTesting || endpoint.Scheme != "http") {
		return nil, fmt.Errorf("confluent endpoint must use HTTPS")
	}
	if config.Scope == "" || interfaceIsNil(config.Transport) || config.RequestTimeout <= 0 ||
		config.MaxResponseBytes <= 0 || config.MaxAttempts <= 0 || config.MaxConcurrent <= 0 || config.RetryDelay < 0 ||
		config.ReferenceLimits.MaxSchemas <= 0 || config.ReferenceLimits.MaxDepth <= 0 ||
		config.ReferenceLimits.MaxReferences <= 0 || len(config.Canonicalizers) == 0 {
		return nil, fmt.Errorf("invalid Confluent provider config")
	}
	canonicalizers := make(map[schemaregistry.Format]schemaregistry.Canonicalizer, len(config.Canonicalizers))
	formats := make([]schemaregistry.Format, 0, len(config.Canonicalizers))
	for format, canonicalizer := range config.Canonicalizers {
		if _, err := schemaType(format); err != nil {
			return nil, fmt.Errorf("invalid Confluent canonicalizer format: %w", err)
		}
		if interfaceIsNil(canonicalizer) {
			return nil, fmt.Errorf("nil %s canonicalizer", format)
		}
		canonicalizers[format] = canonicalizer
		formats = append(formats, format)
	}
	slices.Sort(formats)
	capabilities := schemaregistry.Capabilities{
		Provider: ProviderName,
		Formats:  formats,
		Lookups: []schemaregistry.LookupKind{
			schemaregistry.LookupByProviderID,
			schemaregistry.LookupByVersion,
			schemaregistry.LookupLatest,
		},
		CompatibilityModes: []schemaregistry.CompatibilityMode{
			schemaregistry.CompatibilityBackward,
			schemaregistry.CompatibilityBackwardTransitive,
			schemaregistry.CompatibilityForward,
			schemaregistry.CompatibilityForwardTransitive,
			schemaregistry.CompatibilityFull,
			schemaregistry.CompatibilityFullTransitive,
			schemaregistry.CompatibilityNone,
		},
		NumericVersions:             true,
		SchemaReferences:            true,
		BoundedListing:              true,
		RegistrationCreationOutcome: false,
		SoftDelete:                  true,
		HardDelete:                  true,
	}
	return &Provider{
		endpoint:        endpoint,
		scope:           config.Scope,
		transport:       config.Transport,
		credentials:     config.Credentials,
		timeout:         config.RequestTimeout,
		maxResponse:     config.MaxResponseBytes,
		maxAttempts:     config.MaxAttempts,
		retryDelay:      config.RetryDelay,
		slots:           make(chan struct{}, config.MaxConcurrent),
		referenceLimits: config.ReferenceLimits,
		canonicalizers:  canonicalizers,
		capabilities:    capabilities,
	}, nil
}

// Capabilities returns an immutable snapshot of Confluent semantics.
func (provider *Provider) Capabilities() schemaregistry.Capabilities {
	capabilities := provider.capabilities
	capabilities.Formats = append([]schemaregistry.Format(nil), capabilities.Formats...)
	capabilities.Lookups = append([]schemaregistry.LookupKind(nil), capabilities.Lookups...)
	capabilities.CompatibilityModes = append([]schemaregistry.CompatibilityMode(nil), capabilities.CompatibilityModes...)
	return capabilities
}

type schemaRequest struct {
	Schema     string            `json:"schema"`
	SchemaType string            `json:"schemaType,omitempty"`
	References []schemaReference `json:"references,omitempty"`
}

type schemaReference struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Version uint64 `json:"version"`
}

type registeredSchema struct {
	Subject    string            `json:"subject"`
	Version    uint64            `json:"version"`
	ID         int64             `json:"id"`
	Schema     string            `json:"schema"`
	SchemaType string            `json:"schemaType"`
	References []schemaReference `json:"references"`
}

// Register performs exact-content lookup before idempotent registration.
func (provider *Provider) Register(ctx context.Context, request schemaregistry.RegisterRequest) (schemaregistry.RegisterResult, error) {
	ctx, cancel := provider.withBudget(ctx)
	defer cancel()
	if request.Subject.Name == "" || request.Subject.Registry != "" {
		return schemaregistry.RegisterResult{}, fmt.Errorf("%w: Confluent subject", schemaregistry.ErrInvalidRequest)
	}
	body, err := provider.requestForSchema(request.Schema)
	if err != nil {
		return schemaregistry.RegisterResult{}, err
	}
	lookupPath := "/subjects/" + url.PathEscape(request.Subject.Name)
	var existing registeredSchema
	err = provider.doJSON(ctx, http.MethodPost, lookupPath, body, &existing)
	if err == nil {
		if existing.ID <= 0 || existing.Version == 0 {
			return schemaregistry.RegisterResult{}, ErrInvalidResponse
		}
		return schemaregistry.RegisterResult{
			Outcome: schemaregistry.RegistrationExisting,
			ID:      provider.id(existing.ID),
			Version: schemaregistry.Version{Number: existing.Version},
		}, nil
	}
	if !errors.Is(err, schemaregistry.ErrNotFound) {
		return schemaregistry.RegisterResult{}, err
	}
	var response struct {
		ID int64 `json:"id"`
	}
	if err := provider.doJSON(ctx, http.MethodPost, lookupPath+"/versions", body, &response); err != nil {
		if errors.Is(err, schemaregistry.ErrUnavailable) {
			return schemaregistry.RegisterResult{Outcome: schemaregistry.RegistrationUnknown}, fmt.Errorf("%w: %v", schemaregistry.ErrUnknownOutcome, err)
		}
		return schemaregistry.RegisterResult{}, err
	}
	if response.ID <= 0 {
		return schemaregistry.RegisterResult{}, ErrInvalidResponse
	}
	// Confluent does not return whether this request won a concurrent create.
	return schemaregistry.RegisterResult{Outcome: schemaregistry.RegistrationUnknown, ID: provider.id(response.ID)}, nil
}

// Resolve loads one schema by an advertised Confluent selector and resolves its bounded reference graph.
func (provider *Provider) Resolve(ctx context.Context, lookup schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
	ctx, cancel := provider.withBudget(ctx)
	defer cancel()
	var path string
	result := schemaregistry.ResolveResult{Lifecycle: schemaregistry.LifecycleAvailable}
	switch lookup.Kind() {
	case schemaregistry.LookupByProviderID:
		id := lookup.ProviderID()
		if id.Provider != ProviderName || id.Scope != provider.scope {
			return schemaregistry.ResolveResult{}, fmt.Errorf("%w: Confluent provider ID", schemaregistry.ErrInvalidRequest)
		}
		value, err := strconv.ParseUint(id.Value, 10, 32)
		if err != nil || value == 0 {
			return schemaregistry.ResolveResult{}, fmt.Errorf("%w: Confluent schema ID", schemaregistry.ErrInvalidRequest)
		}
		path = "/schemas/ids/" + id.Value
		result.ID = id
	case schemaregistry.LookupByVersion, schemaregistry.LookupLatest:
		subject := lookup.Subject()
		if subject.Name == "" || subject.Registry != "" {
			return schemaregistry.ResolveResult{}, fmt.Errorf("%w: Confluent subject", schemaregistry.ErrInvalidRequest)
		}
		version := "latest"
		if lookup.Kind() == schemaregistry.LookupByVersion {
			if lookup.Version().Number == 0 || lookup.Version().Opaque != "" {
				return schemaregistry.ResolveResult{}, fmt.Errorf("%w: Confluent version", schemaregistry.ErrInvalidRequest)
			}
			version = strconv.FormatUint(lookup.Version().Number, 10)
		}
		path = "/subjects/" + url.PathEscape(subject.Name) + "/versions/" + version
		result.Subject = subject
	default:
		return schemaregistry.ResolveResult{}, fmt.Errorf("%w: Confluent lookup", schemaregistry.ErrUnsupportedOperation)
	}

	var response registeredSchema
	if err := provider.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return schemaregistry.ResolveResult{}, err
	}
	if lookup.Kind() == schemaregistry.LookupByVersion || lookup.Kind() == schemaregistry.LookupLatest {
		if response.ID <= 0 || response.Subject != lookup.Subject().Name || response.Version == 0 ||
			(lookup.Kind() == schemaregistry.LookupByVersion && response.Version != lookup.Version().Number) {
			return schemaregistry.ResolveResult{}, ErrInvalidResponse
		}
	}
	rootCoordinate := referenceCoordinate(response.Subject, response.Version)
	state := make(map[schemaregistry.ReferenceCoordinate]uint8, provider.referenceLimits.MaxSchemas)
	schemaCount := 0
	referenceCount := 0
	schema, err := provider.compileResponse(ctx, response, rootCoordinate, state, &schemaCount, &referenceCount, 1)
	if err != nil {
		return schemaregistry.ResolveResult{}, err
	}
	result.Schema = schema
	if lookup.Kind() == schemaregistry.LookupByProviderID {
		if response.Subject != "" {
			result.Subject = schemaregistry.Subject{Name: response.Subject}
		}
		result.Version = schemaregistry.Version{Number: response.Version}
	} else {
		result.ID = provider.id(response.ID)
		result.Subject = schemaregistry.Subject{Name: response.Subject}
		result.Version = schemaregistry.Version{Number: response.Version}
	}
	return result, nil
}

// CheckCompatibility checks the candidate only when the configured subject mode matches the requested mode.
func (provider *Provider) CheckCompatibility(ctx context.Context, request schemaregistry.CompatibilityRequest) (schemaregistry.CompatibilityResult, error) {
	ctx, cancel := provider.withBudget(ctx)
	defer cancel()
	if request.Subject.Name == "" || request.Subject.Registry != "" {
		return schemaregistry.CompatibilityResult{}, fmt.Errorf("%w: Confluent subject", schemaregistry.ErrInvalidRequest)
	}
	body, err := provider.requestForSchema(request.Candidate)
	if err != nil {
		return schemaregistry.CompatibilityResult{}, err
	}
	var response struct {
		Compatible bool     `json:"is_compatible"`
		Messages   []string `json:"messages"`
	}
	var configuration struct {
		Level string `json:"compatibilityLevel"`
	}
	configPath := "/config/" + url.PathEscape(request.Subject.Name) + "?defaultToGlobal=true"
	if err := provider.doJSON(ctx, http.MethodGet, configPath, nil, &configuration); err != nil {
		return schemaregistry.CompatibilityResult{}, err
	}
	configuredMode, err := compatibilityMode(configuration.Level)
	if err != nil {
		return schemaregistry.CompatibilityResult{}, err
	}
	if configuredMode != request.Mode {
		return schemaregistry.CompatibilityResult{
			Supported: false,
			Diagnostics: []schemaregistry.Diagnostic{{
				Code:    "confluent-mode-mismatch",
				Message: "subject compatibility policy does not match requested mode",
			}},
		}, nil
	}
	path := "/compatibility/subjects/" + url.PathEscape(request.Subject.Name) + "/versions?verbose=true"
	if err := provider.doJSON(ctx, http.MethodPost, path, body, &response); err != nil {
		return schemaregistry.CompatibilityResult{}, err
	}
	diagnostics := make([]schemaregistry.Diagnostic, 0, len(response.Messages))
	for _, message := range response.Messages {
		diagnostics = append(diagnostics, schemaregistry.Diagnostic{Code: "confluent", Message: message})
	}
	return schemaregistry.CompatibilityResult{Supported: true, Compatible: response.Compatible, Diagnostics: diagnostics}, nil
}

// List returns a bounded, deterministic page of subject descriptors. Confluent
// does not expose portable fingerprints or schema versions in its subject-list
// response, so those fields remain unset.
func (provider *Provider) List(ctx context.Context, request schemaregistry.ListRequest) (schemaregistry.ListPage, error) {
	ctx, cancel := provider.withBudget(ctx)
	defer cancel()
	if request.Limit <= 0 {
		return schemaregistry.ListPage{}, fmt.Errorf("%w: list limit", schemaregistry.ErrInvalidRequest)
	}
	offset := 0
	if request.PageToken != "" {
		parsed, err := strconv.Atoi(request.PageToken)
		if err != nil || parsed < 0 {
			return schemaregistry.ListPage{}, fmt.Errorf("%w: page token", schemaregistry.ErrInvalidRequest)
		}
		offset = parsed
	}
	fetchLimit := request.Limit
	if fetchLimit < int(^uint(0)>>1) {
		fetchLimit++
	}
	query := url.Values{}
	query.Set("deleted", "true")
	query.Set("subjectPrefix", request.SubjectPrefix)
	query.Set("offset", strconv.Itoa(offset))
	query.Set("limit", strconv.Itoa(fetchLimit))
	var subjects []string
	if err := provider.doJSON(ctx, http.MethodGet, "/subjects?"+query.Encode(), nil, &subjects); err != nil {
		return schemaregistry.ListPage{}, err
	}
	if len(subjects) > fetchLimit {
		return schemaregistry.ListPage{}, ErrInvalidResponse
	}
	for _, subject := range subjects {
		if !strings.HasPrefix(subject, request.SubjectPrefix) {
			return schemaregistry.ListPage{}, ErrInvalidResponse
		}
	}
	slices.Sort(subjects)
	hasNext := len(subjects) > request.Limit
	pageSubjects := subjects
	if hasNext {
		pageSubjects = make([]string, request.Limit)
		copy(pageSubjects, subjects)
	}
	page := schemaregistry.ListPage{Schemas: make([]schemaregistry.SchemaDescriptor, 0)}
	for _, subject := range pageSubjects {
		page.Schemas = append(page.Schemas, schemaregistry.SchemaDescriptor{
			Subject: schemaregistry.Subject{Name: subject}, Lifecycle: schemaregistry.LifecycleUnknown,
		})
	}
	if hasNext {
		if offset > int(^uint(0)>>1)-len(pageSubjects) {
			return schemaregistry.ListPage{}, fmt.Errorf("%w: page token", schemaregistry.ErrInvalidRequest)
		}
		page.NextPageToken = strconv.Itoa(offset + len(pageSubjects))
	}
	return page, nil
}

// Delete resolves the exact target first and refuses deletion unless its
// portable fingerprint matches the caller's confirmation guard.
func (provider *Provider) Delete(ctx context.Context, request schemaregistry.DeleteRequest) (schemaregistry.DeleteResult, error) {
	ctx, cancel := provider.withBudget(ctx)
	defer cancel()
	if request.Subject.Name == "" || request.Subject.Registry != "" || request.Version.Number == 0 ||
		request.Version.Opaque != "" || request.Policy.ExpectedFingerprint == (schemaregistry.Fingerprint{}) {
		return schemaregistry.DeleteResult{}, fmt.Errorf("%w: Confluent deletion", schemaregistry.ErrInvalidRequest)
	}
	resolved, err := provider.Resolve(ctx, schemaregistry.AtVersion(request.Subject, request.Version))
	if err != nil {
		return schemaregistry.DeleteResult{}, err
	}
	if resolved.Schema.Fingerprint() != request.Policy.ExpectedFingerprint {
		return schemaregistry.DeleteResult{}, schemaregistry.ErrConfirmationRequired
	}
	requestPath := "/subjects/" + url.PathEscape(request.Subject.Name) + "/versions/" + strconv.FormatUint(request.Version.Number, 10)
	state := schemaregistry.LifecycleDeleted
	switch request.Policy.Mode {
	case schemaregistry.DeleteSoft:
		state = schemaregistry.LifecycleDeleting
	case schemaregistry.DeleteHard:
		requestPath += "?permanent=true"
	default:
		return schemaregistry.DeleteResult{}, fmt.Errorf("%w: deletion mode", schemaregistry.ErrInvalidRequest)
	}
	var deletedVersion uint64
	if err := provider.doJSON(ctx, http.MethodDelete, requestPath, nil, &deletedVersion); err != nil {
		return schemaregistry.DeleteResult{}, err
	}
	if deletedVersion != request.Version.Number {
		return schemaregistry.DeleteResult{}, ErrInvalidResponse
	}
	return schemaregistry.DeleteResult{Lifecycle: state}, nil
}

func (provider *Provider) compileResponse(
	ctx context.Context,
	response registeredSchema,
	coordinate schemaregistry.ReferenceCoordinate,
	state map[schemaregistry.ReferenceCoordinate]uint8,
	schemaCount *int,
	referenceCount *int,
	depth int,
) (schemaregistry.Schema, error) {
	if depth > provider.referenceLimits.MaxDepth {
		return schemaregistry.Schema{}, fmt.Errorf("%w: depth", schemaregistry.ErrReferenceLimit)
	}
	if coordinate.Subject.Name != "" && coordinate.Version.Number != 0 {
		switch state[coordinate] {
		case 1:
			return schemaregistry.Schema{}, fmt.Errorf("%w: %s", schemaregistry.ErrReferenceCycle, coordinate.Subject.Name)
		case 2:
			// A shared dependency is a valid DAG edge. It is recompiled here so
			// each reference retains an independently verified fingerprint.
		}
		state[coordinate] = 1
		defer func() { state[coordinate] = 2 }()
	}
	*schemaCount++
	if *schemaCount > provider.referenceLimits.MaxSchemas {
		return schemaregistry.Schema{}, fmt.Errorf("%w: schemas", schemaregistry.ErrReferenceLimit)
	}
	references := make([]schemaregistry.Reference, 0, len(response.References))
	for _, reference := range response.References {
		*referenceCount++
		if *referenceCount > provider.referenceLimits.MaxReferences || reference.Name == "" ||
			reference.Subject == "" || reference.Version == 0 {
			return schemaregistry.Schema{}, fmt.Errorf("%w: Confluent references", schemaregistry.ErrReferenceLimit)
		}
		var dependency registeredSchema
		path := "/subjects/" + url.PathEscape(reference.Subject) + "/versions/" + strconv.FormatUint(reference.Version, 10)
		if err := provider.doJSON(ctx, http.MethodGet, path, nil, &dependency); err != nil {
			if errors.Is(err, schemaregistry.ErrNotFound) {
				return schemaregistry.Schema{}, fmt.Errorf("%w: %s", schemaregistry.ErrReferenceMissing, reference.Name)
			}
			return schemaregistry.Schema{}, err
		}
		if dependency.Subject != reference.Subject || dependency.Version != reference.Version {
			return schemaregistry.Schema{}, ErrInvalidResponse
		}
		dependencyCoordinate := referenceCoordinate(reference.Subject, reference.Version)
		compiled, err := provider.compileResponse(ctx, dependency, dependencyCoordinate, state, schemaCount, referenceCount, depth+1)
		if err != nil {
			return schemaregistry.Schema{}, err
		}
		references = append(references, schemaregistry.Reference{
			Name: reference.Name, Subject: reference.Subject, Version: reference.Version,
			Fingerprint: compiled.Fingerprint(),
		})
	}
	format, err := confluentFormat(response.SchemaType)
	if err != nil {
		return schemaregistry.Schema{}, err
	}
	canonicalizer := provider.canonicalizers[format]
	if interfaceIsNil(canonicalizer) {
		return schemaregistry.Schema{}, fmt.Errorf("%w: %s", schemaregistry.ErrUnsupportedFormat, format)
	}
	return schemaregistry.Compile(ctx, schemaregistry.Definition{
		Format: format, Content: []byte(response.Schema), References: references,
	}, canonicalizer)
}

func referenceCoordinate(subject string, version uint64) schemaregistry.ReferenceCoordinate {
	return schemaregistry.ReferenceCoordinate{
		Subject: schemaregistry.Subject{Name: subject}, Version: schemaregistry.Version{Number: version},
	}
}

func (provider *Provider) requestForSchema(schema schemaregistry.Schema) ([]byte, error) {
	definition := schema.Definition()
	schemaType, err := schemaType(definition.Format)
	if err != nil {
		return nil, err
	}
	references := make([]schemaReference, 0, len(definition.References))
	for _, reference := range definition.References {
		if reference.Name == "" || reference.Subject == "" || reference.Version == 0 {
			return nil, fmt.Errorf("%w: Confluent reference coordinates", schemaregistry.ErrInvalidRequest)
		}
		references = append(references, schemaReference{Name: reference.Name, Subject: reference.Subject, Version: reference.Version})
	}
	return json.Marshal(schemaRequest{Schema: string(definition.Content), SchemaType: schemaType, References: references})
}

func (provider *Provider) doJSON(ctx context.Context, method, requestPath string, body []byte, target any) error {
	ctx, cancel := context.WithTimeout(ctx, provider.timeout)
	defer cancel()
	select {
	case provider.slots <- struct{}{}:
		defer func() { <-provider.slots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	for attempt := 1; ; attempt++ {
		requestURL := *provider.endpoint
		pathPart, rawQuery, hasQuery := strings.Cut(requestPath, "?")
		escapedPath := strings.TrimRight(provider.endpoint.EscapedPath(), "/") + pathPart
		decodedPath, err := url.PathUnescape(escapedPath)
		if err != nil {
			return fmt.Errorf("construct Confluent request path: %w", err)
		}
		requestURL.Path = decodedPath
		requestURL.RawPath = escapedPath
		if hasQuery {
			requestURL.RawQuery = rawQuery
		}
		request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("construct Confluent request: %w", err)
		}
		request.Header.Set("Accept", mediaType)
		request.Header.Set("Confluent-Accept-Unknown-Properties", "true")
		if body != nil {
			request.Header.Set("Content-Type", mediaType)
		}
		if !interfaceIsNil(provider.credentials) {
			authorization, err := provider.credentials.Authorization(ctx)
			if err != nil {
				return fmt.Errorf("load Confluent credentials: %w", err)
			}
			request.Header.Set("Authorization", authorization)
		}
		response, err := provider.transport.RoundTrip(request)
		if err != nil {
			if attempt < provider.maxAttempts {
				if err := provider.waitRetry(ctx); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("%w: Confluent transport", schemaregistry.ErrUnavailable)
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, int64(provider.maxResponse)+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			return fmt.Errorf("%w: read Confluent response", schemaregistry.ErrUnavailable)
		}
		if len(responseBody) > provider.maxResponse {
			return fmt.Errorf("%w: Confluent response bytes", schemaregistry.ErrLimitExceeded)
		}
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			if attempt < provider.maxAttempts {
				if err := provider.waitRetry(ctx); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("%w: Confluent HTTP %d", schemaregistry.ErrUnavailable, response.StatusCode)
		}
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return schemaregistry.ErrUnauthorized
		case http.StatusNotFound:
			return schemaregistry.ErrNotFound
		case http.StatusConflict:
			return schemaregistry.ErrIncompatible
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("%w: Confluent HTTP %d", schemaregistry.ErrRejected, response.StatusCode)
		}
		if target == nil || len(responseBody) == 0 {
			return nil
		}
		decoder := json.NewDecoder(bytes.NewReader(responseBody))
		if err := decoder.Decode(target); err != nil {
			return fmt.Errorf("%w: decode JSON", ErrInvalidResponse)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: trailing JSON", ErrInvalidResponse)
		}
		return nil
	}
}

func (provider *Provider) withBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, provider.timeout)
}

func (provider *Provider) waitRetry(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(provider.retryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

func (provider *Provider) id(id int64) schemaregistry.ProviderID {
	return schemaregistry.ProviderID{Provider: ProviderName, Scope: provider.scope, Value: strconv.FormatInt(id, 10)}
}

func schemaType(format schemaregistry.Format) (string, error) {
	switch format {
	case schemaregistry.FormatAvro:
		return "AVRO", nil
	case schemaregistry.FormatJSONSchema:
		return "JSON", nil
	case schemaregistry.FormatProtobuf:
		return "PROTOBUF", nil
	default:
		return "", fmt.Errorf("%w: %s", schemaregistry.ErrUnsupportedFormat, format)
	}
}

func confluentFormat(value string) (schemaregistry.Format, error) {
	switch value {
	case "", "AVRO":
		return schemaregistry.FormatAvro, nil
	case "JSON":
		return schemaregistry.FormatJSONSchema, nil
	case "PROTOBUF":
		return schemaregistry.FormatProtobuf, nil
	default:
		return "", fmt.Errorf("%w: Confluent schema type %q", ErrInvalidResponse, value)
	}
}

func compatibilityMode(value string) (schemaregistry.CompatibilityMode, error) {
	switch value {
	case "BACKWARD":
		return schemaregistry.CompatibilityBackward, nil
	case "BACKWARD_TRANSITIVE":
		return schemaregistry.CompatibilityBackwardTransitive, nil
	case "FORWARD":
		return schemaregistry.CompatibilityForward, nil
	case "FORWARD_TRANSITIVE":
		return schemaregistry.CompatibilityForwardTransitive, nil
	case "FULL":
		return schemaregistry.CompatibilityFull, nil
	case "FULL_TRANSITIVE":
		return schemaregistry.CompatibilityFullTransitive, nil
	case "NONE":
		return schemaregistry.CompatibilityNone, nil
	default:
		return "", fmt.Errorf("%w: Confluent compatibility level %q", ErrInvalidResponse, value)
	}
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
