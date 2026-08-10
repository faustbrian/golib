package oidc

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/authentication/authtest"
	jose "github.com/go-jose/go-jose/v4"
)

func TestRemoteVerificationHonorsExactFreshnessBoundaries(t *testing.T) {
	t.Parallel()

	t.Run("exact expiry refreshes", func(t *testing.T) {
		private := mustRSAKey(t)
		key := jose.JSONWebKey{Key: &private.PublicKey, KeyID: "known", Algorithm: "RS256", Use: "sig"}
		body, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{key}})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		clock := authtest.NewClock(time.Unix(1, 0))
		var requests atomic.Int64
		set := testRemoteKeySet(t, clock, 1, roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			requests.Add(1)
			return jwkResponse(request, http.StatusOK, body, nil), nil
		}))
		set.keys = []jose.JSONWebKey{key}
		set.nextRefresh = clock.Now()
		token := signCompact(t, private, "known", []byte(`{"sub":"user"}`))
		if payload, verifyErr := set.VerifySignature(context.Background(), token); verifyErr != nil || string(payload) != `{"sub":"user"}` {
			t.Fatalf("VerifySignature(exact expiry) = %q, %v", payload, verifyErr)
		}
		if got := requests.Load(); got != 1 {
			t.Fatalf("refresh requests = %d, want 1", got)
		}
	})

	t.Run("zero expiry after shared refresh is fresh", func(t *testing.T) {
		private := mustRSAKey(t)
		key := jose.JSONWebKey{Key: &private.PublicKey, KeyID: "known", Algorithm: "RS256", Use: "sig"}
		token := signCompact(t, private, "known", []byte(`{"sub":"user"}`))
		done := make(chan struct{})
		var requests atomic.Int64
		set := testRemoteKeySet(t, authtest.NewClock(time.Unix(1, 0)), 1, roundTripperFunc(
			func(*http.Request) (*http.Response, error) {
				requests.Add(1)
				return nil, errors.New("unexpected refresh")
			},
		))
		set.refreshing = true
		set.refreshDone = done
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		result := make(chan error, 1)
		go func() {
			payload, verifyErr := set.VerifySignature(ctx, token)
			if verifyErr == nil && string(payload) != `{"sub":"user"}` {
				verifyErr = errors.New("unexpected payload")
			}
			result <- verifyErr
		}()
		deadline := time.NewTimer(time.Second)
		defer deadline.Stop()
		for len(set.waiters) != 1 {
			select {
			case <-deadline.C:
				t.Fatal("verification did not wait for the shared refresh")
			default:
				time.Sleep(time.Millisecond)
			}
		}
		set.mutex.Lock()
		set.keys = []jose.JSONWebKey{key}
		set.nextRefresh = time.Time{}
		set.refreshing = false
		close(done)
		set.mutex.Unlock()
		if verifyErr := <-result; verifyErr != nil {
			t.Fatalf("VerifySignature(shared refresh) error = %v", verifyErr)
		}
		if got := requests.Load(); got != 0 {
			t.Fatalf("refresh requests = %d, want 0", got)
		}
	})

	t.Run("shared refresh failure keeps the unavailable error", func(t *testing.T) {
		private := mustRSAKey(t)
		token := signCompact(t, private, "unknown", []byte(`{"sub":"user"}`))
		done := make(chan struct{})
		clock := authtest.NewClock(time.Unix(1, 0))
		set := testRemoteKeySet(t, clock, 1, roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("unexpected refresh")
		}))
		set.refreshing = true
		set.refreshDone = done
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		result := make(chan error, 1)
		go func() {
			_, verifyErr := set.VerifySignature(ctx, token)
			result <- verifyErr
		}()
		deadline := time.NewTimer(time.Second)
		defer deadline.Stop()
		for len(set.waiters) != 1 {
			select {
			case <-deadline.C:
				t.Fatal("verification did not wait for the shared refresh")
			default:
				time.Sleep(time.Millisecond)
			}
		}
		set.mutex.Lock()
		set.keys = []jose.JSONWebKey{{Key: &private.PublicKey, KeyID: "known", Algorithm: "RS256", Use: "sig"}}
		set.nextRefresh = clock.Now().Add(time.Minute)
		set.nextAttempt = clock.Now().Add(time.Minute)
		set.refreshErr = errors.New("provider unavailable")
		set.refreshing = false
		close(done)
		set.mutex.Unlock()
		if verifyErr := <-result; verifyErr == nil || verifyErr.Error() != "OIDC keys unavailable" {
			t.Fatalf("VerifySignature(shared refresh failure) error = %v", verifyErr)
		}
	})

	t.Run("future expiry serves the cached key", func(t *testing.T) {
		private := mustRSAKey(t)
		key := jose.JSONWebKey{Key: &private.PublicKey, KeyID: "known", Algorithm: "RS256", Use: "sig"}
		token := signCompact(t, private, "known", []byte(`{"sub":"user"}`))
		clock := authtest.NewClock(time.Unix(1, 0))
		var requests atomic.Int64
		set := testRemoteKeySet(t, clock, 1, roundTripperFunc(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return nil, errors.New("unexpected refresh")
		}))
		set.keys = []jose.JSONWebKey{key}
		set.nextRefresh = clock.Now().Add(time.Minute)
		set.waiters <- struct{}{}
		defer func() { <-set.waiters }()
		if payload, verifyErr := set.VerifySignature(context.Background(), token); verifyErr != nil || string(payload) != `{"sub":"user"}` {
			t.Fatalf("VerifySignature(future expiry) = %q, %v", payload, verifyErr)
		}
		if got := requests.Load(); got != 0 {
			t.Fatalf("refresh requests = %d, want 0", got)
		}
	})

	t.Run("empty zero-expiry cache is unavailable during backoff", func(t *testing.T) {
		private := mustRSAKey(t)
		token := signCompact(t, private, "unknown", []byte(`{"sub":"user"}`))
		clock := authtest.NewClock(time.Unix(1, 0))
		set := testRemoteKeySet(t, clock, 1, roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("unexpected refresh")
		}))
		set.nextAttempt = clock.Now().Add(time.Minute)
		_, verifyErr := set.VerifySignature(context.Background(), token)
		if verifyErr == nil || verifyErr.Error() != "OIDC keys unavailable" {
			t.Fatalf("VerifySignature(empty backoff) error = %v", verifyErr)
		}
	})
}

