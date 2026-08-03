# Kafka slog adapter

`golog` translates stable, payload-free `kafka.Observation` values into fixed
structured records through Go's standard `log/slog` API. It is part of the root
Kafka module and adds no logging dependency.

## Setup

```go
adapter, err := golog.New(golog.Config{
    Logger: slog.New(slog.NewJSONHandler(os.Stdout, nil)),
    Level:  slog.LevelInfo,
    Identities: golog.IdentityPolicy{
        AllowedClientIDs:      []string{"orders-producer"},
        AllowedTopics:         []string{"orders"},
        AllowedConsumerGroups: []string{"fulfillment"},
    },
})
if err != nil {
    return err
}

observers := kafka.ObserverPolicy{
    Observers:      []kafka.ObserverFunc{adapter.Observer()},
    FailureHandler: reportObservationFailure,
    Timeout:        100 * time.Millisecond,
}
```

One adapter starts no goroutines and is safe for concurrent use when its
`slog.Handler` follows the standard concurrency contract.

## Data and cardinality policy

Every record has a fixed message and bounded scalar fields for operation,
outcome, stable error category, start and elapsed time, counts, byte estimates,
queue and throttle time, and known numeric Kafka coordinates. Adapter-generated
fields never contain record keys, values, headers, credentials, broker
endpoints, application error text, or panic values. Attributes already attached
to the supplied logger or its handler remain application-owned and must follow
the same redaction policy.
Replay observations add fixed signed-64-bit processed, skipped, failed, and
remaining counts. They preserve exact replay progress without turning source
coordinates or identities into unbounded fields.
Inspector observations add fixed broker, topic, consumer-group, member, and
partition counts plus dependency-health and readiness-hysteresis fields.
Broker-connect observations add only the bounded configured
`kafka.authentication.method`.
Inspected identities, broker hosts, cluster IDs, assignments, and lag
coordinates never enter adapter-generated fields.

Client IDs, topics, and consumer groups are denied by default. Each identity is
logged only when exactly present in its constructor-copied allowlist. Lists are
duplicate-free and limited to 128 entries; Kafka identity and topic bounds are
validated before construction.

The adapter validates every observation through `kafka.Observation.Validate`
before logging. A handler panic is contained as `ErrLoggerPanic` without
retaining its value. The `slog.Logger` API intentionally does not return
`Handler.Handle` errors, so those errors cannot reach the observer failure
handler. Supply a handler whose own bounded delivery and failure policy matches
the application.

Logging is synchronous at the handler boundary. A handler that ignores the
callback context can block the Kafka operation despite the root observer's
cooperative deadline. Use only bounded handlers and queues.
