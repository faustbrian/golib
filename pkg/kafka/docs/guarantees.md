# Guarantees and failure model

## Producer

The producer explicitly requests all in-sync replica acknowledgements and does
not disable franz-go idempotence. Calls use bounded retries, delivery timeout,
request timeout, dial timeout, buffering, batch size, and record size.

A nil publish result means Kafka acknowledged the record. It does not prove a
downstream consumer completed and does not make a database/Kafka dual write
atomic.

## Consumer

Automatic commits are disabled. A poll is processed in fetch order and committed
only after all handlers return nil. A handler error or panic leaves the batch
uncommitted. Rebalances are released after each poll.

Delivery is at least once. A crash after a durable side effect but before the
offset commit replays the record. Kafka may partially persist a multi-partition
commit before returning an error, so result counters never claim a failed commit
was wholly persisted or wholly rejected. Side effects must be idempotent.

## Context and memory

Handler deadlines are cooperative; a handler must honor context cancellation.
Consumed byte slices reference the current fetch and must be copied before
retention. Configuration and record bounds prevent unbounded caller-controlled
allocation inside this module.
