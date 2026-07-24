package passwordauth_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
	"github.com/faustbrian/golib/pkg/password/passwordauth"
	"github.com/faustbrian/golib/pkg/password/passwordtest"
)

type lookupFunc func(context.Context, string) (passwordauth.Record, bool, error)

func (f lookupFunc) LookupPassword(ctx context.Context, username string) (passwordauth.Record, bool, error) {
	return f(ctx, username)
}

type nilLookup struct{}

func (*nilLookup) LookupPassword(context.Context, string) (passwordauth.Record, bool, error) {
	panic("typed-nil lookup invoked")
}

func services(t *testing.T) (*password.Service, string, string) {
	t.Helper()
	limits := password.DefaultPolicy().Limits()
	limits.MemoryKiB = 64
	limits.Argon2Time = 2
	limits.Concurrent = 2
	limits.Queue = 2
	argonPolicy, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Argon2id, Argon2id: password.Argon2idParameters{Version: 19, Time: 1, MemoryKiB: 8, Parallelism: 1, SaltLength: 8, OutputLength: 16}, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	argon, err := passwordtest.NewService(argonPolicy, []byte("synthetic deterministic entropy"))
	if err != nil {
		t.Fatal(err)
	}
	bcryptPolicy, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Bcrypt, BcryptCost: 4, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	bcryptService, err := password.New(bcryptPolicy)
	if err != nil {
		t.Fatal(err)
	}
	current, err := bcryptService.Hash(context.Background(), []byte("synthetic user password"))
	if err != nil {
		t.Fatal(err)
	}
	dummy, err := bcryptService.Hash(context.Background(), []byte("synthetic dummy password"))
	if err != nil {
		t.Fatal(err)
	}
	return argon, current.String(), dummy.String()
}

type hashStore struct {
	mu   sync.Mutex
	hash string
}

func (s *hashStore) lookup(context.Context, string) (passwordauth.Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return passwordauth.Record{Subject: "user-123", EncodedHash: s.hash}, true, nil
}

func (s *hashStore) compareAndSwap(expected, replacement string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hash != expected {
		return false
	}
	s.hash = replacement
	return true
}

func (s *hashStore) current() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hash
}

func TestAuthenticateReturnsExplicitCASUpgrade(t *testing.T) {
	service, current, dummy := services(t)
	lookup := lookupFunc(func(context.Context, string) (passwordauth.Record, bool, error) {
		return passwordauth.Record{Subject: "user-123", EncodedHash: current}, true, nil
	})
	authenticator, err := passwordauth.New(passwordauth.Config{Passwords: service, Lookup: lookup, DummyHash: dummy})
	if err != nil {
		t.Fatal(err)
	}
	result, err := authenticator.Authenticate(context.Background(), "synthetic-user", []byte("synthetic user password"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Subject() != "user-123" || !result.Upgrade().Required() || result.Upgrade().Expected().String() != current || result.Upgrade().Replacement().Algorithm() != password.Argon2id {
		t.Fatalf("result = %#v", result)
	}
}

func TestConcurrentUpgradeCrashAndCASStatesPreserveUsableHash(t *testing.T) {
	service, current, dummy := services(t)
	store := &hashStore{hash: current}
	authenticator, err := passwordauth.New(passwordauth.Config{Passwords: service, Lookup: lookupFunc(store.lookup), DummyHash: dummy})
	if err != nil {
		t.Fatal(err)
	}

	results := make(chan passwordauth.Result, 2)
	errorsChannel := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			result, err := authenticator.Authenticate(context.Background(), "synthetic-user", []byte("synthetic user password"))
			results <- result
			errorsChannel <- err
		})
	}
	group.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
	if store.current() != current {
		t.Fatal("verification or replacement hashing changed durable state")
	}

	upgrades := make([]passwordauth.Upgrade, 0, 2)
	for result := range results {
		if !result.Upgrade().Required() {
			t.Fatal("concurrent login did not produce an explicit upgrade")
		}
		upgrades = append(upgrades, result.Upgrade())
	}
	if !store.compareAndSwap(upgrades[0].Expected().String(), upgrades[0].Replacement().String()) {
		t.Fatal("first durable compare-and-swap did not commit")
	}
	if store.compareAndSwap(upgrades[1].Expected().String(), upgrades[1].Replacement().String()) {
		t.Fatal("stale concurrent compare-and-swap overwrote a newer hash")
	}
	if _, err := service.Verify(context.Background(), []byte("synthetic user password"), store.current()); err != nil {
		t.Fatalf("durable replacement is unusable after commit: %v", err)
	}
}

