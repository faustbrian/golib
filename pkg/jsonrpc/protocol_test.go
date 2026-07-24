package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestIDRoundTripAndEquality(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		kind  IDKind
	}{
		{name: "string", input: `"request-1"`, kind: IDString},
		{name: "integer", input: `1`, kind: IDNumber},
		{name: "fractional", input: `1.25`, kind: IDNumber},
		{name: "null", input: `null`, kind: IDNull},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var id ID
			if err := json.Unmarshal([]byte(tt.input), &id); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if id.Kind() != tt.kind {
				t.Fatalf("Kind() = %v, want %v", id.Kind(), tt.kind)
			}
			encoded, err := json.Marshal(id)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(encoded) != tt.input {
				t.Errorf("Marshal() = %s, want %s", encoded, tt.input)
			}
			if !id.Equal(id) {
				t.Error("Equal() = false for same ID")
			}
		})
	}

	if StringID("1").Equal(NumberID(json.Number("1"))) {
		t.Error("string and number IDs must not compare equal")
	}
	var escapedString, decimalNumber, exponentNumber ID
	for input, target := range map[string]*ID{
		`"\u0061"`: &escapedString,
		`1.0`:      &decimalNumber,
		`1e0`:      &exponentNumber,
	} {
		if err := json.Unmarshal([]byte(input), target); err != nil {
			t.Fatal(err)
		}
	}
	if !StringID("a").Equal(escapedString) {
		t.Error("equivalent escaped string IDs do not compare equal")
	}
	if !NumberID("1").Equal(decimalNumber) || !NumberID("1").Equal(exponentNumber) {
		t.Error("equivalent numeric IDs do not compare equal")
	}
}

func TestStringIDCorrelationMatchesItsJSONEncoding(t *testing.T) {
	t.Parallel()

	original := StringID(string([]byte{0xff, 0xfe}))
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ID
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !original.Equal(decoded) {
		t.Fatalf("StringID did not match its wire value: %q", encoded)
	}
}

func TestIDRejectsInvalidJSONTypes(t *testing.T) {
	t.Parallel()

	for _, input := range []string{`true`, `false`, `{}`, `[]`} {
		var id ID
		if err := json.Unmarshal([]byte(input), &id); err == nil {
			t.Errorf("Unmarshal(%s) unexpectedly succeeded", input)
		}
	}
}

func TestIDNumberCanonicalizationIsBounded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		left  string
		right string
	}{
		{left: "1e1000000", right: "10e999999"},
		{left: "1.2300e2", right: "123"},
		{left: "-0", right: "0e999999"},
	}
	for _, test := range tests {
		var left, right ID
		if err := json.Unmarshal([]byte(test.left), &left); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(test.right), &right); err != nil {
			t.Fatal(err)
		}
		if !left.Equal(right) {
			t.Errorf("numeric IDs %s and %s do not compare equal", test.left, test.right)
		}
		if len(left.canonical) > len(test.left)+8 {
			t.Errorf("canonical %s expanded from %d to %d bytes", test.left, len(test.left), len(left.canonical))
		}
	}
	for _, invalid := range []string{"-", "1.", "1e+", "1e1x", "1x"} {
		if got := canonicalNumber(invalid); got != invalid {
			t.Errorf("canonicalNumber(%q) = %q", invalid, got)
		}
	}
	if canonicalNumber("1e+1") != canonicalNumber("10") || canonicalNumber("1e-1") != canonicalNumber("0.1") {
		t.Error("signed exponents do not canonicalize by value")
	}
}

func TestIDCanonicalizationAllocationsDoNotScaleWithExponentDigits(t *testing.T) {
	payload := []byte(`1e` + strings.Repeat("9", 64<<10))
	allocations := testing.AllocsPerRun(1, func() {
		var id ID
		if err := json.Unmarshal(payload, &id); err != nil {
			t.Fatal(err)
		}
	})
	if allocations > 50 {
		t.Fatalf("64-KiB exponent used %.0f allocations, want at most 50", allocations)
	}
}

