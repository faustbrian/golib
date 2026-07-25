package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fixedIDGenerator struct{ id ID }

func (generator fixedIDGenerator) NextID() ID { return generator.id }

func TestConstructorsIgnoreNilOptions(t *testing.T) {
	t.Parallel()

	t.Run("client", func(t *testing.T) {
		if NewClient(nil, nil) == nil {
			t.Fatal("NewClient(nil option) returned nil")
		}
	})
	t.Run("dispatcher", func(t *testing.T) {
		if NewDispatcher(nil, nil) == nil {
			t.Fatal("NewDispatcher(nil option) returned nil")
		}
	})
	t.Run("HTTP handler", func(t *testing.T) {
		if NewHTTPHandler(nil, nil) == nil {
			t.Fatal("NewHTTPHandler(nil option) returned nil")
		}
	})
	t.Run("HTTP transport", func(t *testing.T) {
		if transport, err := NewHTTPTransport("http://example.test", nil); err != nil || transport == nil {
			t.Fatalf("NewHTTPTransport(nil option) = (%v, %v)", transport, err)
		}
	})
}

func TestClientOptionsAndDefensivePaths(t *testing.T) {
	t.Parallel()

	client := NewClient(TransportFunc(func(_ context.Context, payload []byte) ([]byte, error) {
		return []byte(`{"jsonrpc":"2.0","result":null,"id":"fixed"}`), nil
	}), WithIDGenerator(nil), WithIDGenerator(fixedIDGenerator{id: StringID("fixed")}))
	if err := client.Call(context.Background(), "discard", nil, nil); err != nil {
		t.Fatalf("Call(discard) error = %v", err)
	}
	if err := client.Call(context.Background(), "rpc.reserved", nil, nil); !errors.Is(err, ErrInvalidMethodName) {
		t.Errorf("Call(invalid method) error = %v", err)
	}
	if err := client.Call(context.Background(), "method", make(chan int), nil); err == nil {
		t.Error("Call(unencodable params) unexpectedly succeeded")
	}
	if err := client.Notify(context.Background(), "rpc.reserved", nil); !errors.Is(err, ErrInvalidMethodName) {
		t.Errorf("Notify(invalid method) error = %v", err)
	}
	if err := client.Notify(context.Background(), "method", make(chan int)); err == nil {
		t.Error("Notify(unencodable params) unexpectedly succeeded")
	}

	nilTransport := NewClient(nil)
	if err := nilTransport.Notify(context.Background(), "notice", nil); !errors.Is(err, ErrTransport) {
		t.Errorf("Notify(nil transport) error = %v", err)
	}
}

func TestClientBatchDefensivePaths(t *testing.T) {
	t.Parallel()

	client := NewClient(TransportFunc(func(context.Context, []byte) ([]byte, error) { return nil, nil }))
	if err := client.Batch(context.Background(), nil); err == nil {
		t.Error("Batch(nil call) unexpectedly succeeded")
	}
	if err := client.Batch(context.Background(), &BatchCall{Method: "rpc.reserved"}); !errors.Is(err, ErrInvalidMethodName) {
		t.Errorf("Batch(invalid request) error = %v", err)
	}
	if err := client.Batch(context.Background(), &BatchCall{Method: "rpc.reserved", Notification: true}); !errors.Is(err, ErrInvalidMethodName) {
		t.Errorf("Batch(invalid notification) error = %v", err)
	}
	if err := client.Batch(context.Background(), &BatchCall{Method: "notice", Notification: true}); err != nil {
		t.Errorf("notification-only Batch() error = %v", err)
	}

	responses := []struct {
		response string
		want     error
		call     *BatchCall
	}{
		{response: `[`, want: ErrInvalidResponse, call: &BatchCall{Method: "one"}},
		{response: `[{"jsonrpc":"1.0","result":1,"id":1}]`, want: ErrInvalidResponse, call: &BatchCall{Method: "one"}},
		{response: `[{"jsonrpc":"2.0","result":"bad","id":1}]`, want: ErrInvalidResponse, call: &BatchCall{Method: "one", Result: new(int)}},
	}
	for _, test := range responses {
		client := NewClient(TransportFunc(func(context.Context, []byte) ([]byte, error) {
			return []byte(test.response), nil
		}))
		if err := client.Batch(context.Background(), test.call); !errors.Is(err, test.want) {
			t.Errorf("Batch(response %q) error = %v, want %v", test.response, err, test.want)
		}
	}

	unexpected := NewClient(TransportFunc(func(context.Context, []byte) ([]byte, error) {
		return []byte(`[]`), nil
	}))
	if err := unexpected.Batch(context.Background(), &BatchCall{Method: "notice", Notification: true}); !errors.Is(err, ErrUnexpectedResponse) {
		t.Errorf("notification Batch(response) error = %v", err)
	}

	offline := NewClient(TransportFunc(func(context.Context, []byte) ([]byte, error) {
		return nil, io.EOF
	}))
	if err := offline.Batch(context.Background(), &BatchCall{Method: "one"}); !errors.Is(err, ErrTransport) {
		t.Errorf("Batch(transport error) = %v", err)
	}
}

