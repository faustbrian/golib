package workflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestActivityRequestOwnsBoundedAttemptMetadata(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	startedAt := time.Date(2026, 8, 9, 6, 0, 0, 123, time.FixedZone("EEST", 3*60*60))
	input := []byte("order-42")
	request, err := workflow.NewActivityRequest(workflow.ActivityRequestSpec{
		InstanceID: "instance-1", Definition: definition.Reference(), StepName: "execute",
		Attempt: 2, MaxAttempts: 3, IdempotencyKey: "instance-1/execute/2",
		StartedAt: startedAt, Deadline: startedAt.Add(time.Minute),
		Input: input, InputLimit: 1024, ResultLimit: 2048,
		TenantID: "tenant-1", CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	input[0] = 'X'

	if request.InstanceID() != "instance-1" || request.Definition() != definition.Reference() || request.StepName() != "execute" {
		t.Fatal("unexpected activity identity")
	}
	if request.Attempt() != 2 || request.MaxAttempts() != 3 || request.IdempotencyKey() != "instance-1/execute/2" {
		t.Fatal("unexpected attempt metadata")
	}
	if !request.StartedAt().Equal(startedAt.UTC()) || !request.Deadline().Equal(startedAt.Add(time.Minute).UTC()) {
		t.Fatal("request time was not canonicalized")
	}
	if request.InputLimit() != 1024 || request.ResultLimit() != 2048 || request.TenantID() != "tenant-1" || request.CorrelationID() != "correlation-1" {
		t.Fatal("unexpected request bounds or propagation metadata")
	}
	got := request.Input()
	if string(got) != "order-42" {
		t.Fatalf("input = %q", got)
	}
	got[0] = 'X'
	if string(request.Input()) != "order-42" {
		t.Fatal("request returned caller-mutable input")
	}
}

func TestActivityRequestRejectsMissingOrUnboundedMetadata(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	now := time.Date(2026, 8, 9, 6, 0, 0, 0, time.UTC)
	valid := workflow.ActivityRequestSpec{
		InstanceID: "instance-1", Definition: definition.Reference(), StepName: "execute",
		Attempt: 1, MaxAttempts: 3, IdempotencyKey: "key-1",
		StartedAt: now, Deadline: now.Add(time.Minute), Input: []byte("input"),
		InputLimit: 1024, ResultLimit: 1024,
	}

	tests := map[string]func() workflow.ActivityRequestSpec{
		"instance": func() workflow.ActivityRequestSpec { spec := valid; spec.InstanceID = ""; return spec },
		"definition": func() workflow.ActivityRequestSpec {
			spec := valid
			spec.Definition = workflow.DefinitionReference{}
			return spec
		},
		"step":                   func() workflow.ActivityRequestSpec { spec := valid; spec.StepName = ""; return spec },
		"attempt":                func() workflow.ActivityRequestSpec { spec := valid; spec.Attempt = 0; return spec },
		"max attempts":           func() workflow.ActivityRequestSpec { spec := valid; spec.MaxAttempts = 0; return spec },
		"attempt exceeds policy": func() workflow.ActivityRequestSpec { spec := valid; spec.Attempt = 4; return spec },
		"idempotency key":        func() workflow.ActivityRequestSpec { spec := valid; spec.IdempotencyKey = ""; return spec },
		"started time":           func() workflow.ActivityRequestSpec { spec := valid; spec.StartedAt = time.Time{}; return spec },
		"deadline":               func() workflow.ActivityRequestSpec { spec := valid; spec.Deadline = spec.StartedAt; return spec },
		"input limit":            func() workflow.ActivityRequestSpec { spec := valid; spec.InputLimit = 0; return spec },
		"result limit":           func() workflow.ActivityRequestSpec { spec := valid; spec.ResultLimit = 0; return spec },
		"oversized input": func() workflow.ActivityRequestSpec {
			spec := valid
			spec.Input = make([]byte, valid.InputLimit+1)
			return spec
		},
		"tenant":      func() workflow.ActivityRequestSpec { spec := valid; spec.TenantID = " spaces "; return spec },
		"correlation": func() workflow.ActivityRequestSpec { spec := valid; spec.CorrelationID = " spaces "; return spec },
	}

	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := workflow.NewActivityRequest(build()); !errors.Is(err, workflow.ErrInvalidActivityRequest) {
				t.Fatalf("error = %v, want ErrInvalidActivityRequest", err)
			}
		})
	}
}

