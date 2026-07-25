package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRegistryRegistration(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	handler := func(context.Context, json.RawMessage) (any, error) { return "ok", nil }
	if err := registry.Register("ping", handler); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, ok := registry.Lookup("ping"); !ok {
		t.Error("Lookup() did not find registered method")
	}
	if err := registry.Register("ping", handler); !errors.Is(err, ErrMethodAlreadyRegistered) {
		t.Errorf("duplicate Register() error = %v", err)
	}
	if err := registry.Register("", handler); err != nil {
		t.Errorf("Register(empty method) error = %v", err)
	}
	if err := registry.Register("rpc.internal", handler); !errors.Is(err, ErrInvalidMethodName) {
		t.Errorf("Register(reserved method) error = %v", err)
	}
	if err := registry.Register("nil", nil); !errors.Is(err, ErrNilHandler) {
		t.Errorf("Register(nil) error = %v", err)
	}
}

func TestRegistryZeroValueIsUsable(t *testing.T) {
	t.Parallel()

	var registry Registry
	if err := registry.Register("ping", func(context.Context, json.RawMessage) (any, error) {
		return "pong", nil
	}); err != nil {
		t.Fatalf("zero-value Register() error = %v", err)
	}
	if _, ok := registry.Lookup("ping"); !ok {
		t.Fatal("zero-value Lookup() did not find registered method")
	}
}

func TestRegistryRegistersReservedSystemMethodsExplicitly(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	handler := func(context.Context, json.RawMessage) (any, error) { return "ok", nil }
	if err := registry.RegisterSystem("rpc.discover", handler); err != nil {
		t.Fatalf("RegisterSystem() error = %v", err)
	}
	if registered, ok := registry.Lookup("rpc.discover"); !ok || registered == nil {
		t.Fatal("Lookup() did not find registered system method")
	}
	if err := registry.RegisterSystem("rpc.discover", handler); !errors.Is(err, ErrMethodAlreadyRegistered) {
		t.Errorf("duplicate RegisterSystem() error = %v", err)
	}
	if err := registry.RegisterSystem("application.method", handler); !errors.Is(err, ErrInvalidMethodName) {
		t.Errorf("application RegisterSystem() error = %v", err)
	}
	if err := registry.RegisterSystem("rpc.nil", nil); !errors.Is(err, ErrNilHandler) {
		t.Errorf("nil RegisterSystem() error = %v", err)
	}
}

func TestRegistrySupportsConcurrentRegistrationAndLookup(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	var workers sync.WaitGroup
	for index := range 64 {
		name := fmt.Sprintf("method-%d", index)
		workers.Go(func() {
			if err := registry.Register(name, func(context.Context, json.RawMessage) (any, error) {
				return nil, nil
			}); err != nil {
				t.Errorf("Register(%q) error = %v", name, err)
				return
			}
			if _, ok := registry.Lookup(name); !ok {
				t.Errorf("Lookup(%q) missed concurrent registration", name)
			}
		})
	}
	workers.Wait()
}

func TestDispatcherSingleRequests(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	_ = registry.Register("subtract", func(_ context.Context, params json.RawMessage) (any, error) {
		var values []int
		if err := json.Unmarshal(params, &values); err != nil || len(values) != 2 {
			return nil, InvalidParams()
		}
		return values[0] - values[1], nil
	})
	dispatcher := NewDispatcher(registry)

	tests := []struct {
		name     string
		input    string
		response string
		hasReply bool
	}{
		{
			name:     "positional parameters",
			input:    `{"jsonrpc":"2.0","method":"subtract","params":[42,23],"id":1}`,
			response: `{"jsonrpc":"2.0","result":19,"id":1}`,
			hasReply: true,
		},
		{
			name:     "method not found",
			input:    `{"jsonrpc":"2.0","method":"missing","id":"a"}`,
			response: `{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":"a"}`,
			hasReply: true,
		},
		{
			name:     "notification",
			input:    `{"jsonrpc":"2.0","method":"subtract","params":[42,23]}`,
			hasReply: false,
		},
		{
			name:     "null id is a request",
			input:    `{"jsonrpc":"2.0","method":"subtract","params":[42,23],"id":null}`,
			response: `{"jsonrpc":"2.0","result":19,"id":null}`,
			hasReply: true,
		},
		{
			name:     "invalid params",
			input:    `{"jsonrpc":"2.0","method":"subtract","params":[1],"id":1}`,
			response: `{"jsonrpc":"2.0","error":{"code":-32602,"message":"Invalid params"},"id":1}`,
			hasReply: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			response, hasReply := dispatcher.Dispatch(context.Background(), []byte(tt.input))
			if hasReply != tt.hasReply {
				t.Fatalf("Dispatch() hasReply = %v, want %v", hasReply, tt.hasReply)
			}
			assertJSONEqual(t, response, []byte(tt.response))
		})
	}
}

