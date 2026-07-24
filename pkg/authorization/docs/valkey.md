# Valkey invalidation

The `valkey` package distributes policy revision changes across application
processes. It stores the latest published revision in a durable Valkey key and
publishes a wakeup on a channel in the same Lua operation.

```go
invalidator, err := authvalkey.New(client, authvalkey.Options{
    Prefix:       "my-service:authorization",
    PollInterval: 15 * time.Second,
})
if err != nil {
    return err
}

_, err = invalidator.Publish(ctx, stored.Revision)
```

Consumers pass their active revision to `Watch` and reload the authoritative
manifest when a newer revision appears. `policy.Synchronizer` provides that
verified activation path:

```go
synchronizer, err := policy.NewSynchronizer(repository, compiler, engine)
if err != nil {
    return err
}

group.Go(func() error { return synchronizer.Run(ctx) })
group.Go(func() error {
    return invalidator.Watch(ctx, engine.Revision(), synchronizer.Observe)
})
```

Pub/sub payloads are deliberately ignored. A message only wakes the watcher,
which then reads the durable revision key. The watcher also polls that key at
the configured interval and continues polling after pub/sub disconnects. This
makes dropped, duplicated, delayed, and out-of-order messages harmless.

Valkey is an invalidation transport, not the policy source of truth. A failure
to read Valkey or reload the manifest is returned to the caller and must not be
converted into an allow decision. The synchronizer polls the repository
directly as the correctness path, so a PostgreSQL publication that occurs while
Valkey is unavailable is still discovered. Valkey only reduces propagation
latency.
