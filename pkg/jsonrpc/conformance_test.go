package jsonrpc

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// TestSpecificationExamples covers the normative request/response examples in
// the JSON-RPC 2.0 specification, including named parameters and mixed batches.
// See https://www.jsonrpc.org/specification#examples.
func TestSpecificationExamples(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	_ = registry.Register("subtract", func(_ context.Context, params json.RawMessage) (any, error) {
		var positional []int
		if len(params) > 0 && params[0] == '[' {
			if err := json.Unmarshal(params, &positional); err != nil || len(positional) != 2 {
				return nil, InvalidParams()
			}
			return positional[0] - positional[1], nil
		}
		var named struct {
			Minuend    int `json:"minuend"`
			Subtrahend int `json:"subtrahend"`
		}
		if err := json.Unmarshal(params, &named); err != nil {
			return nil, InvalidParams()
		}
		return named.Minuend - named.Subtrahend, nil
	})
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
	_ = registry.Register("get_data", func(context.Context, json.RawMessage) (any, error) {
		return []any{"hello", 5}, nil
	})
	dispatcher := NewDispatcher(registry)

	fixture, err := os.ReadFile("testdata/conformance/jsonrpc-2.0-specification.json")
	if err != nil {
		t.Fatal(err)
	}
	var tests []struct {
		Name   string `json:"name"`
		Input  string `json:"input"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(fixture, &tests); err != nil {
		t.Fatalf("decode specification fixture: %v", err)
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			response, ok := dispatcher.Dispatch(context.Background(), []byte(test.Input))
			if test.Output == "" {
				if ok || len(response) != 0 {
					t.Fatalf("notification response = %q, %v", response, ok)
				}
				return
			}
			if !ok {
				t.Fatal("specification request unexpectedly omitted its response")
			}
			assertJSONEqual(t, response, []byte(test.Output))
		})
	}
}
