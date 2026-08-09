package workflow_test

import (
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestWorkClaimRequestIsBoundedAndImmutable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)
	request, err := workflow.NewWorkClaimRequest(workflow.WorkClaimRequestSpec{
		Owner: "worker-1", Now: now, LeaseDuration: time.Minute, Limit: 10,
	})
	if err != nil {
		t.Fatalf("construct claim: %v", err)
	}
	if request.Owner() != "worker-1" || request.Now() != now ||
		request.LeaseDuration() != time.Minute || request.Limit() != 10 || !request.Valid() {
		t.Fatalf("claim request = %#v", request)
	}

	invalid := []workflow.WorkClaimRequestSpec{
		{},
		{Owner: "unsafe owner", Now: now, LeaseDuration: time.Minute, Limit: 1},
		{Owner: "worker-1", LeaseDuration: time.Minute, Limit: 1},
		{Owner: "worker-1", Now: now, LeaseDuration: time.Minute},
		{Owner: "worker-1", Now: now, LeaseDuration: 0, Limit: 1},
		{Owner: "worker-1", Now: now, LeaseDuration: workflow.MaxWorkLeaseDuration + time.Nanosecond, Limit: 1},
		{Owner: "worker-1", Now: now, LeaseDuration: time.Minute, Limit: workflow.MaxWorkClaimItems + 1},
	}
	for _, spec := range invalid {
		if _, err := workflow.NewWorkClaimRequest(spec); !errors.Is(err, workflow.ErrInvalidWorkLease) {
			t.Fatalf("invalid claim error = %v for %#v", err, spec)
		}
	}
	maximum, err := workflow.NewWorkClaimRequest(workflow.WorkClaimRequestSpec{
		Owner: "worker-1", Now: now,
		LeaseDuration: workflow.MaxWorkLeaseDuration, Limit: workflow.MaxWorkClaimItems,
	})
	if err != nil || maximum.LeaseDuration() != workflow.MaxWorkLeaseDuration || maximum.Limit() != workflow.MaxWorkClaimItems {
		t.Fatalf("maximum bounded claim = %#v, %v", maximum, err)
	}
}

func TestWorkLeaseCarriesAnImmutableFencingToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)
	payload := []byte("dispatch")
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "work-1", Kind: workflow.WorkActivity, InstanceID: "instance-1", Sequence: 2,
		AvailableAt: now, Deadline: now.Add(time.Hour), Payload: payload,
	})
	if err != nil {
		t.Fatalf("construct work: %v", err)
	}
	lease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 7, Attempt: 3,
		ClaimedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct lease: %v", err)
	}
	payload[0] = 'X'
	if lease.Work().ID() != "work-1" || string(lease.Work().Payload()) != "dispatch" ||
		lease.Owner() != "worker-1" || lease.Token() != 7 || lease.Attempt() != 3 ||
		lease.ClaimedAt() != now || lease.ExpiresAt() != now.Add(time.Minute) || !lease.Valid() {
		t.Fatalf("lease = %#v", lease)
	}
	if _, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{}); !errors.Is(err, workflow.ErrInvalidWorkLease) {
		t.Fatalf("zero lease error = %v", err)
	}
	base := workflow.WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	invalid := []workflow.WorkLeaseSpec{
		{Owner: base.Owner, Token: base.Token, Attempt: base.Attempt, ClaimedAt: base.ClaimedAt, ExpiresAt: base.ExpiresAt},
		{Work: base.Work, Owner: "unsafe owner", Token: base.Token, Attempt: base.Attempt, ClaimedAt: base.ClaimedAt, ExpiresAt: base.ExpiresAt},
		{Work: base.Work, Owner: base.Owner, Attempt: base.Attempt, ClaimedAt: base.ClaimedAt, ExpiresAt: base.ExpiresAt},
		{Work: base.Work, Owner: base.Owner, Token: base.Token, ClaimedAt: base.ClaimedAt, ExpiresAt: base.ExpiresAt},
		{Work: base.Work, Owner: base.Owner, Token: base.Token, Attempt: base.Attempt, ExpiresAt: base.ExpiresAt},
		{Work: base.Work, Owner: base.Owner, Token: base.Token, Attempt: base.Attempt, ClaimedAt: now, ExpiresAt: now},
		{Work: base.Work, Owner: base.Owner, Token: base.Token, Attempt: base.Attempt, ClaimedAt: now, ExpiresAt: now.Add(workflow.MaxWorkLeaseDuration + time.Nanosecond)},
	}
	for _, spec := range invalid {
		if _, err := workflow.NewWorkLease(spec); !errors.Is(err, workflow.ErrInvalidWorkLease) {
			t.Fatalf("invalid lease error = %v for %#v", err, spec)
		}
	}
	if _, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now, ExpiresAt: now.Add(workflow.MaxWorkLeaseDuration),
	}); err != nil {
		t.Fatalf("maximum lease duration: %v", err)
	}
}