func TestAuthenticationFailuresAreClassifiedAndSecretSafe(t *testing.T) {
	service, current, dummy := services(t)
	tests := []struct {
		name   string
		lookup passwordauth.Lookup
		secret string
		want   error
	}{
		{"missing", lookupFunc(func(context.Context, string) (passwordauth.Record, bool, error) {
			return passwordauth.Record{}, false, nil
		}), "candidate", passwordauth.ErrRejected},
		{"missing dummy collision", lookupFunc(func(context.Context, string) (passwordauth.Record, bool, error) {
			return passwordauth.Record{}, false, nil
		}), "synthetic dummy password", passwordauth.ErrRejected},
		{"mismatch", lookupFunc(func(context.Context, string) (passwordauth.Record, bool, error) {
			return passwordauth.Record{Subject: "user", EncodedHash: current}, true, nil
		}), "wrong secret", passwordauth.ErrRejected},
		{"malformed stored", lookupFunc(func(context.Context, string) (passwordauth.Record, bool, error) {
			return passwordauth.Record{Subject: "user", EncodedHash: "broken"}, true, nil
		}), "candidate", passwordauth.ErrUnavailable},
		{"lookup failure", lookupFunc(func(context.Context, string) (passwordauth.Record, bool, error) {
			return passwordauth.Record{}, false, errors.New("database detail")
		}), "candidate", passwordauth.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authenticator, err := passwordauth.New(passwordauth.Config{Passwords: service, Lookup: tt.lookup, DummyHash: dummy})
			if err != nil {
				t.Fatal(err)
			}
			_, err = authenticator.Authenticate(context.Background(), "synthetic-user", []byte(tt.secret))
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v", err)
			}
			if strings.Contains(err.Error(), tt.secret) || strings.Contains(err.Error(), "database") {
				t.Fatalf("error leaked: %v", err)
			}
		})
	}
}

func TestMissingIdentityPreservesCancellation(t *testing.T) {
	service, _, dummy := services(t)
	ctx, cancel := context.WithCancel(context.Background())
	lookup := lookupFunc(func(context.Context, string) (passwordauth.Record, bool, error) {
		cancel()
		return passwordauth.Record{}, false, nil
	})
	authenticator, err := passwordauth.New(passwordauth.Config{Passwords: service, Lookup: lookup, DummyHash: dummy})
	if err != nil {
		t.Fatal(err)
	}
	_, err = authenticator.Authenticate(ctx, "missing", []byte("synthetic"))
	if !errors.Is(err, passwordauth.ErrCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled missing identity error = %v", err)
	}
}

func TestLookupCancellationIsClassified(t *testing.T) {
	service, _, dummy := services(t)
	lookup := lookupFunc(func(context.Context, string) (passwordauth.Record, bool, error) {
		return passwordauth.Record{}, false, context.DeadlineExceeded
	})
	authenticator, err := passwordauth.New(passwordauth.Config{Passwords: service, Lookup: lookup, DummyHash: dummy})
	if err != nil {
		t.Fatal(err)
	}
	_, err = authenticator.Authenticate(context.Background(), "missing", []byte("synthetic"))
	if !errors.Is(err, passwordauth.ErrCanceled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lookup cancellation error = %v", err)
	}
}

func TestAlreadyCanceledAuthenticationSkipsLookup(t *testing.T) {
	service, _, dummy := services(t)
	called := false
	lookup := lookupFunc(func(context.Context, string) (passwordauth.Record, bool, error) {
		called = true
		return passwordauth.Record{}, false, nil
	})
	authenticator, err := passwordauth.New(passwordauth.Config{Passwords: service, Lookup: lookup, DummyHash: dummy})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = authenticator.Authenticate(ctx, "missing", []byte("synthetic"))
	if !errors.Is(err, passwordauth.ErrCanceled) || !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled authentication error = %v, lookup called = %v", err, called)
	}
}

func TestConfigurationIsStrict(t *testing.T) {
	service, _, dummy := services(t)
	validLookup := lookupFunc(func(context.Context, string) (passwordauth.Record, bool, error) {
		return passwordauth.Record{}, false, nil
	})
	for _, config := range []passwordauth.Config{{}, {Passwords: service}, {Passwords: service, Lookup: validLookup}, {Passwords: service, Lookup: validLookup, DummyHash: "broken"}, {Passwords: service, Lookup: validLookup, DummyHash: dummy}} {
		_, err := passwordauth.New(config)
		if config.DummyHash == dummy {
			if err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, passwordauth.ErrInvalidConfig) {
			t.Fatalf("config=%#v error=%v", config, err)
		}
	}
	var lookup *nilLookup
	if _, err := passwordauth.New(passwordauth.Config{Passwords: service, Lookup: lookup, DummyHash: dummy}); !errors.Is(err, passwordauth.ErrInvalidConfig) {
		t.Fatalf("typed-nil lookup error = %v", err)
	}
}

