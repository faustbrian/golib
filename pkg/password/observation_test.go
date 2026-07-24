package password_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
	"github.com/faustbrian/golib/pkg/password/passwordtest"
)

type recordingObserver struct {
	mu     sync.Mutex
	events []password.Observation
}

func (o *recordingObserver) Observe(_ context.Context, event password.Observation) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, event)
}
func (o *recordingObserver) snapshot() []password.Observation {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]password.Observation(nil), o.events...)
}

func TestOperationsEmitBoundedSecretSafeObservations(t *testing.T) {
	observer := &recordingObserver{}
	svc, err := passwordtest.NewService(testArgonPolicy(t), []byte("synthetic entropy"), password.WithObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := svc.Hash(context.Background(), []byte("secret material"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(context.Background(), []byte("wrong material"), hash.String()); err == nil {
		t.Fatal("mismatch succeeded")
	}
	if _, err := svc.Verify(context.Background(), []byte("secret material"), "malformed"); err == nil {
		t.Fatal("malformed succeeded")
	}
	result, upgraded, err := svc.VerifyAndUpgrade(context.Background(), []byte(passwordtest.SyntheticPassword), passwordtest.LaravelBcrypt)
	if err != nil || !result.Match() || upgraded.String() == "" {
		t.Fatalf("upgrade: %+v %v", result, err)
	}
	events := observer.snapshot()
	want := []struct {
		operation password.Operation
		outcome   password.Outcome
	}{{password.OperationHash, password.OutcomeSuccess}, {password.OperationVerify, password.OutcomeMismatch}, {password.OperationVerify, password.OutcomeMalformed}, {password.OperationVerifyAndUpgrade, password.OutcomeUpgraded}}
	if len(events) != len(want) {
		t.Fatalf("events = %#v", events)
	}
	for index, expected := range want {
		if events[index].Operation != expected.operation || events[index].Outcome != expected.outcome || events[index].Algorithm != password.Argon2id || events[index].Duration < 0 {
			t.Fatalf("event[%d] = %#v", index, events[index])
		}
	}
	if !events[3].NeedsRehash {
		t.Fatal("upgrade event omitted rehash state")
	}
}

func TestObserverConfigurationRejectsNil(t *testing.T) {
	if _, err := password.New(testArgonPolicy(t), password.WithObserver(nil)); err == nil {
		t.Fatal("nil observer accepted")
	}
	var observer *recordingObserver
	if _, err := password.New(testArgonPolicy(t), password.WithObserver(observer)); err == nil {
		t.Fatal("typed-nil observer accepted")
	}
	var entropy *zeroReader
	if _, err := password.NewTestService(testArgonPolicy(t), entropy); !errors.Is(err, password.ErrEntropy) {
		t.Fatalf("typed-nil entropy error = %v", err)
	}
}

func TestObservationOutcomeMatrixAndPanicIsolation(t *testing.T) {
	observer := &recordingObserver{}
	svc, err := passwordtest.NewService(testArgonPolicy(t), []byte("synthetic entropy"), password.WithObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := svc.Hash(context.Background(), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.VerifyAndUpgrade(context.Background(), []byte("secret"), hash.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(context.Background(), []byte("secret"), "$scrypt$v=1$x"); !errors.Is(err, password.ErrUnsupportedAlgorithm) {
		t.Fatalf("unsupported: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Verify(ctx, []byte("secret"), hash.String()); !errors.Is(err, password.ErrCanceled) {
		t.Fatalf("canceled: %v", err)
	}
	if _, err := svc.Hash(context.Background(), make([]byte, 1025)); !errors.Is(err, password.ErrResourceRejected) {
		t.Fatalf("resource: %v", err)
	}
	failing, err := password.NewTestService(testArgonPolicy(t), io.LimitReader(&zeroReader{}, 0), password.WithObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failing.Hash(context.Background(), []byte("secret")); !errors.Is(err, password.ErrEntropy) {
		t.Fatalf("failure: %v", err)
	}
	events := observer.snapshot()
	wants := []password.Outcome{password.OutcomeSuccess, password.OutcomeSuccess, password.OutcomeUnsupported, password.OutcomeCanceled, password.OutcomeResourceRejected, password.OutcomeFailed}
	if len(events) != len(wants) {
		t.Fatalf("events = %#v", events)
	}
	for index, want := range wants {
		if events[index].Outcome != want {
			t.Fatalf("event[%d] = %#v, want %q", index, events[index], want)
		}
	}

	panicking, err := password.New(testArgonPolicy(t), password.WithObserver(panicObserver{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := panicking.Verify(context.Background(), []byte("secret"), hash.String()); err != nil {
		t.Fatalf("observer panic changed result: %v", err)
	}
}

type zeroReader struct{}

func (*zeroReader) Read([]byte) (int, error) { return 0, nil }

type panicObserver struct{}

func (panicObserver) Observe(context.Context, password.Observation) { panic("observer failure") }