func TestProtocolDefensivePaths(t *testing.T) {
	t.Parallel()

	missing, err := json.Marshal(ID{})
	if err != nil || string(missing) != "null" {
		t.Errorf("Marshal(missing ID) = %s, %v", missing, err)
	}
	if (ID{}).valid() {
		t.Error("missing ID is valid")
	}
	if !NullID().valid() {
		t.Error("null ID is invalid")
	}
	if (ID{kind: IDKind(99), raw: json.RawMessage(`null`)}).valid() {
		t.Error("unknown ID kind is valid")
	}
	var id ID
	if err := json.Unmarshal([]byte(`"unterminated`), &id); err == nil {
		t.Error("Unmarshal(malformed ID) unexpectedly succeeded")
	}
	if err := id.UnmarshalJSON([]byte(`"unterminated`)); err == nil {
		t.Error("ID.UnmarshalJSON(malformed) unexpectedly succeeded")
	}
	if err := id.UnmarshalJSON([]byte(`1 2`)); err == nil {
		t.Error("ID.UnmarshalJSON(trailing data) unexpectedly succeeded")
	}
	if err := id.UnmarshalJSON([]byte(`1 {`)); err == nil {
		t.Error("ID.UnmarshalJSON(malformed trailing data) unexpectedly succeeded")
	}
	var request Request
	if err := json.Unmarshal([]byte(`{`), &request); err == nil {
		t.Error("Unmarshal(malformed request) unexpectedly succeeded")
	}
	if err := request.UnmarshalJSON([]byte(`{`)); err == nil {
		t.Error("Request.UnmarshalJSON(malformed) unexpectedly succeeded")
	}
	if err := request.UnmarshalJSON([]byte(`{} null`)); err == nil {
		t.Error("Request.UnmarshalJSON(trailing data) unexpectedly succeeded")
	}
	if !NumberID("invalid").Equal(NumberID("invalid")) {
		t.Error("identical invalid NumberID values do not compare equal")
	}

	for _, input := range []string{
		`{`,
		`{"jsonrpc":"2.0","error":null,"id":1}`,
		`{"jsonrpc":"2.0","error":1,"id":1}`,
		`{"jsonrpc":"2.0","result":1,"id":true}`,
	} {
		var response Response
		if err := json.Unmarshal([]byte(input), &response); err == nil {
			t.Errorf("Unmarshal(invalid response %s) unexpectedly succeeded", input)
		}
	}
	var malformedResponse Response
	if err := malformedResponse.UnmarshalJSON([]byte(`{`)); err == nil {
		t.Error("Response.UnmarshalJSON(malformed) unexpectedly succeeded")
	}
	if err := malformedResponse.UnmarshalJSON([]byte(`{} null`)); err == nil {
		t.Error("Response.UnmarshalJSON(trailing data) unexpectedly succeeded")
	}
	for _, input := range []string{"", `{]`, `{1}`, `{"member":}`} {
		if err := rejectDuplicateMembers([]byte(input)); err == nil {
			t.Errorf("rejectDuplicateMembers(%q) unexpectedly succeeded", input)
		}
	}
	if err := rejectDuplicateMembers([]byte(`null`)); err != nil {
		t.Errorf("rejectDuplicateMembers(non-object) error = %v", err)
	}
	for _, input := range []string{
		`{"code":"bad","message":"message"}`,
		`{"code":1,"message":2}`,
	} {
		var rpcErr Error
		if err := json.Unmarshal([]byte(input), &rpcErr); err == nil {
			t.Errorf("Unmarshal(invalid error %s) unexpectedly succeeded", input)
		}
	}

	response := Response{JSONRPC: Version, ID: NullID(), idSet: true, resultSet: true}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, encoded, []byte(`{"jsonrpc":"2.0","result":null,"id":null}`))

	invalidError := Response{
		JSONRPC:  Version,
		Error:    &Error{Code: 1},
		ID:       StringID("x"),
		errorSet: true,
		idSet:    true,
	}
	if err := invalidError.Validate(); err != nil {
		t.Errorf("Validate(programmatic error) error = %v", err)
	}

	rpcErr := NewError(1, "bad").WithData(make(chan int))
	if rpcErr.Unwrap() == nil {
		t.Error("WithData(unencodable) did not retain marshal error")
	}
}

