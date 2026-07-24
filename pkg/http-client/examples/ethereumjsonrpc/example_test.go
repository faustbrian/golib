package ethereumjsonrpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	httpclient "github.com/faustbrian/golib/pkg/http-client"
)

func Example() {
	type rpcRequest struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int64  `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}
	type rpcError struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	type rpcResponse struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int64           `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *rpcError       `json:"error"`
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
			http.Error(writer, "unexpected request", http.StatusBadRequest)
			return
		}
		var requestBody rpcRequest
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil ||
			requestBody.Method != "eth_blockNumber" {
			http.Error(writer, "invalid JSON-RPC request", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"result":"0x10"}`)
	}))
	defer server.Close()

	client, err := httpclient.New(httpclient.Config{
		Profile:   httpclient.PolicyProfileInteractiveV1,
		Transport: server.Client().Transport,
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = client.Close() }()

	call := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		payload, marshalErr := json.Marshal(rpcRequest{
			JSONRPC: "2.0", ID: 1, Method: method, Params: params,
		})
		if marshalErr != nil {
			return nil, marshalErr
		}
		body, bodyErr := httpclient.NewBytesBody("application/json", payload)
		if bodyErr != nil {
			return nil, bodyErr
		}
		spec, specErr := httpclient.NewRequestSpec(server.URL+"/", "")
		if specErr != nil {
			return nil, specErr
		}
		spec, specErr = spec.WithBody(body)
		if specErr != nil {
			return nil, specErr
		}
		request, requestErr := spec.Build(ctx, http.MethodPost)
		if requestErr != nil {
			return nil, requestErr
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			return nil, requestErr
		}
		if statusErr := httpclient.ClassifyResponse(response, httpclient.StatusOptions{}); statusErr != nil {
			return nil, statusErr
		}
		decoded, decodeErr := httpclient.DecodeJSONResponse[rpcResponse](
			response,
			httpclient.DecodeOptions{MaximumBodyBytes: 1 << 20},
		)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if decoded.Error != nil {
			return nil, errors.New("JSON-RPC call rejected")
		}
		return append(json.RawMessage(nil), decoded.Result...), nil
	}

	result, err := call(context.Background(), "eth_blockNumber", []any{})
	if err != nil {
		panic(err)
	}
	fmt.Println(string(result))
	// Output:
	// "0x10"
}
