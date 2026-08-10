package schemaregistry

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
)

var (
	// ErrInvalidRequest marks an invalid operation request.
	ErrInvalidRequest = errors.New("schema registry: invalid request")
	// ErrUnsupportedOperation marks behavior the selected provider cannot
	// represent safely.
	ErrUnsupportedOperation = errors.New("schema registry: unsupported operation")
	// ErrLimitExceeded marks a configured resource bound violation.
	ErrLimitExceeded = errors.New("schema registry: limit exceeded")
	// ErrConfirmationRequired marks a destructive request without an exact
	// portable identity guard.
	ErrConfirmationRequired = errors.New("schema registry: deletion confirmation required")
)

// ProviderID is an opaque provider-issued identity scoped to one provider and
// provider-defined namespace. It is not a portable schema fingerprint.
type ProviderID struct {
	Provider string
	Scope    string
	Value    string
}

// Subject is a provider subject or schema name within an optional registry.
type Subject struct {
	Registry string
	Name     string
}

// Version keeps provider version numbers separate from opaque version tokens.
// A provider capability documents which field it uses.
type Version struct {
	Number uint64
	Opaque string
}

// LifecycleState is the provider-observed schema version lifecycle.
type LifecycleState string

const (
	// LifecyclePending identifies a provider version not yet available.
	LifecyclePending LifecycleState = "pending"
	// LifecycleAvailable identifies a provider version available for use.
	LifecycleAvailable LifecycleState = "available"
	// LifecycleDeleting identifies a transitional deletion state.
	LifecycleDeleting LifecycleState = "deleting"
	// LifecycleDeleted identifies a provider-confirmed deleted version.
	LifecycleDeleted LifecycleState = "deleted"
	// LifecycleFailed identifies a provider-reported failed version.
	LifecycleFailed LifecycleState = "failed"
	// LifecycleUnknown identifies lifecycle state the provider response cannot establish.
	LifecycleUnknown LifecycleState = "unknown"
)

// LookupKind identifies one unambiguous resolution selector.
type LookupKind string

const (
	// LookupByProviderID selects an opaque provider-issued identity.
	LookupByProviderID LookupKind = "provider-id"
	// LookupByFingerprint selects a portable canonical identity.
	LookupByFingerprint LookupKind = "fingerprint"
	// LookupByVersion selects one exact subject version.
	LookupByVersion LookupKind = "subject-version"
	// LookupLatest selects the provider-defined latest subject version.
	LookupLatest LookupKind = "latest"
)

// Lookup is constructed by the selector functions below so conflicting
// identity systems cannot be combined accidentally.
type Lookup struct {
	kind        LookupKind
	providerID  ProviderID
	fingerprint Fingerprint
	subject     Subject
	version     Version
}

// ByProviderID constructs a lookup scoped to one provider-issued identity.
func ByProviderID(id ProviderID) Lookup { return Lookup{kind: LookupByProviderID, providerID: id} }

// ByFingerprint constructs a lookup by portable canonical identity.
func ByFingerprint(fingerprint Fingerprint) Lookup {
	return Lookup{kind: LookupByFingerprint, fingerprint: fingerprint}
}

// AtVersion constructs a lookup for one exact subject version.
func AtVersion(subject Subject, version Version) Lookup {
	return Lookup{kind: LookupByVersion, subject: subject, version: version}
}

// Latest constructs a provider-defined latest-version lookup for a subject.
func Latest(subject Subject) Lookup { return Lookup{kind: LookupLatest, subject: subject} }

// Kind returns the lookup's single selector kind.
func (lookup Lookup) Kind() LookupKind { return lookup.kind }

// ProviderID returns the provider selector, or its zero value for other kinds.
func (lookup Lookup) ProviderID() ProviderID { return lookup.providerID }

// Fingerprint returns the portable selector, or its zero value for other kinds.
func (lookup Lookup) Fingerprint() Fingerprint { return lookup.fingerprint }

