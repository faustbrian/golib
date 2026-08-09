// Package audit provides immutable-by-contract records for security-relevant
// and business-relevant actions. It is not a logging, tracing, metrics,
// authorization, event-sourcing, or domain-event framework.
package audit

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	canonicalTimeLayout  = "2006-01-02T15:04:05.999999999Z07:00"
	integrityDigestBytes = 32
)

var (
	// ErrInvalidArgument classifies a caller-supplied value that violates the
	// bounded audit record contract.
	ErrInvalidArgument = errors.New("audit: invalid argument")
	// ErrSensitiveData classifies data rejected before persistence because it
	// belongs to a prohibited secret-bearing namespace.
	ErrSensitiveData = errors.New("audit: sensitive data is prohibited")
	// ErrIntegrityInvalid reports a digest, sequence, partition, or key mismatch.
	ErrIntegrityInvalid = errors.New("audit: integrity verification failed")
)

// Outcome is the explicit result of an audited action. Its zero value is
// invalid so callers cannot accidentally omit the outcome.
type Outcome uint8

const (
	// OutcomeSucceeded reports a completed successful action.
	OutcomeSucceeded Outcome = iota + 1
	// OutcomeFailed reports an attempted action that failed.
	OutcomeFailed
	// OutcomeDenied reports an action rejected by caller-owned policy.
	OutcomeDenied
	// OutcomeUnknown reports an outcome the caller cannot determine.
	OutcomeUnknown
)

// ActorKind distinguishes known identities from explicit system, anonymous,
// and unknown actors. Its zero value is invalid.
type ActorKind uint8

const (
	// ActorHuman identifies a person by a required stable ID.
	ActorHuman ActorKind = iota + 1
	// ActorService identifies a workload by a required stable ID.
	ActorService
	// ActorSystem identifies an explicit system or subsystem actor.
	ActorSystem
	// ActorAnonymous identifies a deliberately unauthenticated actor without ID.
	ActorAnonymous
	// ActorUnknown identifies an unavailable actor identity without guessing.
	ActorUnknown
)

// Limits bounds every caller-controlled collection and variable-length field.
type Limits struct {
	MaxRecordBytes      int
	MaxFieldBytes       int
	MaxDescriptionBytes int
	MaxAttributeEntries int
	MaxAttributeBytes   int
	MaxChangeEntries    int
	MaxChangeBytes      int
	MaxIntegrityBytes   int
}

// DefaultLimits returns conservative limits suitable for online recording.
func DefaultLimits() Limits {
	return Limits{
		MaxRecordBytes:      1 << 20,
		MaxFieldBytes:       1 << 10,
		MaxDescriptionBytes: 4 << 10,
		MaxAttributeEntries: 64,
		MaxAttributeBytes:   32 << 10,
		MaxChangeEntries:    256,
		MaxChangeBytes:      256 << 10,
		MaxIntegrityBytes:   integrityDigestBytes,
	}
}

// Validate reports whether every configured limit is positive and coherent.
func (limits Limits) Validate() error {
	if limits.MaxRecordBytes <= 0 || limits.MaxFieldBytes <= 0 ||
		limits.MaxDescriptionBytes <= 0 || limits.MaxAttributeEntries <= 0 ||
		limits.MaxAttributeBytes <= 0 || limits.MaxChangeEntries <= 0 ||
		limits.MaxChangeBytes <= 0 || limits.MaxIntegrityBytes != integrityDigestBytes {
		return invalid("limits", "all limits must be positive")
	}
	return nil
}

// ActorInput supplies actor identity to a new record.
type ActorInput struct {
	Kind                 ActorKind
	ID                   string
	AuthenticationMethod string
	DelegatedBy          *ActorInput
}

// Actor is an immutable actor identity. A zero Actor represents no delegated
// actor; top-level records always contain a validated non-zero actor kind.
type Actor struct {
	kind                 ActorKind
	id                   string
	authenticationMethod string
	delegatedBy          *Actor
}

// Kind returns the actor identity semantics.
func (actor Actor) Kind() ActorKind { return actor.kind }

// ID returns the stable actor ID, or empty for anonymous and unknown actors.
func (actor Actor) ID() string { return actor.id }

// AuthenticationMethod returns the safe optional authentication-method label.
func (actor Actor) AuthenticationMethod() string { return actor.authenticationMethod }

// DelegatedBy returns the immediate delegating actor, or the zero Actor when
// delegation was absent. Nested delegation is rejected during construction.
func (actor Actor) DelegatedBy() Actor {
	if actor.delegatedBy == nil {
		return Actor{}
	}
	return *actor.delegatedBy
}

