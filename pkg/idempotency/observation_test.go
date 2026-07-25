package idempotency_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
)

func TestServiceObserverReceivesAcquisitionOutcome(t *testing.T) {
	key, err := idempotency.NewKey("queue", "tenant", "consume", "worker", "message-1")
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("payload-v1", []byte("payload"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	hash, err := idempotency.NewHMACKeyHasher(
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	if err != nil {
		t.Fatalf("NewHMACKeyHasher() error = %v", err)
	}
	var events []idempotency.Observation
	service, err := idempotency.NewServiceWithOptions(&stubStore{
		acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
			return idempotency.AcquireResult{
				Outcome: idempotency.OutcomeAcquired,
				Record: idempotency.Record{
					Key: key, Fingerprint: fingerprint, State: idempotency.StateAcquired,
				},
			}, nil
		},
	}, idempotency.ServiceOptions{
		Observer: idempotency.ObserverFunc(func(_ context.Context, event idempotency.Observation) {
			events = append(events, event)
		}),
		KeyHasher: hash,
	})
	if err != nil {
		t.Fatalf("NewServiceWithOptions() error = %v", err)
	}
	result, err := service.Begin(context.Background(), idempotency.BeginRequest{
		Acquire: idempotency.AcquireRequest{
			Key: key, Fingerprint: fingerprint, Lease: time.Minute,
		},
	})
	if err != nil || result.Outcome != idempotency.OutcomeAcquired {
		t.Fatalf("Begin() = %#v, %v", result, err)
	}
	if len(events) != 1 {
		t.Fatalf("observation count = %d, want 1", len(events))
	}
	want := idempotency.Observation{
		Transition:  idempotency.TransitionAcquire,
		Outcome:     idempotency.OutcomeAcquired,
		Durable:     true,
		Correlation: hash(key),
	}
	if events[0] != want {
		t.Fatalf("observation = %#v, want %#v", events[0], want)
	}
}

func TestServiceObserverReportsEveryStateTransition(t *testing.T) {
	key, err := idempotency.NewKey("command", "tenant", "import", "worker", "row-1")
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	wantRecord := idempotency.Record{Key: key, State: idempotency.StateRunning}
	store := &stubStore{
		inspect: func(context.Context, idempotency.Key) (idempotency.Record, error) {
			return wantRecord, nil
		},
		heartbeat: func(context.Context, idempotency.HeartbeatRequest) (idempotency.Record, error) {
			return wantRecord, nil
		},
		complete: func(context.Context, idempotency.CompleteRequest) (idempotency.Record, error) {
			return wantRecord, nil
		},
		fail: func(context.Context, idempotency.FailRequest) (idempotency.Record, error) {
			return wantRecord, nil
		},
		release: func(context.Context, idempotency.Ownership) (idempotency.Record, error) {
			return wantRecord, nil
		},
		expire: func(context.Context, idempotency.Key) (idempotency.Record, error) {
			return wantRecord, nil
		},
	}
	var events []idempotency.Observation
	service, err := idempotency.NewServiceWithOptions(store, idempotency.ServiceOptions{
		Observer: idempotency.ObserverFunc(func(_ context.Context, event idempotency.Observation) {
			events = append(events, event)
		}),
	})
	if err != nil {
		t.Fatalf("NewServiceWithOptions() error = %v", err)
	}
	ownership := idempotency.Ownership{Key: key, OwnerToken: "owner", FencingToken: 1}
	if _, err := service.Inspect(context.Background(), key); err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if _, err := service.Heartbeat(context.Background(), idempotency.HeartbeatRequest{
		Ownership: ownership, Lease: time.Minute,
	}); err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if _, err := service.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: ownership,
	}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if _, err := service.Fail(context.Background(), idempotency.FailRequest{
		Ownership: ownership,
	}); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	if _, err := service.Release(context.Background(), ownership); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if _, err := service.Expire(context.Background(), key); err != nil {
		t.Fatalf("Expire() error = %v", err)
	}
	want := []idempotency.Transition{
		idempotency.TransitionInspect,
		idempotency.TransitionHeartbeat,
		idempotency.TransitionComplete,
		idempotency.TransitionFail,
		idempotency.TransitionRelease,
		idempotency.TransitionExpire,
	}
	if len(events) != len(want) {
		t.Fatalf("observation count = %d, want %d", len(events), len(want))
	}
	for index, transition := range want {
		if events[index].Transition != transition || !events[index].Durable ||
			events[index].Outcome != "" || events[index].Reason != "" ||
			events[index].Correlation != "" {
			t.Fatalf("observation %d = %#v", index, events[index])
		}
	}
}

func TestServiceObserverReportsUnavailableUntrackedExecution(t *testing.T) {
	backendErr := errors.New("backend unavailable")
	var events []idempotency.Observation
	service, err := idempotency.NewServiceWithOptions(&stubStore{
		acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
			return idempotency.AcquireResult{}, backendErr
		},
	}, idempotency.ServiceOptions{
		Observer: idempotency.ObserverFunc(func(_ context.Context, event idempotency.Observation) {
			events = append(events, event)
		}),
	})
	if err != nil {
		t.Fatalf("NewServiceWithOptions() error = %v", err)
	}
	result, err := service.Begin(context.Background(), idempotency.BeginRequest{
		Availability: idempotency.AvailabilityAllowUntracked,
	})
	if err != nil || !result.Execute || result.Durable ||
		result.Outcome != idempotency.OutcomeUnavailable {
		t.Fatalf("Begin() = %#v, %v", result, err)
	}
	if len(events) != 1 {
		t.Fatalf("observation count = %d, want 1", len(events))
	}
	want := idempotency.Observation{
		Transition: idempotency.TransitionAcquire,
		Outcome:    idempotency.OutcomeUnavailable,
		Reason:     idempotency.ReasonUnavailable,
	}
	if events[0] != want {
		t.Fatalf("observation = %#v, want %#v", events[0], want)
	}
}