// Subject returns the subject selector, or its zero value for identity-only kinds.
func (lookup Lookup) Subject() Subject { return lookup.subject }

// Version returns the exact version selector, or its zero value for other kinds.
func (lookup Lookup) Version() Version { return lookup.version }

// RegistrationOutcome distinguishes idempotent success and every material
// uncertain or rejected provider result.
type RegistrationOutcome string

const (
	// RegistrationCreated reports a provider-confirmed new version.
	RegistrationCreated RegistrationOutcome = "created"
	// RegistrationExisting reports an idempotently resolved existing version.
	RegistrationExisting RegistrationOutcome = "existing"
	// RegistrationIncompatible reports a definitive compatibility rejection.
	RegistrationIncompatible RegistrationOutcome = "incompatible"
	// RegistrationRejected reports another definitive request rejection.
	RegistrationRejected RegistrationOutcome = "rejected"
	// RegistrationUnauthorized reports an authentication or authorization rejection.
	RegistrationUnauthorized RegistrationOutcome = "unauthorized"
	// RegistrationUnavailable reports no provider result because the provider was unavailable.
	RegistrationUnavailable RegistrationOutcome = "unavailable"
	// RegistrationUnknown reports that the provider effect cannot be determined.
	RegistrationUnknown RegistrationOutcome = "unknown"
)

// CompatibilityMode is a portable request only when advertised by provider
// capabilities. Provider-specific modes use CompatibilityProviderSpecific and
// ProviderMode.
type CompatibilityMode string

const (
	// CompatibilityBackward requests provider-defined backward compatibility.
	CompatibilityBackward CompatibilityMode = "backward"
	// CompatibilityBackwardTransitive requests backward compatibility across provider-defined history.
	CompatibilityBackwardTransitive CompatibilityMode = "backward-transitive"
	// CompatibilityForward requests provider-defined forward compatibility.
	CompatibilityForward CompatibilityMode = "forward"
	// CompatibilityForwardTransitive requests forward compatibility across provider-defined history.
	CompatibilityForwardTransitive CompatibilityMode = "forward-transitive"
	// CompatibilityFull requests both backward and forward compatibility.
	CompatibilityFull CompatibilityMode = "full"
	// CompatibilityFullTransitive requests full compatibility across provider-defined history.
	CompatibilityFullTransitive CompatibilityMode = "full-transitive"
	// CompatibilityNone requests an explicit provider policy with compatibility disabled.
	CompatibilityNone CompatibilityMode = "none"
	// CompatibilityProviderSpecific carries an explicit non-portable ProviderMode.
	CompatibilityProviderSpecific CompatibilityMode = "provider-specific"
)

// Capabilities describes semantic support without claiming providers are
// interchangeable.
type Capabilities struct {
	Provider           string
	Formats            []Format
	Lookups            []LookupKind
	CompatibilityModes []CompatibilityMode
	NumericVersions    bool
	OpaqueVersions     bool
	SchemaReferences   bool
	BoundedListing     bool
	// RegistrationCreationOutcome reports whether the provider can safely
	// distinguish newly-created from concurrently-existing registration.
	RegistrationCreationOutcome bool
	SoftDelete                  bool
	HardDelete                  bool
}

// Limits are caller-selected operation bounds.
type Limits struct {
	MaxSchemaBytes int
	MaxListResults int
	MaxConcurrent  int
}

// RegisterRequest registers one compiled schema under an explicit subject.
type RegisterRequest struct {
	Subject Subject
	Schema  Schema
}

// RegisterResult reports the provider's explicit registration outcome.
type RegisterResult struct {
	Outcome RegistrationOutcome
	ID      ProviderID
	Version Version
}

// ResolveResult identifies both portable schema content and provider identity.
type ResolveResult struct {
	Schema    Schema
	ID        ProviderID
	Subject   Subject
	Version   Version
	Lifecycle LifecycleState
}