func TestDispatcherProtocolErrors(t *testing.T) {
	t.Parallel()

	dispatcher := NewDispatcher(NewRegistry())
	tests := []struct {
		name     string
		input    string
		response string
	}{
		{name: "parse error", input: `{`, response: `{"jsonrpc":"2.0","error":{"code":-32700,"message":"Parse error"},"id":null}`},
		{name: "scalar", input: `1`, response: `{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request"},"id":null}`},
		{name: "wrong version", input: `{"jsonrpc":"1.0","method":"x","id":1}`, response: `{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request"},"id":null}`},
		{name: "invalid id", input: `{"jsonrpc":"2.0","method":"x","id":true}`, response: `{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request"},"id":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			response, hasReply := dispatcher.Dispatch(context.Background(), []byte(tt.input))
			if !hasReply {
				t.Fatal("Dispatch() unexpectedly omitted protocol error")
			}
			assertJSONEqual(t, response, []byte(tt.response))
		})
	}
}

func TestDispatcherBatch(t *testing.T) {
	t.Parallel()

	var notifications atomic.Int64
	registry := NewRegistry()
	_ = registry.Register("sum", func(_ context.Context, params json.RawMessage) (any, error) {
		var values []int
		if err := json.Unmarshal(params, &values); err != nil {
			return nil, InvalidParams()
		}
		total := 0
		for _, value := range values {
			total += value
		}
		return total, nil
	})
	_ = registry.Register("notify", func(context.Context, json.RawMessage) (any, error) {
		notifications.Add(1)
		return nil, nil
	})
	dispatcher := NewDispatcher(registry)

	input := `[
		{"jsonrpc":"2.0","method":"sum","params":[1,2,4],"id":"1"},
		{"jsonrpc":"2.0","method":"notify","params":[7]},
		{"jsonrpc":"2.0","method":"missing","id":"2"},
		{"foo":"boo"},
		1
	]`
	want := `[
		{"jsonrpc":"2.0","result":7,"id":"1"},
		{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":"2"},
		{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request"},"id":null},
		{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request"},"id":null}
	]`
	response, hasReply := dispatcher.Dispatch(context.Background(), []byte(input))
	if !hasReply {
		t.Fatal("Dispatch() omitted batch response")
	}
	assertJSONEqual(t, response, []byte(want))
	if notifications.Load() != 1 {
		t.Errorf("notifications executed = %d, want 1", notifications.Load())
	}
}

func TestDispatcherBatchEdgeCases(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	_ = registry.Register("notify", func(context.Context, json.RawMessage) (any, error) {
		return nil, nil
	})
	dispatcher := NewDispatcher(registry)

	response, hasReply := dispatcher.Dispatch(context.Background(), []byte(`[]`))
	if !hasReply {
		t.Fatal("empty batch unexpectedly omitted response")
	}
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request"},"id":null}`))

	response, hasReply = dispatcher.Dispatch(context.Background(), []byte(`[
		{"jsonrpc":"2.0","method":"notify"},
		{"jsonrpc":"2.0","method":"notify"}
	]`))
	if hasReply || response != nil {
		t.Errorf("notification-only batch = (%s, %v), want no response", response, hasReply)
	}
}

func TestDispatcherRejectsResourceLimitViolationsBeforeDispatch(t *testing.T) {
	t.Parallel()

	tooLarge := bytes.Repeat([]byte{' '}, (4<<20)+1)
	response, ok := NewDispatcher(nil).Dispatch(context.Background(), tooLarge)
	if !ok {
		t.Fatal("Dispatch(oversized payload) omitted limit response")
	}
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"Request limit exceeded"},"id":null}`))

	var calls atomic.Int64
	registry := NewRegistry()
	_ = registry.Register("work", func(context.Context, json.RawMessage) (any, error) {
		calls.Add(1)
		return nil, nil
	})
	var batch strings.Builder
	batch.WriteByte('[')
	for index := range 1025 {
		if index > 0 {
			batch.WriteByte(',')
		}
		batch.WriteString(`{"jsonrpc":"2.0","method":"work"}`)
	}
	batch.WriteByte(']')
	response, ok = NewDispatcher(registry).Dispatch(context.Background(), []byte(batch.String()))
	if !ok {
		t.Fatal("Dispatch(oversized batch) omitted limit response")
	}
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"Request limit exceeded"},"id":null}`))
	if calls.Load() != 0 {
		t.Fatalf("oversized batch invoked %d handlers", calls.Load())
	}

	payload := []byte(`{"jsonrpc":"2.0","method":"missing","id":1}`)
	limited := NewDispatcher(
		nil,
		WithMaxDispatchBytes(int64(len(payload))),
		WithMaxDispatchBytes(0),
		WithMaxBatchItems(1),
		WithMaxBatchItems(0),
	)
	response, ok = limited.Dispatch(context.Background(), payload)
	if !ok {
		t.Fatal("Dispatch(exact byte limit) omitted response")
	}
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":1}`))
	response, _ = limited.Dispatch(context.Background(), append(payload, ' '))
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"Request limit exceeded"},"id":null}`))
	batchLimited := NewDispatcher(nil, WithMaxBatchItems(1), WithMaxBatchItems(0))
	response, _ = batchLimited.Dispatch(context.Background(), []byte(`[{"jsonrpc":"2.0","method":"missing","id":1}]`))
	assertJSONEqual(t, response, []byte(`[{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":1}]`))
	response, _ = batchLimited.Dispatch(context.Background(), []byte(`[{"jsonrpc":"2.0","method":"missing","id":1},{"jsonrpc":"2.0","method":"missing","id":2}]`))
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"Request limit exceeded"},"id":null}`))
}

func TestDispatcherErrorMappingPanicRecoveryAndMiddleware(t *testing.T) {
	t.Parallel()

	type contextKey string
	const key contextKey = "trace"
	registry := NewRegistry()
	_ = registry.Register("failure", func(ctx context.Context, _ json.RawMessage) (any, error) {
		if ctx.Value(key) != "present" {
			t.Error("middleware context did not reach handler")
		}
		return nil, errors.New("secret database detail")
	})
	_ = registry.Register("panic", func(context.Context, json.RawMessage) (any, error) {
		panic("boom")
	})

	var events []string
	middleware := func(next Handler) Handler {
		return func(ctx context.Context, params json.RawMessage) (any, error) {
			events = append(events, "before")
			result, err := next(context.WithValue(ctx, key, "present"), params)
			events = append(events, "after")
			return result, err
		}
	}
	dispatcher := NewDispatcher(
		registry,
		WithMiddleware(middleware),
		WithErrorMapper(func(err error) *Error {
			return NewError(-32001, "Dependency unavailable").WithCause(err)
		}),
	)

	response, _ := dispatcher.Dispatch(context.Background(), []byte(`{"jsonrpc":"2.0","method":"failure","id":1}`))
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","error":{"code":-32001,"message":"Dependency unavailable"},"id":1}`))
	if !reflect.DeepEqual(events, []string{"before", "after"}) {
		t.Errorf("middleware events = %v", events)
	}

	response, _ = dispatcher.Dispatch(context.Background(), []byte(`{"jsonrpc":"2.0","method":"panic","id":2}`))
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error"},"id":2}`))
}