// SubjectInput supplies the stable identity of an audited resource. Deleted
// identities retain their stable ID; Deleted describes lifecycle state only.
type SubjectInput struct {
	Type    string
	ID      string
	Deleted bool
}

// Subject is an immutable resource identity.
type Subject struct {
	resourceType, id string
	deleted          bool
}

// Type returns the stable application-defined resource type.
func (subject Subject) Type() string { return subject.resourceType }

// ID returns the stable resource identifier, including for deleted identities.
func (subject Subject) ID() string { return subject.id }

// Deleted reports the resource lifecycle state without erasing its identity.
func (subject Subject) Deleted() bool { return subject.deleted }

// ContextInput supplies optional request and deployment context. Empty values
// mean the value was absent; callers use explicit "unknown" values only when
// policy requires distinguishing unknown from absent.
type ContextInput struct {
	TenantID, CorrelationID, CausationID, RequestID, TraceID, IdempotencyID string
	SourceService, SourceVersion, Environment, NetworkOrigin, UserAgent     string
}

// RecordContext is immutable contextual metadata.
type RecordContext struct {
	tenantID, correlationID, causationID, requestID, traceID, idempotencyID string
	sourceService, sourceVersion, environment, networkOrigin, userAgent     string
}

// TenantID returns the optional caller-supplied stable tenant ID.
func (value RecordContext) TenantID() string { return value.tenantID }

// CorrelationID returns the optional correlation identifier.
func (value RecordContext) CorrelationID() string { return value.correlationID }

// CausationID returns the optional causal predecessor identifier.
func (value RecordContext) CausationID() string { return value.causationID }

// RequestID returns the optional request identifier.
func (value RecordContext) RequestID() string { return value.requestID }

// TraceID returns the optional trace identifier retained by caller policy.
func (value RecordContext) TraceID() string { return value.traceID }

// IdempotencyID returns the optional business-operation idempotency identifier.
func (value RecordContext) IdempotencyID() string { return value.idempotencyID }

// SourceService returns the optional producing service name.
func (value RecordContext) SourceService() string { return value.sourceService }

// SourceVersion returns the optional producing service version.
func (value RecordContext) SourceVersion() string { return value.sourceVersion }

// Environment returns the optional deployment environment.
func (value RecordContext) Environment() string { return value.environment }

// NetworkOrigin returns the optional policy-permitted network origin.
func (value RecordContext) NetworkOrigin() string { return value.networkOrigin }

// UserAgent returns the optional policy-permitted user agent.
func (value RecordContext) UserAgent() string { return value.userAgent }

// ChangeSetInput supplies either structured before/after values or an explicit
// NoChange state. Values are safe bounded strings, not arbitrary request or
// response bodies.
type ChangeSetInput struct {
	NoChange bool
	Before   map[string]string
	After    map[string]string
}

// ChangeSet is an immutable structured change description.
type ChangeSet struct {
	noChange      bool
	before, after map[string]string
}

// NoChange reports an explicit assertion that no structured state changed.
func (changes ChangeSet) NoChange() bool { return changes.noChange }

// Before returns a defensive copy of the safe pre-action fields.
func (changes ChangeSet) Before() map[string]string { return cloneMap(changes.before) }

// After returns a defensive copy of the safe post-action fields.
func (changes ChangeSet) After() map[string]string { return cloneMap(changes.after) }

// PolicyMetadata identifies the caller-owned audit and redaction policy used
// to construct a record.
type PolicyMetadata struct{ PolicyID, Version string }

// IntegrityInput carries optional chain metadata. Digest bytes are defensively
// copied; a zero Sequence with empty digests means integrity is disabled.
type IntegrityInput struct {
	Algorithm              IntegrityAlgorithm
	Partition, KeyID       string
	Sequence               uint64
	PreviousDigest, Digest []byte
}

// Integrity is immutable optional chain metadata.
type Integrity struct {
	algorithm              IntegrityAlgorithm
	partition, keyID       string
	sequence               uint64
	previousDigest, digest []byte
}

// Algorithm returns the selected integrity algorithm, or zero when disabled.
func (value Integrity) Algorithm() IntegrityAlgorithm { return value.algorithm }

// Partition returns the stable chain partition.
func (value Integrity) Partition() string { return value.partition }

// KeyID returns external key-rotation metadata for HMAC records.
func (value Integrity) KeyID() string { return value.keyID }

// Sequence returns the caller-allocated partition sequence.
func (value Integrity) Sequence() uint64 { return value.sequence }

// PreviousDigest returns a defensive copy of the prior link digest.
func (value Integrity) PreviousDigest() []byte { return append([]byte(nil), value.previousDigest...) }

