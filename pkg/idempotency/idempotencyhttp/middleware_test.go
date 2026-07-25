package idempotencyhttp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencyhttp"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
)

func TestMiddlewareExecutesOnceAndReplaysResponse(t *testing.T) {
	middleware, _ := fixture(t, 1024)
	var calls atomic.Int64
	handler := middleware.Handler(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		ownership, found := idempotency.OwnershipFromContext(request.Context())
		if !found || ownership.FencingToken != 1 || ownership.OwnerToken == "" {
			t.Fatalf("handler ownership = %#v, %t", ownership, found)
		}
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Location", "/widgets/42")
		response.WriteHeader(http.StatusCreated)
		_, _ = response.Write([]byte(`{"id":42}`))
	}))

	first := perform(handler, "key-1", "payload-a")
	second := perform(handler, "key-1", "payload-a")

	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d", calls.Load())
	}
	if first.Code != http.StatusCreated || second.Code != first.Code || second.Body.String() != first.Body.String() {
		t.Fatalf("responses = (%d, %q), (%d, %q)", first.Code, first.Body, second.Code, second.Body)
	}
	if second.Header().Get("Idempotency-Replayed") != "true" ||
		second.Header().Get("Content-Type") != "application/json" ||
		second.Header().Get("Location") != "/widgets/42" {
		t.Fatalf("replay headers = %#v", second.Header())
	}
}

func TestMiddlewareReturnsExplicitProtocolOutcomes(t *testing.T) {
	middleware, store := fixture(t, 1024)
	handler := middleware.Handler(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	_ = perform(handler, "conflict-key", "payload-a")
	conflict := perform(handler, "conflict-key", "payload-b")
	if conflict.Code != http.StatusConflict || conflict.Header().Get("Idempotency-Outcome") != "conflict" {
		t.Fatalf("conflict = %d, %#v", conflict.Code, conflict.Header())
	}

	key := requestKey(t, "running-key")
	fingerprint := requestFingerprint(t, "payload-a")
	if _, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	}); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	inProgress := perform(handler, "running-key", "payload-a")
	if inProgress.Code != http.StatusConflict ||
		inProgress.Header().Get("Idempotency-Outcome") != "in_progress" {
		t.Fatalf("in progress = %d, %#v", inProgress.Code, inProgress.Header())
	}
}

func TestMiddlewareRequiresIdempotencyKey(t *testing.T) {
	middleware, _ := fixture(t, 1024)
	handler := middleware.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler executed")
	}))

	response := perform(handler, "", "payload")
	if response.Code != http.StatusBadRequest || response.Header().Get("Idempotency-Outcome") != "invalid_key" {
		t.Fatalf("response = %d, %#v", response.Code, response.Header())
	}
}

func TestMiddlewareBoundsHandlerResponseAndRecordsTerminalFailure(t *testing.T) {
	middleware, _ := fixture(t, 32)
	var calls atomic.Int64
	handler := middleware.Handler(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = response.Write([]byte(strings.Repeat("x", 33)))
	}))

	first := perform(handler, "large-key", "payload")
	second := perform(handler, "large-key", "payload")
	if calls.Load() != 1 || first.Code != http.StatusInternalServerError || second.Code != first.Code {
		t.Fatalf("calls = %d, statuses = %d, %d", calls.Load(), first.Code, second.Code)
	}
	if second.Header().Get("Idempotency-Replayed") != "true" ||
		second.Header().Get("Idempotency-Outcome") != "terminal_failure" {
		t.Fatalf("replay headers = %#v", second.Header())
	}
}