func TestDecodeParams(t *testing.T) {
	t.Parallel()

	type input struct {
		Name string `json:"name"`
	}
	decoded, rpcErr := DecodeParams[input](json.RawMessage(`{"name":"Ada"}`))
	if rpcErr != nil || decoded.Name != "Ada" {
		t.Fatalf("DecodeParams() = (%#v, %v)", decoded, rpcErr)
	}
	if _, rpcErr = DecodeParams[input](json.RawMessage(`{"name":`)); rpcErr == nil || rpcErr.Code != CodeInvalidParams {
		t.Errorf("DecodeParams(invalid) = %v", rpcErr)
	}
	if _, rpcErr = DecodeParams[input](json.RawMessage(`{"name":1}`)); rpcErr == nil || rpcErr.Code != CodeInvalidParams {
		t.Errorf("DecodeParams(type mismatch) = %v", rpcErr)
	}
	if _, rpcErr = DecodeParams[input](nil); rpcErr == nil || rpcErr.Code != CodeInvalidParams {
		t.Errorf("DecodeParams(nil) = %v", rpcErr)
	}
	if _, rpcErr = DecodeParams[input](json.RawMessage(`   `)); rpcErr == nil || rpcErr.Code != CodeInvalidParams {
		t.Errorf("DecodeParams(whitespace) = %v", rpcErr)
	}
	if _, rpcErr = DecodeParams[input](json.RawMessage(`{"name":"Ada"} {}`)); rpcErr == nil || rpcErr.Code != CodeInvalidParams {
		t.Errorf("DecodeParams(trailing JSON) = %v", rpcErr)
	}
	if _, rpcErr = DecodeParams[input](json.RawMessage(`{"NAME":"Ada"}`)); rpcErr == nil || rpcErr.Code != CodeInvalidParams {
		t.Errorf("DecodeParams(case-mismatched name) = %v", rpcErr)
	}
	if _, rpcErr = DecodeParams[input](json.RawMessage(`{"name":"first","name":"second"}`)); rpcErr == nil || rpcErr.Code != CodeInvalidParams {
		t.Errorf("DecodeParams(duplicate name) = %v", rpcErr)
	}
	if values, rpcErr := DecodeParams[map[string]string](json.RawMessage(`{"arbitrary":"value"}`)); rpcErr != nil || values["arbitrary"] != "value" {
		t.Errorf("DecodeParams(map) = (%v, %v)", values, rpcErr)
	}
	if values, rpcErr := DecodeParams[[]int](json.RawMessage(`[1,2]`)); rpcErr != nil || !reflect.DeepEqual(values, []int{1, 2}) {
		t.Errorf("DecodeParams(slice) = (%v, %v)", values, rpcErr)
	}
}

