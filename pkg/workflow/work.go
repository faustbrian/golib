package workflow

import (
	"context"
	"errors"
	"time"
)

const (
	// MaxWorkClaimItems bounds one atomic durable-work admission batch.
	MaxWorkClaimItems uint32 = 100
	// MaxWorkLeaseDuration bounds one ownership interval. Owners must renew
	// before expiry; process lifetime never implies durable ownership.
	MaxWorkLeaseDuration = 15 * time.Minute
)

var (
	// ErrInvalidWorkLease classifies malformed or unbounded claim and fencing input.
	ErrInvalidWorkLease = errors.New("invalid workflow work lease")
)

// WorkClaimRequestSpec supplies one bounded due-work claim operation.
type WorkClaimRequestSpec struct {
	Owner         string
	Now           time.Time
	LeaseDuration time.Duration
	Limit         uint32
}

// WorkClaimRequest is an immutable atomic due-work admission request.
type WorkClaimRequest struct {
	owner         string
	now           time.Time
	leaseDuration time.Duration
	limit         uint32
}

// NewWorkClaimRequest validates one bounded claim request.
func NewWorkClaimRequest(spec WorkClaimRequestSpec) (WorkClaimRequest, error) {
	request := WorkClaimRequest{
		owner: spec.Owner, now: canonicalTime(spec.Now),
		leaseDuration: spec.LeaseDuration, limit: spec.Limit,
	}
	if !request.Valid() {
		return WorkClaimRequest{}, ErrInvalidWorkLease
	}
	return request, nil
}

// Owner returns the stable process-specific lease owner identity.
func (request WorkClaimRequest) Owner() string { return request.owner }

// Now returns the caller-supplied deterministic admission time.
func (request WorkClaimRequest) Now() time.Time { return request.now }

// LeaseDuration returns the bounded initial ownership interval.
func (request WorkClaimRequest) LeaseDuration() time.Duration { return request.leaseDuration }

// Limit returns the maximum number of records claimed atomically.
func (request WorkClaimRequest) Limit() uint32 { return request.limit }

// Valid reports whether the request is bounded and internally coherent.
func (request WorkClaimRequest) Valid() bool {
	return instanceIDPattern.MatchString(request.owner) && !request.now.IsZero() &&
		request.leaseDuration > 0 && request.leaseDuration <= MaxWorkLeaseDuration &&
		request.limit > 0 && request.limit <= MaxWorkClaimItems
}

// WorkLeaseSpec supplies one durable claimed-work record.
type WorkLeaseSpec struct {
	Work      PendingWork
	Owner     string
	Token     uint64
	Attempt   uint32
	ClaimedAt time.Time
	ExpiresAt time.Time
}

// WorkLease is immutable claimed work. Token is a monotonically increasing
// fencing value; every renewal or terminal mutation must match it.
type WorkLease struct {
	work      PendingWork
	owner     string
	token     uint64
	attempt   uint32
	claimedAt time.Time
	expiresAt time.Time
}

// NewWorkLease validates and owns one claimed-work record.
func NewWorkLease(spec WorkLeaseSpec) (WorkLease, error) {
	lease := WorkLease{
		work: spec.Work, owner: spec.Owner, token: spec.Token, attempt: spec.Attempt,
		claimedAt: canonicalTime(spec.ClaimedAt), expiresAt: canonicalTime(spec.ExpiresAt),
	}
	lease.work.payload = cloneBytes(spec.Work.payload)
	if !lease.Valid() {
		return WorkLease{}, ErrInvalidWorkLease
	}
	return lease, nil
}

// Work returns an owned durable-work value.
func (lease WorkLease) Work() PendingWork {
	work := lease.work
	work.payload = cloneBytes(lease.work.payload)
	return work
}

// Owner returns the current lease owner.
func (lease WorkLease) Owner() string { return lease.owner }

// Token returns the current fencing token.
func (lease WorkLease) Token() uint64 { return lease.token }

// Attempt returns the one-based durable claim attempt.
func (lease WorkLease) Attempt() uint32 { return lease.attempt }

// ClaimedAt returns the persisted claim time.
func (lease WorkLease) ClaimedAt() time.Time { return lease.claimedAt }

// ExpiresAt returns the persisted ownership expiry.
func (lease WorkLease) ExpiresAt() time.Time { return lease.expiresAt }

// Valid reports whether the claimed work and fence are coherent.
func (lease WorkLease) Valid() bool {
	return lease.work.valid() && instanceIDPattern.MatchString(lease.owner) &&
		lease.token > 0 && lease.attempt > 0 && !lease.claimedAt.IsZero() &&
		lease.expiresAt.After(lease.claimedAt) &&
		lease.expiresAt.Sub(lease.claimedAt) <= MaxWorkLeaseDuration &&
		!lease.expiresAt.After(lease.work.deadline)
}

// WorkLeaseRenewalSpec supplies one fenced lease extension.
type WorkLeaseRenewalSpec struct {
	WorkID   string
	Owner    string
	Token    uint64
	Now      time.Time
	ExtendBy time.Duration
}

// WorkLeaseRenewal is an immutable fenced lease extension.
type WorkLeaseRenewal struct {
	workID   string
	owner    string
	token    uint64
	now      time.Time
	extendBy time.Duration
}

// NewWorkLeaseRenewal validates one bounded fenced extension.
func NewWorkLeaseRenewal(spec WorkLeaseRenewalSpec) (WorkLeaseRenewal, error) {
	renewal := WorkLeaseRenewal{
		workID: spec.WorkID, owner: spec.Owner, token: spec.Token,
		now: canonicalTime(spec.Now), extendBy: spec.ExtendBy,
	}
	if !renewal.Valid() {
		return WorkLeaseRenewal{}, ErrInvalidWorkLease
	}
	return renewal, nil
}

