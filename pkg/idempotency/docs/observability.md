# Logging and telemetry

`Service` can emit one bounded `Observation` after each semantic transition.
The core does not depend on a logger, metrics client, tracer, or datastore
client. Applications adapt the signal to their own observability stack.

The optional ecosystem adapters accept the standard types exposed by
`log` and `telemetry`, so they do not require global logger or telemetry
registration:

```go
logger, err := log.New(slog.NewJSONHandler(logWriter, nil))
if err != nil {
	return err
}
logObserver, err := idempotencylog.New(logger)
if err != nil {
	return err
}

telemetryObserver, err := idempotencytelemetry.New(runtime.MeterProvider())
if err != nil {
	return err
}

hash, err := idempotency.NewHMACKeyHasher(correlationSecret)
if err != nil {
	return err
}

observer := idempotency.ObserverFunc(func(
	ctx context.Context,
	event idempotency.Observation,
) {
	logObserver.Observe(ctx, event)
	telemetryObserver.Observe(ctx, event)
})
service, err := idempotency.NewServiceWithOptions(store, idempotency.ServiceOptions{
	KeyHasher: hash,
	Observer:  observer,
})
```

Here `log.New` is `github.com/faustbrian/golib/pkg/log.New`, and `runtime` is a
`*github.com/faustbrian/golib/pkg/telemetry.Runtime`. The logging adapter writes the
five bounded observation fields. The telemetry adapter increments
`idempotency.transitions` with only `transition`, `outcome`, `reason`, and
`durable` attributes; it deliberately excludes correlation.

The observer receives only:

- a fixed transition: `acquire`, `inspect`, `heartbeat`, `complete`, `fail`,
  `release`, or `expire`;
- a stable acquisition outcome when the transition is `acquire`;
- a stable reason code for semantic errors;
- whether the requested durable state or result was established;
- an optional keyed correlation digest.

It does not receive the logical key fields, fingerprint, owner token, fencing
token, result, metadata, or backend error. Keep the original error in
restricted application diagnostics when its cause is operationally necessary.

## Correlation hashing

`NewHMACKeyHasher` requires at least 32 bytes of secret key material and
computes a full hexadecimal HMAC-SHA-256 over length-delimited key fields. It
copies the supplied secret. The digest is deterministic for one secret and
logical key, does not reveal raw field values, and avoids ambiguous field
concatenation.

Manage the HMAC secret outside application source, restrict access, and rotate
it under the same policy as other logging secrets. Rotation intentionally
changes every correlation value, so investigations cannot join records across
rotation without a controlled overlap strategy. A hash does not anonymize a
small guessable key space against an attacker who has the secret.

Correlation remains high-cardinality and is for restricted logs or traces,
not metric labels. Metrics should use only bounded transition, outcome, reason,
durability, adapter name, and a curated application operation class. Do not
label metrics with tenant, caller, raw operation, source identity, correlation
hash, owner token, fence, or request fingerprint.

## Failure and performance behavior

Observation is synchronous after the semantic result is known. Implementations
should return quickly, honor the context, and hand off expensive export work to
a bounded observability pipeline. The service isolates panics from both the
observer and key hasher so instrumentation cannot change acquisition or
transition semantics. Observer APIs intentionally have no error return; an
export failure must be surfaced by the observability implementation's own
health signals.

Do not perform business writes, ownership decisions, retries, or unbounded
queueing in an observer. Delivery of telemetry is not part of the idempotency
transaction and may be lost on process failure.

## Optional ecosystem adapters

`idempotencylog.New` accepts the `*slog.Logger` returned by `log`.
`idempotencytelemetry.New` accepts the standard OpenTelemetry meter provider
returned by `telemetry.Runtime.MeterProvider`. The compatibility module pins
and compiles the published upstream contracts used by this repository. An
application must still compile its selected dependency versions and run the
compatibility tests as part of an upgrade.