func TestDecodeParamsEmbeddedAndTaggedFields(t *testing.T) {
	t.Parallel()

	type Embedded struct {
		Name string `json:"name"`
	}
	type input struct {
		*Embedded
		Count   int `json:"count,omitempty"`
		Public  string
		Ignore  string `json:"-"`
		private string
	}
	decoded, rpcErr := DecodeParams[input](json.RawMessage(`{"name":"Ada","count":1,"Public":"ok"}`))
	if rpcErr != nil || decoded.Embedded == nil || decoded.Name != "Ada" || decoded.Count != 1 || decoded.Public != "ok" || decoded.private != "" {
		t.Fatalf("DecodeParams(embedded) = (%#v, %v)", decoded, rpcErr)
	}
	if _, rpcErr := DecodeParams[input](json.RawMessage(`{"Ignore":"bad"}`)); rpcErr == nil {
		t.Error("DecodeParams(ignored field) unexpectedly succeeded")
	}
	if _, rpcErr := DecodeParams[input](json.RawMessage(`{`)); rpcErr == nil {
		t.Error("DecodeParams(malformed object) unexpectedly succeeded")
	}
	if _, rpcErr := DecodeParams[*input](json.RawMessage(`{"Public":"pointer"}`)); rpcErr != nil {
		t.Errorf("DecodeParams(pointer) error = %v", rpcErr)
	}
}

func TestRequestIsAvailableFromHandlerContext(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	_ = registry.Register("inspect", func(ctx context.Context, _ json.RawMessage) (any, error) {
		request, ok := RequestFromContext(ctx)
		if !ok {
			t.Fatal("RequestFromContext() did not find request")
		}
		return request.Method, nil
	})
	response, _ := NewDispatcher(registry).Dispatch(
		context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"inspect","id":1}`),
	)
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","result":"inspect","id":1}`))
}

