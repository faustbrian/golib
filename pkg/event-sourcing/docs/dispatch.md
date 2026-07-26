# Dispatcher and consumer semantics

`Dispatcher` is the replaceable delivery boundary:

```go
type Dispatcher interface {
	Dispatch(context.Context, []Delivery) error
}
```

`SyncDispatcher` invokes consumers synchronously in message-major order: for
each delivery, consumers run in registration order. Consumer identities must
be unique. Filters run in declaration order before their consumer.

The default stops at the first filter or consumer failure.
`ContinueOnConsumerError` attempts later consumers and joins failures.
Cancellation stops new callbacks. Empty batches succeed. Panics in filters or
consumers are contained as redacted `ConsumerError` values. The dispatcher
holds no lock across callbacks, starts no goroutines, and permits reentrant
dispatch.

Consumers receive immutable persisted `Delivery` values and can distinguish
live processing from replay. A successful return proves only that the
synchronous callbacks completed. It does not add queue durability, retries,
transactions, or exactly-once delivery.

## Persistence boundary

The aggregate repository appends messages before dispatch. It never dispatches
externally before successful persistence. Direct dispatch after a PostgreSQL
commit is not atomic with Kafka, a queue, or another external system. Use the
optional outbox composition when durable asynchronous publication is required.

## Intentional difference from EventSauce's event-dispatcher utility

The core does not provide an ephemeral publisher that silently decorates and
wraps arbitrary non-aggregate values as if they were persisted event messages.
That utility would blur whether a `Delivery` has durable stream identity and
version.

For event-sourced behavior, construct messages through the validated aggregate
repository lifecycle and dispatch the returned persisted deliveries. For
non-event-sourced application notifications, use a small application-owned
callback or messaging contract that does not claim event-store persistence.
Applications that deliberately create an integration stream may explicitly
construct a valid `PendingMessage`, persist it, and then create a `Delivery`;
the library does not hide those steps behind a publisher helper.

## Custom dispatchers

Custom implementations preserve context handling, input order, empty-batch
behavior, panic policy, partial-progress semantics, and live/replay intent.
Queue and Kafka integrations remain independently versioned adapters because
their delivery guarantees are materially different from synchronous
in-process dispatch.
