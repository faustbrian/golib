package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestIDRepresentationBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   ID
		want bool
	}{
		{name: "empty string", id: ID{kind: IDString, raw: json.RawMessage(`""`)}, want: true},
		{name: "string kind with number", id: ID{kind: IDString, raw: json.RawMessage(`0`)}},
		{name: "empty number", id: ID{kind: IDNumber}},
		{name: "negative number", id: ID{kind: IDNumber, raw: json.RawMessage(`-1`)}, want: true},
		{name: "lower numeric boundary", id: ID{kind: IDNumber, raw: json.RawMessage(`0`)}, want: true},
		{name: "upper numeric boundary", id: ID{kind: IDNumber, raw: json.RawMessage(`9`)}, want: true},
		{name: "number kind with string", id: ID{kind: IDNumber, raw: json.RawMessage(`"9"`)}},
		{name: "spaced null", id: ID{kind: IDNull, raw: json.RawMessage(` null `)}, want: true},
		{name: "null kind with boolean", id: ID{kind: IDNull, raw: json.RawMessage(`true`)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := test.id.valid(); got != test.want {
				t.Fatalf("ID.valid() = %v, want %v for kind %d and raw %q", got, test.want, test.id.kind, test.id.raw)
			}
		})
	}
}

func TestNumericIDCanonicalBoundaries(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":          "",
		"-":         "-",
		"9":         "9",
		"1.9":       "19e-1",
		"1E2":       "1e2",
		"1e+2":      "1e2",
		"1e-2":      "1e-2",
		"1e0":       "1",
		"90":        "9e1",
		"100":       "1e2",
		"10.0":      "1e1",
		"0.00100":   "1e-3",
		"-0.00100":  "-1e-3",
		"1e":        "1e",
		"1e+":       "1e+",
		"1e1x":      "1e1x",
		"1.0suffix": "1.0suffix",
	}
	for input, want := range tests {
		if got := canonicalNumber(input); got != want {
			t.Errorf("canonicalNumber(%q) = %q, want %q", input, got, want)
		}
	}

	arithmetic := []struct {
		name       string
		sign       int
		digits     string
		adjustment int
		wantSign   int
		wantDigits string
	}{
		{name: "zero adjustment", sign: 1, digits: "7", wantSign: 1, wantDigits: "7"},
		{name: "zero plus positive", digits: "0", adjustment: 1, wantSign: 1, wantDigits: "1"},
		{name: "zero plus negative", digits: "0", adjustment: -1, wantSign: -1, wantDigits: "1"},
		{name: "same sign", sign: 1, digits: "9", adjustment: 1, wantSign: 1, wantDigits: "10"},
		{name: "opposite equal", sign: 1, digits: "1", adjustment: -1, wantDigits: "0"},
		{name: "existing magnitude greater", sign: 1, digits: "2", adjustment: -1, wantSign: 1, wantDigits: "1"},
		{name: "adjustment magnitude greater", sign: 1, digits: "1", adjustment: -2, wantSign: -1, wantDigits: "1"},
	}
	for _, test := range arithmetic {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			sign, digits := addSignedDecimalInt(test.sign, test.digits, test.adjustment)
			if sign != test.wantSign || digits != test.wantDigits {
				t.Fatalf("addSignedDecimalInt() = (%d, %q), want (%d, %q)", sign, digits, test.wantSign, test.wantDigits)
			}
		})
	}
}

func TestRequestAndResponsePresenceBoundaries(t *testing.T) {
	t.Parallel()

	requestWithProgrammaticID := Request{JSONRPC: Version, Method: "call", ID: StringID("request")}
	encoded, err := json.Marshal(requestWithProgrammaticID)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, encoded, []byte(`{"jsonrpc":"2.0","method":"call","id":"request"}`))

	requestWithExplicitMissingID := Request{JSONRPC: Version, Method: "call", idSet: true}
	encoded, err = json.Marshal(requestWithExplicitMissingID)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, encoded, []byte(`{"jsonrpc":"2.0","method":"call","id":null}`))

	notificationCases := []struct {
		name string
		item Request
		want bool
	}{
		{name: "omitted", item: Request{}, want: true},
		{name: "explicit missing", item: Request{idSet: true}},
		{name: "programmatic id", item: Request{ID: NumberID("1")}},
		{name: "explicit id", item: Request{ID: NumberID("1"), idSet: true}},
	}
	for _, test := range notificationCases {
		if got := test.item.IsNotification(); got != test.want {
			t.Errorf("%s IsNotification() = %v, want %v", test.name, got, test.want)
		}
	}

	responseWithExplicitNilError := Response{JSONRPC: Version, ID: NullID(), idSet: true, errorSet: true}
	encoded, err = json.Marshal(responseWithExplicitNilError)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, encoded, []byte(`{"jsonrpc":"2.0","error":null,"id":null}`))

	responseWithProgrammaticError := Response{JSONRPC: Version, ID: NumberID("1"), idSet: true, Error: NewError(1, "failure")}
	encoded, err = json.Marshal(responseWithProgrammaticError)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, encoded, []byte(`{"jsonrpc":"2.0","error":{"code":1,"message":"failure"},"id":1}`))
}

