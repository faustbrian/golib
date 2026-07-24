# Queue consumer middleware

`idempotencyqueue` deduplicates completed redeliveries and preserves retry
behavior for failed handlers. Its structural `Message` interface is satisfied
by `queue/core.TaskMessage` without coupling the semantic core to a broker.

```go
deduplicator, err := idempotencyqueue.New(idempotencyqueue.Options{
	Service: service,
	Lease: 2 * time.Minute,
	TransitionTimeout: 5 * time.Second,
	Key: func(ctx context.Context, message idempotencyqueue.Message) (idempotency.Key, error) {
		delivery, ok := message.(DeliveryIdentity)
		if !ok {
			return idempotency.Key{}, &idempotency.Error{
				Reason: idempotency.ReasonInvalidPayload,
				Field: "delivery_identity",
			}
		}
		return idempotency.NewKey(
			"queue",
			delivery.TenantID(),
			"widgets.consume",
			"widget-worker",
			delivery.DeliveryID(),
		)
	},
	Fingerprint: func(message idempotencyqueue.Message) (idempotency.Fingerprint, error) {
		return canonical.BytesFingerprint("widget-job-v1", message.Payload(), 64*1024)
	},
})
if err != nil {
	return err
}

run := idempotencyqueue.Wrap(
	deduplicator,
	func(ctx context.Context, task core.TaskMessage) error {
		return consumeWidget(ctx, task.Payload())
	},
)

q, err := queue.NewQueue(
	queue.WithWorker(worker),
	queue.WithFn(run),
)
```

The returned function retains the concrete `core.TaskMessage` type expected by
`queue.WithFn`. The example's `DeliveryIdentity` is an application interface;
adapt broker metadata to a stable delivery ID instead of hashing a transient
receipt handle or retry counter.

## Settlement mapping

- A completed replay returns `nil`; `queue` may acknowledge it without
  executing the handler again.
- A handler error is returned after ownership is released. The broker may reject
  or negatively acknowledge it for redelivery.
- `ErrInProgress` should be retried after backoff or broker visibility timeout.
- `ErrConflict` means one delivery ID was reused for different payloads. Route
  it to investigation or a dead-letter policy; blind retry cannot resolve it.
- `ErrTerminalFailure` represents a deliberately persisted permanent failure.
- Storage errors fail closed and should not acknowledge the delivery as handled.

When a handler fails or panics, release uses a context detached from handler
cancellation and bounded by `TransitionTimeout`. If release itself fails, the
returned error joins both failures because ownership is then unknown.

Completion before broker acknowledgement is intentional. A crash after durable
completion but before acknowledgement causes redelivery, which replays as
already handled. A crash after a business side effect but before completion is
still ambiguous. Read ownership with
`idempotency.OwnershipFromContext(ctx)` inside the handler and apply its fencing
token or a database uniqueness invariant in the business transaction.
