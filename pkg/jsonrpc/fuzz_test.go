package jsonrpc

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type specificationFuzzCase struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

func loadSpecificationFuzzCases(f *testing.F) []specificationFuzzCase {
	f.Helper()
	fixture, err := os.ReadFile("testdata/conformance/jsonrpc-2.0-specification.json")
	if err != nil {
		f.Fatal(err)
	}
	var cases []specificationFuzzCase
	if err := json.Unmarshal(fixture, &cases); err != nil {
		f.Fatal(err)
	}
	return cases
}

func FuzzDispatcher(f *testing.F) {
	seeds := []string{
		`{"jsonrpc":"2.0","method":"ping","id":1}`,
		`[{"jsonrpc":"2.0","method":"ping"}]`,
		`{`,
		`null`,
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	for _, test := range loadSpecificationFuzzCases(f) {
		f.Add([]byte(test.Input))
	}
	f.Add(append([]byte(`{"jsonrpc":"2.0","method":"`), 0xff, '"', '}'))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"ping","id":1e` + strings.Repeat("9", 1024) + `}`))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"deep","params":` + strings.Repeat("[", 128) + `0` + strings.Repeat("]", 128) + `}`))
	f.Add(notificationBatch(128))
	dispatcher := NewDispatcher(NewRegistry())
	f.Fuzz(func(t *testing.T, payload []byte) {
		response, ok := dispatcher.Dispatch(context.Background(), payload)
		if ok && !json.Valid(response) {
			t.Fatalf("Dispatch() returned invalid JSON: %q", response)
		}
	})
}

func FuzzRequestUnmarshal(f *testing.F) {
	for _, seed := range []string{
		`{"jsonrpc":"2.0","method":"ping","id":1}`,
		`{"jsonrpc":"2.0","method":"ping"}`,
		`{`,
	} {
		f.Add([]byte(seed))
	}
	for _, test := range loadSpecificationFuzzCases(f) {
		f.Add([]byte(test.Input))
	}
	f.Add(append([]byte(`{"jsonrpc":"2.0","method":"`), 0xff, '"', '}'))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"ping","id":1e` + strings.Repeat("9", 1024) + `}`))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"deep","params":` + strings.Repeat("[", 128) + `0` + strings.Repeat("]", 128) + `}`))
	f.Fuzz(func(t *testing.T, payload []byte) {
		var request Request
		if json.Unmarshal(payload, &request) == nil {
			if _, err := json.Marshal(request); err != nil {
				t.Fatalf("valid request failed to marshal: %v", err)
			}
		}
	})
}

func FuzzResponseUnmarshal(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{"jsonrpc":"2.0","result":{"ok":true},"id":1}`),
		[]byte(`{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request","data":null},"id":null}`),
		[]byte(`{"jsonrpc":"2.0","result":1,"result":2,"id":1}`),
		append([]byte(`{"jsonrpc":"2.0","result":"`), 0xff, '"', ',', '"', 'i', 'd', '"', ':', '1', '}'),
	} {
		f.Add(seed)
	}
	for _, test := range loadSpecificationFuzzCases(f) {
		if test.Output != "" {
			f.Add([]byte(test.Output))
		}
	}
	f.Add([]byte(`{"jsonrpc":"2.0","result":` + strings.Repeat("[", 128) + `0` + strings.Repeat("]", 128) + `,"id":1}`))
	f.Fuzz(func(t *testing.T, payload []byte) {
		var response Response
		if json.Unmarshal(payload, &response) != nil {
			return
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("decoded response failed to marshal: %v", err)
		}
		if !json.Valid(encoded) {
			t.Fatalf("response marshaled as invalid JSON: %q", encoded)
		}
		_ = response.Validate()
	})
}

func FuzzErrorUnmarshal(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{"code":-32001,"message":"application error","data":{"retry":false}}`),
		[]byte(`{"code":-32603,"message":"Internal error","data":[null,true,1,"x"]}`),
		[]byte(`{"code":1,"code":2,"message":"duplicate"}`),
		append([]byte(`{"code":1,"message":"`), 0xff, '"', '}'),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, payload []byte) {
		var rpcErr Error
		if json.Unmarshal(payload, &rpcErr) != nil {
			return
		}
		encoded, err := json.Marshal(&rpcErr)
		if err != nil {
			t.Fatalf("decoded error failed to marshal: %v", err)
		}
		if !json.Valid(encoded) {
			t.Fatalf("error marshaled as invalid JSON: %q", encoded)
		}
	})
}

func FuzzIDRoundTrip(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`"request-1"`),
		[]byte(`null`),
		[]byte(`1`),
		[]byte(`1.0e+2`),
		[]byte(`1e` + strings.Repeat("9", 1024)),
		append([]byte{'"'}, 0xff, '"'),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, payload []byte) {
		var original ID
		if original.UnmarshalJSON(payload) != nil {
			return
		}
		encoded, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("decoded ID failed to marshal: %v", err)
		}
		var roundTripped ID
		if err := json.Unmarshal(encoded, &roundTripped); err != nil {
			t.Fatalf("marshaled ID failed to decode: %v", err)
		}
		if !original.Equal(roundTripped) || original.Kind() != roundTripped.Kind() {
			t.Fatalf("ID changed across round trip: %q -> %q", payload, encoded)
		}
	})
}

func FuzzClientCorrelation(f *testing.F) {
	for _, seed := range []string{
		`{"jsonrpc":"2.0","result":true,"id":1}`,
		`{"jsonrpc":"2.0","result":true,"id":1.0}`,
		`{"jsonrpc":"2.0","result":true,"id":2}`,
		`{"jsonrpc":"2.0","error":{"code":-32001,"message":"no"},"id":1}`,
		`{"jsonrpc":"2.0","result":true,"id":1,"id":2}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, reply []byte) {
		client := NewClient(TransportFunc(func(context.Context, []byte) ([]byte, error) {
			return reply, nil
		}), WithIDGenerator(fixedIDGenerator{id: NumberID("1")}))
		var result any
		err := client.Call(context.Background(), "probe", nil, &result)
		if err == nil && len(reply) > int(defaultMaxClientResponseBytes) {
			t.Fatal("oversized response unexpectedly succeeded")
		}
	})
}

func FuzzClientBatchCorrelation(f *testing.F) {
	for _, seed := range []string{
		`[{"jsonrpc":"2.0","result":"a","id":1},{"jsonrpc":"2.0","result":"b","id":2}]`,
		`[{"jsonrpc":"2.0","result":"b","id":2},{"jsonrpc":"2.0","result":"a","id":1}]`,
		`[{"jsonrpc":"2.0","result":"a","id":1},{"jsonrpc":"2.0","result":"again","id":1}]`,
		`[{"jsonrpc":"2.0","result":"unknown","id":3}]`,
		`[]`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, reply []byte) {
		client := NewClient(TransportFunc(func(context.Context, []byte) ([]byte, error) {
			return reply, nil
		}), WithIDGenerator(NewAtomicIDGenerator(0)))
		var first, second any
		err := client.Batch(context.Background(),
			&BatchCall{Method: "first", Result: &first},
			&BatchCall{Method: "second", Result: &second},
		)
		if err == nil {
			var responses []Response
			if json.Unmarshal(reply, &responses) != nil || len(responses) != 2 {
				t.Fatalf("batch accepted a non-two-response reply: %q", reply)
			}
		}
	})
}
