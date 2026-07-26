# outbox Kafka adapter

`gokafka` adapts `outbox.Envelope` to the first-party `kafka.Producer`.

It publishes the envelope payload to the envelope topic, uses `OrderingKey`,
then `IdempotencyKey`, then envelope `ID` as the deterministic partition-key
fallback, and adds content type, event identity, schema version, and optional
idempotency headers. Envelope metadata is propagated as deterministically
sorted record headers. Event-sourcing envelopes use `es.content_type` as the
record content type while retaining that original metadata header.

Publication is synchronous and at least once. The outbox relay marks delivery
only after Kafka acknowledges the record. A crash after Kafka acknowledgement
and before the database transition can publish the same envelope again, so
consumers must deduplicate the event identity.

```go
publisher, err := gokafka.New(producer)
if err != nil {
    return err
}

relay, err := outboxrelay.New(store, publisher, relayConfig)
```

Run `go test ./...` through the repository workspace until the independently
versioned `kafka` and `outbox` modules are published. The adapter is licensed
under the [MIT License](LICENSE).