// WorkID returns the durable work identity.
func (renewal WorkLeaseRenewal) WorkID() string { return renewal.workID }

// Owner returns the expected current owner.
func (renewal WorkLeaseRenewal) Owner() string { return renewal.owner }

// Token returns the expected current fencing token.
func (renewal WorkLeaseRenewal) Token() uint64 { return renewal.token }

// Now returns the deterministic renewal time.
func (renewal WorkLeaseRenewal) Now() time.Time { return renewal.now }

// ExtendBy returns the bounded new lease interval.
func (renewal WorkLeaseRenewal) ExtendBy() time.Duration { return renewal.extendBy }

// Valid reports whether the renewal is bounded and internally coherent.
func (renewal WorkLeaseRenewal) Valid() bool {
	return leaseIdentityValid(renewal.workID, renewal.owner, renewal.token) &&
		!renewal.now.IsZero() && renewal.extendBy > 0 && renewal.extendBy <= MaxWorkLeaseDuration
}

// WorkCompletionSpec supplies one fenced successful terminal mutation.
type WorkCompletionSpec struct {
	WorkID      string
	Owner       string
	Token       uint64
	CompletedAt time.Time
}

// WorkCompletion is an immutable fenced successful terminal mutation.
type WorkCompletion struct {
	workID      string
	owner       string
	token       uint64
	completedAt time.Time
}

// NewWorkCompletion validates one fenced completion.
func NewWorkCompletion(spec WorkCompletionSpec) (WorkCompletion, error) {
	completion := WorkCompletion{
		workID: spec.WorkID, owner: spec.Owner, token: spec.Token,
		completedAt: canonicalTime(spec.CompletedAt),
	}
	if !completion.Valid() {
		return WorkCompletion{}, ErrInvalidWorkLease
	}
	return completion, nil
}

// WorkID returns the durable work identity.
func (completion WorkCompletion) WorkID() string { return completion.workID }

// Owner returns the expected current owner.
func (completion WorkCompletion) Owner() string { return completion.owner }

// Token returns the expected current fencing token.
func (completion WorkCompletion) Token() uint64 { return completion.token }

// CompletedAt returns the persisted completion time.
func (completion WorkCompletion) CompletedAt() time.Time { return completion.completedAt }

// Valid reports whether the completion fence is coherent.
func (completion WorkCompletion) Valid() bool {
	return leaseIdentityValid(completion.workID, completion.owner, completion.token) &&
		!completion.completedAt.IsZero()
}

// WorkDisposition selects durable handling for one known work failure.
type WorkDisposition uint8

const (
	// WorkRetry returns work to due admission at an explicit future time.
	WorkRetry WorkDisposition = 1
	// WorkDeadLetter makes poison work unavailable pending operator resolution.
	WorkDeadLetter WorkDisposition = 2
)

// WorkFailureSpec supplies one fenced known failure decision.
type WorkFailureSpec struct {
	WorkID      string
	Owner       string
	Token       uint64
	FailedAt    time.Time
	Code        string
	Disposition WorkDisposition
	RetryAt     time.Time
}

// WorkFailure is an immutable fenced retry or dead-letter decision.
type WorkFailure struct {
	workID      string
	owner       string
	token       uint64
	failedAt    time.Time
	code        string
	disposition WorkDisposition
	retryAt     time.Time
}

// NewWorkFailure validates one explicit known failure decision.
func NewWorkFailure(spec WorkFailureSpec) (WorkFailure, error) {
	failure := WorkFailure{
		workID: spec.WorkID, owner: spec.Owner, token: spec.Token,
		failedAt: canonicalTime(spec.FailedAt), code: spec.Code,
		disposition: spec.Disposition, retryAt: canonicalTime(spec.RetryAt),
	}
	if !failure.Valid() {
		return WorkFailure{}, ErrInvalidWorkLease
	}
	return failure, nil
}

// WorkID returns the durable work identity.
func (failure WorkFailure) WorkID() string { return failure.workID }

// Owner returns the expected current owner.
func (failure WorkFailure) Owner() string { return failure.owner }

// Token returns the expected current fencing token.
func (failure WorkFailure) Token() uint64 { return failure.token }

// FailedAt returns the persisted known-failure time.
func (failure WorkFailure) FailedAt() time.Time { return failure.failedAt }

// Code returns the stable safe failure classification.
func (failure WorkFailure) Code() string { return failure.code }

// Disposition returns retry or dead-letter handling.
func (failure WorkFailure) Disposition() WorkDisposition { return failure.disposition }

// RetryAt returns retry admission time, or zero for dead-letter handling.
func (failure WorkFailure) RetryAt() time.Time { return failure.retryAt }

// Valid reports whether the failure decision and fence are coherent.
func (failure WorkFailure) Valid() bool {
	if !leaseIdentityValid(failure.workID, failure.owner, failure.token) ||
		failure.failedAt.IsZero() || !stableName.MatchString(failure.code) {
		return false
	}
	switch failure.disposition {
	case WorkRetry:
		return failure.retryAt.After(failure.failedAt)
	case WorkDeadLetter:
		return failure.retryAt.IsZero()
	default:
		return false
	}
}

func leaseIdentityValid(workID, owner string, token uint64) bool {
	return instanceIDPattern.MatchString(workID) && instanceIDPattern.MatchString(owner) && token > 0
}

// WorkStore atomically claims due work and rejects every renewal or terminal
// mutation whose owner or fencing token is stale.
type WorkStore interface {
	Claim(context.Context, WorkClaimRequest) ([]WorkLease, error)
	Renew(context.Context, WorkLeaseRenewal) (WorkLease, error)
	Complete(context.Context, WorkCompletion) error
	Fail(context.Context, WorkFailure) error
}