func TestServiceObservationCannotChangeSemanticResult(t *testing.T) {
	key, err := idempotency.NewKey("rpc", "tenant", "method", "caller", "request")
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	store := &stubStore{
		acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
			return idempotency.AcquireResult{
				Outcome: idempotency.OutcomeAcquired,
				Record:  idempotency.Record{Key: key},
			}, nil
		},
	}
	tests := map[string]idempotency.ServiceOptions{
		"observer panic": {
			Observer: idempotency.ObserverFunc(func(context.Context, idempotency.Observation) {
				panic("observer failed")
			}),
		},
		"hasher panic": {
			Observer: idempotency.ObserverFunc(func(context.Context, idempotency.Observation) {}),
			KeyHasher: func(idempotency.Key) string {
				panic("hasher failed")
			},
		},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			service, err := idempotency.NewServiceWithOptions(store, options)
			if err != nil {
				t.Fatalf("NewServiceWithOptions() error = %v", err)
			}
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("Begin() observation panic = %v", recovered)
				}
			}()
			result, err := service.Begin(context.Background(), idempotency.BeginRequest{
				Acquire: idempotency.AcquireRequest{Key: key},
			})
			if err != nil || result.Outcome != idempotency.OutcomeAcquired || !result.Durable {
				t.Fatalf("Begin() = %#v, %v", result, err)
			}
		})
	}
}

func TestNewHMACKeyHasherProtectsLogicalIdentity(t *testing.T) {
	key, err := idempotency.NewKey(
		"billing", "tenant-secret", "create-invoice", "caller-secret", "request-secret",
	)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	secret := []byte("0123456789abcdef0123456789abcdef")
	hash, err := idempotency.NewHMACKeyHasher(secret)
	if err != nil {
		t.Fatalf("NewHMACKeyHasher() error = %v", err)
	}
	first := hash(key)
	secret[0] = 'x'
	if second := hash(key); second != first {
		t.Fatalf("hash changed after caller mutated secret: %q != %q", second, first)
	}
	if len(first) != 64 {
		t.Fatalf("hash length = %d, want 64", len(first))
	}
	for _, raw := range []string{
		key.Namespace(), key.Tenant(), key.Operation(), key.Caller(), key.Value(),
	} {
		if strings.Contains(first, raw) {
			t.Fatalf("hash exposed logical identity %q", raw)
		}
	}
	other, err := idempotency.NewKey(
		"billing", "tenant-secret", "create-invoice", "caller-secret", "other",
	)
	if err != nil {
		t.Fatalf("NewKey() other error = %v", err)
	}
	if hash(other) == first {
		t.Fatal("different logical keys produced the same correlation hash")
	}

	_, err = idempotency.NewHMACKeyHasher([]byte("short"))
	var semantic *idempotency.Error
	if !errors.As(err, &semantic) ||
		semantic.Reason != idempotency.ReasonInvalidConfiguration ||
		semantic.Field != "correlation_secret" {
		t.Fatalf("NewHMACKeyHasher() short secret error = %v", err)
	}
}
