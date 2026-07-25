package goqueue_test

import (
	"context"
	"fmt"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/goqueue"
	"github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/faustbrian/golib/pkg/outbox/relay"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func Example_relay() {
	queue := &exampleQueue{}
	publisher, _ := goqueue.New(queue)
	store := &exampleStore{}
	worker, _ := relay.New(store, publisher, relay.Config{Owner: "relay-a"})

	result, _ := worker.RunOnce(context.Background())

	fmt.Println(result.Claimed, queue.calls, store.delivered)
	// Output: 1 1 1
}

type exampleQueue struct {
	calls int
}

func (queue *exampleQueue) Queue(core.QueuedMessage, ...job.AllowOption) error {
	queue.calls++

	return nil
}

type exampleStore struct {
	claimed   bool
	delivered int
}

func (*exampleStore) Ping(context.Context) error { return nil }

func (*exampleStore) ExtendLease(context.Context, postgres.LeaseRef, time.Duration) (time.Time, error) {
	return time.Now(), nil
}

func (store *exampleStore) Claim(context.Context, postgres.ClaimRequest) ([]postgres.Claim, error) {
	if store.claimed {
		return nil, nil
	}
	store.claimed = true

	return []postgres.Claim{{
		Envelope: outbox.Envelope{
			ID: "evt-1", Topic: "orders.created", PayloadVersion: 1,
			AvailableAt: time.Unix(1, 0), CreatedAt: time.Unix(1, 0),
		},
		LeaseToken: "lease-token",
	}}, nil
}

func (store *exampleStore) MarkDelivered(context.Context, postgres.LeaseRef) error {
	store.delivered++

	return nil
}

func (*exampleStore) Retry(context.Context, postgres.LeaseRef, time.Duration, error) error {
	return nil
}

func (*exampleStore) DeadLetter(context.Context, postgres.LeaseRef, error) error {
	return nil
}

func (*exampleStore) ReleaseLease(context.Context, postgres.LeaseRef) error { return nil }