func TestClientAndHTTPConfigurationBoundaries(t *testing.T) {
	t.Parallel()

	handler := NewHTTPHandler(nil, WithMaxRequestBytes(1), WithMaxRequestBytes(0), WithMaxRequestBytes(-1))
	if handler.maxRequestBytes != 1 {
		t.Fatalf("max request bytes = %d, want 1", handler.maxRequestBytes)
	}

	customClient := &http.Client{}
	transport, err := NewHTTPTransport(
		"http://example.test",
		WithHTTPClient(customClient),
		WithHTTPClient(nil),
		WithMaxResponseBytes(4),
		WithMaxResponseBytes(0),
		WithMaxResponseBytes(-1),
	)
	if err != nil {
		t.Fatal(err)
	}
	if transport.client != customClient {
		t.Fatal("nil HTTP client option replaced the configured client")
	}
	if transport.maxResponseBytes != 4 {
		t.Fatalf("max response bytes = %d, want 4", transport.maxResponseBytes)
	}

	transport.client = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader("null")),
		}, nil
	})}
	body, err := transport.RoundTrip(context.Background(), []byte(`{}`))
	if err != nil || string(body) != "null" {
		t.Fatalf("RoundTrip(exact limit) = (%q, %v), want null, nil", body, err)
	}

	for name, response := range map[string][]byte{"empty": nil, "object": []byte(`{}`)} {
		client := NewClient(TransportFunc(func(context.Context, []byte) ([]byte, error) { return response, nil }))
		err := client.Batch(context.Background(), &BatchCall{Method: "call"})
		if !errors.Is(err, ErrInvalidResponse) {
			t.Errorf("Batch(%s response) error = %v, want invalid response", name, err)
		}
	}
}

func TestDispatcherScannerAndHookBoundaries(t *testing.T) {
	t.Parallel()

	if exceedsNestingDepth([]byte(`{}{} `), 1) {
		t.Fatal("completed objects accumulated nesting depth")
	}
	if !exceedsNestingDepth([]byte(`{"text":"closed","nested":{}}`), 1) {
		t.Fatal("object after a closed string was not counted")
	}
	if exceedsNestingDepth([]byte(`"a{"`), 0) {
		t.Fatal("delimiter inside a string was counted as nesting")
	}

	dispatcher := NewDispatcher(nil)
	response, reply := dispatcher.dispatchItem(context.Background(), nil)
	if !reply || response.Error == nil || response.Error.Code != CodeInvalidRequest {
		t.Fatalf("dispatchItem(nil) = (%#v, %v), want invalid-request reply", response, reply)
	}

	type contextKey struct{}
	registry := NewRegistry()
	if err := registry.Register("context", func(ctx context.Context, _ json.RawMessage) (any, error) {
		if ctx == nil || ctx.Value(contextKey{}) != "original" {
			t.Fatalf("handler context = %#v, want original context", ctx)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	var failureHookCalled bool
	var failureRequestWasNil bool
	dispatcher = NewDispatcher(registry, WithHooks(Hooks{
		OnRequest: func(_ context.Context, request *Request) context.Context {
			if request == nil {
				failureHookCalled = true
				failureRequestWasNil = true
				return context.Background()
			}
			return nil
		},
	}))
	ctx := context.WithValue(context.Background(), contextKey{}, "original")
	encoded, ok := dispatcher.Dispatch(ctx, []byte(`{"jsonrpc":"2.0","method":"context","id":1}`))
	if !ok {
		t.Fatal("valid request omitted response")
	}
	assertJSONEqual(t, encoded, []byte(`{"jsonrpc":"2.0","result":true,"id":1}`))
	dispatcher.Dispatch(ctx, []byte(`{`))
	if !failureHookCalled || !failureRequestWasNil {
		t.Fatal("protocol failure hook did not receive a nil request")
	}
}

func TestErrorResponseAndParameterNameBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *Error
		want int
	}{
		{name: "nil", want: CodeInternalError},
		{name: "decoded incomplete", err: &Error{decoded: true}, want: CodeInternalError},
		{name: "valid data", err: NewError(1, "failure").WithData("safe"), want: 1},
		{name: "malformed data", err: &Error{Code: 1, Message: "failure", Data: json.RawMessage(`{`)}, want: CodeInternalError},
	}
	for _, test := range tests {
		response := errorResponse(NumberID("1"), test.err)
		if response.Error == nil || response.Error.Code != test.want {
			t.Errorf("errorResponse(%s) = %#v, want code %d", test.name, response.Error, test.want)
		}
	}

	type Embedded struct {
		Promoted string `json:"promoted"`
	}
	type parameters struct {
		Embedded
		Ignored      string `json:"-"`
		private      string
		Named        Embedded `json:"named"`
		Tail         string   `json:"tail"`
		Public       string
		PublicStruct Embedded
	}
	fixture := parameters{private: "private"}
	names, structured := parameterNameSet(reflect.TypeOf(fixture))
	if !structured {
		t.Fatal("struct parameters were not classified as structured")
	}
	want := map[string]struct{}{
		"promoted":     {},
		"named":        {},
		"tail":         {},
		"Public":       {},
		"PublicStruct": {},
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("parameter names = %#v, want %#v", names, want)
	}
	if _, exposed := names[fixture.private]; exposed {
		t.Fatal("unexported field was accepted as a named parameter")
	}
}
