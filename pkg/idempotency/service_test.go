package idempotency_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/idempotency"
)

func TestNewServiceRequiresAStore(t *testing.T) {
	t.Parallel()

	_, err := idempotency.NewService(nil)

	assertReason(t, err, idempotency.ReasonInvalidConfiguration)
}

func TestBeginMapsDurableOutcomesToExecutionDecisions(t *testing.T) {
	t.Parallel()

	tests := map[idempotency.Outcome]bool{
		idempotency.OutcomeAcquired:           true,
		idempotency.OutcomeStaleOwnerTakeover: true,
		idempotency.OutcomeReplayed:           false,
		idempotency.OutcomeInProgress:         false,
		idempotency.OutcomeConflict:           false,
		idempotency.OutcomeTerminalFailure:    false,
	}

	for outcome, execute := range tests {
		t.Run(string(outcome), func(t *testing.T) {
			t.Parallel()

			record := idempotency.Record{State: idempotency.StateAcquired}
			store := &stubStore{
				acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
					return idempotency.AcquireResult{Outcome: outcome, Record: record}, nil
				},
			}
			service, err := idempotency.NewService(store)
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}

			result, err := service.Begin(context.Background(), idempotency.BeginRequest{})
			if err != nil {
				t.Fatalf("Begin() error = %v", err)
			}
			if result.Outcome != outcome || result.Execute != execute ||
				!result.Durable || result.Record.State != record.State || result.Failure != nil {
				t.Fatalf("Begin() = %#v", result)
			}
		})
	}
}

func TestBeginFailsClosedWhenStorageIsUnavailable(t *testing.T) {
	t.Parallel()

	backendErr := errors.New("connection lost")
	service, err := idempotency.NewService(&stubStore{
		acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
			return idempotency.AcquireResult{}, backendErr
		},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.Begin(context.Background(), idempotency.BeginRequest{})

	assertReason(t, err, idempotency.ReasonUnavailable)
	if !errors.Is(err, backendErr) {
		t.Fatalf("Begin() error = %v, want wrapped backend error", err)
	}
	if result.Outcome != idempotency.OutcomeUnavailable || result.Execute ||
		result.Durable || !errors.Is(result.Failure, backendErr) {
		t.Fatalf("Begin() = %#v", result)
	}
}

func TestBeginAllowsExplicitUntrackedExecution(t *testing.T) {
	t.Parallel()

	backendErr := errors.New("connection lost")
	service, err := idempotency.NewService(&stubStore{
		acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
			return idempotency.AcquireResult{}, backendErr
		},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.Begin(context.Background(), idempotency.BeginRequest{
		Availability: idempotency.AvailabilityAllowUntracked,
	})

	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if result.Outcome != idempotency.OutcomeUnavailable || !result.Execute ||
		result.Durable || !errors.Is(result.Failure, backendErr) {
		t.Fatalf("Begin() = %#v", result)
	}
}

func TestBeginPreservesSemanticAndContextErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]error{
		"semantic": &idempotency.Error{Reason: idempotency.ReasonInvalidLease, Field: "lease"},
		"context":  context.Canceled,
	}
	for name, storeErr := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			service, err := idempotency.NewService(&stubStore{
				acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
					return idempotency.AcquireResult{}, storeErr
				},
			})
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}

			result, err := service.Begin(context.Background(), idempotency.BeginRequest{})

			if !errors.Is(err, storeErr) || result.Outcome == idempotency.OutcomeUnavailable {
				t.Fatalf("Begin() = %#v, %v", result, err)
			}
		})
	}
}