func TestWorkLeaseCommandsRequireTheCurrentFence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)
	renewal, err := workflow.NewWorkLeaseRenewal(workflow.WorkLeaseRenewalSpec{
		WorkID: "work-1", Owner: "worker-1", Token: 4, Now: now, ExtendBy: time.Minute,
	})
	if err != nil || renewal.WorkID() != "work-1" || renewal.Owner() != "worker-1" ||
		renewal.Token() != 4 || renewal.Now() != now || renewal.ExtendBy() != time.Minute || !renewal.Valid() {
		t.Fatalf("renewal = %#v, %v", renewal, err)
	}
	completion, err := workflow.NewWorkCompletion(workflow.WorkCompletionSpec{
		WorkID: "work-1", Owner: "worker-1", Token: 4, CompletedAt: now,
	})
	if err != nil || completion.WorkID() != "work-1" || completion.Owner() != "worker-1" ||
		completion.Token() != 4 || completion.CompletedAt() != now || !completion.Valid() {
		t.Fatalf("completion = %#v, %v", completion, err)
	}
	failure, err := workflow.NewWorkFailure(workflow.WorkFailureSpec{
		WorkID: "work-1", Owner: "worker-1", Token: 4, FailedAt: now,
		Code: "poison-payload", Disposition: workflow.WorkDeadLetter,
	})
	if err != nil || failure.WorkID() != "work-1" || failure.Owner() != "worker-1" ||
		failure.Token() != 4 || failure.FailedAt() != now || failure.Code() != "poison-payload" ||
		failure.Disposition() != workflow.WorkDeadLetter || !failure.RetryAt().IsZero() || !failure.Valid() {
		t.Fatalf("failure = %#v, %v", failure, err)
	}

	if _, err := workflow.NewWorkLeaseRenewal(workflow.WorkLeaseRenewalSpec{}); !errors.Is(err, workflow.ErrInvalidWorkLease) {
		t.Fatalf("zero renewal error = %v", err)
	}
	invalidRenewals := []workflow.WorkLeaseRenewalSpec{
		{WorkID: "unsafe work", Owner: "worker-1", Token: 1, Now: now, ExtendBy: time.Minute},
		{WorkID: "work-1", Owner: "unsafe owner", Token: 1, Now: now, ExtendBy: time.Minute},
		{WorkID: "work-1", Owner: "worker-1", Now: now, ExtendBy: time.Minute},
		{WorkID: "work-1", Owner: "worker-1", Token: 1, ExtendBy: time.Minute},
		{WorkID: "work-1", Owner: "worker-1", Token: 1, Now: now},
		{WorkID: "work-1", Owner: "worker-1", Token: 1, Now: now, ExtendBy: workflow.MaxWorkLeaseDuration + time.Nanosecond},
	}
	for _, spec := range invalidRenewals {
		if _, err := workflow.NewWorkLeaseRenewal(spec); !errors.Is(err, workflow.ErrInvalidWorkLease) {
			t.Fatalf("invalid renewal error = %v for %#v", err, spec)
		}
	}
	if _, err := workflow.NewWorkLeaseRenewal(workflow.WorkLeaseRenewalSpec{
		WorkID: "work-1", Owner: "worker-1", Token: 1, Now: now,
		ExtendBy: workflow.MaxWorkLeaseDuration,
	}); err != nil {
		t.Fatalf("maximum renewal: %v", err)
	}
	if _, err := workflow.NewWorkCompletion(workflow.WorkCompletionSpec{}); !errors.Is(err, workflow.ErrInvalidWorkLease) {
		t.Fatalf("zero completion error = %v", err)
	}
	for _, spec := range []workflow.WorkCompletionSpec{
		{WorkID: "unsafe work", Owner: "worker-1", Token: 1, CompletedAt: now},
		{WorkID: "work-1", Owner: "unsafe owner", Token: 1, CompletedAt: now},
		{WorkID: "work-1", Owner: "worker-1", CompletedAt: now},
		{WorkID: "work-1", Owner: "worker-1", Token: 1},
	} {
		if _, err := workflow.NewWorkCompletion(spec); !errors.Is(err, workflow.ErrInvalidWorkLease) {
			t.Fatalf("invalid completion error = %v for %#v", err, spec)
		}
	}
	if _, err := workflow.NewWorkFailure(workflow.WorkFailureSpec{
		WorkID: "work-1", Owner: "worker-1", Token: 4, FailedAt: now,
		Code: "retry", Disposition: workflow.WorkRetry,
	}); !errors.Is(err, workflow.ErrInvalidWorkLease) {
		t.Fatalf("retry without admission error = %v", err)
	}
	if _, err := workflow.NewWorkFailure(workflow.WorkFailureSpec{
		WorkID: "work-1", Owner: "worker-1", Token: 4, FailedAt: now,
		Code: "failure", Disposition: workflow.WorkDisposition(99),
	}); !errors.Is(err, workflow.ErrInvalidWorkLease) {
		t.Fatalf("unknown disposition error = %v", err)
	}
	if _, err := workflow.NewWorkFailure(workflow.WorkFailureSpec{
		WorkID: "work-1", Owner: "worker-1", Token: 4,
		Code: "failure", Disposition: workflow.WorkDeadLetter,
	}); !errors.Is(err, workflow.ErrInvalidWorkLease) {
		t.Fatalf("missing failure time error = %v", err)
	}
}

func TestWorkLeaseCannotOutliveTheWorkDeadline(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "work-1", Kind: workflow.WorkActivity, InstanceID: "instance-1", Sequence: 1,
		AvailableAt: now, Deadline: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct work: %v", err)
	}
	if _, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now, ExpiresAt: now.Add(2 * time.Minute),
	}); !errors.Is(err, workflow.ErrInvalidWorkLease) {
		t.Fatalf("lease beyond work deadline error = %v", err)
	}
}
