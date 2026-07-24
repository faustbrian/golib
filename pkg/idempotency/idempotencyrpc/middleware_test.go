package idempotencyrpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencyrpc"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
)

func TestMiddlewareExecutesOnceAndReplaysResult(t *testing.T) {
	middleware, _ := fixture(t, 1024)
	request := idempotencyrpc.Request{
		Method: "widgets.create", Params: json.RawMessage(`{"name":"one"}`),
	}
	var calls atomic.Int64
	handler := func(ctx context.Context, _ idempotencyrpc.Request) idempotencyrpc.Response {
		calls.Add(1)
		ownership, found := idempotency.OwnershipFromContext(ctx)
		if !found || ownership.FencingToken != 1 || ownership.OwnerToken == "" {
			t.Fatalf("handler ownership = %#v, %t", ownership, found)
		}
		return idempotencyrpc.Response{Result: json.RawMessage(`{"id":42}`)}
	}

	first, err := middleware.Call(context.Background(), request, handler)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	second, err := middleware.Call(context.Background(), request, handler)
	if err != nil {
		t.Fatalf("Call() replay error = %v", err)
	}
	if calls.Load() != 1 || first.Outcome != idempotency.OutcomeAcquired ||
		second.Outcome != idempotency.OutcomeReplayed || !second.Replayed ||
		string(second.Response.Result) != `{"id":42}` {
		t.Fatalf("calls = %d, results = %#v, %#v", calls.Load(), first, second)
	}
}

func TestMiddlewareReplaysJSONRPCError(t *testing.T) {
	middleware, _ := fixture(t, 1024)
	request := idempotencyrpc.Request{
		Method: "widgets.create", Params: json.RawMessage(`{"name":"bad"}`),
	}
	var calls atomic.Int64
	handler := func(context.Context, idempotencyrpc.Request) idempotencyrpc.Response {
		calls.Add(1)
		return idempotencyrpc.Response{Error: &idempotencyrpc.Error{
			Code: -32602, Message: "invalid params", Data: json.RawMessage(`{"field":"name"}`),
		}}
	}
	_, err := middleware.Call(context.Background(), request, handler)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	replayed, err := middleware.Call(context.Background(), request, handler)
	if err != nil {
		t.Fatalf("Call() replay error = %v", err)
	}
	if calls.Load() != 1 || replayed.Response.Error == nil ||
		replayed.Response.Error.Code != -32602 ||
		string(replayed.Response.Error.Data) != `{"field":"name"}` {
		t.Fatalf("calls = %d, replay = %#v", calls.Load(), replayed)
	}
}

func TestMiddlewareReturnsConflictAndInProgressWithoutExecuting(t *testing.T) {
	middleware, store := fixture(t, 1024)
	request := idempotencyrpc.Request{Method: "widgets.create", Params: json.RawMessage(`{"n":1}`)}
	handler := func(context.Context, idempotencyrpc.Request) idempotencyrpc.Response {
		return idempotencyrpc.Response{Result: json.RawMessage(`true`)}
	}
	if _, err := middleware.Call(context.Background(), request, handler); err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	conflict, err := middleware.Call(context.Background(), idempotencyrpc.Request{
		Method: "widgets.create", Params: json.RawMessage(`{"n":2}`),
	}, handler)
	if err != nil || conflict.Outcome != idempotency.OutcomeConflict {
		t.Fatalf("conflict = %#v, %v", conflict, err)
	}

	running := idempotencyrpc.Request{Method: "widgets.update", Params: json.RawMessage(`{"n":1}`)}
	key, fingerprint := identity(t, running)
	if _, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	}); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	inProgress, err := middleware.Call(context.Background(), running, handler)
	if err != nil || inProgress.Outcome != idempotency.OutcomeInProgress {
		t.Fatalf("in progress = %#v, %v", inProgress, err)
	}
}

