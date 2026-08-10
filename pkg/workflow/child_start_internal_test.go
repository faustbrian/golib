package workflow

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestChildStartValuesRejectInvalidAndOwnInput(t *testing.T) {
	t.Parallel()

	started, err := NewChildStartOutcome(ChildStartOutcomeSpec{Kind: ChildStarted})
	if err != nil || started.Kind() != ChildStarted || started.Code() != "" || started.Retryable() {
		t.Fatalf("started outcome = %#v, %v", started, err)
	}
	invalidOutcomes := []ChildStartOutcomeSpec{
		{},
		{Kind: ChildStarted, Code: "unexpected"},
		{Kind: ChildStarted, Retryable: true},
		{Kind: ChildStartFailed},
		{Kind: ChildStartUnknown},
		{Kind: ChildStartUnknown, Code: "uncertain", Retryable: true},
		{Kind: ChildStartOutcomeKind(99), Code: "invalid"},
	}
	for index, spec := range invalidOutcomes {
		if _, err := NewChildStartOutcome(spec); !errors.Is(err, ErrInvalidChildStart) {
			t.Fatalf("invalid outcome %d error = %v", index, err)
		}
	}

	now := time.Date(2036, 8, 11, 15, 0, 0, 0, time.UTC)
	parent, child, _ := internalChildDefinitions(t)
	input := []byte("input")
	valid := ChildStartRequestSpec{
		ParentInstanceID: "parent-1", ParentDefinition: parent.Reference(),
		StepName: "child", ChildID: "child-1", ChildDefinition: child.Reference(),
		Attempt: 1, MaxAttempts: 2, IdempotencyKey: "child-key",
		StartedAt: now, Deadline: now.Add(time.Minute), Input: input, InputLimit: 8,
		TenantID: "tenant-1", CorrelationID: "correlation-1",
	}
	request, err := NewChildStartRequest(valid)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	input[0] = 'X'
	returned := request.Input()
	returned[0] = 'Y'
	if string(request.Input()) != "input" || request.ParentInstanceID() != "parent-1" ||
		request.ParentDefinition() != parent.Reference() || request.StepName() != "child" ||
		request.ChildID() != "child-1" || request.ChildDefinition() != child.Reference() ||
		request.Attempt() != 1 || request.MaxAttempts() != 2 ||
		request.IdempotencyKey() != "child-key" || request.StartedAt() != now ||
		request.Deadline() != now.Add(time.Minute) || request.TenantID() != "tenant-1" ||
		request.CorrelationID() != "correlation-1" {
		t.Fatalf("request = %#v", request)
	}
	invalidRequests := []ChildStartRequestSpec{
		func() ChildStartRequestSpec { value := valid; value.ParentInstanceID = ""; return value }(),
		func() ChildStartRequestSpec {
			value := valid
			value.ParentDefinition = DefinitionReference{}
			return value
		}(),
		func() ChildStartRequestSpec { value := valid; value.StepName = ""; return value }(),
		func() ChildStartRequestSpec { value := valid; value.ChildID = ""; return value }(),
		func() ChildStartRequestSpec {
			value := valid
			value.ChildDefinition = DefinitionReference{}
			return value
		}(),
		func() ChildStartRequestSpec { value := valid; value.Attempt = 0; return value }(),
		func() ChildStartRequestSpec { value := valid; value.Attempt = 3; return value }(),
		func() ChildStartRequestSpec { value := valid; value.IdempotencyKey = ""; return value }(),
		func() ChildStartRequestSpec { value := valid; value.StartedAt = time.Time{}; return value }(),
		func() ChildStartRequestSpec { value := valid; value.Deadline = now; return value }(),
		func() ChildStartRequestSpec { value := valid; value.InputLimit = 0; return value }(),
		func() ChildStartRequestSpec { value := valid; value.Input = make([]byte, 9); return value }(),
		func() ChildStartRequestSpec { value := valid; value.TenantID = " spaces "; return value }(),
		func() ChildStartRequestSpec { value := valid; value.CorrelationID = " spaces "; return value }(),
	}
	for index, spec := range invalidRequests {
		if _, err := NewChildStartRequest(spec); !errors.Is(err, ErrInvalidChildStart) {
			t.Fatalf("invalid request %d error = %v", index, err)
		}
	}

	called := false
	start := ChildStartFunc(func(ctx context.Context, got ChildStartRequest) ChildStartOutcome {
		called = ctx != nil && got.ChildID() == "child-1"
		return started
	})
	if outcome := start.Start(context.Background(), request); !called || outcome.Kind() != ChildStarted {
		t.Fatalf("adapted outcome = %#v called = %t", outcome, called)
	}
}