func TestNewValidatesConfiguration(t *testing.T) {
	service := serviceForStore(t, &storeOverride{Store: mustMemoryStore(t)})
	key := func(*http.Request, string) (idempotency.Key, error) {
		return requestKey(t, "key"), nil
	}
	fingerprint := func(*http.Request) (idempotency.Fingerprint, error) {
		return requestFingerprint(t, "payload"), nil
	}
	valid := idempotencyhttp.Options{
		Service: service, Lease: time.Minute, Key: key, Fingerprint: fingerprint,
	}
	tests := map[string]idempotencyhttp.Options{
		"service":             {Lease: time.Minute, Key: key, Fingerprint: fingerprint},
		"lease zero":          {Service: service, Key: key, Fingerprint: fingerprint},
		"lease too long":      {Service: service, Lease: idempotency.MaxLease + 1, Key: key, Fingerprint: fingerprint},
		"key":                 {Service: service, Lease: time.Minute, Fingerprint: fingerprint},
		"fingerprint":         {Service: service, Lease: time.Minute, Key: key},
		"negative limit":      withLimit(valid, -1),
		"oversized limit":     withLimit(valid, idempotencyhttp.MaxReplayResponseBytes+1),
		"empty replay header": withHeaders(valid, []string{" "}),
		"transition timeout":  withTransitionTimeout(valid, -1),
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := idempotencyhttp.New(options); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}

	valid.ReplayHeaders = []string{"content-type", "Content-Type"}
	if _, err := idempotencyhttp.New(valid); err != nil {
		t.Fatalf("New() defaults and duplicate headers error = %v", err)
	}
}

func TestMiddlewareRejectsKeyAndFingerprintFailures(t *testing.T) {
	tests := map[string]struct {
		key         idempotencyhttp.KeyFunc
		fingerprint idempotencyhttp.FingerprintFunc
		outcome     string
	}{
		"semantic key": {
			key: func(*http.Request, string) (idempotency.Key, error) {
				return idempotency.Key{}, &idempotency.Error{Reason: idempotency.ReasonLimitExceeded}
			},
			fingerprint: validFingerprint(t), outcome: "limit_exceeded",
		},
		"opaque key": {
			key: func(*http.Request, string) (idempotency.Key, error) {
				return idempotency.Key{}, errors.New("bad identity")
			},
			fingerprint: validFingerprint(t), outcome: "invalid_payload",
		},
		"fingerprint": {
			key: validKey(t),
			fingerprint: func(*http.Request) (idempotency.Fingerprint, error) {
				return idempotency.Fingerprint{}, errors.New("bad body")
			},
			outcome: "invalid_payload",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			middleware := middlewareForStore(t, mustMemoryStore(t), 1024, test.key, test.fingerprint)
			response := perform(middleware.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("handler executed")
			})), "key", "payload")
			if response.Code != http.StatusBadRequest ||
				response.Header().Get("Idempotency-Outcome") != test.outcome {
				t.Fatalf("response = %d, %#v", response.Code, response.Header())
			}
		})
	}
}

func TestMiddlewareFailsClosedAtStorageAndReplayBoundaries(t *testing.T) {
	backendErr := errors.New("backend unavailable")
	key := requestKey(t, "key")
	fingerprint := requestFingerprint(t, "payload")
	record := idempotency.Record{
		Key: key, Fingerprint: fingerprint, State: idempotency.StateAcquired,
		OwnerToken: "owner", FencingToken: 1,
	}
	tests := map[string]idempotency.Store{
		"acquire": &storeOverride{
			Store: mustMemoryStore(t),
			acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
				return idempotency.AcquireResult{}, backendErr
			},
		},
		"complete": &storeOverride{
			Store: mustMemoryStore(t),
			acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
				return idempotency.AcquireResult{Outcome: idempotency.OutcomeAcquired, Record: record}, nil
			},
			complete: func(context.Context, idempotency.CompleteRequest) (idempotency.Record, error) {
				return idempotency.Record{}, backendErr
			},
		},
		"malformed replay": &storeOverride{
			Store: mustMemoryStore(t),
			acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
				record.State = idempotency.StateCompleted
				record.Result = []byte("not-json")
				return idempotency.AcquireResult{Outcome: idempotency.OutcomeReplayed, Record: record}, nil
			},
		},
		"invalid replay": &storeOverride{
			Store: mustMemoryStore(t),
			acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
				record.State = idempotency.StateCompleted
				record.Result = []byte(`{"status":0}`)
				return idempotency.AcquireResult{Outcome: idempotency.OutcomeReplayed, Record: record}, nil
			},
		},
	}
	for name, store := range tests {
		t.Run(name, func(t *testing.T) {
			middleware := middlewareForStore(t, store, 1024, validKey(t), validFingerprint(t))
			response := perform(middleware.Handler(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				_, _ = response.Write([]byte("ok"))
			})), "key", "payload")
			if response.Code != http.StatusServiceUnavailable ||
				response.Header().Get("Idempotency-Outcome") != "unavailable" {
				t.Fatalf("response = %d, %#v", response.Code, response.Header())
			}
		})
	}
}