func TestMiddlewareRecordsInvalidOrOversizedHandlerResponseAsTerminal(t *testing.T) {
	tests := map[string]idempotencyrpc.Response{
		"empty": {},
		"both result and error": {
			Result: json.RawMessage(`true`), Error: &idempotencyrpc.Error{Code: -1, Message: "bad"},
		},
		"oversized":           {Result: json.RawMessage(`"` + strings.Repeat("x", 512) + `"`)},
		"invalid result":      {Result: json.RawMessage(`{`)},
		"empty error message": {Error: &idempotencyrpc.Error{Code: -1}},
		"invalid error data": {
			Error: &idempotencyrpc.Error{Code: -1, Message: "bad", Data: json.RawMessage(`{`)},
		},
	}
	for name, response := range tests {
		t.Run(name, func(t *testing.T) {
			middleware, _ := fixture(t, idempotencyrpc.MinResponseBytes)
			request := idempotencyrpc.Request{Method: "widgets.create", Params: json.RawMessage(`{}`)}
			var calls atomic.Int64
			handler := func(context.Context, idempotencyrpc.Request) idempotencyrpc.Response {
				calls.Add(1)
				return response
			}
			first, err := middleware.Call(context.Background(), request, handler)
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			second, err := middleware.Call(context.Background(), request, handler)
			if err != nil || calls.Load() != 1 ||
				first.Outcome != idempotency.OutcomeTerminalFailure ||
				second.Outcome != idempotency.OutcomeTerminalFailure ||
				second.Response.Error == nil || second.Response.Error.Code != -32603 {
				t.Fatalf("calls = %d, results = %#v, %#v, %v", calls.Load(), first, second, err)
			}
		})
	}
}

