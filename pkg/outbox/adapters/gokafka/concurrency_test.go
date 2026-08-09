package gokafka_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gokafka"
)

func TestPublisherRacesSharedCallsWithClientShutdown(t *testing.T) {
	t.Parallel()

	client := &shutdownClient{}
	publisher, err := gokafka.New(client)
	if err != nil {
		t.Fatal(err)
	}
	const calls = 64
	start := make(chan struct{})
	errorsByCall := make(chan error, calls)
	var wait sync.WaitGroup
	for index := range calls {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errorsByCall <- publisher.Publish(t.Context(), outbox.Envelope{
				ID: fmt.Sprintf("event-%d", index), Topic: "events.v1",
				OrderingKey: fmt.Sprintf("stream-%d", index), PayloadVersion: 1,
			})
		}()
	}
	close(start)
	client.Close()
	wait.Wait()
	close(errorsByCall)

	accepted := client.Accepted()
	rejected := 0
	for publishErr := range errorsByCall {
		if publishErr == nil {
			continue
		}
		var categorized interface{ Category() kafka.ErrorCategory }
		if !errors.As(publishErr, &categorized) ||
			categorized.Category() != kafka.ErrorShutdown {
			t.Fatalf("Publish() shutdown error = %v", publishErr)
		}
		rejected++
	}
	if accepted+rejected != calls {
		t.Fatalf("accepted/rejected = %d/%d, want total %d", accepted, rejected, calls)
	}
}

type shutdownClient struct {
	lock     sync.Mutex
	closed   bool
	accepted int
}

func (client *shutdownClient) Publish(context.Context, kafka.Message) error {
	client.lock.Lock()
	defer client.lock.Unlock()
	if client.closed {
		return categorizedError{category: kafka.ErrorShutdown, message: "secret close cause"}
	}
	client.accepted++

	return nil
}

func (client *shutdownClient) Health(context.Context) error { return nil }

func (client *shutdownClient) Close() {
	client.lock.Lock()
	client.closed = true
	client.lock.Unlock()
}

func (client *shutdownClient) Accepted() int {
	client.lock.Lock()
	defer client.lock.Unlock()

	return client.accepted
}
