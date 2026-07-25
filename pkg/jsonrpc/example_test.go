package jsonrpc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"

	jsonrpc "github.com/faustbrian/golib/pkg/jsonrpc"
)

func Example() {
	registry := jsonrpc.NewRegistry()
	_ = registry.Register("add", func(_ context.Context, raw json.RawMessage) (any, error) {
		values, rpcErr := jsonrpc.DecodeParams[[]int](raw)
		if rpcErr != nil || len(values) != 2 {
			return nil, jsonrpc.InvalidParams()
		}
		return values[0] + values[1], nil
	})
	server := httptest.NewServer(jsonrpc.NewHTTPHandler(jsonrpc.NewDispatcher(registry)))
	defer server.Close()
	transport, _ := jsonrpc.NewHTTPTransport(server.URL)
	result, _ := jsonrpc.Call[int](context.Background(), jsonrpc.NewClient(transport), "add", []int{2, 3})
	fmt.Println(result)
	// Output: 5
}

func ExampleClient_Batch() {
	transport := jsonrpc.TransportFunc(func(context.Context, []byte) ([]byte, error) {
		return []byte(`[
			{"jsonrpc":"2.0","result":4,"id":2},
			{"jsonrpc":"2.0","result":2,"id":1}
		]`), nil
	})
	client := jsonrpc.NewClient(transport)
	var first, second int
	_ = client.Batch(context.Background(),
		&jsonrpc.BatchCall{Method: "double", Params: []int{1}, Result: &first},
		&jsonrpc.BatchCall{Method: "double", Params: []int{2}, Result: &second},
	)
	fmt.Println(first, second)
	// Output: 2 4
}

func ExampleWithMiddleware() {
	registry := jsonrpc.NewRegistry()
	_ = registry.Register("ping", func(context.Context, json.RawMessage) (any, error) {
		return "pong", nil
	})
	logging := func(next jsonrpc.Handler) jsonrpc.Handler {
		return func(ctx context.Context, params json.RawMessage) (any, error) {
			request, _ := jsonrpc.RequestFromContext(ctx)
			fmt.Println(request.Method)
			return next(ctx, params)
		}
	}
	dispatcher := jsonrpc.NewDispatcher(registry, jsonrpc.WithMiddleware(logging))
	dispatcher.Dispatch(context.Background(), []byte(`{"jsonrpc":"2.0","method":"ping","id":1}`))
	// Output: ping
}