// CompatibilityRequest asks the provider to compare a candidate against an
// explicit subject history according to one advertised mode.
type CompatibilityRequest struct {
	Subject      Subject
	Candidate    Schema
	Mode         CompatibilityMode
	ProviderMode string
}

// CompatibilityResult never treats unsupported or indeterminate checks as
// compatible.
type CompatibilityResult struct {
	Supported   bool
	Compatible  bool
	Diagnostics []Diagnostic
}

// Diagnostic is safe structured context; adapters must not place full schema
// contents or credentials in these fields.
type Diagnostic struct {
	Code    string
	Message string
	Path    string
}

// Provider is the narrow remote-provider boundary used by Client.
type Provider interface {
	Capabilities() Capabilities
	Register(context.Context, RegisterRequest) (RegisterResult, error)
	Resolve(context.Context, Lookup) (ResolveResult, error)
	CheckCompatibility(context.Context, CompatibilityRequest) (CompatibilityResult, error)
}

// ListingProvider is an optional bounded administrative capability.
type ListingProvider interface {
	List(context.Context, ListRequest) (ListPage, error)
}

// DeletingProvider is an optional destructive administrative capability.
type DeletingProvider interface {
	Delete(context.Context, DeleteRequest) (DeleteResult, error)
}

// ListRequest is always bounded. PageToken is provider-opaque.
type ListRequest struct {
	SubjectPrefix string
	Limit         int
	PageToken     string
}

// SchemaDescriptor is bounded metadata returned by administrative listing.
type SchemaDescriptor struct {
	ID          ProviderID
	Subject     Subject
	Version     Version
	Format      Format
	Fingerprint Fingerprint
	Lifecycle   LifecycleState
}

// ListPage is one bounded provider page.
type ListPage struct {
	Schemas       []SchemaDescriptor
	NextPageToken string
}

// DeletionMode preserves soft and hard deletion differences.
type DeletionMode string

const (
	// DeleteSoft requests recoverable provider deletion when advertised.
	DeleteSoft DeletionMode = "soft"
	// DeleteHard requests permanent provider deletion when advertised.
	DeleteHard DeletionMode = "hard"
)

// DeletionPolicy makes destructive intent exact and collision-resistant.
type DeletionPolicy struct {
	Mode                DeletionMode
	ExpectedFingerprint Fingerprint
}

// DeleteRequest targets one exact subject version.
type DeleteRequest struct {
	Subject Subject
	Version Version
	Policy  DeletionPolicy
}

// DeleteResult reports the provider-observed terminal or transitional state.
type DeleteResult struct {
	Lifecycle LifecycleState
}

// Client validates portable requests against provider capabilities and caller
// bounds before any provider I/O.
type Client struct {
	provider      Provider
	capabilities  Capabilities
	limits        Limits
	slots         chan struct{}
	mu            sync.Mutex
	registrations map[registrationKey]*registrationFlight
}

type registrationKey struct {
	subject     Subject
	fingerprint Fingerprint
}

type registrationFlight struct {
	done   chan struct{}
	result RegisterResult
	err    error
}

// NewClient constructs a bounded client over one provider.
func NewClient(provider Provider, limits Limits) (*Client, error) {
	if interfaceIsNil(provider) {
		return nil, fmt.Errorf("%w: nil provider", ErrInvalidRequest)
	}
	if limits.MaxSchemaBytes <= 0 || limits.MaxListResults <= 0 || limits.MaxConcurrent <= 0 {
		return nil, fmt.Errorf("%w: limits must be positive", ErrInvalidRequest)
	}
	capabilities := cloneCapabilities(provider.Capabilities())
	if capabilities.Provider == "" {
		return nil, fmt.Errorf("%w: empty provider capability name", ErrInvalidRequest)
	}
	return &Client{
		provider: provider, capabilities: capabilities, limits: limits,
		slots:         make(chan struct{}, limits.MaxConcurrent),
		registrations: make(map[registrationKey]*registrationFlight),
	}, nil
}