func TestDecimalExponentArithmetic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sign       int
		digits     string
		adjustment int
		wantSign   int
		wantDigits string
	}{
		{name: "positive borrow", sign: 1, digits: "1000", adjustment: -1, wantSign: 1, wantDigits: "999"},
		{name: "crosses positive", sign: -1, digits: "1", adjustment: 2, wantSign: 1, wantDigits: "1"},
		{name: "cancels", sign: 1, digits: "1", adjustment: -1, wantSign: 0, wantDigits: "0"},
		{name: "negative carry", sign: -1, digits: "9", adjustment: -1, wantSign: -1, wantDigits: "10"},
		{name: "positive no carry", sign: 1, digits: "8", adjustment: 1, wantSign: 1, wantDigits: "9"},
		{name: "long adjustment", sign: 1, digits: "1", adjustment: 1000, wantSign: 1, wantDigits: "1001"},
		{name: "crosses long adjustment", sign: -1, digits: "9", adjustment: 1000, wantSign: 1, wantDigits: "991"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			sign, digits := addSignedDecimalInt(test.sign, test.digits, test.adjustment)
			if sign != test.wantSign || digits != test.wantDigits {
				t.Fatalf("addSignedDecimalInt() = (%d, %q), want (%d, %q)", sign, digits, test.wantSign, test.wantDigits)
			}
		})
	}

	exponent := strings.Repeat("9", 64<<10)
	previous := exponent[:len(exponent)-1] + "8"
	var left, right ID
	if err := json.Unmarshal([]byte(`1e`+exponent), &left); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`10e`+previous), &right); err != nil {
		t.Fatal(err)
	}
	if !left.Equal(right) {
		t.Fatal("long mathematically equivalent exponents do not compare equal")
	}
}

func TestRequestValidation(t *testing.T) {
	t.Parallel()

	valid := []string{
		`{"jsonrpc":"2.0","method":"subtract","params":[42,23],"id":1}`,
		`{"jsonrpc":"2.0","method":"subtract","params":{"minuend":42},"id":"a"}`,
		`{"jsonrpc":"2.0","method":"update"}`,
		`{"jsonrpc":"2.0","method":"update","id":null}`,
		`{"jsonrpc":"2.0","method":"","id":1}`,
	}
	for _, input := range valid {
		var request Request
		if err := json.Unmarshal([]byte(input), &request); err != nil {
			t.Errorf("Unmarshal(valid %s) error = %v", input, err)
			continue
		}
		if rpcErr := request.Validate(); rpcErr != nil {
			t.Errorf("Validate(valid %s) = %v", input, rpcErr)
		}
	}

	invalid := []string{
		`{"jsonrpc":"1.0","method":"x","id":1}`,
		`{"jsonrpc":"2.0","id":1}`,
		`{"jsonrpc":"2.0","method":null,"id":1}`,
		`{"jsonrpc":"2.0","method":"x","params":"bad","id":1}`,
		`{"jsonrpc":"2.0","method":"x","params":null,"id":1}`,
	}
	for _, input := range invalid {
		var request Request
		if err := json.Unmarshal([]byte(input), &request); err != nil {
			continue
		}
		if rpcErr := request.Validate(); rpcErr == nil || rpcErr.Code != CodeInvalidRequest {
			t.Errorf("Validate(invalid %s) = %v, want invalid request", input, rpcErr)
		}
	}
}

func TestRequestDistinguishesNotificationFromNullID(t *testing.T) {
	t.Parallel()

	var notification Request
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"update"}`), &notification); err != nil {
		t.Fatal(err)
	}
	if !notification.IsNotification() {
		t.Error("request without id is not recognized as notification")
	}

	var nullID Request
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"update","id":null}`), &nullID); err != nil {
		t.Fatal(err)
	}
	if nullID.IsNotification() {
		t.Error("request with explicit null id must not be a notification")
	}
}

func TestRequestUnmarshalClearsReusedState(t *testing.T) {
	t.Parallel()

	var request Request
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"first","id":1}`), &request); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":2}`), &request); err != nil {
		t.Fatal(err)
	}
	if request.Method != "" || request.Validate() == nil {
		t.Errorf("reused request retained stale method: %#v", request)
	}
}

