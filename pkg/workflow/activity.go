package workflow

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrInvalidActivityRequest classifies incomplete or unbounded attempt input.
	ErrInvalidActivityRequest = errors.New("invalid workflow activity request")
	// ErrInvalidActivityOutcome classifies ambiguous or oversized activity
	// results. Unknown external outcomes must use ActivityUnknown explicitly.
	ErrInvalidActivityOutcome = errors.New("invalid workflow activity outcome")
	// ErrInvalidActivity classifies malformed explicit activity registrations.
	ErrInvalidActivity = errors.New("invalid workflow activity")
	// ErrDuplicateActivity classifies duplicate explicit activity names.
	ErrDuplicateActivity = errors.New("duplicate workflow activity")
	// ErrActivityNotFound classifies an unavailable explicitly named activity.
	ErrActivityNotFound = errors.New("workflow activity not found")
)

// ActivityRequestSpec supplies bounded persisted activity-attempt metadata.
type ActivityRequestSpec struct {
	InstanceID     string
	Definition     DefinitionReference
	StepName       string
	Attempt        uint32
	MaxAttempts    uint32
	IdempotencyKey string
	StartedAt      time.Time
	Deadline       time.Time
	Input          []byte
	InputLimit     uint32
	ResultLimit    uint32
	TenantID       string
	CorrelationID  string
}

// ActivityRequest is immutable attempt input. Its exact idempotency key is
// stable for one attempt; an unknown outcome must be reconciled before a caller
// starts another key or attempt.
type ActivityRequest struct {
	instanceID     string
	definition     DefinitionReference
	stepName       string
	attempt        uint32
	maxAttempts    uint32
	idempotencyKey string
	startedAt      time.Time
	deadline       time.Time
	input          []byte
	inputLimit     uint32
	resultLimit    uint32
	tenantID       string
	correlationID  string
}

// NewActivityRequest validates and owns one bounded activity attempt.
func NewActivityRequest(spec ActivityRequestSpec) (ActivityRequest, error) {
	request := ActivityRequest{
		instanceID: spec.InstanceID, definition: spec.Definition,
		stepName: spec.StepName, attempt: spec.Attempt, maxAttempts: spec.MaxAttempts,
		idempotencyKey: spec.IdempotencyKey,
		startedAt:      canonicalTime(spec.StartedAt), deadline: canonicalTime(spec.Deadline),
		input: cloneBytes(spec.Input), inputLimit: spec.InputLimit, resultLimit: spec.ResultLimit,
		tenantID: spec.TenantID, correlationID: spec.CorrelationID,
	}
	if !request.valid() {
		return ActivityRequest{}, ErrInvalidActivityRequest
	}
	return request, nil
}

// InstanceID returns the durable workflow instance identity.
func (request ActivityRequest) InstanceID() string { return request.instanceID }

// Definition returns the exact behavior identity that scheduled the attempt.
func (request ActivityRequest) Definition() DefinitionReference { return request.definition }

// StepName returns the stable definition step name.
func (request ActivityRequest) StepName() string { return request.stepName }

// Attempt returns the one-based attempt number.
func (request ActivityRequest) Attempt() uint32 { return request.attempt }

// MaxAttempts returns the immutable definition retry bound.
func (request ActivityRequest) MaxAttempts() uint32 { return request.maxAttempts }

// IdempotencyKey returns the stable application-visible attempt key.
func (request ActivityRequest) IdempotencyKey() string { return request.idempotencyKey }

// StartedAt returns canonical persisted attempt-start time.
func (request ActivityRequest) StartedAt() time.Time { return request.startedAt }

// Deadline returns the persisted attempt deadline.
func (request ActivityRequest) Deadline() time.Time { return request.deadline }

// Input returns an owned copy of bounded activity input.
func (request ActivityRequest) Input() []byte { return cloneBytes(request.input) }

// InputLimit returns the immutable maximum input size.
func (request ActivityRequest) InputLimit() uint32 { return request.inputLimit }

// ResultLimit returns the immutable maximum result or failure-detail size.
func (request ActivityRequest) ResultLimit() uint32 { return request.resultLimit }

// TenantID returns optional propagated tenant identity. It is data, not a
// metric-label recommendation.
func (request ActivityRequest) TenantID() string { return request.tenantID }

// CorrelationID returns optional propagated correlation identity.
func (request ActivityRequest) CorrelationID() string { return request.correlationID }

func (request ActivityRequest) valid() bool {
	return instanceIDPattern.MatchString(request.instanceID) && request.definition.valid() &&
		stableName.MatchString(request.stepName) && request.attempt > 0 &&
		request.attempt <= request.maxAttempts &&
		instanceIDPattern.MatchString(request.idempotencyKey) &&
		!request.startedAt.IsZero() && request.deadline.After(request.startedAt) &&
		validPayloadLimit(request.inputLimit) && validPayloadLimit(request.resultLimit) &&
		len(request.input) <= int(request.inputLimit) && optionalMetadataValid(request.tenantID) &&
		optionalMetadataValid(request.correlationID)
}

func optionalMetadataValid(value string) bool {
	return value == "" || instanceIDPattern.MatchString(value)
}

