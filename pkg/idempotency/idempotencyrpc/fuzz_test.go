package idempotencyrpc_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencyrpc"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
)

func FuzzMalformedReplayFailsClosed(f *testing.F) {
	f.Add([]byte("not-json"))
	f.Add([]byte(`{"schema":1,"response":{"result":true}}`))
	f.Add([]byte(`{"schema":1,"response":{"error":{"code":-1,"message":"bad"}}}`))
	request := idempotencyrpc.Request{Method: "widgets.create", Params: json.RawMessage(`{}`)}
	key, err := idempotency.NewKey("rpc", "tenant", request.Method, "caller", "key")
	if err != nil {
		f.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("rpc-v1", request.Params)
	if err != nil {
		f.Fatalf("NewFingerprint() error = %v", err)
	}
	f.Fuzz(func(t *testing.T, payload []byte) {
		base, err := memory.New(memory.Options{
			Clock:       idempotencytest.NewClock(time.Unix(1_700_000_000, 0).UTC()),
			OwnerTokens: idempotencytest.NewTokenSource("fuzz-owner").Next,
		})
		if err != nil {
			t.Fatalf("memory.New() error = %v", err)
		}
		store := &storeOverride{Store: base}
		store.acquire = func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
			return idempotency.AcquireResult{
				Outcome: idempotency.OutcomeReplayed,
				Record: idempotency.Record{
					Key: key, Fingerprint: fingerprint, State: idempotency.StateCompleted,
					Result: append([]byte(nil), payload...),
				},
			}, nil
		}
		service, err := idempotency.NewService(store)
		if err != nil {
			t.Fatalf("NewService() error = %v", err)
		}
		middleware, err := idempotencyrpc.New(idempotencyrpc.Options{
			Service: service, Lease: time.Minute, MaxResponseBytes: 1024,
			Key: func(context.Context, idempotencyrpc.Request) (idempotency.Key, error) {
				return key, nil
			},
			Fingerprint: func(idempotencyrpc.Request) (idempotency.Fingerprint, error) {
				return fingerprint, nil
			},
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		_, _ = middleware.Call(context.Background(), request, func(
			context.Context, idempotencyrpc.Request,
		) idempotencyrpc.Response {
			t.Fatal("handler executed for replay")
			return idempotencyrpc.Response{}
		})
	})
}
