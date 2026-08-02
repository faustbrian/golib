package resilience_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/resilience"
)

type recordingPolicy struct {
	id       resilience.PolicyID
	scope    resilience.Scope
	repeated bool
	order    *[]string
}

type typedError struct{ code int }

func (err *typedError) Error() string { return "typed" }

func (policy recordingPolicy) Descriptor() resilience.PolicyDescriptor {
	return resilience.PolicyDescriptor{ID: policy.id, Scope: policy.scope, Repeatable: policy.repeated}
}

func (policy recordingPolicy) Wrap(next resilience.Stage[string]) resilience.Stage[string] {
	return func(ctx context.Context, execution resilience.Execution, operation resilience.Operation[string]) resilience.Result[string] {
		*policy.order = append(*policy.order, "enter:"+string(policy.id))
		execution.Emit(resilience.EventPolicyEntered, policy.id, "")
		result := next(ctx, execution, operation)
		*policy.order = append(*policy.order, "exit:"+string(policy.id))
		return result
	}
}

func TestExecutorAppliesInspectablePoliciesOuterToInner(t *testing.T) {
	t.Parallel()

	order := make([]string, 0, 5)
	executor, err := resilience.NewExecutor[string](
		recordingPolicy{id: "outer", scope: resilience.ScopeLogical, order: &order},
		recordingPolicy{id: "inner", scope: resilience.ScopeAttempt, order: &order},
	)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	executor, err = executor.WithTimeline(8)
	if err != nil {
		t.Fatalf("with timeline: %v", err)
	}
	metadata, err := resilience.NewMetadata("logical-1", "lookup", "postal:FI")
	if err != nil {
		t.Fatalf("new metadata: %v", err)
	}

	result := executor.Execute(context.Background(), metadata, func(_ context.Context, attempt resilience.Attempt) (string, error) {
		order = append(order, "operation")
		if attempt.Ordinal != 1 || attempt.Origin != resilience.OriginOriginal {
			t.Fatalf("attempt = %+v", attempt)
		}
		return "ok", nil
	})

	wantOrder := []string{"enter:outer", "enter:inner", "operation", "exit:inner", "exit:outer"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("order = %v, want %v", order, wantOrder)
	}
	if result.Value != "ok" || result.Err != nil || result.Outcome.Kind != resilience.OutcomeSuccess {
		t.Fatalf("result = %+v", result)
	}
	if got := executor.Policies(); !reflect.DeepEqual(got, []resilience.PolicyDescriptor{
		{ID: "outer", Scope: resilience.ScopeLogical},
		{ID: "inner", Scope: resilience.ScopeAttempt},
	}) {
		t.Fatalf("policies = %+v", got)
	}
	if len(result.Events) != 6 || result.Events[0].Kind != resilience.EventExecutionStarted || result.Events[5].Kind != resilience.EventExecutionCompleted {
		t.Fatalf("events = %+v", result.Events)
	}
}