// ActivityOutcomeKind distinguishes success, known failure, and an outcome
// that may have committed externally and therefore cannot be blindly retried.
type ActivityOutcomeKind uint8

const (
	// ActivitySucceeded records a known successful external outcome.
	ActivitySucceeded ActivityOutcomeKind = 1
	// ActivityFailed records a known failed external outcome.
	ActivityFailed ActivityOutcomeKind = 2
	// ActivityUnknown records that an external side effect may have committed.
	ActivityUnknown ActivityOutcomeKind = 3
)

// ActivityOutcomeSpec supplies one explicit bounded activity result.
type ActivityOutcomeSpec struct {
	Kind      ActivityOutcomeKind
	Code      string
	Retryable bool
	Data      []byte
}

// ActivityOutcome is one immutable explicit external-operation classification.
type ActivityOutcome struct {
	kind      ActivityOutcomeKind
	code      string
	retryable bool
	data      []byte
}

// NewActivityOutcome validates and owns one explicit activity result.
func NewActivityOutcome(spec ActivityOutcomeSpec) (ActivityOutcome, error) {
	outcome := ActivityOutcome{
		kind: spec.Kind, code: spec.Code, retryable: spec.Retryable,
		data: cloneBytes(spec.Data),
	}
	if !outcome.valid() {
		return ActivityOutcome{}, ErrInvalidActivityOutcome
	}
	return outcome, nil
}

// Kind returns the explicit external-operation classification.
func (outcome ActivityOutcome) Kind() ActivityOutcomeKind { return outcome.kind }

// Code returns the stable safe application failure or reconciliation code.
func (outcome ActivityOutcome) Code() string { return outcome.code }

// Retryable reports whether a known failure permits definition-policy retry.
// It is always false for unknown outcomes.
func (outcome ActivityOutcome) Retryable() bool { return outcome.retryable }

// Data returns an owned copy of result or safe persisted failure details.
func (outcome ActivityOutcome) Data() []byte { return cloneBytes(outcome.data) }

func (outcome ActivityOutcome) valid() bool {
	if len(outcome.data) > MaxPayloadBytes {
		return false
	}
	switch outcome.kind {
	case ActivitySucceeded:
		return outcome.code == "" && !outcome.retryable
	case ActivityFailed:
		return stableName.MatchString(outcome.code)
	case ActivityUnknown:
		return stableName.MatchString(outcome.code) && !outcome.retryable
	default:
		return false
	}
}

// ActivityHandler performs one explicitly bounded external activity attempt.
// It must return ActivityUnknown when cancellation, timeout, or transport loss
// leaves external commitment uncertain.
type ActivityHandler func(context.Context, ActivityRequest) ActivityOutcome

// Activity is one explicit named handler without reflection or global
// registration.
type Activity struct {
	name    string
	handler ActivityHandler
}

// NewActivity validates one explicit stable activity registration.
func NewActivity(name string, handler ActivityHandler) (Activity, error) {
	if !stableName.MatchString(name) || handler == nil {
		return Activity{}, ErrInvalidActivity
	}
	return Activity{name: name, handler: handler}, nil
}

// Name returns the stable explicit activity name.
func (activity Activity) Name() string { return activity.name }

// Execute derives the persisted deadline, checks cancellation before external
// work, invokes the handler synchronously, and validates its bounded outcome.
// It does not recover panics or claim that context cancellation stopped an
// arbitrary external operation.
func (activity Activity) Execute(ctx context.Context, request ActivityRequest) (ActivityOutcome, error) {
	if activity.handler == nil {
		return ActivityOutcome{}, ErrInvalidActivity
	}
	if ctx == nil || !request.valid() {
		return ActivityOutcome{}, ErrInvalidActivityRequest
	}
	if err := ctx.Err(); err != nil {
		return ActivityOutcome{}, err
	}
	attemptContext, cancel := context.WithDeadline(ctx, request.deadline)
	defer cancel()
	if err := attemptContext.Err(); err != nil {
		return ActivityOutcome{}, err
	}

	outcome := activity.handler(attemptContext, request)
	if !outcome.valid() || len(outcome.data) > int(request.resultLimit) {
		return ActivityOutcome{}, ErrInvalidActivityOutcome
	}
	return outcome, nil
}

// ActivityRegistry is an immutable explicit activity registry.
type ActivityRegistry struct {
	activities map[string]Activity
}

// CompileActivities validates activities and rejects duplicate stable names.
func CompileActivities(activities ...Activity) (*ActivityRegistry, error) {
	registry := &ActivityRegistry{activities: make(map[string]Activity, len(activities))}
	for _, activity := range activities {
		if activity.handler == nil {
			return nil, ErrInvalidActivity
		}
		if _, exists := registry.activities[activity.name]; exists {
			return nil, ErrDuplicateActivity
		}
		registry.activities[activity.name] = activity
	}
	return registry, nil
}

// Resolve returns one explicitly registered activity.
func (registry *ActivityRegistry) Resolve(name string) (Activity, error) {
	if registry == nil {
		return Activity{}, ErrActivityNotFound
	}
	activity, exists := registry.activities[name]
	if !exists {
		return Activity{}, ErrActivityNotFound
	}
	return activity, nil
}