func TestErrorUnmarshalClearsReusedState(t *testing.T) {
	t.Parallel()

	var rpcErr Error
	if err := json.Unmarshal([]byte(`{"code":1,"message":"first","data":{"safe":true}}`), &rpcErr); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"message":"second"}`), &rpcErr); err != nil {
		t.Fatal(err)
	}
	if rpcErr.Code != 0 || rpcErr.Data != nil || rpcErr.valid() {
		t.Errorf("reused error retained stale state: %#v", rpcErr)
	}
}

func TestResponseValidation(t *testing.T) {
	t.Parallel()

	valid := []string{
		`{"jsonrpc":"2.0","result":19,"id":1}`,
		`{"jsonrpc":"2.0","result":null,"id":"a"}`,
		`{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":1}`,
		`{"jsonrpc":"2.0","error":{"code":0,"message":""},"id":1}`,
	}
	for _, input := range valid {
		var response Response
		if err := json.Unmarshal([]byte(input), &response); err != nil {
			t.Errorf("Unmarshal(valid %s) error = %v", input, err)
			continue
		}
		if err := response.Validate(); err != nil {
			t.Errorf("Validate(valid %s) error = %v", input, err)
		}
	}

	invalid := []string{
		`{"jsonrpc":"1.0","result":19,"id":1}`,
		`{"jsonrpc":"2.0","id":1}`,
		`{"jsonrpc":"2.0","result":19,"error":{"code":-32603,"message":"Internal error"},"id":1}`,
		`{"jsonrpc":"2.0","result":19}`,
		`{"jsonrpc":"2.0","error":{"message":"missing code"},"id":1}`,
		`{"jsonrpc":"2.0","error":{"code":1},"id":1}`,
		`{"jsonrpc":"2.0","error":{"code":null,"message":"bad"},"id":1}`,
		`{"jsonrpc":"2.0","error":{"code":1,"message":null},"id":1}`,
	}
	for _, input := range invalid {
		var response Response
		if err := json.Unmarshal([]byte(input), &response); err != nil {
			continue
		}
		if err := response.Validate(); err == nil {
			t.Errorf("Validate(invalid %s) unexpectedly succeeded", input)
		}
	}
}

func TestResponseUnmarshalClearsReusedState(t *testing.T) {
	t.Parallel()

	var response Response
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","result":1,"id":1}`), &response); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","error":{"code":1,"message":"bad"},"id":2}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.Result != nil || response.Error == nil || !response.ID.Equal(NumberID("2")) {
		t.Errorf("reused error response retained stale state: %#v", response)
	}
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","result":3,"id":3}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil || !response.ID.Equal(NumberID("3")) {
		t.Errorf("reused result response retained stale state: %#v", response)
	}
}

