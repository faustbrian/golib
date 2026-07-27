package valkeystream

import (
	"context"
	"errors"
	"sync/atomic"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/safeerr"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/job"
)

// Publisher appends durable Valkey Stream jobs without joining a consumer
// group or starting worker read and reclaim loops.
type Publisher struct {
	opts      options
	transport streamqueue.Transport
	stopped   atomic.Bool
}

// NewPublisherE constructs a producer-only Valkey Streams client and validates
// initial connectivity without creating or joining a consumer group.
func NewPublisherE(option ...Option) (*Publisher, error) {
	opts, err := newOptions(option...)
	if err != nil {
		return nil, err
	}
	client, err := newValkeyClient(nativeClientOptions(opts))
	if err != nil {
		return nil, safeerr.Wrap("valkeystream: initialize native client", err)
	}
	transport := newNativeTransport(client, opts.maxLength, job.DefaultMaxMessageBytes)
	ctx, cancel := context.WithTimeout(context.Background(), opts.commandTimeout)
	defer cancel()
	if err = client.Do(ctx, client.B().Ping().Build()).Error(); err != nil {
		_ = transport.Close()
		return nil, safeerr.Wrap("valkeystream: connect to server", err)
	}
	return &Publisher{opts: opts, transport: transport}, nil
}

// BackendName identifies this adapter in lifecycle events.
func (*Publisher) BackendName() string { return "valkey-streams" }

// QueueName returns the configured stream name.
func (publisher *Publisher) QueueName() string { return publisher.opts.stream }

// Queue validates, encodes, and appends one job to the configured stream.
func (publisher *Publisher) Queue(
	message core.QueuedMessage,
	options ...job.AllowOption,
) error {
	if publisher.stopped.Load() {
		return queue.ErrQueueShutdown
	}
	if message == nil {
		return errors.New("valkeystream: queued message is required")
	}
	queued := job.NewMessage(message, options...)
	if err := queued.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), publisher.opts.commandTimeout)
	defer cancel()
	_, err := publisher.transport.Add(ctx, streamqueue.AddRequest{
		Stream: publisher.opts.stream, MaxLength: publisher.opts.maxLength,
		Body: queued.Bytes(),
	})
	return err
}

// Shutdown closes the producer-owned Valkey connections.
func (publisher *Publisher) Shutdown() error {
	if !publisher.stopped.CompareAndSwap(false, true) {
		return queue.ErrQueueShutdown
	}
	return publisher.transport.Close()
}