func TestDispatcherContainsInvalidHandlerErrors(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	_ = registry.Register("invalid-data", func(context.Context, json.RawMessage) (any, error) {
		rpcErr := NewError(42, "bad data")
		rpcErr.Data = json.RawMessage(`{`)
		return nil, rpcErr
	})
	_ = registry.Register("invalid-shape", func(context.Context, json.RawMessage) (any, error) {
		return nil, &Error{}
	})
	dispatcher := NewDispatcher(registry)
	response, ok := dispatcher.Dispatch(
		context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"invalid-data","id":1}`),
	)
	if !ok {
		t.Fatal("Dispatch(invalid-data) unexpectedly omitted response")
	}
	assertJSONEqual(t, response, []byte(
		`{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error"},"id":1}`,
	))

	response, ok = dispatcher.Dispatch(
		context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"invalid-shape","id":2}`),
	)
	if !ok {
		t.Fatal("Dispatch(zero application error) unexpectedly omitted response")
	}
	assertJSONEqual(t, response, []byte(
		`{"jsonrpc":"2.0","error":{"code":0,"message":""},"id":2}`,
	))
}

func TestDispatcherLifecycleHooksObserveAllOutcomes(t *testing.T) {
	t.Parallel()

	type hookKey struct{}
	registry := NewRegistry()
	_ = registry.Register("ok", func(ctx context.Context, _ json.RawMessage) (any, error) {
		if ctx.Value(hookKey{}) != "hooked" {
			t.Error("OnRequest context did not reach handler")
		}
		return true, nil
	})
	_ = registry.Register("panic", func(context.Context, json.RawMessage) (any, error) {
		panic("hook-visible panic")
	})
	type event struct {
		method       string
		code         int
		notification bool
		cause        string
	}
	var events []event
	hooks := Hooks{
		OnRequest: func(ctx context.Context, _ *Request) context.Context {
			return context.WithValue(ctx, hookKey{}, "hooked")
		},
		OnResponse: func(_ context.Context, request *Request, response *Response) {
			observed := event{}
			if request != nil {
				observed.method = request.Method
				observed.notification = request.IsNotification()
			}
			if response != nil && response.Error != nil {
				observed.code = response.Error.Code
				if cause := response.Error.Unwrap(); cause != nil {
					observed.cause = cause.Error()
				}
			}
			events = append(events, observed)
		},
	}
	dispatcher := NewDispatcher(registry, WithHooks(hooks))
	inputs := []string{
		`{`,
		`{"jsonrpc":"2.0","id":1}`,
		`{"jsonrpc":"2.0","method":"missing","id":2}`,
		`{"jsonrpc":"2.0","method":"ok"}`,
		`{"jsonrpc":"2.0","method":"missing-notification"}`,
		`{"jsonrpc":"2.0","method":"panic","id":3}`,
	}
	for _, input := range inputs {
		dispatcher.Dispatch(context.Background(), []byte(input))
	}
	if len(events) != len(inputs) {
		t.Fatalf("hook events = %d, want %d: %#v", len(events), len(inputs), events)
	}
	if events[0].code != CodeParseError || events[0].method != "" {
		t.Errorf("parse event = %#v", events[0])
	}
	if events[1].code != CodeInvalidRequest || events[1].method != "" {
		t.Errorf("invalid event = %#v", events[1])
	}
	if events[2].code != CodeMethodNotFound || events[2].method != "missing" {
		t.Errorf("missing event = %#v", events[2])
	}
	if !events[3].notification || events[3].method != "ok" || events[3].code != 0 {
		t.Errorf("notification event = %#v", events[3])
	}
	if events[4].code != CodeMethodNotFound || !events[4].notification {
		t.Errorf("failed notification event = %#v", events[4])
	}
	if events[5].code != CodeInternalError || !strings.Contains(events[5].cause, "goroutine") {
		t.Errorf("panic event = %#v", events[5])
	}
}

func TestDispatcherLifecycleHooksCannotBreakDispatch(t *testing.T) {
	t.Parallel()

	hooks := Hooks{
		OnRequest: func(context.Context, *Request) context.Context { panic("start") },
		OnResponse: func(_ context.Context, _ *Request, response *Response) {
			response.Error.Data = json.RawMessage(`{`)
			panic("finish")
		},
	}
	response, ok := NewDispatcher(nil, WithHooks(hooks)).Dispatch(
		context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"missing","id":1}`),
	)
	if !ok {
		t.Fatal("hook panic suppressed response")
	}
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":1}`))
}

func TestDispatcherOnRequestHookReceivesIsolatedRequest(t *testing.T) {
	t.Parallel()

	var executed bool
	registry := NewRegistry()
	_ = registry.Register("notice", func(context.Context, json.RawMessage) (any, error) {
		executed = true
		return nil, nil
	})
	hooks := Hooks{OnRequest: func(ctx context.Context, request *Request) context.Context {
		request.Method = "mutated"
		request.ID = StringID("forced-response")
		request.Params[0] = '['
		return ctx
	}}
	response, ok := NewDispatcher(registry, WithHooks(hooks)).Dispatch(
		context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"notice","params":{"safe":true}}`),
	)
	if ok || response != nil {
		t.Errorf("mutated notification produced response %s", response)
	}
	if !executed {
		t.Error("hook mutation changed dispatched method")
	}
}

func assertJSONEqual(t *testing.T, got, want []byte) {
	t.Helper()
	if len(want) == 0 {
		if len(got) != 0 {
			t.Fatalf("got JSON %s, want no JSON", got)
		}
		return
	}
	var gotValue, wantValue any
	gotDecoder := json.NewDecoder(strings.NewReader(string(got)))
	gotDecoder.UseNumber()
	if err := gotDecoder.Decode(&gotValue); err != nil {
		t.Fatalf("invalid got JSON %q: %v", got, err)
	}
	wantDecoder := json.NewDecoder(strings.NewReader(string(want)))
	wantDecoder.UseNumber()
	if err := wantDecoder.Decode(&wantValue); err != nil {
		t.Fatalf("invalid want JSON %q: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("JSON mismatch\n got: %s\nwant: %s", got, want)
	}
}