func TestMiddlewareFailsClosedWhenTerminalFailureCannotPersist(t *testing.T) {
	store := &storeOverride{Store: mustMemoryStore(t)}
	store.acquire = func(_ context.Context, request idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
		return idempotency.AcquireResult{Outcome: idempotency.OutcomeAcquired, Record: idempotency.Record{
			Key: request.Key, Fingerprint: request.Fingerprint, State: idempotency.StateAcquired,
			OwnerToken: "owner", FencingToken: 1,
		}}, nil
	}
	store.fail = func(context.Context, idempotency.FailRequest) (idempotency.Record, error) {
		return idempotency.Record{}, errors.New("write failed")
	}
	middleware := middlewareForStore(t, store, 1, validKey(t), validFingerprint(t))
	response := perform(middleware.Handler(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("xx"))
	})), "key", "payload")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("response = %d", response.Code)
	}
}

func TestMiddlewareReleasesOwnershipWhenHandlerPanics(t *testing.T) {
	store := mustMemoryStore(t)
	middleware := middlewareForStore(t, store, 1024, validKey(t), validFingerprint(t))
	ctx, cancel := context.WithCancel(context.Background())
	handler := middleware.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		cancel()
		panic("handler panic")
	}))
	defer func() {
		if recover() != "handler panic" {
			t.Fatal("panic was not propagated")
		}
		record, err := store.Inspect(context.Background(), requestKey(t, "panic-key"))
		if err != nil || record.State != idempotency.StateAbandoned {
			t.Fatalf("Inspect() = %#v, %v", record, err)
		}
	}()
	request := httptest.NewRequest(http.MethodPost, "/widgets", nil).WithContext(ctx)
	request.Header.Set("Idempotency-Key", "panic-key")
	request.Header.Set("X-Payload", "payload")
	handler.ServeHTTP(httptest.NewRecorder(), request)
}

func TestMiddlewareHandlesEmptyAndOversizedEncodedResponses(t *testing.T) {
	middleware, _ := fixture(t, 1024)
	empty := perform(middleware.Handler(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusAccepted)
		response.WriteHeader(http.StatusCreated)
	})), "empty-key", "payload")
	if empty.Code != http.StatusAccepted || empty.Body.Len() != 0 {
		t.Fatalf("empty response = %d, %q", empty.Code, empty.Body)
	}

	largeHeader := strings.Repeat("x", idempotency.MaxResultBytes)
	large := perform(middleware.Handler(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Location", largeHeader)
	})), "header-key", "payload")
	if large.Code != http.StatusInternalServerError {
		t.Fatalf("large header response = %d", large.Code)
	}
}

func fixture(t *testing.T, limit int) (*idempotencyhttp.Middleware, *memory.Store) {
	t.Helper()
	store, err := memory.New(memory.Options{
		Clock:       idempotencytest.NewClock(time.Unix(1_700_000_000, 0).UTC()),
		OwnerTokens: idempotencytest.NewTokenSource("http-owner").Next,
	})
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}
	service, err := idempotency.NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	middleware, err := idempotencyhttp.New(idempotencyhttp.Options{
		Service:          service,
		Lease:            time.Minute,
		MaxResponseBytes: limit,
		ReplayHeaders:    []string{"Content-Type", "Location"},
		Key: func(_ *http.Request, value string) (idempotency.Key, error) {
			return requestKey(t, value), nil
		},
		Fingerprint: func(request *http.Request) (idempotency.Fingerprint, error) {
			return requestFingerprint(t, request.Header.Get("X-Payload")), nil
		},
	})
	if err != nil {
		t.Fatalf("idempotencyhttp.New() error = %v", err)
	}
	return middleware, store
}