func TestActivityRequestAcceptsExactAttemptAndInputBounds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 6, 0, 0, 0, time.UTC)
	request, err := workflow.NewActivityRequest(workflow.ActivityRequestSpec{
		InstanceID: "instance-1", Definition: mustDefinition(t, "orders", "1").Reference(),
		StepName: "execute", Attempt: 3, MaxAttempts: 3, IdempotencyKey: "key-3",
		StartedAt: now, Deadline: now.Add(time.Nanosecond),
		Input: make([]byte, 8), InputLimit: 8, ResultLimit: workflow.MaxPayloadBytes,
	})
	if err != nil {
		t.Fatalf("exact activity request bounds rejected: %v", err)
	}
	if len(request.Input()) != 8 || request.Attempt() != request.MaxAttempts() {
		t.Fatal("exact activity request bounds were not preserved")
	}
}

func TestActivityOutcomeDistinguishesKnownFailureAndUnknownOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		spec      workflow.ActivityOutcomeSpec
		kind      workflow.ActivityOutcomeKind
		retryable bool
	}{
		{name: "success", spec: workflow.ActivityOutcomeSpec{Kind: workflow.ActivitySucceeded, Data: []byte("result")}, kind: workflow.ActivitySucceeded},
		{name: "retryable failure", spec: workflow.ActivityOutcomeSpec{Kind: workflow.ActivityFailed, Code: "temporary", Retryable: true, Data: []byte("safe-details")}, kind: workflow.ActivityFailed, retryable: true},
		{name: "permanent failure", spec: workflow.ActivityOutcomeSpec{Kind: workflow.ActivityFailed, Code: "declined", Data: []byte("safe-details")}, kind: workflow.ActivityFailed},
		{name: "unknown", spec: workflow.ActivityOutcomeSpec{Kind: workflow.ActivityUnknown, Code: "connection-lost", Data: []byte("safe-details")}, kind: workflow.ActivityUnknown},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data := append([]byte(nil), test.spec.Data...)
			expectedData := string(data)
			test.spec.Data = data
			outcome, err := workflow.NewActivityOutcome(test.spec)
			if err != nil {
				t.Fatalf("construct outcome: %v", err)
			}
			if len(data) > 0 {
				data[0] = 'X'
			}
			if outcome.Kind() != test.kind || outcome.Code() != test.spec.Code || outcome.Retryable() != test.retryable {
				t.Fatal("unexpected outcome classification")
			}
			if string(outcome.Data()) != expectedData {
				t.Fatalf("outcome data = %q", outcome.Data())
			}
			got := outcome.Data()
			if len(got) > 0 {
				got[0] = 'X'
				if outcome.Data()[0] == 'X' {
					t.Fatal("outcome returned caller-mutable data")
				}
			}
		})
	}
}

