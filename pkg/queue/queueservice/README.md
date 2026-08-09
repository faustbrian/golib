# Queue service lifecycle adapter

`queueservice` is the independently versioned lifecycle integration between
[`github.com/faustbrian/golib/pkg/queue`](..) and
`github.com/faustbrian/golib/pkg/service`. It connects caller-owned producers
and workers to service startup, readiness, supervision, drain, and shutdown
without choosing a backend or moving retry, scheduling, acknowledgement,
redelivery, or dead-letter policy out of `queue`.

The module is pre-v1. Consumers should pin an exact revision until its first
stable release.

## Quick start

```go
producer, err := queueservice.NewProducer(
	queueservice.ProducerOptions[*queue.Queue]{
		Name:        "orders-producer",
		Resource:    concreteQueue,
		Correlation: correlationFactory,
		Publish: func(
			_ context.Context,
			resource *queue.Queue,
			message core.QueuedMessage,
			options ...job.AllowOption,
		) error {
			return resource.Queue(message, options...)
		},
	},
)
if err != nil {
	return err
}

runtime, err := service.New(service.Config{
	Components: []service.Component{producer.Component()},
})
if err != nil {
	return err
}
```

Use `NewHandler` around the application handler before constructing a concrete
`queue.Worker`. Use `NewWorker` when a `*queue.Queue` owns scheduling and drain.
Use `NewLifecycleWorker` when a backend exposes explicit startup, readiness,
blocking run, and shutdown callbacks that should be supervised as one service
plan.

## Lifecycle sequence

### Producer

1. Construction validates the stable name, typed resource, correlation and
   trace dependencies, and exactly one publish callback without starting work.
2. Component start runs the optional bounded startup callback and opens
   admission only after it succeeds.
3. The optional readiness check runs only while admission is open.
4. Service drain synchronously closes adapter admission, making readiness and
   new publishes unavailable before the service cancels supervised work.
5. Component stop waits for admitted publish and readiness calls under the
   caller's shutdown context, then runs an owned shutdown callback. Concurrent
   callers share one attempt, and repeated stops return the same result without
   invoking resource closure again.

A startup failure closes an owned producer immediately. `StartupError` keeps
both the validation and cleanup causes available to `errors.Is` and
`errors.As` without including their text in its diagnostic.

### Typed worker

`NewLifecycleWorker` returns a `service.Plan` fragment with one component, one
supervised task, and an optional readiness check under the same name.
Its optional `CloseAdmission` callback synchronously stops backend intake at
service drain; it must return promptly and must not wait for handlers.

```text
start resource -> expose readiness -> run intake
remove readiness -> stop intake -> cancel run -> join handlers
-> settle completed work -> release incomplete work -> close resource
```

The run callback owns intake and backend settlement. It returns only after it
has stopped admitting deliveries and joined every handler it admitted. A
completed handler may be settled according to backend policy; an incomplete
handler is left or released for backend redelivery. The component shutdown
callback runs only after the supervised run and concurrent readiness calls
return. Its caller-owned shutdown context is the single budget for handler
drain and resource closure; the preceding synchronous admission hook must
return promptly and must not wait for handlers.

An unexpected successful run return produces `ErrWorkerExited` and makes the
adapter unavailable. The service runtime converts that result, or any other
non-cancellation task failure, into process drain and shutdown. A return after
the run context is canceled is a normal shutdown result.

### Concrete queue convenience

`NewWorker` adapts an existing `*queue.Queue`. Its component calls `Start` and
`ReleaseContext`, inheriting the queue module's tested admission withdrawal,
handler join, settlement, and redelivery behavior. The same queue must not be
released independently after its ownership is transferred to the component.

## Publish cancellation and duplicate windows

`PublishWithAcceptance` calls the backend once and returns one of:

| Result | Meaning | Safe caller action |
| --- | --- | --- |
| `PublishNotAccepted` | The backend definitely rejected the task | Retry according to application policy |
| `PublishAccepted` | The backend definitely accepted the task | Do not retry the same logical task |
| `PublishUnknown` | Acceptance may have occurred | Reconcile or use an idempotency key; do not retry blindly |

A context already canceled at adapter admission produces `PublishNotAccepted`
without calling the backend. Once the callback begins, the callback owns
cancellation and acceptance classification. The adapter never retries. The
compatibility `Publish` callback cannot classify failures, so any callback
error is reported as `PublishUnknown` and matches `ErrPublishOutcomeUnknown`.

No queue adapter can make a publish and an application-side effect atomic.
Process death after either side commits creates a duplicate or missing-work
window. Durable backends, idempotent handlers, transactional outbox patterns,
and reconciliation remain application and backend choices.

## Readiness, SIGTERM, and scaling