func TestRemoteRefreshBookkeepingExactBoundaries(t *testing.T) {
	t.Parallel()

	t.Run("nil waiter release returns", func(t *testing.T) {
		done := make(chan struct{})
		go func() {
			(&remoteKeySet{}).releaseWaiter()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(250 * time.Millisecond):
			t.Fatal("releaseWaiter(nil) blocked")
		}
	})

	t.Run("empty refreshed URL preserves endpoint", func(t *testing.T) {
		private := mustRSAKey(t)
		set := &remoteKeySet{
			url: "https://issuer.example.test/original", refreshing: true, refreshDone: make(chan struct{}),
			minRefreshInterval: time.Second, maxRefreshInterval: time.Minute,
		}
		set.finishRefresh(time.Unix(1, 0), fetchResult{
			keys:   []jose.JSONWebKey{{Key: &private.PublicKey, KeyID: "known", Algorithm: "RS256", Use: "sig"}},
			header: http.Header{"Cache-Control": {"max-age=10"}},
		}, nil)
		if set.url != "https://issuer.example.test/original" {
			t.Fatalf("refreshed JWK URL = %q", set.url)
		}
	})

	t.Run("transport failure is not replaced by rollback validation", func(t *testing.T) {
		private := mustRSAKey(t)
		key := jose.JSONWebKey{Key: &private.PublicKey, KeyID: "known", Algorithm: "RS256", Use: "sig"}
		fingerprint, err := key.Thumbprint(crypto.SHA256)
		if err != nil {
			t.Fatalf("Thumbprint() error = %v", err)
		}
		providerErr := errors.New("provider unavailable")
		set := &remoteKeySet{
			refreshing: true, refreshDone: make(chan struct{}), retiredKeys: map[string]struct{}{string(fingerprint): {}},
			minRefreshInterval: time.Second, maxRefreshInterval: time.Minute,
		}
		set.finishRefresh(time.Unix(1, 0), fetchResult{keys: []jose.JSONWebKey{key}}, providerErr)
		if !errors.Is(set.refreshErr, providerErr) {
			t.Fatalf("refresh error = %v", set.refreshErr)
		}
	})
}

func TestRemoteHeaderAndClientExactBounds(t *testing.T) {
	t.Parallel()

	if got := responseHeaderBytes(http.Header{"X": {"y"}}); got != 6 {
		t.Fatalf("responseHeaderBytes(small) = %d, want 6", got)
	}
	if got := responseHeaderBytes(http.Header{"X": {"y"}, "Long": {"z"}}); got != 15 {
		t.Fatalf("responseHeaderBytes(multiple names) = %d, want 15", got)
	}
	valueAtLimit := strings.Repeat("x", maximumHTTPHeaderBytes-len("X")-4)
	exact := http.Header{"X": {valueAtLimit}}
	if got := responseHeaderBytes(exact); got != maximumHTTPHeaderBytes {
		t.Fatalf("responseHeaderBytes(exact) = %d, want %d", got, maximumHTTPHeaderBytes)
	}
	over := http.Header{"X": {valueAtLimit, "z"}}
	if got := responseHeaderBytes(over); got <= maximumHTTPHeaderBytes {
		t.Fatalf("responseHeaderBytes(over) = %d", got)
	}
	transport := boundedTransport{base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotModified, Header: exact,
			Body: io.NopCloser(bytes.NewReader(nil)), Request: request,
		}, nil
	}), maximum: 1}
	response, err := transport.RoundTrip(&http.Request{})
	if err != nil {
		t.Fatalf("RoundTrip(exact headers) error = %v", err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("Close(exact headers) error = %v", closeErr)
	}
}

func TestIssuerURLRequiresBothHTTPOptInAndLoopback(t *testing.T) {
	t.Parallel()

	loopback := &url.URL{Scheme: "http", Host: "127.0.0.1", Path: "/issuer"}
	if validIssuerURL(loopback, false) {
		t.Fatal("validIssuerURL(loopback without opt-in) = true")
	}
	nonLoopback := &url.URL{Scheme: "http", Host: "192.0.2.1", Path: "/issuer"}
	if validIssuerURL(nonLoopback, true) {
		t.Fatal("validIssuerURL(non-loopback with opt-in) = true")
	}
}
