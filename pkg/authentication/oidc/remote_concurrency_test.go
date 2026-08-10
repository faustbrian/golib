package oidc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"runtime"
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
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for len(set.waiters) != cap(set.waiters) {
		select {
		case <-deadline.C:
			t.Fatal("refresh waiter did not enter the synchronized wait")
		default:
			runtime.Gosched()
		}
	}
	cancel()
	if err := <-waiterDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("VerifySignature(canceled waiter) error = %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	<-done
}

func TestRemoteKeySetCancellationDoesNotPoisonSharedRefresh(t *testing.T) {
	t.Parallel()

	private := mustRSAKey(t)
	token := signCompact(t, private, "known", []byte(`{"sub":"user"}`))
	key := jose.JSONWebKey{Key: &private.PublicKey, KeyID: "known", Algorithm: "RS256", Use: "sig"}
	body, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{key}})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	started := make(chan struct{})
	var requests atomic.Int64
	set := testRemoteKeySet(t, authtest.NewClock(time.Unix(1, 0)), 2, roundTripperFunc(
		func(request *http.Request) (*http.Response, error) {
			if requests.Add(1) == 1 {
				close(started)
				<-request.Context().Done()
				return nil, request.Context().Err()
			}
			return jwkResponse(request, http.StatusOK, body, nil), nil
		},
	))

	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, verifyErr := set.VerifySignature(ctx, token)
		firstDone <- verifyErr
	}()
	<-started
	cancel()
	if verifyErr := <-firstDone; verifyErr == nil {
		t.Fatal("VerifySignature(canceled owner) error = nil")
	}

	payload, verifyErr := set.VerifySignature(context.Background(), token)
	if verifyErr != nil || string(payload) != `{"sub":"user"}` {
		t.Fatalf("VerifySignature(retry) = %q, %v", payload, verifyErr)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("refresh requests = %d, want 2", got)
	}
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

func TestRemoteKeySetRefreshesRotationMissBeforeCacheExpiry(t *testing.T) {
	t.Parallel()

	clock := authtest.NewClock(time.Unix(1, 0))
	first := mustRSAKey(t)
	second := mustRSAKey(t)
	encode := func(key jose.JSONWebKey) []byte {
		body, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{key}})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		return body
	}
	var requests atomic.Int64
	set := testRemoteKeySet(t, clock, 2, roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		key := jose.JSONWebKey{Key: &first.PublicKey, KeyID: "first", Algorithm: "RS256", Use: "sig"}
		if requests.Add(1) == 2 {
			key = jose.JSONWebKey{Key: &second.PublicKey, KeyID: "second", Algorithm: "RS256", Use: "sig"}
		}
		return jwkResponse(request, http.StatusOK, encode(key), http.Header{"Cache-Control": {"max-age=60"}}), nil
	}))
	firstToken := signCompact(t, first, "first", []byte(`{}`))
	secondToken := signCompact(t, second, "second", []byte(`{}`))
	if _, err := set.VerifySignature(context.Background(), firstToken); err != nil {
		t.Fatalf("VerifySignature(first) error = %v", err)
	}
	clock.Advance(2 * time.Second)
	if _, err := set.VerifySignature(context.Background(), secondToken); err != nil {
		t.Fatalf("VerifySignature(rotated before expiry) error = %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("rotation requests = %d, want 2", got)
	}
}

func TestRemoteKeySetRejectsRetiredKeyRollback(t *testing.T) {
	t.Parallel()

	clock := authtest.NewClock(time.Unix(1, 0))
	first := mustRSAKey(t)
	second := mustRSAKey(t)
	encode := func(key jose.JSONWebKey) []byte {
		body, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{key}})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		return body
	}
	sets := [][]byte{
		encode(jose.JSONWebKey{Key: &first.PublicKey, KeyID: "first", Algorithm: "RS256", Use: "sig"}),
		encode(jose.JSONWebKey{Key: &second.PublicKey, KeyID: "second", Algorithm: "RS256", Use: "sig"}),
		encode(jose.JSONWebKey{Key: &first.PublicKey, KeyID: "first", Algorithm: "RS256", Use: "sig"}),
	}
	var requests atomic.Int64
	set := testRemoteKeySet(t, clock, 2, roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		index := int(requests.Add(1)) - 1
		if index >= len(sets) {
			t.Fatalf("unexpected JWK request %d", index+1)
		}
		return jwkResponse(request, http.StatusOK, sets[index], http.Header{"Cache-Control": {"max-age=60"}}), nil
	}))
	firstToken := signCompact(t, first, "first", []byte(`{}`))
	secondToken := signCompact(t, second, "second", []byte(`{}`))
	if _, err := set.VerifySignature(context.Background(), firstToken); err != nil {
		t.Fatalf("VerifySignature(first) error = %v", err)
	}
	clock.Advance(2 * time.Second)
	if _, err := set.VerifySignature(context.Background(), secondToken); err != nil {
		t.Fatalf("VerifySignature(second) error = %v", err)
	}
	clock.Advance(2 * time.Second)
	if _, err := set.VerifySignature(context.Background(), firstToken); err == nil {
		t.Fatal("VerifySignature(retired rollback) error = nil")
	}
	if _, err := set.VerifySignature(context.Background(), secondToken); err != nil {
		t.Fatalf("VerifySignature(active after rollback) error = %v", err)
	}
}