func TestNewValidatesConfiguration(t *testing.T) {
	store := mustStore(t)
	service := mustService(t, store)
	key := validKey(t)
	fingerprint := validFingerprint(t)
	valid := idempotencyrpc.Options{
		Service: service, Lease: time.Minute, Key: key, Fingerprint: fingerprint,
	}
	tests := map[string]idempotencyrpc.Options{
		"service":            {Lease: time.Minute, Key: key, Fingerprint: fingerprint},
		"lease zero":         {Service: service, Key: key, Fingerprint: fingerprint},
		"lease too long":     {Service: service, Lease: idempotency.MaxLease + 1, Key: key, Fingerprint: fingerprint},
		"key":                {Service: service, Lease: time.Minute, Fingerprint: fingerprint},
		"fingerprint":        {Service: service, Lease: time.Minute, Key: key},
		"limit too small":    withLimit(valid, idempotencyrpc.MinResponseBytes-1),
		"limit too large":    withLimit(valid, idempotencyrpc.MaxResponseBytes+1),
		"transition timeout": withTransitionTimeout(valid, -1),
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := idempotencyrpc.New(options); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
	if _, err := idempotencyrpc.New(valid); err != nil {
		t.Fatalf("New() default limit error = %v", err)
	}
}

func TestMiddlewareValidatesInvocationIdentity(t *testing.T) {
	request := idempotencyrpc.Request{Method: "widgets.create", Params: json.RawMessage(`{}`)}
	backendErr := errors.New("identity failed")
	tests := map[string]struct {
		request     idempotencyrpc.Request
		handler     idempotencyrpc.Handler
		key         idempotencyrpc.KeyFunc
		fingerprint idempotencyrpc.FingerprintFunc
	}{
		"method":  {request: idempotencyrpc.Request{}, handler: validHandler, key: validKey(t), fingerprint: validFingerprint(t)},
		"handler": {request: request, key: validKey(t), fingerprint: validFingerprint(t)},
		"key error": {
			request: request, handler: validHandler,
			key: func(context.Context, idempotencyrpc.Request) (idempotency.Key, error) {
				return idempotency.Key{}, backendErr
			},
			fingerprint: validFingerprint(t),
		},
		"method namespace": {
			request: request, handler: validHandler,
			key: func(context.Context, idempotencyrpc.Request) (idempotency.Key, error) {
				key, err := idempotency.NewKey("rpc", "tenant", "wrong", "caller", "key")
				return key, err
			},
			fingerprint: validFingerprint(t),
		},
		"fingerprint error": {
			request: request, handler: validHandler, key: validKey(t),
			fingerprint: func(idempotencyrpc.Request) (idempotency.Fingerprint, error) {
				return idempotency.Fingerprint{}, backendErr
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			middleware := mustMiddleware(t, mustStore(t), test.key, test.fingerprint, 1024)
			if _, err := middleware.Call(context.Background(), test.request, test.handler); err == nil {
				t.Fatal("Call() error = nil")
			}
		})
	}
}

func TestMiddlewareFailsClosedAtStorageAndPersistedReplayBoundaries(t *testing.T) {
	request := idempotencyrpc.Request{Method: "widgets.create", Params: json.RawMessage(`{}`)}
	key, fingerprint := identity(t, request)
	record := idempotency.Record{
		Key: key, Fingerprint: fingerprint, State: idempotency.StateAcquired,
		OwnerToken: "owner", FencingToken: 1,
	}
	backendErr := errors.New("backend unavailable")
	tests := map[string]*storeOverride{
		"acquire": {
			Store: mustStore(t),
			acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
				return idempotency.AcquireResult{}, backendErr
			},
		},
		"complete": {
			Store:   mustStore(t),
			acquire: acquired(record),
			complete: func(context.Context, idempotency.CompleteRequest) (idempotency.Record, error) {
				return idempotency.Record{}, backendErr
			},
		},
		"fail": {
			Store: mustStore(t), acquire: acquired(record),
			fail: func(context.Context, idempotency.FailRequest) (idempotency.Record, error) {
				return idempotency.Record{}, backendErr
			},
		},
	}
	for name, store := range tests {
		t.Run(name, func(t *testing.T) {
			middleware := mustMiddleware(t, store, validKey(t), validFingerprint(t), 1024)
			handler := validHandler
			if name == "fail" {
				handler = func(context.Context, idempotencyrpc.Request) idempotencyrpc.Response {
					return idempotencyrpc.Response{}
				}
			}
			if _, err := middleware.Call(context.Background(), request, handler); err == nil {
				t.Fatal("Call() error = nil")
			}
		})
	}

	replays := [][]byte{
		[]byte("not-json"),
		[]byte(`{"schema":2,"response":{"result":true}}`),
		[]byte(`{"schema":1,"response":{}}`),
		[]byte(strings.Repeat("x", 1025)),
	}
	for index, payload := range replays {
		t.Run("replay "+string(rune('a'+index)), func(t *testing.T) {
			replayRecord := record
			replayRecord.State = idempotency.StateCompleted
			replayRecord.Result = payload
			store := &storeOverride{Store: mustStore(t), acquire: func(
				context.Context, idempotency.AcquireRequest,
			) (idempotency.AcquireResult, error) {
				return idempotency.AcquireResult{
					Outcome: idempotency.OutcomeReplayed, Record: replayRecord,
				}, nil
			}}
			middleware := mustMiddleware(t, store, validKey(t), validFingerprint(t), 1024)
			if _, err := middleware.Call(context.Background(), request, validHandler); err == nil {
				t.Fatal("Call() error = nil")
			}
		})
	}
}

func TestMiddlewareReleasesOwnershipWhenHandlerPanics(t *testing.T) {
	store := mustStore(t)
	middleware := mustMiddleware(t, store, validKey(t), validFingerprint(t), 1024)
	request := idempotencyrpc.Request{Method: "widgets.create", Params: json.RawMessage(`{}`)}
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		if recover() != "rpc panic" {
			t.Fatal("panic was not propagated")
		}
		key, _ := identity(t, request)
		record, err := store.Inspect(context.Background(), key)
		if err != nil || record.State != idempotency.StateAbandoned {
			t.Fatalf("Inspect() = %#v, %v", record, err)
		}
	}()
	_, _ = middleware.Call(ctx, request, func(context.Context, idempotencyrpc.Request) idempotencyrpc.Response {
		cancel()
		panic("rpc panic")
	})
}