func TestActivityOutcomeRejectsAmbiguousClassification(t *testing.T) {
	t.Parallel()

	tests := map[string]workflow.ActivityOutcomeSpec{
		"unknown kind":         {Kind: workflow.ActivityOutcomeKind(99)},
		"success with code":    {Kind: workflow.ActivitySucceeded, Code: "unexpected"},
		"success retryable":    {Kind: workflow.ActivitySucceeded, Retryable: true},
		"failure without code": {Kind: workflow.ActivityFailed},
		"unknown without code": {Kind: workflow.ActivityUnknown},
		"unknown retryable":    {Kind: workflow.ActivityUnknown, Code: "unknown", Retryable: true},
		"oversized data":       {Kind: workflow.ActivitySucceeded, Data: make([]byte, workflow.MaxPayloadBytes+1)},
	}

	for name, spec := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := workflow.NewActivityOutcome(spec); !errors.Is(err, workflow.ErrInvalidActivityOutcome) {
				t.Fatalf("error = %v, want ErrInvalidActivityOutcome", err)
			}
		})
	}
}

func TestActivityOutcomeAcceptsExactPayloadLimit(t *testing.T) {
	t.Parallel()

	outcome, err := workflow.NewActivityOutcome(workflow.ActivityOutcomeSpec{
		Kind: workflow.ActivitySucceeded, Data: make([]byte, workflow.MaxPayloadBytes),
	})
	if err != nil {
		t.Fatalf("exact outcome limit rejected: %v", err)
	}
	if len(outcome.Data()) != workflow.MaxPayloadBytes {
		t.Fatalf("outcome size = %d", len(outcome.Data()))
	}
}

func TestActivityExecutesExplicitHandlerWithBoundedContext(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	now := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	request, err := workflow.NewActivityRequest(workflow.ActivityRequestSpec{
		InstanceID: "instance-1", Definition: definition.Reference(), StepName: "execute",
		Attempt: 1, MaxAttempts: 1, IdempotencyKey: "key-1",
		StartedAt: now, Deadline: now.Add(time.Minute), InputLimit: 1024, ResultLimit: 8,
	})
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}

	called := false
	activity, err := workflow.NewActivity("orders.execute", func(ctx context.Context, got workflow.ActivityRequest) workflow.ActivityOutcome {
		called = true
		if deadline, ok := ctx.Deadline(); !ok || !deadline.Equal(request.Deadline()) {
			t.Fatal("activity context did not use the persisted deadline")
		}
		if got.IdempotencyKey() != "key-1" {
			t.Fatal("activity lost the idempotency key")
		}
		outcome, outcomeErr := workflow.NewActivityOutcome(workflow.ActivityOutcomeSpec{Kind: workflow.ActivitySucceeded, Data: []byte("result-8")})
		if outcomeErr != nil {
			t.Fatalf("construct outcome: %v", outcomeErr)
		}
		return outcome
	})
	if err != nil {
		t.Fatalf("construct activity: %v", err)
	}

	outcome, err := activity.Execute(context.Background(), request)
	if err != nil {
		t.Fatalf("execute activity: %v", err)
	}
	if !called || outcome.Kind() != workflow.ActivitySucceeded {
		t.Fatal("activity handler did not produce the explicit outcome")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	called = false
	if _, err := activity.Execute(cancelled, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled execution error = %v", err)
	}
	if called {
		t.Fatal("activity ran after cancellation")
	}
}