func TestProtocolDecodersRejectDuplicateMembers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		decode func([]byte) error
	}{
		{
			name:  "request jsonrpc",
			input: `{"jsonrpc":"2.0","jsonrpc":"1.0","method":"ping","id":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Request))
			},
		},
		{
			name:  "request method",
			input: `{"jsonrpc":"2.0","method":"ping","method":"other","id":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Request))
			},
		},
		{
			name:  "request params",
			input: `{"jsonrpc":"2.0","method":"ping","params":[],"params":{},"id":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Request))
			},
		},
		{
			name:  "request id",
			input: `{"jsonrpc":"2.0","method":"ping","id":1,"id":2}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Request))
			},
		},
		{
			name:  "response jsonrpc",
			input: `{"jsonrpc":"2.0","jsonrpc":"1.0","result":1,"id":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Response))
			},
		},
		{
			name:  "response result",
			input: `{"jsonrpc":"2.0","result":1,"result":2,"id":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Response))
			},
		},
		{
			name:  "response error",
			input: `{"jsonrpc":"2.0","error":{"code":1,"message":"a"},"error":{"code":2,"message":"b"},"id":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Response))
			},
		},
		{
			name:  "response id",
			input: `{"jsonrpc":"2.0","result":1,"id":1,"id":2}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Response))
			},
		},
		{
			name:  "error code",
			input: `{"code":1,"code":2,"message":"failure"}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Error))
			},
		},
		{
			name:  "error message",
			input: `{"code":1,"message":"a","message":"b"}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Error))
			},
		},
		{
			name:  "error data",
			input: `{"code":1,"message":"failure","data":1,"data":2}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Error))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := test.decode([]byte(test.input)); err == nil {
				t.Fatalf("decoding duplicate members unexpectedly succeeded: %s", test.input)
			}
		})
	}
}

func TestProtocolDecodersRejectCaseVariantsOfReservedMembers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		decode func([]byte) error
	}{
		{
			name:  "request jsonrpc",
			input: `{"JSONRPC":"2.0","method":"ping","id":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Request))
			},
		},
		{
			name:  "request method",
			input: `{"jsonrpc":"2.0","Method":"ping","id":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Request))
			},
		},
		{
			name:  "request params",
			input: `{"jsonrpc":"2.0","method":"ping","Params":[],"id":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Request))
			},
		},
		{
			name:  "request id",
			input: `{"jsonrpc":"2.0","method":"ping","Id":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Request))
			},
		},
		{
			name:  "response jsonrpc",
			input: `{"JSONRPC":"2.0","result":1,"id":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Response))
			},
		},
		{
			name:  "response result",
			input: `{"jsonrpc":"2.0","Result":1,"id":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Response))
			},
		},
		{
			name:  "response error",
			input: `{"jsonrpc":"2.0","Error":{"code":1,"message":"failure"},"id":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Response))
			},
		},
		{
			name:  "response id",
			input: `{"jsonrpc":"2.0","result":1,"Id":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Response))
			},
		},
		{
			name:  "error code",
			input: `{"Code":1,"message":"failure"}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Error))
			},
		},
		{
			name:  "error message",
			input: `{"code":1,"Message":"failure"}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Error))
			},
		},
		{
			name:  "error data",
			input: `{"code":1,"message":"failure","Data":1}`,
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Error))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := test.decode([]byte(test.input)); err == nil {
				t.Fatalf("decoding reserved member case variant unexpectedly succeeded: %s", test.input)
			}
		})
	}

	for name, decode := range map[string]func([]byte) error{
		"request":  func(data []byte) error { return json.Unmarshal(data, new(Request)) },
		"response": func(data []byte) error { return json.Unmarshal(data, new(Response)) },
		"error":    func(data []byte) error { return json.Unmarshal(data, new(Error)) },
	} {
		t.Run(name+" unknown extension", func(t *testing.T) {
			t.Parallel()

			if err := decode([]byte(`{"extension":true}`)); err != nil {
				t.Fatalf("decoding unknown extension member failed: %v", err)
			}
		})
	}
}

func TestProtocolRejectsInvalidUTF8(t *testing.T) {
	t.Parallel()

	invalidString := func(prefix, suffix string) []byte {
		value := append([]byte(prefix), 0xff)
		return append(value, suffix...)
	}
	tests := []struct {
		name   string
		input  []byte
		decode func([]byte) error
	}{
		{
			name:  "id",
			input: invalidString(`"`, `"`),
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(ID))
			},
		},
		{
			name:  "request method",
			input: invalidString(`{"jsonrpc":"2.0","method":"`, `","id":1}`),
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Request))
			},
		},
		{
			name:  "request params",
			input: invalidString(`{"jsonrpc":"2.0","method":"ping","params":{"value":"`, `"},"id":1}`),
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Request))
			},
		},
		{
			name:  "response result",
			input: invalidString(`{"jsonrpc":"2.0","result":"`, `","id":1}`),
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Response))
			},
		},
		{
			name:  "error data",
			input: invalidString(`{"code":1,"message":"failure","data":"`, `"}`),
			decode: func(data []byte) error {
				return json.Unmarshal(data, new(Error))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := test.decode(test.input); err == nil {
				t.Fatalf("decoding invalid UTF-8 unexpectedly succeeded: %q", test.input)
			}
		})
	}

	payload := invalidString(`{"jsonrpc":"2.0","method":"`, `","id":1}`)
	response, ok := NewDispatcher(nil).Dispatch(context.Background(), payload)
	if !ok {
		t.Fatal("Dispatch() omitted the parse-error response")
	}
	assertJSONEqual(t, response, []byte(`{"jsonrpc":"2.0","error":{"code":-32700,"message":"Parse error"},"id":null}`))
}

func TestRPCErrorModel(t *testing.T) {
	t.Parallel()

	cause := errors.New("database unavailable")
	err := NewError(42, "application failure").WithData(map[string]string{"field": "name"}).WithCause(cause)
	if err.Code != 42 || err.Message != "application failure" {
		t.Fatalf("NewError() = %#v", err)
	}
	if !errors.Is(err, cause) {
		t.Error("RPC error does not preserve its internal cause")
	}
	encoded, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if string(encoded) != `{"code":42,"message":"application failure","data":{"field":"name"}}` {
		t.Errorf("Marshal() = %s", encoded)
	}
	if got := err.Error(); got != "jsonrpc error 42: application failure" {
		t.Errorf("Error() = %q", got)
	}
	_ = err.WithData(make(chan int))
	if err.Data != nil {
		t.Error("WithData(unencodable) retained stale public data")
	}
}

func TestStandardErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err     *Error
		code    int
		message string
	}{
		{RequestLimitExceeded(), CodeRequestLimitExceeded, "Request limit exceeded"},
		{ParseError(), CodeParseError, "Parse error"},
		{InvalidRequest(), CodeInvalidRequest, "Invalid Request"},
		{MethodNotFound(), CodeMethodNotFound, "Method not found"},
		{InvalidParams(), CodeInvalidParams, "Invalid params"},
		{InternalError(), CodeInternalError, "Internal error"},
	}
	for _, tt := range tests {
		if tt.err.Code != tt.code || tt.err.Message != tt.message {
			t.Errorf("error = %#v, want code %d and message %q", tt.err, tt.code, tt.message)
		}
	}
}