// Digest returns a defensive copy of this link's digest.
func (value Integrity) Digest() []byte { return append([]byte(nil), value.digest...) }

// Enabled reports whether any integrity metadata is present.
func (value Integrity) Enabled() bool {
	return value.sequence != 0 || len(value.previousDigest) != 0 || len(value.digest) != 0
}

// RecordInput contains the caller-owned values used to construct one record.
type RecordInput struct {
	OccurredAt                      time.Time
	Action, ReasonCode, Description string
	Outcome                         Outcome
	Actor                           ActorInput
	Subject                         SubjectInput
	Context                         ContextInput
	Changes                         ChangeSetInput
	Policy                          PolicyMetadata
	Attributes                      map[string]string
	Integrity                       IntegrityInput
}

// Record is immutable by contract. All mutable inputs and returned values are
// copied, so callers cannot mutate a constructed record through aliases.
type Record struct {
	id                              string
	occurredAt, recordedAt          time.Time
	action, reasonCode, description string
	outcome                         Outcome
	actor                           Actor
	subject                         Subject
	context                         RecordContext
	changes                         ChangeSet
	policy                          PolicyMetadata
	attributes                      map[string]string
	integrity                       Integrity
}

// ID returns the globally unique stable record identifier.
func (record Record) ID() string { return record.id }

// OccurredAt returns when the audited action occurred in canonical UTC.
func (record Record) OccurredAt() time.Time { return record.occurredAt }

// RecordedAt returns when the record was constructed in canonical UTC.
func (record Record) RecordedAt() time.Time { return record.recordedAt }

// Action returns the application-defined action name.
func (record Record) Action() string { return record.action }

// Outcome returns the explicit action outcome.
func (record Record) Outcome() Outcome { return record.outcome }

// ReasonCode returns the optional stable machine-readable reason.
func (record Record) ReasonCode() string { return record.reasonCode }

// Description returns the optional policy-safe human-readable summary.
func (record Record) Description() string { return record.description }

// Actor returns the immutable actor context.
func (record Record) Actor() Actor { return record.actor }

// Subject returns the immutable resource identity.
func (record Record) Subject() Subject { return record.subject }

// Context returns the immutable request and source context.
func (record Record) Context() RecordContext { return record.context }

// Changes returns a defensive copy of structured before/after state.
func (record Record) Changes() ChangeSet {
	return ChangeSet{record.changes.noChange, cloneMap(record.changes.before), cloneMap(record.changes.after)}
}

// Policy returns the policy identifier and version applied by the caller.
func (record Record) Policy() PolicyMetadata { return record.policy }

// Attributes returns a defensive copy of namespaced extensible attributes.
func (record Record) Attributes() map[string]string { return cloneMap(record.attributes) }

// Integrity returns defensive copies of the optional chain metadata.
func (record Record) Integrity() Integrity {
	return Integrity{record.integrity.algorithm, record.integrity.partition, record.integrity.keyID, record.integrity.sequence, record.integrity.PreviousDigest(), record.integrity.Digest()}
}

// BuilderConfig supplies bounded construction dependencies.
type BuilderConfig struct {
	Clock       func() time.Time
	IDGenerator func() (string, error)
	Limits      Limits
}

// Builder constructs immutable validated records.
type Builder struct {
	clock       func() time.Time
	idGenerator func() (string, error)
	limits      Limits
}

// NewBuilder constructs a record builder. Nil functions use secure defaults.
func NewBuilder(config BuilderConfig) (*Builder, error) {
	limits := config.Limits
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	idGenerator := config.IDGenerator
	if idGenerator == nil {
		idGenerator = func() (string, error) { return randomID(rand.Reader) }
	}
	return &Builder{clock: clock, idGenerator: idGenerator, limits: limits}, nil
}

// Build validates input, generates a globally random record identity, and
// defensively owns every mutable value.
func (builder *Builder) Build(input RecordInput) (Record, error) {
	if builder == nil {
		return Record{}, invalid("builder", "must be assigned")
	}
	id, err := builder.idGenerator()
	if err != nil {
		return Record{}, fmt.Errorf("audit: generate record ID: %w", err)
	}
	recordedAt := canonicalTime(builder.clock())
	if err := validateInput(id, recordedAt, input, builder.limits); err != nil {
		return Record{}, err
	}
	record := recordFromInput(id, recordedAt, input)
	encoded, _ := CanonicalJSON(record)
	if len(encoded) > builder.limits.MaxRecordBytes {
		return Record{}, invalid("record", "exceeds total byte limit")
	}
	return record, nil
}