func TestExecutorPreservesTypedErrorsAndClassifiesContexts(t *testing.T) {
	t.Parallel()

	funcError := &typedError{code: 42}
	executor, err := resilience.NewExecutor[int]()
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	metadata, err := resilience.NewMetadata("logical-2", "read", "resource")
	if err != nil {
		t.Fatalf("new metadata: %v", err)
	}

	result := executor.Execute(context.Background(), metadata, func(context.Context, resilience.Attempt) (int, error) {
		return 7, funcError
	})
	if result.Value != 7 || !errors.Is(result.Err, funcError) || result.Outcome.Kind != resilience.OutcomeOperationFailure {
		t.Fatalf("operation failure result = %+v", result)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	result = executor.Execute(canceled, metadata, func(context.Context, resilience.Attempt) (int, error) {
		t.Fatal("operation ran after caller cancellation")
		return 0, nil
	})
	if !errors.Is(result.Err, context.Canceled) || result.Outcome.Kind != resilience.OutcomeCancellation {
		t.Fatalf("cancellation result = %+v", result)
	}
}

func TestExecutorRejectsInvalidCompositionsBeforeExecution(t *testing.T) {
	t.Parallel()

	order := []string{}
	tests := []struct {
		name     string
		policies []resilience.Policy[string]
	}{
		{name: "nil", policies: []resilience.Policy[string]{nil}},
		{name: "blank identity", policies: []resilience.Policy[string]{recordingPolicy{scope: resilience.ScopeLogical, order: &order}}},
		{name: "long identity", policies: []resilience.Policy[string]{recordingPolicy{id: resilience.PolicyID(strings.Repeat("x", resilience.MaxIdentityLength+1)), scope: resilience.ScopeLogical, order: &order}}},
		{name: "invalid scope", policies: []resilience.Policy[string]{recordingPolicy{id: "bad", scope: "other", order: &order}}},
		{name: "duplicate", policies: []resilience.Policy[string]{
			recordingPolicy{id: "same", scope: resilience.ScopeLogical, order: &order},
			recordingPolicy{id: "same", scope: resilience.ScopeLogical, order: &order},
		}},
		{name: "repeatable scope mismatch", policies: []resilience.Policy[string]{
			recordingPolicy{id: "same", scope: resilience.ScopeLogical, repeated: true, order: &order},
			recordingPolicy{id: "same", scope: resilience.ScopeAttempt, repeated: true, order: &order},
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := resilience.NewExecutor[string](test.policies...)
			if !errors.Is(err, resilience.ErrInvalidComposition) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestExecutorAcceptsOnlyFullyCompatibleRepeatedPolicies(t *testing.T) {
	t.Parallel()

	order := []string{}
	compatible := recordingPolicy{id: "same", scope: resilience.ScopeLogical, repeated: true, order: &order}
	if _, err := resilience.NewExecutor[string](compatible, compatible); err != nil {
		t.Fatalf("compatible duplicate error = %v", err)
	}
	for _, policies := range [][]resilience.Policy[string]{
		{
			recordingPolicy{id: "same", scope: resilience.ScopeLogical, repeated: false, order: &order},
			recordingPolicy{id: "same", scope: resilience.ScopeLogical, repeated: true, order: &order},
		},
		{
			recordingPolicy{id: "same", scope: resilience.ScopeLogical, repeated: true, order: &order},
			recordingPolicy{id: "same", scope: resilience.ScopeLogical, repeated: false, order: &order},
		},
	} {
		if _, err := resilience.NewExecutor[string](policies...); !errors.Is(err, resilience.ErrInvalidComposition) {
			t.Fatalf("incompatible duplicate error = %v", err)
		}
	}
}

func TestMetadataAndAttemptInputsAreBounded(t *testing.T) {
	t.Parallel()

	if _, err := resilience.NewMetadata("", "lookup", "resource"); !errors.Is(err, resilience.ErrInvalidMetadata) {
		t.Fatalf("blank logical ID error = %v", err)
	}
	if _, err := resilience.NewMetadata("logical", "lookup", string(make([]byte, resilience.MaxIdentityLength+1))); !errors.Is(err, resilience.ErrInvalidMetadata) {
		t.Fatalf("long resource error = %v", err)
	}
	exact := strings.Repeat("x", resilience.MaxIdentityLength)
	if _, err := resilience.NewMetadata(exact, exact, exact); err != nil {
		t.Fatalf("exact-bound metadata error = %v", err)
	}
	started := time.Unix(1, 0)
	if _, err := resilience.NewAttempt(0, resilience.OriginRetry, 0, started); !errors.Is(err, resilience.ErrInvalidAttempt) {
		t.Fatalf("zero ordinal error = %v", err)
	}
	if _, err := resilience.NewAttempt(2, resilience.OriginOriginal, 0, started); !errors.Is(err, resilience.ErrInvalidAttempt) {
		t.Fatalf("original ordinal error = %v", err)
	}
	if _, err := resilience.NewAttempt(2, resilience.OriginHedge, 2, started); !errors.Is(err, resilience.ErrInvalidAttempt) {
		t.Fatalf("self parent error = %v", err)
	}
}
