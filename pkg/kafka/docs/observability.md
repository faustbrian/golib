# Observability hooks

The root module exposes a vendor-neutral `ObserverPolicy`. The current surface
reports completion of synchronous record production, synchronous batch
production, and final asynchronous record delivery. It does not expose
franz-go hooks, records, requests, responses, or clients.

## Configuration and execution

`ProducerConfig.Observers` accepts 1 to 16 ordered `ObserverFunc` callbacks.
The callback slice is copied during producer construction. A non-empty policy
requires an explicit `ObservationFailureFunc`; observation failures never
change an acknowledged or failed Kafka delivery into a different application
outcome.

The default callback budget is 100 milliseconds. Explicit values must be from
1 millisecond through 5 seconds. All observers and failure callbacks for one
event execute synchronously in registration order and share that single
budget. Different producer operations can invoke the same observer
concurrently, so callbacks must be concurrency-safe.

When an observer fails, its failure handler runs immediately before the next
registered observer.

Go cannot forcibly stop an arbitrary callback. The package supplies a derived
deadline and detects a cooperative late return, but a callback that ignores its
context can block the calling operation indefinitely. For asynchronous
production, franz-go completes its promise synchronously; a blocked observer
therefore also delays the delivery channel and can delay flush or shutdown.
Observers must perform only bounded in-process work and hand off to their own
explicitly bounded infrastructure when export cannot complete immediately.

The callback context retains caller values for correlation but is detached
from caller cancellation and receives the observer-policy deadline. This lets
an asynchronous final outcome remain observable after the caller stops
waiting. The context is callback-scoped and must not be retained.

## Event contract

`Observation.Kind` is one of:

- `ObservationProduceRecord` after `PublishRecord` resolves;
- `ObservationProduceBatch` after the complete input-ordered batch resolves;
  or
- `ObservationProduceAsync` immediately before the final delivery is made
  available on the result channel.

Every observation contains its copied client ID, start time, elapsed duration,
record count, conservative policy record bytes, success flag, and stable
failure category. Successful events use `ErrorUnknown` because no failure
category applies. Single-record success includes the delivered partition,
offset, and broker timestamp. Topic is copied only for validated single-topic
metadata; mixed-topic batches omit it so an adapter cannot accidentally fan
one operation into an unbounded topic list.

Observations never contain keys, values, headers, credentials, broker URLs,
application error text, or franz-go values. Adapter cardinality remains an
adapter and deployment decision. Topics must not become metric dimensions
without an explicit bounded allowlist; keys and arbitrary headers must never
be attributes by default.

## Failures and reentrancy

An observer-returned error is sent to the configured failure handler as an
`ObservationFailure`. Its normal formatting is redacted. `Cause` deliberately
returns the application-owned error for local classification and must not be
exported without application redaction. Observer panic values are discarded
and represented only by `ErrObserverPanic`. A panic in the failure handler is
contained and discarded because recursively reporting reporter failure would
be unbounded.

Observers must not call the producer that invoked them. Producer operations
using the callback context fail with `ErrObserverReentry`, and `Close` fails
with the same error while a callback is active. Replacing the callback context
to bypass that fence violates the contract and can deadlock lifecycle work.
The package holds no producer state lock while application observer code runs.

## Current boundary

The root observer model is implemented only for producer delivery completion.
Broker connection, authentication, request, throttle, retry, consumer,
rebalance, commit, transaction-lifecycle, replay, inspection, health, and
shutdown events remain unimplemented. The planned
`kafka/adapters/gotelemetry` nested module must translate only stable root
observations and pin a reviewed OpenTelemetry messaging semantic-convention
version; OpenTelemetry will not become a root dependency.