Service drain makes readiness false and invokes each adapter's idempotent
admission hook before component stop begins. In Kubernetes, send SIGTERM
through `service.Run` or the platform runtime, configure a
termination grace period longer than the service shutdown timeout, and let the
readiness endpoint withdraw the pod before the worker drain completes. A
preStop sleep is not a substitute for readiness withdrawal and bounded drain.

During scale-down or rolling deployment, durable backends may redeliver work
whose lease or acknowledgement was not completed before the deadline. At-most-
once backends may lose that work instead. Size handler concurrency and the
termination budget so normal work can finish, while keeping every handler's
external operations bounded by its context.

## Backend differences

The adapter does not flatten backend guarantees:

- the in-memory ring exists only in one process and cannot redeliver after
  process loss;
- Redis Pub/Sub and NATS Core are transient and may lose disconnected work;
- Redis Streams and Valkey Streams use pending-entry ownership and reclaim;
- RabbitMQ uses publisher confirmation plus acknowledgement or rejection;
- NSQ uses its own finish, requeue, and timeout behavior.

Read the queue module's backend and delivery-semantics documentation before
selecting retry, visibility, dead-letter, and shutdown settings. The CI backend
gate delegates to that module's integration suite because this adapter owns no
transport implementation.

## API and ownership

- `NewProducer` retains a typed producer and optional startup, readiness, and
  shutdown callbacks. Omitting shutdown keeps the resource shared.
- `Producer.Component` provides ordered startup, admission closure, and drain.
- `Producer.Readiness` returns an opt-in `service.ReadinessCheck`.
- `Producer.PublishWithAcceptance` exposes safe retry information;
  `Producer.Publish` is the compatibility surface.
- `NewHandler` establishes a fresh delivery-attempt request ID and optional
  caller-owned trace extraction.
- `NewLifecycleWorker` retains a typed worker and returns a supervised plan.
  Its optional `CloseAdmission` callback owns backend intake withdrawal.
- `NewWorker` is the focused `*queue.Queue` convenience adapter.

Names are valid UTF-8, non-blank, and at most `MaxNameBytes`. Callback errors
become `CallbackError`, preserving their causes without formatting their text.
Callback panics become `CallbackPanicError`; panic values are not retained or
formatted. Backend and application errors remain available for programmatic
inspection, but the module does not log them, task payloads, credentials, or
endpoints.

## Adoption and migration

For a new service:

1. construct and validate the concrete backend client;
2. wrap the application handler with `NewHandler` or configure it through
   `NewLifecycleWorker`;
3. add the worker plan before dependent producer components so reverse shutdown
   drains producers first;
4. add only backend checks that are required to accept new work;
5. run the backend integration suite for the selected transport.

Existing `NewWorker(WorkerOptions{Name, Queue})` users remain compatible. To
supervise a backend run loop or surface worker exit, migrate to
`NewLifecycleWorker`, move fallible validation into `Startup`, move the
blocking intake loop into `Run`, and move final transport closure into
`Shutdown`. Existing producer callbacks remain valid; migrate to
`PublishWithAcceptance` when the backend can classify definite rejection and
ambiguous acceptance.

## Security

Inbound correlation metadata is untrusted by default. Enable
`TrustedMetadata` only after authenticating the immediate queue boundary.
Trace propagation is disabled unless a caller supplies an explicit
OpenTelemetry propagator. Metadata is bounded and cloned before transport use,
so caller-owned maps and timestamps are not aliased.

## FAQ

### Does the adapter retry publish or handler work?

No. Publish retry belongs to the application and concrete producer. Handler
retry, scheduling, dead-letter, and settlement belong to `queue` and its
backend.

### What happens when shutdown times out?

The stop call returns the context cause without closing a resource still used
by an admitted callback. A later stop can resume drain. Once resource shutdown
begins, its result is cached so repeated signals cannot double-close the
resource. Work not completed and settled before process termination follows
the concrete backend's redelivery or loss semantics.

### Should readiness prove that every broker operation will succeed?

No. Readiness should answer only whether this process may accept new work. Use
a bounded backend check when that dependency is required; omit it for roles
that can remain ready while a transient dependency recovers.

### How is callback failure text handled?

The adapter preserves errors for `errors.Is` and `errors.As` and emits no logs.
Its callback, aggregate, and panic errors use bounded diagnostics that do not
include callback error text or panic values. Applications remain responsible
for a safe error-rendering policy outside the adapter boundary.

## Development

```sh
make check
```

The module contract includes formatting, vet, unit tests, race detection,
exact statement coverage, fuzzing, allocation-reporting benchmarks,
documentation examples, and delegated backend integration. Repository gates
add API compatibility, mutation, static analysis, vulnerability, secret,
license, and SBOM checks.
