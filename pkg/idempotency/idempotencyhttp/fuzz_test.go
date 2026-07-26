package idempotencyhttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencyhttp"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
)

func FuzzMalformedReplayFailsClosed(f *testing.F) {
	f.Add([]byte("not-json"))
	f.Add([]byte(`{"schema":1,"status":200,"body":"b2s="}`))
	f.Add([]byte(`{"schema":2,"status":200}`))

	key, err := idempotency.NewKey("http", "tenant", "POST /widgets", "caller", "key")
	if err != nil {
		f.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("payload"))
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
		middleware, err := idempotencyhttp.New(idempotencyhttp.Options{
			Service: service, Lease: time.Minute, MaxResponseBytes: 1024,
			Key: func(*http.Request, string) (idempotency.Key, error) { return key, nil },
			Fingerprint: func(*http.Request) (idempotency.Fingerprint, error) {
				return fingerprint, nil
			},
		})
		if err != nil {
			t.Fatalf("idempotencyhttp.New() error = %v", err)
		}
		handler := middleware.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("handler executed for replay")
		}))
		request := httptest.NewRequest(http.MethodPost, "/widgets", nil)
		request.Header.Set(idempotencyhttp.HeaderKey, "key")
		handler.ServeHTTP(httptest.NewRecorder(), request)
	})
}
