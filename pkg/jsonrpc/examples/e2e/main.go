package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http/httptest"

	jsonrpc "github.com/faustbrian/golib/pkg/jsonrpc"
)

func main() {
	registry := jsonrpc.NewRegistry()
	if err := registry.Register("add", func(_ context.Context, raw json.RawMessage) (any, error) {
		values, rpcErr := jsonrpc.DecodeParams[[]int](raw)
		if rpcErr != nil || len(values) != 2 {
			return nil, jsonrpc.InvalidParams()
		}
		return values[0] + values[1], nil
	}); err != nil {
		log.Fatal(err)
	}

	server := httptest.NewServer(jsonrpc.NewHTTPHandler(jsonrpc.NewDispatcher(registry)))
	defer server.Close()
	transport, err := jsonrpc.NewHTTPTransport(server.URL)
	if err != nil {
		log.Fatal(err)
	}
	client := jsonrpc.NewClient(transport)
	result, err := jsonrpc.Call[int](context.Background(), client, "add", []int{20, 22})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result)
}