func TestNoUpgradeCancellationInvalidRecordAndFormatting(t *testing.T) {
	service, _, dummy := services(t)
	current, err := service.Hash(context.Background(), []byte("current secret"))
	if err != nil {
		t.Fatal(err)
	}
	lookup := lookupFunc(func(context.Context, string) (passwordauth.Record, bool, error) {
		return passwordauth.Record{Subject: "user", EncodedHash: current.String()}, true, nil
	})
	authenticator, err := passwordauth.New(passwordauth.Config{Passwords: service, Lookup: lookup, DummyHash: dummy})
	if err != nil {
		t.Fatal(err)
	}
	result, err := authenticator.Authenticate(context.Background(), "user", []byte("current secret"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Upgrade().Required() || result.Upgrade().Expected().String() != "" || result.Upgrade().Replacement().String() != "" {
		t.Fatalf("unexpected upgrade: %#v", result.Upgrade())
	}
	if result.String() != "password authentication result" || result.GoString() != "passwordauth.Result{redacted}" || result.Upgrade().String() != "password upgrade [redacted]" || result.Upgrade().GoString() != "passwordauth.Upgrade{redacted}" {
		t.Fatal("unsafe result formatting")
	}
	record := passwordauth.Record{Subject: "user", EncodedHash: current.String()}
	if record.String() != "password record [redacted]" || record.GoString() != "passwordauth.Record{redacted}" || fmt.Sprintf("%v", record) != "password record [redacted]" {
		t.Fatal("unsafe record formatting")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := authenticator.Authenticate(ctx, "user", []byte("current secret")); !errors.Is(err, passwordauth.ErrCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
	if _, err := authenticator.Authenticate(context.Background(), "user", make([]byte, 1025)); !errors.Is(err, passwordauth.ErrUnavailable) || !errors.Is(err, password.ErrResourceRejected) {
		t.Fatalf("resource rejection: %v", err)
	}
	for _, record := range []passwordauth.Record{{EncodedHash: current.String()}, {Subject: "user"}} {
		invalid := lookupFunc(func(context.Context, string) (passwordauth.Record, bool, error) { return record, true, nil })
		authenticator, err := passwordauth.New(passwordauth.Config{Passwords: service, Lookup: invalid, DummyHash: dummy})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := authenticator.Authenticate(context.Background(), "user", []byte("candidate")); !errors.Is(err, passwordauth.ErrUnavailable) {
			t.Fatalf("invalid record: %v", err)
		}
	}
}

func TestErrorUnwrapOmitsNilCause(t *testing.T) {
	_, err := passwordauth.New(passwordauth.Config{})
	var classified *passwordauth.Error
	if !errors.As(err, &classified) {
		t.Fatalf("error = %v", err)
	}
	unwrapped := classified.Unwrap()
	if len(unwrapped) != 1 || !errors.Is(unwrapped[0], passwordauth.ErrInvalidConfig) || !errors.Is(classified.Kind(), passwordauth.ErrInvalidConfig) || classified.Cause() != nil {
		t.Fatalf("Unwrap = %#v", unwrapped)
	}
}

func TestErrorFormattingDoesNotExposeCause(t *testing.T) {
	service, _, dummy := services(t)
	lookup := lookupFunc(func(context.Context, string) (passwordauth.Record, bool, error) {
		return passwordauth.Record{}, false, errors.New("sensitive database detail")
	})
	authenticator, err := passwordauth.New(passwordauth.Config{Passwords: service, Lookup: lookup, DummyHash: dummy})
	if err != nil {
		t.Fatal(err)
	}
	_, err = authenticator.Authenticate(context.Background(), "synthetic-user", []byte("synthetic"))
	var classified *passwordauth.Error
	if !errors.As(err, &classified) || !errors.Is(classified.Kind(), passwordauth.ErrUnavailable) || classified.Cause() == nil {
		t.Fatalf("classified error = %v", err)
	}
	for _, format := range []string{"%s", "%q", "%v", "%+v", "%#v"} {
		if rendered := fmt.Sprintf(format, err); strings.Contains(rendered, "sensitive") || strings.Contains(rendered, "database") {
			t.Fatalf("format %s leaked cause: %s", format, rendered)
		}
	}
}