func TestServerDefensivePaths(t *testing.T) {
	t.Parallel()

	dispatcher := NewDispatcher(nil)
	response, _ := dispatcher.Dispatch(context.Background(), []byte(`{"jsonrpc":"2.0","method":"missing","id":1}`))
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":1}`))

	registry := NewRegistry()
	_ = registry.Register("failure", func(context.Context, json.RawMessage) (any, error) {
		return nil, errors.New("failure")
	})
	_ = registry.Register("encode", func(context.Context, json.RawMessage) (any, error) {
		return make(chan int), nil
	})
	dispatcher = NewDispatcher(registry, WithMiddleware(nil))
	response, _ = dispatcher.Dispatch(context.Background(), []byte(`{"jsonrpc":"2.0","method":"failure","id":1}`))
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error"},"id":1}`))
	response, _ = dispatcher.Dispatch(context.Background(), []byte(`{"jsonrpc":"2.0","method":"encode","id":2}`))
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error"},"id":2}`))

	nilMapper := NewDispatcher(registry, WithErrorMapper(func(error) *Error { return nil }))
	response, _ = nilMapper.Dispatch(context.Background(), []byte(`{"jsonrpc":"2.0","method":"failure","id":3}`))
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error"},"id":3}`))

	fallback := marshalResponse(Response{
		JSONRPC:   Version,
		Result:    json.RawMessage(`{`),
		ID:        NumberID("1"),
		resultSet: true,
		idSet:     true,
	})
	assertJSONEqual(t, fallback, []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error"},"id":null}`))
}

func TestHTTPDefensivePaths(t *testing.T) {
	t.Parallel()

	if NewHTTPHandler(nil) == nil {
		t.Fatal("NewHTTPHandler(nil) returned nil")
	}
	withBody := (&HTTPStatusError{StatusCode: 500, Body: "failure"}).Error()
	withoutBody := (&HTTPStatusError{StatusCode: 500}).Error()
	if !strings.Contains(withBody, "failure") || strings.Contains(withoutBody, "failure") {
		t.Errorf("HTTPStatusError strings = %q and %q", withBody, withoutBody)
	}

	body := &failingBody{}
	transport, _ := NewHTTPTransport("http://example.test", WithHTTPClient(&http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       body,
			}, nil
		}),
	}))
	if _, err := transport.RoundTrip(context.Background(), []byte(`{}`)); err == nil {
		t.Error("RoundTrip(read error) unexpectedly succeeded")
	}
	if !body.closed {
		t.Error("RoundTrip(read error) did not close the response body")
	}
}

func TestCancellationPropagatesAcrossClientAndDispatcher(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	registry := NewRegistry()
	_ = registry.Register("observe", func(handlerContext context.Context, _ json.RawMessage) (any, error) {
		if !errors.Is(handlerContext.Err(), context.Canceled) {
			t.Errorf("handler context error = %v", handlerContext.Err())
		}
		return nil, nil
	})
	if _, ok := NewDispatcher(registry).Dispatch(ctx, []byte(`{"jsonrpc":"2.0","method":"observe","id":1}`)); !ok {
		t.Fatal("canceled dispatch unexpectedly omitted its response")
	}

	client := NewClient(TransportFunc(func(transportContext context.Context, _ []byte) ([]byte, error) {
		return nil, transportContext.Err()
	}))
	err := client.Notify(ctx, "observe", nil)
	if !errors.Is(err, ErrTransport) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Notify(canceled context) error = %v", err)
	}
}

type failingBody struct{ closed bool }

func (*failingBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (body *failingBody) Close() error {
	body.closed = true
	return nil
}