func TestRemoteKeySetKeepsFreshKnownKeyAfterRotationProbeOutage(t *testing.T) {
	t.Parallel()

	clock := authtest.NewClock(time.Unix(1, 0))
	private := mustRSAKey(t)
	known := jose.JSONWebKey{Key: &private.PublicKey, KeyID: "known", Algorithm: "RS256", Use: "sig"}
	body, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{known}})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var requests atomic.Int64
	set := testRemoteKeySet(t, clock, 2, roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return jwkResponse(request, http.StatusOK, body, http.Header{"Cache-Control": {"max-age=60"}}), nil
		}
		return jwkResponse(request, http.StatusServiceUnavailable, nil, nil), nil
	}))
	knownToken := signCompact(t, private, "known", []byte(`{}`))
	unknownToken := signCompact(t, private, "unknown", []byte(`{}`))
	if _, err := set.VerifySignature(context.Background(), knownToken); err != nil {
		t.Fatalf("VerifySignature(known) error = %v", err)
	}
	clock.Advance(2 * time.Second)
	if _, err := set.VerifySignature(context.Background(), unknownToken); err == nil {
		t.Fatal("VerifySignature(rotation probe outage) error = nil")
	}
	if _, err := set.VerifySignature(context.Background(), unknownToken); err == nil {
		t.Fatal("VerifySignature(cached rotation probe outage) error = nil")
	}
	if _, err := set.VerifySignature(context.Background(), knownToken); err != nil {
		t.Fatalf("VerifySignature(fresh known after probe outage) error = %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("outage requests = %d, want 2", got)
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

func TestRemoteKeySetSynchronizesLargeRefreshBurst(t *testing.T) {
	t.Parallel()

	private := mustRSAKey(t)
	token := signCompact(t, private, "known", []byte(`{"sub":"user"}`))
	key := jose.JSONWebKey{Key: &private.PublicKey, KeyID: "known", Algorithm: "RS256", Use: "sig"}
	body, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{key}})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var requests atomic.Int64
	const callers = 256
	set := testRemoteKeySet(t, authtest.NewClock(time.Unix(1, 0)), callers, roundTripperFunc(
		func(request *http.Request) (*http.Response, error) {
			requests.Add(1)
			return jwkResponse(request, http.StatusOK, body, http.Header{"Cache-Control": {"max-age=3600"}}), nil
		},
	))
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			_, verifyErr := set.VerifySignature(context.Background(), token)
			results <- verifyErr
		}()
	}
	close(start)
	for range callers {
		if verifyErr := <-results; verifyErr != nil {
			t.Fatalf("VerifySignature() error = %v", verifyErr)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("refresh requests = %d, want 1", got)
	}
}

func TestRemoteRefreshJitterSpreadsReplicaFleet(t *testing.T) {
	t.Parallel()

	const replicas = 1024
	start := time.Unix(1, 0)
	clock := authtest.NewClock(start)
	private := mustRSAKey(t)
	key := jose.JSONWebKey{Key: &private.PublicKey, KeyID: "known", Algorithm: "RS256", Use: "sig"}
	body, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{key}})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	token := signCompact(t, private, "known", []byte(`{"sub":"user"}`))
	type barrier struct {
		expected int64
		arrived  atomic.Int64
		release  chan struct{}
	}
	var barrierMutex sync.RWMutex
	var activeBarrier *barrier
	var requests atomic.Int64
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		barrierMutex.RLock()
		current := activeBarrier
		barrierMutex.RUnlock()
		if current != nil {
			if current.arrived.Add(1) == current.expected {
				close(current.release)
			}
			<-current.release
		}
		return jwkResponse(request, http.StatusOK, body, http.Header{"Cache-Control": {"max-age=3600"}}), nil
	})
	generator := rand.New(rand.NewPCG(1, 2))
	buckets := make(map[int64][]*remoteKeySet)
	for replica := range replicas {
		set := testRemoteKeySet(t, clock, 1, transport)
		set.jitter = generator.Uint64()
		if err := set.initialize(context.Background()); err != nil {
			t.Fatalf("initialize(replica %d) error = %v", replica, err)
		}
		seconds := int64((set.nextRefresh.Sub(start) + time.Second - 1) / time.Second)
		buckets[seconds] = append(buckets[seconds], set)
	}
	if len(buckets) < 300 {
		t.Fatalf("refresh buckets = %d, want at least 300", len(buckets))
	}
	maximumConcentration := 0
	lastSecond := int64(0)
	for second := int64(54 * 60); second <= int64(time.Hour/time.Second); second++ {
		sets := buckets[second]
		if len(sets) == 0 {
			continue
		}
		clock.Advance(time.Duration(second-lastSecond) * time.Second)
		lastSecond = second
		maximumConcentration = max(maximumConcentration, len(sets))
		current := &barrier{expected: int64(len(sets)), release: make(chan struct{})}
		barrierMutex.Lock()
		activeBarrier = current
		barrierMutex.Unlock()
		results := make(chan error, len(sets))
		for _, set := range sets {
			go func() {
				_, verifyErr := set.VerifySignature(context.Background(), token)
				results <- verifyErr
			}()
		}
		for range sets {
			if verifyErr := <-results; verifyErr != nil {
				t.Fatalf("VerifySignature(fleet refresh) error = %v", verifyErr)
			}
		}
		barrierMutex.Lock()
		activeBarrier = nil
		barrierMutex.Unlock()
	}
	if maximumConcentration > 12 {
		t.Fatalf("maximum one-second refresh concentration = %d, want at most 12", maximumConcentration)
	}
	if got := requests.Load(); got != 2*replicas {
		t.Fatalf("fleet requests = %d, want %d", got, 2*replicas)
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
	if headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/jwk-set+json")
	}
	return &http.Response{
		StatusCode: status,
		Header:     headers,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    request,
	}
}
