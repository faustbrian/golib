package oidc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/authentication/authtest"
	jose "github.com/go-jose/go-jose/v4"
)

func TestRemoteKeySetRefreshWaitHonorsCancellation(t *testing.T) {
	t.Parallel()

	private := mustRSAKey(t)
	token := signCompact(t, private, "unknown", []byte(`{"sub":"user"}`))
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	set := testRemoteKeySet(t, authtest.NewClock(time.Unix(1, 0)), 2, roundTripperFunc(
		func(request *http.Request) (*http.Response, error) {
			close(started)
			select {
			case <-release:
				return jwkResponse(request, http.StatusServiceUnavailable, nil, nil), nil
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
		},
	))

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = set.VerifySignature(context.Background(), token)
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	go func() {
		_, err := set.VerifySignature(ctx, token)
		waiterDone <- err
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	if err := <-waiterDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("VerifySignature(canceled waiter) error = %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	<-done
}

func TestRemoteKeySetBoundsRefreshWaiters(t *testing.T) {
	t.Parallel()

	private := mustRSAKey(t)
	token := signCompact(t, private, "unknown", []byte(`{"sub":"user"}`))
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	set := testRemoteKeySet(t, authtest.NewClock(time.Unix(1, 0)), 1, roundTripperFunc(
		func(request *http.Request) (*http.Response, error) {
			close(started)
			<-release
			return jwkResponse(request, http.StatusServiceUnavailable, nil, nil), nil
		},
	))

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = set.VerifySignature(context.Background(), token)
	}()
	<-started

	if _, err := set.VerifySignature(context.Background(), token); !errors.Is(err, errOIDCRefreshBusy) {
		t.Fatalf("VerifySignature(excess waiter) error = %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	<-done
}

func TestRemoteKeySetLimitsRefreshAndRevalidatesHTTPResponse(t *testing.T) {
	t.Parallel()

	clock := authtest.NewClock(time.Unix(1, 0))
	private := mustRSAKey(t)
	known := jose.JSONWebKey{Key: &private.PublicKey, KeyID: "known", Algorithm: "RS256", Use: "sig"}
	body, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{known}})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var requests atomic.Int64
	client := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		switch requests.Add(1) {
		case 1:
			return jwkResponse(request, http.StatusOK, body, http.Header{
				"Cache-Control": {"max-age=60"},
				"Etag":          {`"keys-v1"`},
				"Last-Modified": {"Wed, 15 Jul 2026 12:00:00 GMT"},
			}), nil
		case 2:
			if got := request.Header.Get("If-None-Match"); got != `"keys-v1"` {
				t.Errorf("If-None-Match = %q", got)
			}
			if got := request.Header.Get("If-Modified-Since"); got != "Wed, 15 Jul 2026 12:00:00 GMT" {
				t.Errorf("If-Modified-Since = %q", got)
			}
			return jwkResponse(request, http.StatusNotModified, nil, http.Header{
				"Cache-Control": {"max-age=60"},
			}), nil
		default:
			t.Fatalf("unexpected JWK request %d", requests.Load())
			return nil, errors.New("unexpected JWK request")
		}
	})
	set := testRemoteKeySet(t, clock, 2, client)
	knownToken := signCompact(t, private, "known", []byte(`{"sub":"user"}`))
	unknownToken := signCompact(t, private, "unknown", []byte(`{"sub":"user"}`))

	if _, err := set.VerifySignature(context.Background(), knownToken); err != nil {
		t.Fatalf("VerifySignature(initial) error = %v", err)
	}
	if _, err := set.VerifySignature(context.Background(), unknownToken); err == nil {
		t.Fatal("VerifySignature(fresh unknown key) error = nil")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("fresh-cache requests = %d, want 1", got)
	}

	clock.Advance(61 * time.Second)
	if _, err := set.VerifySignature(context.Background(), unknownToken); err == nil {
		t.Fatal("VerifySignature(revalidated unknown key) error = nil")
	}
	if _, err := set.VerifySignature(context.Background(), knownToken); err != nil {
		t.Fatalf("VerifySignature(retained key) error = %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("revalidated-cache requests = %d, want 2", got)
	}
}

func TestRemoteKeySetCachesRefreshFailureWithinRateLimit(t *testing.T) {
	t.Parallel()

	private := mustRSAKey(t)
	token := signCompact(t, private, "unknown", []byte(`{"sub":"user"}`))
	var requests atomic.Int64
	set := testRemoteKeySet(t, authtest.NewClock(time.Unix(1, 0)), 2, roundTripperFunc(
		func(request *http.Request) (*http.Response, error) {
			requests.Add(1)
			return jwkResponse(request, http.StatusServiceUnavailable, nil, nil), nil
		},
	))
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := set.VerifySignature(context.Background(), token); err == nil {
			t.Fatalf("VerifySignature(attempt %d) error = nil", attempt)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("rate-limited failure requests = %d, want 1", got)
	}
}

func testRemoteKeySet(t *testing.T, clock Clock, maxWaiters int, transport http.RoundTripper) *remoteKeySet {
	t.Helper()
	return &remoteKeySet{
		url:                "https://issuer.example.test/keys",
		client:             &http.Client{Transport: transport},
		algorithms:         []jose.SignatureAlgorithm{jose.RS256},
		allowed:            map[string]struct{}{"RS256": {}},
		maxBodyBytes:       1 << 20,
		maxKeys:            8,
		clock:              clock,
		minRefreshInterval: time.Second,
		maxRefreshInterval: time.Hour,
		waiters:            make(chan struct{}, maxWaiters),
	}
}

func jwkResponse(request *http.Request, status int, body []byte, headers http.Header) *http.Response {
	if headers == nil {
		headers = make(http.Header)
	}
	return &http.Response{
		StatusCode: status,
		Header:     headers,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    request,
	}
}