func TestBeginRejectsUnknownAvailabilityPolicy(t *testing.T) {
	t.Parallel()

	called := false
	service, err := idempotency.NewService(&stubStore{
		acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
			called = true
			return idempotency.AcquireResult{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = service.Begin(context.Background(), idempotency.BeginRequest{
		Availability: idempotency.AvailabilityPolicy(255),
	})

	assertReason(t, err, idempotency.ReasonInvalidConfiguration)
	if called {
		t.Fatal("Begin() called the store for an invalid policy")
	}
}

func TestBeginRejectsNonDurableStoreOutcomesWithoutAnError(t *testing.T) {
	t.Parallel()

	for _, outcome := range []idempotency.Outcome{
		idempotency.OutcomeUnavailable,
		idempotency.Outcome("unknown"),
	} {
		t.Run(string(outcome), func(t *testing.T) {
			t.Parallel()

			service, err := idempotency.NewService(&stubStore{
				acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
					return idempotency.AcquireResult{Outcome: outcome}, nil
				},
			})
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}

			result, err := service.Begin(context.Background(), idempotency.BeginRequest{})

			assertReason(t, err, idempotency.ReasonInvalidTransition)
			if result.Execute || result.Durable {
				t.Fatalf("Begin() = %#v", result)
			}
		})
	}
}

func TestServiceDelegatesStateOperations(t *testing.T) {
	t.Parallel()

	want := idempotency.Record{State: idempotency.StateRunning}
	store := &stubStore{
		inspect:   func(context.Context, idempotency.Key) (idempotency.Record, error) { return want, nil },
		heartbeat: func(context.Context, idempotency.HeartbeatRequest) (idempotency.Record, error) { return want, nil },
		complete:  func(context.Context, idempotency.CompleteRequest) (idempotency.Record, error) { return want, nil },
		fail:      func(context.Context, idempotency.FailRequest) (idempotency.Record, error) { return want, nil },
		release:   func(context.Context, idempotency.Ownership) (idempotency.Record, error) { return want, nil },
		expire:    func(context.Context, idempotency.Key) (idempotency.Record, error) { return want, nil },
	}
	service, err := idempotency.NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	results := make([]idempotency.Record, 0, 6)
	record, err := service.Inspect(context.Background(), idempotency.Key{})
	results = append(results, record)
	assertNoError(t, err)
	record, err = service.Heartbeat(context.Background(), idempotency.HeartbeatRequest{})
	results = append(results, record)
	assertNoError(t, err)
	record, err = service.Complete(context.Background(), idempotency.CompleteRequest{})
	results = append(results, record)
	assertNoError(t, err)
	record, err = service.Fail(context.Background(), idempotency.FailRequest{})
	results = append(results, record)
	assertNoError(t, err)
	record, err = service.Release(context.Background(), idempotency.Ownership{})
	results = append(results, record)
	assertNoError(t, err)
	record, err = service.Expire(context.Background(), idempotency.Key{})
	results = append(results, record)
	assertNoError(t, err)

	for index, result := range results {
		if result.State != want.State {
			t.Fatalf("result %d = %#v", index, result)
		}
	}
}

func TestServiceNormalizesStateOperationStorageErrors(t *testing.T) {
	t.Parallel()

	backendErr := errors.New("backend unavailable")
	store := &stubStore{
		inspect: func(context.Context, idempotency.Key) (idempotency.Record, error) {
			return idempotency.Record{}, backendErr
		},
		heartbeat: func(context.Context, idempotency.HeartbeatRequest) (idempotency.Record, error) {
			return idempotency.Record{}, backendErr
		},
		complete: func(context.Context, idempotency.CompleteRequest) (idempotency.Record, error) {
			return idempotency.Record{}, backendErr
		},
		fail: func(context.Context, idempotency.FailRequest) (idempotency.Record, error) {
			return idempotency.Record{}, backendErr
		},
		release: func(context.Context, idempotency.Ownership) (idempotency.Record, error) {
			return idempotency.Record{}, backendErr
		},
		expire: func(context.Context, idempotency.Key) (idempotency.Record, error) {
			return idempotency.Record{}, backendErr
		},
	}
	service, err := idempotency.NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	errorsFromService := make([]error, 0, 6)
	_, err = service.Inspect(context.Background(), idempotency.Key{})
	errorsFromService = append(errorsFromService, err)
	_, err = service.Heartbeat(context.Background(), idempotency.HeartbeatRequest{})
	errorsFromService = append(errorsFromService, err)
	_, err = service.Complete(context.Background(), idempotency.CompleteRequest{})
	errorsFromService = append(errorsFromService, err)
	_, err = service.Fail(context.Background(), idempotency.FailRequest{})
	errorsFromService = append(errorsFromService, err)
	_, err = service.Release(context.Background(), idempotency.Ownership{})
	errorsFromService = append(errorsFromService, err)
	_, err = service.Expire(context.Background(), idempotency.Key{})
	errorsFromService = append(errorsFromService, err)

	for index, err := range errorsFromService {
		var semanticError *idempotency.Error
		if !errors.As(err, &semanticError) || semanticError.Reason != idempotency.ReasonUnavailable ||
			!errors.Is(err, backendErr) {
			t.Fatalf("error %d = %v", index, err)
		}
	}
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

type stubStore struct {
	acquire   func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error)
	inspect   func(context.Context, idempotency.Key) (idempotency.Record, error)
	heartbeat func(context.Context, idempotency.HeartbeatRequest) (idempotency.Record, error)
	complete  func(context.Context, idempotency.CompleteRequest) (idempotency.Record, error)
	fail      func(context.Context, idempotency.FailRequest) (idempotency.Record, error)
	release   func(context.Context, idempotency.Ownership) (idempotency.Record, error)
	expire    func(context.Context, idempotency.Key) (idempotency.Record, error)
}

func (s *stubStore) Acquire(ctx context.Context, request idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
	return s.acquire(ctx, request)
}

func (s *stubStore) Inspect(ctx context.Context, key idempotency.Key) (idempotency.Record, error) {
	return s.inspect(ctx, key)
}

func (s *stubStore) Heartbeat(ctx context.Context, request idempotency.HeartbeatRequest) (idempotency.Record, error) {
	return s.heartbeat(ctx, request)
}

func (s *stubStore) Complete(ctx context.Context, request idempotency.CompleteRequest) (idempotency.Record, error) {
	return s.complete(ctx, request)
}

func (s *stubStore) Fail(ctx context.Context, request idempotency.FailRequest) (idempotency.Record, error) {
	return s.fail(ctx, request)
}

func (s *stubStore) Release(ctx context.Context, ownership idempotency.Ownership) (idempotency.Record, error) {
	return s.release(ctx, ownership)
}

func (s *stubStore) Expire(ctx context.Context, key idempotency.Key) (idempotency.Record, error) {
	return s.expire(ctx, key)
}

var _ idempotency.Store = (*stubStore)(nil)
