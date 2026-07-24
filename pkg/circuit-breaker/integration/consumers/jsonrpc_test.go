package consumerintegration_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	jsonrpc "github.com/faustbrian/golib/pkg/jsonrpc"
)

func TestJSONRPCClientSeparatesLocalValidationAndTransportFailure(t *testing.T) {
	t.Parallel()

	errDependency := errors.New("RPC peer unavailable")
	var transportCalls atomic.Int64
	client := jsonrpc.NewClient(jsonrpc.TransportFunc(func(context.Context, []byte) ([]byte, error) {
		transportCalls.Add(1)

		return nil, errDependency
	}))
	circuit := newConsumerCircuit(t, "jsonrpc", func(completion breaker.Completion) breaker.Outcome {
		switch {
		case errors.Is(completion.Err, jsonrpc.ErrInvalidMethodName):
			return breaker.OutcomeIgnored
		case errors.Is(completion.Err, jsonrpc.ErrTransport):
			return breaker.OutcomeFailure
		case completion.Err != nil:
			return breaker.OutcomeIgnored
		default:
			return breaker.OutcomeSuccess
		}
	})

	_, err := breaker.Execute(context.Background(), circuit, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, client.Call(ctx, "rpc.reserved", nil, nil)
	})
	if !errors.Is(err, jsonrpc.ErrInvalidMethodName) || transportCalls.Load() != 0 {
		t.Fatalf("local validation = %v, transport calls = %d", err, transportCalls.Load())
	}
	if snapshot := circuit.Snapshot(); snapshot.TotalIgnored != 1 || snapshot.State != breaker.StateClosed {
		t.Fatalf("local validation snapshot = %+v", snapshot)
	}

	_, err = breaker.Execute(context.Background(), circuit, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, client.Call(ctx, "inventory.get", nil, nil)
	})
	if !errors.Is(err, jsonrpc.ErrTransport) || !errors.Is(err, errDependency) {
		t.Fatalf("transport failure = %v", err)
	}
	if snapshot := circuit.Snapshot(); snapshot.TotalFailures != 1 || snapshot.State != breaker.StateOpen {
		t.Fatalf("transport failure snapshot = %+v", snapshot)
	}
	_, err = breaker.Execute(context.Background(), circuit, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, client.Call(ctx, "inventory.get", nil, nil)
	})
	if !errors.Is(err, breaker.ErrOpen) || transportCalls.Load() != 1 {
		t.Fatalf("open rejection = %v, transport calls = %d", err, transportCalls.Load())
	}
}