// Capabilities returns an immutable snapshot of provider semantics.
func (client *Client) Capabilities() Capabilities {
	return cloneCapabilities(client.capabilities)
}

// Register validates bounds and format support before provider I/O.
func (client *Client) Register(ctx context.Context, request RegisterRequest) (RegisterResult, error) {
	if err := ctx.Err(); err != nil {
		return RegisterResult{}, err
	}
	if request.Subject.Name == "" {
		return RegisterResult{}, fmt.Errorf("%w: empty subject", ErrInvalidRequest)
	}
	definition := request.Schema.Definition()
	if len(definition.Content) > client.limits.MaxSchemaBytes {
		return RegisterResult{}, fmt.Errorf("%w: schema bytes", ErrLimitExceeded)
	}
	if !slices.Contains(client.capabilities.Formats, definition.Format) {
		return RegisterResult{}, fmt.Errorf("%w: format %s", ErrUnsupportedOperation, definition.Format)
	}
	key := registrationKey{subject: request.Subject, fingerprint: request.Schema.Fingerprint()}
	flight, leader := client.registration(key)
	if !leader {
		select {
		case <-ctx.Done():
			return RegisterResult{}, ctx.Err()
		case <-flight.done:
			return flight.result, flight.err
		}
	}
	if err := client.acquire(ctx); err != nil {
		client.finishRegistration(key, flight, RegisterResult{}, err)
		return RegisterResult{}, err
	}
	result, err := client.provider.Register(ctx, request)
	client.release()
	if err != nil {
		result.Outcome = registrationOutcome(err)
	}
	if err == nil {
		err = client.validateRegistrationResult(result)
	}
	client.finishRegistration(key, flight, result, err)
	return result, err
}

func registrationOutcome(err error) RegistrationOutcome {
	switch {
	case errors.Is(err, ErrIncompatible):
		return RegistrationIncompatible
	case errors.Is(err, ErrRejected):
		return RegistrationRejected
	case errors.Is(err, ErrUnauthorized):
		return RegistrationUnauthorized
	case errors.Is(err, ErrUnavailable):
		return RegistrationUnavailable
	default:
		return RegistrationUnknown
	}
}

// Resolve rejects selectors the provider cannot define safely.
func (client *Client) Resolve(ctx context.Context, lookup Lookup) (ResolveResult, error) {
	if err := ctx.Err(); err != nil {
		return ResolveResult{}, err
	}
	if !slices.Contains(client.capabilities.Lookups, lookup.kind) {
		return ResolveResult{}, fmt.Errorf("%w: lookup %s", ErrUnsupportedOperation, lookup.kind)
	}
	if err := lookup.validate(client.capabilities.Provider); err != nil {
		return ResolveResult{}, err
	}
	if lookup.kind == LookupByVersion {
		if err := client.validateVersionCapability(lookup.version); err != nil {
			return ResolveResult{}, err
		}
	}
	if err := client.acquire(ctx); err != nil {
		return ResolveResult{}, err
	}
	defer client.release()
	result, err := client.provider.Resolve(ctx, lookup)
	if err != nil {
		return result, err
	}
	if err := validateResolution(lookup, result); err != nil {
		return ResolveResult{}, err
	}
	if result.ID.Provider != client.capabilities.Provider {
		return ResolveResult{}, ErrResolutionMismatch
	}
	return result, nil
}

