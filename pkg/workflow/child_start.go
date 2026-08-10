package workflow

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrInvalidChildStart classifies malformed child-start requests or
	// outcomes. A starter must return an explicit outcome even when creation
	// may have succeeded.
	ErrInvalidChildStart = errors.New("invalid workflow child start")
)

// ChildStartOutcomeKind classifies the observable result of one idempotent
// child-creation attempt.
type ChildStartOutcomeKind uint8

const (
	// ChildStarted means the pinned child instance is known to exist.
	ChildStarted ChildStartOutcomeKind = 1
	// ChildStartFailed means creation is known not to have occurred.
	ChildStartFailed ChildStartOutcomeKind = 2
	// ChildStartUnknown means creation may have occurred and must not be
	// repeated without reconciliation.
	ChildStartUnknown ChildStartOutcomeKind = 3
)

// ChildStartOutcomeSpec supplies one explicit bounded creation result.
type ChildStartOutcomeSpec struct {
	Kind      ChildStartOutcomeKind
	Code      string
	Retryable bool
}

// ChildStartOutcome is an immutable known or uncertain creation result.
type ChildStartOutcome struct {
	kind      ChildStartOutcomeKind
	code      string
	retryable bool
}

// NewChildStartOutcome validates one explicit creation result.
func NewChildStartOutcome(spec ChildStartOutcomeSpec) (ChildStartOutcome, error) {
	outcome := ChildStartOutcome{kind: spec.Kind, code: spec.Code, retryable: spec.Retryable}
	if !outcome.valid() {
		return ChildStartOutcome{}, ErrInvalidChildStart
	}
	return outcome, nil
}

// Kind returns the explicit creation result.
func (outcome ChildStartOutcome) Kind() ChildStartOutcomeKind { return outcome.kind }

// Code returns a stable known-failure or uncertainty classification.
func (outcome ChildStartOutcome) Code() string { return outcome.code }

// Retryable reports whether a known absence permits policy retry.
func (outcome ChildStartOutcome) Retryable() bool { return outcome.retryable }

func (outcome ChildStartOutcome) valid() bool {
	switch outcome.kind {
	case ChildStarted:
		return outcome.code == "" && !outcome.retryable
	case ChildStartFailed:
		return stableName.MatchString(outcome.code)
	case ChildStartUnknown:
		return stableName.MatchString(outcome.code) && !outcome.retryable
	default:
		return false
	}
}

// ChildStartRequestSpec supplies one version-pinned bounded creation attempt.
type ChildStartRequestSpec struct {
	ParentInstanceID string
	ParentDefinition DefinitionReference
	StepName         string
	ChildID          string
	ChildDefinition  DefinitionReference
	Attempt          uint32
	MaxAttempts      uint32
	IdempotencyKey   string
	StartedAt        time.Time
	Deadline         time.Time
	Input            []byte
	InputLimit       uint32
	TenantID         string
	CorrelationID    string
}

// ChildStartRequest is immutable attempt metadata supplied to a caller-owned
// idempotent child creator.
type ChildStartRequest struct {
	parentInstanceID string
	parentDefinition DefinitionReference
	stepName         string
	childID          string
	childDefinition  DefinitionReference
	attempt          uint32
	maxAttempts      uint32
	idempotencyKey   string
	startedAt        time.Time
	deadline         time.Time
	input            []byte
	inputLimit       uint32
	tenantID         string
	correlationID    string
}

// NewChildStartRequest validates and owns one bounded start request.
func NewChildStartRequest(spec ChildStartRequestSpec) (ChildStartRequest, error) {
	request := ChildStartRequest{
		parentInstanceID: spec.ParentInstanceID, parentDefinition: spec.ParentDefinition,
		stepName: spec.StepName, childID: spec.ChildID, childDefinition: spec.ChildDefinition,
		attempt: spec.Attempt, maxAttempts: spec.MaxAttempts, idempotencyKey: spec.IdempotencyKey,
		startedAt: canonicalTime(spec.StartedAt), deadline: canonicalTime(spec.Deadline),
		input: cloneBytes(spec.Input), inputLimit: spec.InputLimit,
		tenantID: spec.TenantID, correlationID: spec.CorrelationID,
	}
	if !request.valid() {
		return ChildStartRequest{}, ErrInvalidChildStart
	}
	return request, nil
}

// ParentInstanceID returns the durable parent identity.
func (request ChildStartRequest) ParentInstanceID() string { return request.parentInstanceID }

// ParentDefinition returns the exact parent behavior identity.
func (request ChildStartRequest) ParentDefinition() DefinitionReference {
	return request.parentDefinition
}

// StepName returns the stable parent child-step name.
func (request ChildStartRequest) StepName() string { return request.stepName }

// ChildID returns the stable child identity and natural deduplication key.
func (request ChildStartRequest) ChildID() string { return request.childID }

// ChildDefinition returns the exact child behavior identity.
func (request ChildStartRequest) ChildDefinition() DefinitionReference {
	return request.childDefinition
}

// Attempt returns the one-based semantic creation attempt.
func (request ChildStartRequest) Attempt() uint32 { return request.attempt }

// MaxAttempts returns the immutable parent policy bound.
func (request ChildStartRequest) MaxAttempts() uint32 { return request.maxAttempts }

// IdempotencyKey returns the stable key for this semantic attempt.
func (request ChildStartRequest) IdempotencyKey() string { return request.idempotencyKey }

// StartedAt returns the persisted attempt start time.
func (request ChildStartRequest) StartedAt() time.Time { return request.startedAt }

// Deadline returns the persisted attempt deadline.
func (request ChildStartRequest) Deadline() time.Time { return request.deadline }

// Input returns an owned copy of the persisted child input.
func (request ChildStartRequest) Input() []byte { return cloneBytes(request.input) }

// TenantID returns caller-supplied routing metadata. It must not be used as an
// unbounded metric label.
func (request ChildStartRequest) TenantID() string { return request.tenantID }

// CorrelationID returns caller-supplied trace and message correlation metadata.
func (request ChildStartRequest) CorrelationID() string { return request.correlationID }

func (request ChildStartRequest) valid() bool {
	return instanceIDPattern.MatchString(request.parentInstanceID) && request.parentDefinition.valid() &&
		stableName.MatchString(request.stepName) && instanceIDPattern.MatchString(request.childID) &&
		request.childDefinition.valid() && request.attempt > 0 && request.attempt <= request.maxAttempts &&
		instanceIDPattern.MatchString(request.idempotencyKey) && !request.startedAt.IsZero() &&
		request.deadline.After(request.startedAt) && request.inputLimit > 0 &&
		len(request.input) <= int(request.inputLimit) &&
		optionalMetadataValid(request.tenantID) && optionalMetadataValid(request.correlationID)
}

// ChildStarter creates or observes one pinned child using the supplied stable
// identity. Implementations must be idempotent and explicitly report unknown
// outcomes.
type ChildStarter interface {
	Start(context.Context, ChildStartRequest) ChildStartOutcome
}

// ChildStartFunc adapts an explicit function without registration or
// reflection-driven discovery.
type ChildStartFunc func(context.Context, ChildStartRequest) ChildStartOutcome

// Start invokes the adapted child starter.
func (start ChildStartFunc) Start(ctx context.Context, request ChildStartRequest) ChildStartOutcome {
	return start(ctx, request)
}