func perform(handler http.Handler, key, payload string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/widgets", nil)
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	request.Header.Set("X-Payload", payload)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func requestKey(t *testing.T, value string) idempotency.Key {
	t.Helper()
	key, err := idempotency.NewKey("http", "tenant", "POST /widgets", "caller", value)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	return key
}

func requestFingerprint(t *testing.T, payload string) idempotency.Fingerprint {
	t.Helper()
	fingerprint, err := idempotency.NewFingerprint("v1", []byte(payload))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	return fingerprint
}

func mustMemoryStore(t *testing.T) *memory.Store {
	t.Helper()
	store, err := memory.New(memory.Options{
		Clock:       idempotencytest.NewClock(time.Unix(1_700_000_000, 0).UTC()),
		OwnerTokens: idempotencytest.NewTokenSource("http-owner").Next,
	})
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}
	return store
}

func serviceForStore(t *testing.T, store idempotency.Store) *idempotency.Service {
	t.Helper()
	service, err := idempotency.NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func middlewareForStore(
	t *testing.T,
	store idempotency.Store,
	limit int,
	key idempotencyhttp.KeyFunc,
	fingerprint idempotencyhttp.FingerprintFunc,
) *idempotencyhttp.Middleware {
	t.Helper()
	middleware, err := idempotencyhttp.New(idempotencyhttp.Options{
		Service: serviceForStore(t, store), Lease: time.Minute,
		MaxResponseBytes: limit, ReplayHeaders: []string{"Content-Type", "Location"},
		Key: key, Fingerprint: fingerprint,
	})
	if err != nil {
		t.Fatalf("idempotencyhttp.New() error = %v", err)
	}
	return middleware
}

func validKey(t *testing.T) idempotencyhttp.KeyFunc {
	t.Helper()
	return func(_ *http.Request, value string) (idempotency.Key, error) {
		return requestKey(t, value), nil
	}
}

func validFingerprint(t *testing.T) idempotencyhttp.FingerprintFunc {
	t.Helper()
	return func(request *http.Request) (idempotency.Fingerprint, error) {
		return requestFingerprint(t, request.Header.Get("X-Payload")), nil
	}
}

func withLimit(options idempotencyhttp.Options, limit int) idempotencyhttp.Options {
	options.MaxResponseBytes = limit
	return options
}

func withHeaders(options idempotencyhttp.Options, headers []string) idempotencyhttp.Options {
	options.ReplayHeaders = headers
	return options
}

func withTransitionTimeout(
	options idempotencyhttp.Options,
	timeout time.Duration,
) idempotencyhttp.Options {
	options.TransitionTimeout = timeout
	return options
}

type storeOverride struct {
	idempotency.Store
	acquire  func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error)
	complete func(context.Context, idempotency.CompleteRequest) (idempotency.Record, error)
	fail     func(context.Context, idempotency.FailRequest) (idempotency.Record, error)
}

func (s *storeOverride) Acquire(
	ctx context.Context,
	request idempotency.AcquireRequest,
) (idempotency.AcquireResult, error) {
	if s.acquire != nil {
		return s.acquire(ctx, request)
	}
	return s.Store.Acquire(ctx, request)
}

func (s *storeOverride) Complete(
	ctx context.Context,
	request idempotency.CompleteRequest,
) (idempotency.Record, error) {
	if s.complete != nil {
		return s.complete(ctx, request)
	}
	return s.Store.Complete(ctx, request)
}

func (s *storeOverride) Fail(
	ctx context.Context,
	request idempotency.FailRequest,
) (idempotency.Record, error) {
	if s.fail != nil {
		return s.fail(ctx, request)
	}
	return s.Store.Fail(ctx, request)
}