// CheckCompatibility returns an explicit unsupported result without provider
// I/O when the requested semantics are unavailable.
func (client *Client) CheckCompatibility(
	ctx context.Context,
	request CompatibilityRequest,
) (CompatibilityResult, error) {
	if err := ctx.Err(); err != nil {
		return CompatibilityResult{}, err
	}
	if !slices.Contains(client.capabilities.CompatibilityModes, request.Mode) {
		return CompatibilityResult{Supported: false}, nil
	}
	definition := request.Candidate.Definition()
	if request.Subject.Name == "" || len(definition.Content) == 0 {
		return CompatibilityResult{}, fmt.Errorf("%w: compatibility request", ErrInvalidRequest)
	}
	if len(definition.Content) > client.limits.MaxSchemaBytes {
		return CompatibilityResult{}, fmt.Errorf("%w: schema bytes", ErrLimitExceeded)
	}
	if !slices.Contains(client.capabilities.Formats, definition.Format) {
		return CompatibilityResult{}, fmt.Errorf("%w: format %s", ErrUnsupportedOperation, definition.Format)
	}
	if (request.Mode == CompatibilityProviderSpecific) != (request.ProviderMode != "") {
		return CompatibilityResult{}, fmt.Errorf("%w: provider compatibility mode", ErrInvalidRequest)
	}
	if err := client.acquire(ctx); err != nil {
		return CompatibilityResult{}, err
	}
	defer client.release()
	result, err := client.provider.CheckCompatibility(ctx, request)
	if err == nil && !result.Supported && result.Compatible {
		return CompatibilityResult{}, fmt.Errorf("%w: contradictory compatibility result", ErrInvalidRequest)
	}
	return result, err
}

// List delegates only when the provider advertises bounded listing and the
// request fits caller limits.
func (client *Client) List(ctx context.Context, request ListRequest) (ListPage, error) {
	if err := ctx.Err(); err != nil {
		return ListPage{}, err
	}
	if !client.capabilities.BoundedListing {
		return ListPage{}, fmt.Errorf("%w: listing", ErrUnsupportedOperation)
	}
	if request.Limit <= 0 {
		return ListPage{}, fmt.Errorf("%w: list limit", ErrInvalidRequest)
	}
	if request.Limit > client.limits.MaxListResults {
		return ListPage{}, fmt.Errorf("%w: list results", ErrLimitExceeded)
	}
	provider, ok := client.provider.(ListingProvider)
	if !ok || interfaceIsNil(provider) {
		return ListPage{}, fmt.Errorf("%w: listing contract", ErrUnsupportedOperation)
	}
	if err := client.acquire(ctx); err != nil {
		return ListPage{}, err
	}
	page, err := provider.List(ctx, request)
	client.release()
	if err != nil {
		return ListPage{}, err
	}
	if len(page.Schemas) > request.Limit {
		return ListPage{}, fmt.Errorf("%w: provider list response", ErrLimitExceeded)
	}
	page.Schemas = append([]SchemaDescriptor(nil), page.Schemas...)
	return page, nil
}

// Delete requires an exact portable fingerprint guard for both soft and hard
// deletion, then delegates only to an advertised provider capability.
func (client *Client) Delete(ctx context.Context, request DeleteRequest) (DeleteResult, error) {
	if err := ctx.Err(); err != nil {
		return DeleteResult{}, err
	}
	if request.Subject.Name == "" || !request.Version.valid() {
		return DeleteResult{}, fmt.Errorf("%w: deletion target", ErrInvalidRequest)
	}
	if err := client.validateVersionCapability(request.Version); err != nil {
		return DeleteResult{}, err
	}
	if request.Policy.ExpectedFingerprint == (Fingerprint{}) {
		return DeleteResult{}, ErrConfirmationRequired
	}
	switch request.Policy.Mode {
	case DeleteSoft:
		if !client.capabilities.SoftDelete {
			return DeleteResult{}, fmt.Errorf("%w: soft deletion", ErrUnsupportedOperation)
		}
	case DeleteHard:
		if !client.capabilities.HardDelete {
			return DeleteResult{}, fmt.Errorf("%w: hard deletion", ErrUnsupportedOperation)
		}
	default:
		return DeleteResult{}, fmt.Errorf("%w: deletion mode", ErrInvalidRequest)
	}
	provider, ok := client.provider.(DeletingProvider)
	if !ok || interfaceIsNil(provider) {
		return DeleteResult{}, fmt.Errorf("%w: deletion contract", ErrUnsupportedOperation)
	}
	if err := client.acquire(ctx); err != nil {
		return DeleteResult{}, err
	}
	defer client.release()
	result, err := provider.Delete(ctx, request)
	if err == nil && !result.Lifecycle.valid() {
		return DeleteResult{}, fmt.Errorf("%w: deletion lifecycle", ErrInvalidRequest)
	}
	return result, err
}