func fixture(t *testing.T, limit int) (*idempotencyrpc.Middleware, *memory.Store) {
	t.Helper()
	store, err := memory.New(memory.Options{
		Clock:       idempotencytest.NewClock(time.Unix(1_700_000_000, 0).UTC()),
		OwnerTokens: idempotencytest.NewTokenSource("rpc-owner").Next,
	})
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}
	service, err := idempotency.NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	middleware, err := idempotencyrpc.New(idempotencyrpc.Options{
		Service: service, Lease: time.Minute, MaxResponseBytes: limit,
		Key: func(_ context.Context, request idempotencyrpc.Request) (idempotency.Key, error) {
			key, _ := identity(t, request)
			return key, nil
		},
		Fingerprint: func(request idempotencyrpc.Request) (idempotency.Fingerprint, error) {
			_, fingerprint := identity(t, request)
			return fingerprint, nil
		},
	})
	if err != nil {
		t.Fatalf("idempotencyrpc.New() error = %v", err)
	}
	return middleware, store
}

func identity(t *testing.T, request idempotencyrpc.Request) (idempotency.Key, idempotency.Fingerprint) {
	t.Helper()
	key, err := idempotency.NewKey("rpc", "tenant", request.Method, "caller", "delivery-key")
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("rpc-v1", request.Params)
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	return key, fingerprint
}

func mustStore(t *testing.T) *memory.Store {
	t.Helper()
	store, err := memory.New(memory.Options{
		Clock:       idempotencytest.NewClock(time.Unix(1_700_000_000, 0).UTC()),
		OwnerTokens: idempotencytest.NewTokenSource("rpc-owner").Next,
	})
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}
	return store
}

func mustService(t *testing.T, store idempotency.Store) *idempotency.Service {
	t.Helper()
	service, err := idempotency.NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func mustMiddleware(
	t *testing.T,
	store idempotency.Store,
	key idempotencyrpc.KeyFunc,
	fingerprint idempotencyrpc.FingerprintFunc,
	limit int,
) *idempotencyrpc.Middleware {
	t.Helper()
	middleware, err := idempotencyrpc.New(idempotencyrpc.Options{
		Service: mustService(t, store), Lease: time.Minute, MaxResponseBytes: limit,
		Key: key, Fingerprint: fingerprint,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return middleware
}

func validKey(t *testing.T) idempotencyrpc.KeyFunc {
	t.Helper()
	return func(_ context.Context, request idempotencyrpc.Request) (idempotency.Key, error) {
		key, _ := identity(t, request)
		return key, nil
	}
}

func validFingerprint(t *testing.T) idempotencyrpc.FingerprintFunc {
	t.Helper()
	return func(request idempotencyrpc.Request) (idempotency.Fingerprint, error) {
		_, fingerprint := identity(t, request)
		return fingerprint, nil
	}
}

func validHandler(context.Context, idempotencyrpc.Request) idempotencyrpc.Response {
	return idempotencyrpc.Response{Result: json.RawMessage(`true`)}
}

func withLimit(options idempotencyrpc.Options, limit int) idempotencyrpc.Options {
	options.MaxResponseBytes = limit
	return options
}

func withTransitionTimeout(
	options idempotencyrpc.Options,
	timeout time.Duration,
) idempotencyrpc.Options {
	options.TransitionTimeout = timeout
	return options
}

func acquired(record idempotency.Record) func(
	context.Context,
	idempotency.AcquireRequest,
) (idempotency.AcquireResult, error) {
	return func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
		return idempotency.AcquireResult{Outcome: idempotency.OutcomeAcquired, Record: record}, nil
	}
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