func recordFromInput(id string, recordedAt time.Time, input RecordInput) Record {
	actor := copyActor(input.Actor)
	return Record{
		id: id, occurredAt: canonicalTime(input.OccurredAt), recordedAt: recordedAt,
		action: input.Action, outcome: input.Outcome, reasonCode: input.ReasonCode,
		description: input.Description, actor: actor,
		subject: Subject{input.Subject.Type, input.Subject.ID, input.Subject.Deleted},
		context: RecordContext{
			input.Context.TenantID, input.Context.CorrelationID, input.Context.CausationID,
			input.Context.RequestID, input.Context.TraceID, input.Context.IdempotencyID,
			input.Context.SourceService, input.Context.SourceVersion, input.Context.Environment,
			input.Context.NetworkOrigin, input.Context.UserAgent,
		},
		changes: ChangeSet{input.Changes.NoChange, cloneMap(input.Changes.Before), cloneMap(input.Changes.After)},
		policy:  input.Policy, attributes: cloneMap(input.Attributes),
		integrity: Integrity{input.Integrity.Algorithm, input.Integrity.Partition, input.Integrity.KeyID, input.Integrity.Sequence, append([]byte(nil), input.Integrity.PreviousDigest...), append([]byte(nil), input.Integrity.Digest...)},
	}
}

func validateInput(id string, recordedAt time.Time, input RecordInput, limits Limits) error {
	if err := boundedRequired("record_id", id, limits.MaxFieldBytes); err != nil {
		return err
	}
	if !validCanonicalTime(input.OccurredAt) || !validCanonicalTime(recordedAt) {
		return invalid("time", "occurrence and recording times are required")
	}
	if err := boundedRequired("action", input.Action, limits.MaxFieldBytes); err != nil {
		return err
	}
	if input.Outcome < OutcomeSucceeded || input.Outcome > OutcomeUnknown {
		return invalid("outcome", "must be explicit")
	}
	if err := boundedOptional("reason_code", input.ReasonCode, limits.MaxFieldBytes); err != nil {
		return err
	}
	if err := boundedOptional("description", input.Description, limits.MaxDescriptionBytes); err != nil {
		return err
	}
	if err := validateActor(input.Actor, limits, false); err != nil {
		return err
	}
	if err := boundedRequired("subject_type", input.Subject.Type, limits.MaxFieldBytes); err != nil {
		return err
	}
	if err := boundedRequired("subject_id", input.Subject.ID, limits.MaxFieldBytes); err != nil {
		return err
	}
	if input.Changes.NoChange == (len(input.Changes.Before) != 0 || len(input.Changes.After) != 0) {
		return invalid("changes", "must be explicit no-change or structured before/after values")
	}
	if err := validateMap("changes", input.Changes.Before, input.Changes.After, limits.MaxChangeEntries, limits.MaxChangeBytes, false); err != nil {
		return err
	}
	if err := validateMap("attributes", input.Attributes, nil, limits.MaxAttributeEntries, limits.MaxAttributeBytes, true); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"tenant_id": input.Context.TenantID, "correlation_id": input.Context.CorrelationID,
		"causation_id": input.Context.CausationID, "request_id": input.Context.RequestID,
		"trace_id": input.Context.TraceID, "idempotency_id": input.Context.IdempotencyID,
		"source_service": input.Context.SourceService, "source_version": input.Context.SourceVersion,
		"environment": input.Context.Environment, "network_origin": input.Context.NetworkOrigin,
		"user_agent": input.Context.UserAgent, "policy_id": input.Policy.PolicyID,
		"policy_version": input.Policy.Version,
	} {
		if err := boundedOptional(name, value, limits.MaxFieldBytes); err != nil {
			return err
		}
	}
	integrityEnabled := input.Integrity.Algorithm != 0 || input.Integrity.Partition != "" ||
		input.Integrity.KeyID != "" || input.Integrity.Sequence != 0 ||
		len(input.Integrity.PreviousDigest) != 0 || len(input.Integrity.Digest) != 0
	if integrityEnabled {
		if input.Integrity.Algorithm != IntegritySHA256 && input.Integrity.Algorithm != IntegrityHMACSHA256 {
			return invalid("integrity_algorithm", "must be supported")
		}
		if input.Integrity.Partition == "" || input.Integrity.Sequence == 0 || len(input.Integrity.Digest) != 32 {
			return invalid("integrity", "requires partition, sequence, and SHA-256 digest")
		}
		if input.Integrity.Sequence == 1 && len(input.Integrity.PreviousDigest) != 0 {
			return invalid("previous_digest", "must be empty for sequence one")
		}
		if input.Integrity.Sequence > 1 && len(input.Integrity.PreviousDigest) != 32 {
			return invalid("previous_digest", "must be a SHA-256 digest")
		}
		if input.Integrity.Algorithm == IntegrityHMACSHA256 && input.Integrity.KeyID == "" {
			return invalid("integrity_key_id", "is required for HMAC")
		}
		if input.Integrity.Algorithm == IntegritySHA256 && input.Integrity.KeyID != "" {
			return invalid("integrity_key_id", "must be empty for unkeyed SHA-256")
		}
	}
	for name, value := range map[string]string{"integrity_partition": input.Integrity.Partition, "integrity_key_id": input.Integrity.KeyID} {
		if err := boundedOptional(name, value, limits.MaxFieldBytes); err != nil {
			return err
		}
	}
	return nil
}