func (client *Client) acquire(ctx context.Context) error {
	select {
	case client.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (client *Client) release() { <-client.slots }

func (client *Client) registration(key registrationKey) (*registrationFlight, bool) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if flight, found := client.registrations[key]; found {
		return flight, false
	}
	flight := &registrationFlight{done: make(chan struct{})}
	client.registrations[key] = flight
	return flight, true
}

func (client *Client) finishRegistration(
	key registrationKey,
	flight *registrationFlight,
	result RegisterResult,
	err error,
) {
	client.mu.Lock()
	flight.result = result
	flight.err = err
	delete(client.registrations, key)
	close(flight.done)
	client.mu.Unlock()
}

func (lookup Lookup) validate(provider string) error {
	switch lookup.kind {
	case LookupByProviderID:
		if provider == "" || lookup.providerID.Provider != provider || lookup.providerID.Value == "" {
			return fmt.Errorf("%w: provider ID", ErrInvalidRequest)
		}
	case LookupByFingerprint:
		if lookup.fingerprint == (Fingerprint{}) {
			return fmt.Errorf("%w: fingerprint", ErrInvalidRequest)
		}
	case LookupByVersion:
		if lookup.subject.Name == "" || !lookup.version.valid() {
			return fmt.Errorf("%w: subject version", ErrInvalidRequest)
		}
	case LookupLatest:
		if lookup.subject.Name == "" {
			return fmt.Errorf("%w: latest subject", ErrInvalidRequest)
		}
	default:
		return fmt.Errorf("%w: lookup kind", ErrInvalidRequest)
	}
	return nil
}

func (version Version) valid() bool {
	return (version.Number != 0) != (version.Opaque != "")
}

func (client *Client) validateVersionCapability(version Version) error {
	if version.Number != 0 && !client.capabilities.NumericVersions {
		return fmt.Errorf("%w: numeric versions", ErrUnsupportedOperation)
	}
	if version.Opaque != "" && !client.capabilities.OpaqueVersions {
		return fmt.Errorf("%w: opaque versions", ErrUnsupportedOperation)
	}
	return nil
}

func (client *Client) validateRegistrationResult(result RegisterResult) error {
	switch result.Outcome {
	case RegistrationCreated:
		if !client.capabilities.RegistrationCreationOutcome {
			return fmt.Errorf("%w: unprovable created registration outcome", ErrInvalidRequest)
		}
	case RegistrationExisting, RegistrationUnknown:
	default:
		return fmt.Errorf("%w: registration outcome", ErrInvalidRequest)
	}
	if result.ID.Provider != client.capabilities.Provider || result.ID.Value == "" {
		return fmt.Errorf("%w: registration provider ID", ErrInvalidRequest)
	}
	if result.Version != (Version{}) {
		if !result.Version.valid() {
			return fmt.Errorf("%w: registration version", ErrInvalidRequest)
		}
		if err := client.validateVersionCapability(result.Version); err != nil {
			return err
		}
	}
	return nil
}

func cloneCapabilities(capabilities Capabilities) Capabilities {
	capabilities.Formats = append([]Format(nil), capabilities.Formats...)
	capabilities.Lookups = append([]LookupKind(nil), capabilities.Lookups...)
	capabilities.CompatibilityModes = append(
		[]CompatibilityMode(nil),
		capabilities.CompatibilityModes...,
	)
	return capabilities
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
