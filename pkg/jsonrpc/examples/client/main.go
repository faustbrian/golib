package main

import (
	"context"
	"fmt"
	"log"

	jsonrpc "github.com/faustbrian/golib/pkg/jsonrpc"
)

func main() {
	transport, err := jsonrpc.NewHTTPTransport("http://localhost:8080/rpc")
	if err != nil {
		log.Fatal(err)
	}
	client := jsonrpc.NewClient(transport)

	type greeting struct {
		Message string `json:"message"`
	}
	result, err := jsonrpc.Call[greeting](
		context.Background(),
		client,
		"greet",
		map[string]string{"name": "Ada"},
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Message)
}