func TestActivityRejectsInvalidHandlerOrOutcome(t *testing.T) {
	t.Parallel()

	if _, err := workflow.NewActivity("", func(context.Context, workflow.ActivityRequest) workflow.ActivityOutcome {
		return workflow.ActivityOutcome{}
	}); !errors.Is(err, workflow.ErrInvalidActivity) {
		t.Fatalf("invalid name error = %v", err)
	}
	if _, err := workflow.NewActivity("orders.execute", nil); !errors.Is(err, workflow.ErrInvalidActivity) {
		t.Fatalf("nil handler error = %v", err)
	}

	activity, err := workflow.NewActivity("orders.execute", func(context.Context, workflow.ActivityRequest) workflow.ActivityOutcome {
		return workflow.ActivityOutcome{}
	})
	if err != nil {
		t.Fatalf("construct activity: %v", err)
	}
	if _, err := activity.Execute(context.Background(), workflow.ActivityRequest{}); !errors.Is(err, workflow.ErrInvalidActivityRequest) {
		t.Fatalf("invalid request error = %v", err)
	}

	now := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	request, err := workflow.NewActivityRequest(workflow.ActivityRequestSpec{
		InstanceID: "instance-1", Definition: mustDefinition(t, "orders", "1").Reference(),
		StepName: "execute", Attempt: 1, MaxAttempts: 1, IdempotencyKey: "key-1",
		StartedAt: now, Deadline: now.Add(time.Minute), InputLimit: 8, ResultLimit: 8,
	})
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	if _, err := (workflow.Activity{}).Execute(context.Background(), request); !errors.Is(err, workflow.ErrInvalidActivity) {
		t.Fatalf("zero activity execution error = %v", err)
	}
	if _, err := activity.Execute(context.Background(), request); !errors.Is(err, workflow.ErrInvalidActivityOutcome) {
		t.Fatalf("zero outcome error = %v", err)
	}

	oversized, err := workflow.NewActivity("orders.oversized", func(context.Context, workflow.ActivityRequest) workflow.ActivityOutcome {
		outcome, outcomeErr := workflow.NewActivityOutcome(workflow.ActivityOutcomeSpec{Kind: workflow.ActivitySucceeded, Data: make([]byte, 9)})
		if outcomeErr != nil {
			t.Fatalf("construct oversized outcome: %v", outcomeErr)
		}
		return outcome
	})
	if err != nil {
		t.Fatalf("construct oversized activity: %v", err)
	}
	if _, err := oversized.Execute(context.Background(), request); !errors.Is(err, workflow.ErrInvalidActivityOutcome) {
		t.Fatalf("result limit error = %v", err)
	}

	pastRequest, err := workflow.NewActivityRequest(workflow.ActivityRequestSpec{
		InstanceID: "instance-1", Definition: mustDefinition(t, "orders", "1").Reference(),
		StepName: "execute", Attempt: 1, MaxAttempts: 1, IdempotencyKey: "key-past",
		StartedAt: time.Unix(1, 0), Deadline: time.Unix(2, 0),
		InputLimit: 8, ResultLimit: 8,
	})
	if err != nil {
		t.Fatalf("construct past request: %v", err)
	}
	if _, err := oversized.Execute(context.Background(), pastRequest); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("past deadline error = %v", err)
	}
}

func TestActivityRegistryRejectsDuplicatesAndResolvesExplicitly(t *testing.T) {
	t.Parallel()

	handler := func(context.Context, workflow.ActivityRequest) workflow.ActivityOutcome {
		return workflow.ActivityOutcome{}
	}
	activity, err := workflow.NewActivity("orders.execute", handler)
	if err != nil {
		t.Fatalf("construct activity: %v", err)
	}
	registry, err := workflow.CompileActivities(activity)
	if err != nil {
		t.Fatalf("compile activities: %v", err)
	}
	resolved, err := registry.Resolve("orders.execute")
	if err != nil {
		t.Fatalf("resolve activity: %v", err)
	}
	if resolved.Name() != "orders.execute" {
		t.Fatalf("activity name = %q", resolved.Name())
	}
	if _, err := workflow.CompileActivities(activity, activity); !errors.Is(err, workflow.ErrDuplicateActivity) {
		t.Fatalf("duplicate activity error = %v", err)
	}
	if _, err := registry.Resolve("orders.missing"); !errors.Is(err, workflow.ErrActivityNotFound) {
		t.Fatalf("missing activity error = %v", err)
	}
	if _, err := workflow.CompileActivities(workflow.Activity{}); !errors.Is(err, workflow.ErrInvalidActivity) {
		t.Fatalf("zero activity error = %v", err)
	}
	if _, err := (*workflow.ActivityRegistry)(nil).Resolve("orders.execute"); !errors.Is(err, workflow.ErrActivityNotFound) {
		t.Fatalf("nil registry error = %v", err)
	}
}
