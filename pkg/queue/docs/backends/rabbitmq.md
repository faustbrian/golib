# RabbitMQ setup

Configure the AMQP URI, durable exchange name and type, routing key, queue,
consumer tag, and `autoAck`. Keep `autoAck=false` for at-least-once processing
and package-owned terminal settlement.

Published jobs use persistent delivery mode and synchronous publisher
confirms. `WithPublishTimeout` bounds publish plus confirmation wait and
defaults to five seconds. A negative confirmation or closed confirmation
channel fails enqueue.

RabbitMQ declares a durable dead-letter exchange and queue by default, derived
from the configured exchange, queue, and routing key. `WithDeadLetter` accepts
an explicit `DeadLetterConfig` with distinct terminal names and a maximum of 2
through 101 broker delivery attempts. Existing queues declared without the
matching `x-dead-letter-exchange` and `x-dead-letter-routing-key` arguments
must be migrated or configured consistently before adoption; RabbitMQ rejects
inequivalent queue redeclaration.

Retryable handler failures are republished persistently to the original
exchange with an incremented bounded attempt header. The publisher confirm is
received before the source is acknowledged. Exhausted, permanent, and
malformed deliveries are instead confirmed to the terminal exchange with
envelope version, classification, stable failure code, attempts, and bounded
source topology headers. Cancellation and infrastructure failures requeue the
source without incrementing the terminal attempt policy. Package job retries
still happen inside each broker delivery before this broker-level decision.

Confirmed publish and source acknowledgement are not atomic. A destination
failure requeues the source. Process death or acknowledgement failure after a
positive confirm may create a duplicate, so consumers must use job identity
and terminal headers for reconciliation. Exactly-once transfer is not
claimed. RabbitMQ record listing and mutation remain explicitly unsupported;
operate the terminal queue with broker tooling. `autoAck=true` disables these
post-handler guarantees.

`WithReconnectConfig` controls initial startup attempts. Runtime reconnection
is not hidden by v1; a closed connection or channel is terminal, so shut down
that queue and construct a replacement worker. The broker-restart integration
test proves both the old-worker failure and replacement-worker recovery. TTL
and priorities remain explicit RabbitMQ concerns. Credential-bearing AMQP URL
failures return sanitized constructor text.

`WithRequestTimeout` bounds an idle delivery request. Integration uses
RabbitMQ 3.13.7 with `amqp091-go` 1.11.0.