func validateActor(input ActorInput, limits Limits, delegated bool) error {
	if input.Kind < ActorHuman || input.Kind > ActorUnknown {
		return invalid("actor_kind", "must be explicit")
	}
	if (input.Kind == ActorHuman || input.Kind == ActorService || input.Kind == ActorSystem) && input.ID == "" {
		return invalid("actor_id", "identified actors require a stable ID")
	}
	if (input.Kind == ActorAnonymous || input.Kind == ActorUnknown) && input.ID != "" {
		return invalid("actor_id", "anonymous and unknown actors cannot have an ID")
	}
	if err := boundedOptional("actor_id", input.ID, limits.MaxFieldBytes); err != nil {
		return err
	}
	if err := boundedOptional("authentication_method", input.AuthenticationMethod, limits.MaxFieldBytes); err != nil {
		return err
	}
	if delegated && input.DelegatedBy != nil {
		return invalid("delegated_actor", "delegation cannot be nested")
	}
	if input.DelegatedBy != nil {
		return validateActor(*input.DelegatedBy, limits, true)
	}
	return nil
}

func copyActor(input ActorInput) Actor {
	actor := Actor{kind: input.Kind, id: input.ID, authenticationMethod: input.AuthenticationMethod}
	if input.DelegatedBy != nil {
		delegated := copyActor(*input.DelegatedBy)
		actor.delegatedBy = &delegated
	}
	return actor
}

func validateMap(name string, first, second map[string]string, maxEntries, maxBytes int, attributes bool) error {
	if len(first)+len(second) > maxEntries {
		return invalid(name, "has too many entries")
	}
	total := 0
	for _, values := range []map[string]string{first, second} {
		for key, value := range values {
			if key == "" {
				return invalid(name, "contains an empty key")
			}
			if !utf8.ValidString(key) || !utf8.ValidString(value) {
				return invalid(name, "contains invalid UTF-8")
			}
			lower := strings.ToLower(key)
			if sensitiveName(lower) {
				return fmt.Errorf("%w: %s contains prohibited key", ErrSensitiveData, name)
			}
			if attributes && (strings.HasPrefix(lower, "audit.") || strings.HasPrefix(lower, "integrity.")) {
				return invalid(name, "uses a reserved namespace")
			}
			total = total + len(key) + len(value)
		}
	}
	if total > maxBytes {
		return invalid(name, "exceeds byte limit")
	}
	return nil
}

func sensitiveName(value string) bool {
	for _, prohibited := range []string{"authorization", "cookie", "password", "secret", "token", "credential"} {
		if strings.Contains(value, prohibited) {
			return true
		}
	}
	normalized := strings.NewReplacer("_", "", "-", "", ".", "", "/", "", " ", "").Replace(value)
	for _, prohibited := range []string{"requestbody", "responsebody", "rawbody", "httprequestbody", "httpresponsebody"} {
		if strings.Contains(normalized, prohibited) {
			return true
		}
	}
	return false
}

func boundedRequired(name, value string, maximum int) error {
	if value == "" {
		return invalid(name, "must be assigned")
	}
	return boundedOptional(name, value, maximum)
}
func boundedOptional(name, value string, maximum int) error {
	if !utf8.ValidString(value) {
		return invalid(name, "must be valid UTF-8")
	}
	if len(value) > maximum {
		return invalid(name, "exceeds byte limit")
	}
	return nil
}
func invalid(field, reason string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidArgument, field, reason)
}
func cloneMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
func canonicalTime(value time.Time) time.Time { return value.Round(0).UTC().Truncate(time.Microsecond) }

func validCanonicalTime(value time.Time) bool {
	return !value.IsZero() && value.Year() >= 0 && value.Year() <= 9999
}

func randomID(reader io.Reader) (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(reader, value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded), nil
}
