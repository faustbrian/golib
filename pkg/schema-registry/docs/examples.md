# Integration examples

## Kafka and outbox

At write time, compile and register the schema during an explicit deployment or
producer initialization step. Encode the business value with a selected
`ValueCodec`, frame it with the provider ID, and store the exact framed bytes in
the outbox transaction. The outbox publisher forwards bytes unchanged.

At consume time, parse the frame, resolve its provider ID through a configured
cache policy, validate the bounded payload, then decode. A Kafka record accessor
must not trigger registry I/O.

## CloudEvents and HTTP

Store the portable fingerprint in a controlled CloudEvents extension or HTTP
metadata field only when both parties agree on that contract. Keep provider IDs
in provider-specific transport framing. Reject content-type/schema-format
mismatches before decoding.

## Queue and workflow history

Persist the framed payload or a tuple containing wire version, provider ID,
portable fingerprint, and payload. Preload the worker with an immutable bundle
covering all replayable history. Workflow replay must use cache-only resolution;
it must not contact a mutable registry.
