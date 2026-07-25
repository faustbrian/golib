package idempotency_test

import (
	"context"
	"fmt"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
)

type exampleClock struct {
	now time.Time
}

func (c exampleClock) Now() time.Time { return c.now }

func ExampleService_Begin() {
	clock := exampleClock{now: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)}
	token := 0
	store, err := memory.New(memory.Options{
		Clock: clock,
		OwnerTokens: func() (string, error) {
			token++
			return fmt.Sprintf("owner-%d", token), nil
		},
	})
	if err != nil {
		panic(err)
	}
	service, err := idempotency.NewService(store)
	if err != nil {
		panic(err)
	}
	key, err := idempotency.NewKey(
		"billing", "tenant-42", "create-invoice", "api-client-7", "request-123",
	)
	if err != nil {
		panic(err)
	}
	fingerprint, err := idempotency.NewFingerprint(
		"invoice-v1", []byte("invoice:9001:EUR"),
	)
	if err != nil {
		panic(err)
	}
	request := idempotency.BeginRequest{Acquire: idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: 30 * time.Second,
	}}

	first, err := service.Begin(context.Background(), request)
	if err != nil {
		panic(err)
	}
	fmt.Println(first.Outcome, first.Execute, first.Durable)
	_, err = service.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: first.Record.Ownership(),
		Result:    []byte(`{"invoice_id":"inv-9001"}`),
	})
	if err != nil {
		panic(err)
	}
	retry, err := service.Begin(context.Background(), request)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s: %s\n", retry.Outcome, retry.Record.Result)

	// Output:
	// acquired true true
	// replayed: {"invoice_id":"inv-9001"}
}
