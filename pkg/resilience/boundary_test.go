package resilience_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/resilience"
)

type zeroClock struct{}

func (zeroClock) Now() time.Time { return time.Time{} }

type panickingWrapPolicy struct{}

func (panickingWrapPolicy) Descriptor() resilience.PolicyDescriptor {
	return resilience.PolicyDescriptor{ID: "panic-wrap", Scope: resilience.ScopeLogical}
}

func (panickingWrapPolicy) Wrap(resilience.Stage[int]) resilience.Stage[int] { panic("wrap") }

type invalidResultPolicy struct{}

func (invalidResultPolicy) Descriptor() resilience.PolicyDescriptor {
	return resilience.PolicyDescriptor{ID: "invalid-result", Scope: resilience.ScopeLogical}
}

func (invalidResultPolicy) Wrap(resilience.Stage[int]) resilience.Stage[int] {
	return func(context.Context, resilience.Execution, resilience.Operation[int]) resilience.Result[int] {
		return resilience.Result[int]{Value: 9}
	}
}

func TestBudgetConfigurationRejectsEveryUnboundedDimension(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(1, 0)}
	valid := validBudgetConfig(clock)
	tests := []struct {
		name   string
		mutate func(*resilience.BudgetConfig)
		field  string
	}{
		{name: "resources", mutate: func(config *resilience.BudgetConfig) { config.MaxResources = 0 }, field: "max_resources"},
		{name: "execution", mutate: func(config *resilience.BudgetConfig) { config.MaxAdditionalPerExecution = 0 }, field: "max_additional_per_execution"},
		{name: "concurrent", mutate: func(config *resilience.BudgetConfig) { config.MaxConcurrentAdditional = 0 }, field: "max_concurrent_additional"},
		{name: "window count", mutate: func(config *resilience.BudgetConfig) { config.MaxAdditionalPerWindow = 0 }, field: "max_additional_per_window"},
		{name: "window duration", mutate: func(config *resilience.BudgetConfig) { config.AdditionalWindow = 0 }, field: "additional_window"},
		{name: "permit ttl", mutate: func(config *resilience.BudgetConfig) { config.PermitTTL = 0 }, field: "permit_ttl"},
		{name: "clock", mutate: func(config *resilience.BudgetConfig) { config.Clock = nil }, field: "clock"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			test.mutate(&config)
			_, err := resilience.NewBudget(config)
			var configuration *resilience.ConfigurationError
			if !errors.As(err, &configuration) || configuration.Field != test.field || !errors.Is(err, resilience.ErrInvalidComposition) || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}

func TestBudgetStartAndAcquireValidateCallerBoundaries(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(2, 0)}
	budget, err := resilience.NewBudget(validBudgetConfig(clock))
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	metadata := metadataFor(t, "logical", "resource")
	//nolint:staticcheck // Verify defensive behavior for a nil caller context.
	//lint:ignore SA1012 Verify defensive behavior for a nil caller context.
	if _, _, err := budget.Start(nil, metadata); !errors.Is(err, resilience.ErrInvalidMetadata) {
		t.Fatalf("nil context error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := budget.Start(canceled, metadata); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled start error = %v", err)
	}
	if _, _, err := budget.Start(context.Background(), resilience.Metadata{}); !errors.Is(err, resilience.ErrInvalidMetadata) {
		t.Fatalf("metadata error = %v", err)
	}
	scope, ctx, err := budget.Start(context.Background(), metadata)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	//nolint:staticcheck // Verify defensive behavior for a nil caller context.
	//lint:ignore SA1012 Verify defensive behavior for a nil caller context.
	if attached, attachErr := resilience.WithBudgetScope(nil, scope); attached != nil || !errors.Is(attachErr, resilience.ErrInvalidMetadata) {
		t.Fatalf("nil attachment = (%v, %v)", attached, attachErr)
	}
	if attached, attachErr := resilience.WithBudgetScope(context.Background(), nil); attached != nil || !errors.Is(attachErr, resilience.ErrInvalidComposition) {
		t.Fatalf("nil scope attachment = (%v, %v)", attached, attachErr)
	}
	if attached, attachErr := resilience.WithBudgetScope(ctx, scope); attached != nil || !errors.Is(attachErr, resilience.ErrBudgetAlreadyAttached) {
		t.Fatalf("duplicate attachment = (%v, %v)", attached, attachErr)
	}
	//nolint:staticcheck // Verify defensive behavior for a nil caller context.
	//lint:ignore SA1012 Verify defensive behavior for a nil caller context.
	if _, err := scope.Acquire(nil, attemptFor(t, 1, resilience.OriginOriginal, 0, clock.Now())); !errors.Is(err, resilience.ErrInvalidMetadata) {
		t.Fatalf("nil acquire context error = %v", err)
	}
	if _, err := scope.Acquire(ctx, resilience.Attempt{}); !errors.Is(err, resilience.ErrInvalidAttempt) {
		t.Fatalf("invalid attempt error = %v", err)
	}
	admitOriginal(ctx, t, scope, clock.Now())
	if _, err := scope.Acquire(ctx, attemptFor(t, 1, resilience.OriginOriginal, 0, clock.Now())); resilience.RejectionReasonOf(err) != resilience.ReasonDuplicateWork || !strings.Contains(err.Error(), string(resilience.ReasonDuplicateWork)) {
		t.Fatalf("duplicate error = %v", err)
	}
	if reason := resilience.RejectionReasonOf(errors.New("other")); reason != "" {
		t.Fatalf("unrelated rejection reason = %q", reason)
	}
	//nolint:staticcheck // Verify defensive behavior for a nil caller context.
	//lint:ignore SA1012 Verify defensive behavior for a nil caller context.
	if _, ok := resilience.BudgetScopeFromContext(nil); ok {
		t.Fatal("nil context exposed a budget scope")
	}
	if _, ok := resilience.BudgetScopeFromContext(context.Background()); ok {
		t.Fatal("plain context exposed a budget scope")
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestAttemptValidationRejectsEveryInvalidLineage(t *testing.T) {
	t.Parallel()

	started := time.Unix(3, 0)
	tests := []struct {
		name    string
		ordinal uint64
		origin  resilience.AttemptOrigin
		parent  uint64
		started time.Time
	}{
		{name: "zero ordinal", origin: resilience.OriginOriginal, started: started},
		{name: "zero time", ordinal: 1, origin: resilience.OriginOriginal},
		{name: "original ordinal", ordinal: 2, origin: resilience.OriginOriginal, started: started},
		{name: "unknown origin", ordinal: 2, origin: "unknown", parent: 1, started: started},
		{name: "additional ordinal one", ordinal: 1, origin: resilience.OriginRetry, parent: 1, started: started},
		{name: "additional missing parent", ordinal: 2, origin: resilience.OriginHedge, started: started},
		{name: "additional future parent", ordinal: 2, origin: resilience.OriginRetry, parent: 3, started: started},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := resilience.NewAttempt(test.ordinal, test.origin, test.parent, test.started); !errors.Is(err, resilience.ErrInvalidAttempt) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestExecutorClassifiesInvalidInputsAndPolicyResults(t *testing.T) {
	t.Parallel()

	executor, err := resilience.NewExecutor[int]()
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	metadata := metadataFor(t, "logical", "resource")
	//nolint:staticcheck // Verify defensive behavior for a nil caller context.
	//lint:ignore SA1012 Verify defensive behavior for a nil caller context.
	if result := executor.Execute(nil, metadata, func(context.Context, resilience.Attempt) (int, error) { return 0, nil }); !errors.Is(result.Err, resilience.ErrInvalidMetadata) || result.Outcome.Kind != resilience.OutcomePolicyFailure {
		t.Fatalf("nil context result = %+v", result)
	}
	if result := executor.Execute(context.Background(), resilience.Metadata{}, func(context.Context, resilience.Attempt) (int, error) { return 0, nil }); !errors.Is(result.Err, resilience.ErrInvalidMetadata) || result.Outcome.Kind != resilience.OutcomePolicyFailure {
		t.Fatalf("metadata result = %+v", result)
	}
	if result := executor.Execute(context.Background(), metadata, nil); !errors.Is(result.Err, resilience.ErrNilOperation) || result.Outcome.Kind != resilience.OutcomePolicyFailure {
		t.Fatalf("nil operation result = %+v", result)
	}
	executor, err = executor.WithClock(zeroClock{})
	if err != nil {
		t.Fatalf("with zero clock: %v", err)
	}
	if result := executor.Execute(context.Background(), metadata, func(context.Context, resilience.Attempt) (int, error) { return 0, nil }); !errors.Is(result.Err, resilience.ErrInvalidAttempt) || result.Outcome.Kind != resilience.OutcomePolicyFailure {
		t.Fatalf("zero clock result = %+v", result)
	}

	invalidExecutor, err := resilience.NewExecutor[int](invalidResultPolicy{})
	if err != nil {
		t.Fatalf("new invalid-result executor: %v", err)
	}
	if result := invalidExecutor.Execute(context.Background(), metadata, func(context.Context, resilience.Attempt) (int, error) { return 0, nil }); !errors.Is(result.Err, resilience.ErrPolicyFailure) || result.Outcome.Kind != resilience.OutcomePolicyFailure {
		t.Fatalf("invalid policy result = %+v", result)
	}
}

func TestExecutorRejectsPanickingWrapAndInvalidScopeOrder(t *testing.T) {
	t.Parallel()

	if _, err := resilience.NewExecutor[int](panickingWrapPolicy{}); !errors.Is(err, resilience.ErrInvalidComposition) {
		t.Fatalf("panicking wrap error = %v", err)
	}
	order := []string{}
	if _, err := resilience.NewExecutor[string](
		recordingPolicy{id: "attempt", scope: resilience.ScopeAttempt, order: &order},
		recordingPolicy{id: "logical", scope: resilience.ScopeLogical, order: &order},
	); !errors.Is(err, resilience.ErrInvalidComposition) {
		t.Fatalf("scope order error = %v", err)
	}
}

func TestLocalAndPolicyErrorsRemainSafeWithoutCauses(t *testing.T) {
	t.Parallel()

	attempt, err := resilience.NewAttempt(1, resilience.OriginOriginal, 0, time.Unix(4, 0))
	if err != nil {
		t.Fatalf("new attempt: %v", err)
	}
	long := strings.Repeat("x", resilience.MaxIdentityLength+20)
	rejection := resilience.LocalRejection[int](attempt, resilience.PolicyID(long), long, nil)
	var rejectionError *resilience.LocalRejectionError
	if !errors.Is(rejection.Err, resilience.ErrLocalRejection) || !errors.As(rejection.Err, &rejectionError) || len(rejectionError.Policy) != resilience.MaxIdentityLength || len(rejectionError.Reason) != resilience.MaxIdentityLength || rejection.Err.Error() == "" {
		t.Fatalf("rejection = %#v", rejection.Err)
	}
	ignored := resilience.Ignored[int](attempt, long)
	var ignoredError *resilience.IgnoredError
	if !errors.Is(ignored.Err, resilience.ErrIgnored) || !errors.As(ignored.Err, &ignoredError) || len(ignoredError.Reason) != resilience.MaxIdentityLength || ignored.Err.Error() == "" {
		t.Fatalf("ignored = %#v", ignored.Err)
	}
	policy := resilience.PolicyFailure[int](attempt, resilience.PolicyID(long), long, nil)
	var policyError *resilience.PolicyExecutionError
	if !errors.Is(policy.Err, resilience.ErrPolicyFailure) || !errors.As(policy.Err, &policyError) || len(policyError.Policy) != resilience.MaxIdentityLength || len(policyError.Stage) != resilience.MaxIdentityLength || policy.Err.Error() == "" {
		t.Fatalf("policy = %#v", policy.Err)
	}
}
