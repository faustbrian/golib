# Compatible queue integration

The independently versioned
`github.com/faustbrian/golib/pkg/event-sourcing/adapters/goqueue` module keeps
the repository queue dependency outside the event-sourcing core.

The adapter preserves complete persisted delivery identity in the versioned
canonical `golib.event-sourcing.queue.v1` envelope. It accepts only its exact
bounded encoding, rejects hostile or incompatible input without a partial
delivery, and distinguishes live delivery from replay.

`NewDispatcher` publishes only live deliveries and stops on the first failed
queue acceptance. `NewReplayDispatcher` is an explicit, separately named
replay operation. The compatible queue contract does not accept a context, so
cancellation is checked between calls but cannot interrupt a call already in
progress.

`NewTaskHandler` handles only live deliveries.
`NewReplayTaskHandler` explicitly enables replay handling. Task handlers return
the consumer outcome without acknowledging or rejecting the task. The queue
coordinator remains the sole settlement owner and may acknowledge only after a
nil result or apply its configured failure policy after an error.

Queue backends are not interchangeable guarantees. Redis Pub/Sub is not a
durable asynchronous transport. Streams, NSQ, RabbitMQ, and other compatible
workers retain their own acknowledgement, rejection, retry, ordering, and
redelivery semantics. The adapter does not claim exactly-once delivery and
does not treat queue acceptance as durable processing.

Backend conformance evidence remains partial. Kafka stays in the dedicated
`gokafka` adapter because topics, partitions, keys, offsets, consumer groups,
rebalances, acknowledgements, idempotence, and transactions must remain
observable.

The adapter integration suite proves the complete dispatch and handling path
through the repository queue and its in-memory worker for both successful and
failed consumers. This does not transfer in-memory evidence to durable
backends; each backend retains its own operational and delivery guarantees.

See the [adapter guide](../adapters/goqueue/README.md) for the wire contract,
bounds, guarantees, and current status.
