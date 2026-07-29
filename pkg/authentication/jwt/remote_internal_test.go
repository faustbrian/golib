package jwt

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

func TestConcurrentCloseStateHonorsCompletionAndCancellation(t *testing.T) {
	t.Parallel()

	completed := &Remote{closing: true, closeDone: make(chan struct{})}
	completed.closeErr = errors.New("shutdown failed")
	close(completed.closeDone)
	if err := completed.Close(context.Background()); err != completed.closeErr {
		t.Fatalf("Close(completed) error = %v", err)
	}

	waiting := &Remote{closing: true, closeDone: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waiting.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close(canceled waiter) error = %v", err)
	}

	closed := &Remote{closed: true}
	if err := closed.closeResult(); err != nil {
		t.Fatalf("closeResult(closed) error = %v", err)
	}
	failed := &Remote{closeErr: context.DeadlineExceeded}
	if err := failed.closeResult(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("closeResult(failed) error = %v", err)
	}

	busy := &Remote{operations: map[uint64]context.CancelFunc{1: func() {}}}
	deadline, deadlineCancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer deadlineCancel()
	<-deadline.Done()
	if err := busy.Close(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close(active operation deadline) error = %v", err)
	}
}

func TestKeySetClassifiesUnregisteredCacheLookup(t *testing.T) {
	t.Parallel()

	cache, err := jwk.NewCache(context.Background(), httprc.NewClient())
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	t.Cleanup(func() { _ = cache.Shutdown(context.Background()) })
	remote := &Remote{cache: cache, url: "https://issuer.example.test/unregistered"}
	if _, err := remote.KeySet(context.Background()); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("KeySet(unregistered) error = %v", err)
	}
}

func TestRemoteConfigurationRejectsEachUnsafeBoundary(t *testing.T) {
	t.Parallel()
	if _, err := NewRemote(context.Background(), "://"); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewRemote(invalid URL) error = %v", err)
	}

	parsed, err := url.Parse("https://issuer.example.test/keys")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	base := remoteConfig{
		client: http.DefaultClient, minRefresh: time.Nanosecond,
		maxRefresh: time.Nanosecond, maxBodyBytes: 1, initTimeout: time.Nanosecond,
	}
	if !validRemoteConfiguration(parsed, base) {
		t.Fatal("validRemoteConfiguration(exact bounds) = false")
	}
	tests := map[string]func(*url.URL, *remoteConfig){
		"host":          func(u *url.URL, _ *remoteConfig) { u.Host = "" },
		"user":          func(u *url.URL, _ *remoteConfig) { u.User = url.User("user") },
		"fragment":      func(u *url.URL, _ *remoteConfig) { u.Fragment = "fragment" },
		"scheme":        func(u *url.URL, _ *remoteConfig) { u.Scheme = "ftp" },
		"client":        func(_ *url.URL, c *remoteConfig) { c.client = nil },
		"minimum":       func(_ *url.URL, c *remoteConfig) { c.minRefresh = -1 },
		"refresh order": func(_ *url.URL, c *remoteConfig) { c.minRefresh = 2; c.maxRefresh = 1 },
		"body":          func(_ *url.URL, c *remoteConfig) { c.maxBodyBytes = -1 },
		"timeout":       func(_ *url.URL, c *remoteConfig) { c.initTimeout = -1 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidateURL := *parsed
			candidateConfig := base
			mutate(&candidateURL, &candidateConfig)
			if validRemoteConfiguration(&candidateURL, candidateConfig) {
				t.Fatal("validRemoteConfiguration() = true")
			}
		})
	}
	httpURL := *parsed
	httpURL.Scheme = "http"
	base.allowHTTP = true
	if !validRemoteConfiguration(&httpURL, base) {
		t.Fatal("validRemoteConfiguration(HTTP opt-in) = false")
	}
}

func TestRemoteOperationStateRejectsClosingAndIgnoresUnknownCompletion(t *testing.T) {
	t.Parallel()

	remote := &Remote{closing: true}
	if _, _, _, err := remote.beginOperation(context.Background()); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("beginOperation(closing) error = %v", err)
	}
	remote = &Remote{operations: make(map[uint64]context.CancelFunc)}
	remote.endOperation(99)

	idle := make(chan struct{})
	remote = &Remote{
		closing:    true,
		idle:       idle,
		operations: map[uint64]context.CancelFunc{1: func() {}},
	}
	remote.endOperation(1)
	select {
	case <-idle:
	default:
		t.Fatal("endOperation(last operation) did not signal idle")
	}
}
