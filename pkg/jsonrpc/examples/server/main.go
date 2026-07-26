package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	jsonrpc "github.com/faustbrian/golib/pkg/jsonrpc"
)

func main() {
	registry := jsonrpc.NewRegistry()
	if err := registry.Register("greet", greet); err != nil {
		log.Fatal(err)
	}

	handler := jsonrpc.NewHTTPHandler(jsonrpc.NewDispatcher(registry))
	server := &http.Server{
		Addr:              ":8080",
		Handler:           http.StripPrefix("/rpc", handler),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Println("JSON-RPC listening on http://localhost:8080/rpc")
	log.Fatal(server.ListenAndServe())
}

func greet(_ context.Context, raw json.RawMessage) (any, error) {
	params, rpcErr := jsonrpc.DecodeParams[struct {
		Name string `json:"name"`
	}](raw)
	if rpcErr != nil || params.Name == "" {
		return nil, jsonrpc.InvalidParams()
	}
	return map[string]string{"message": "Hello " + params.Name}, nil
}
