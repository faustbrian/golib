# Circuit-Breaker Composition

Circuit breaking protects one logical dependency operation. It must not become
a second retry loop or a second breaker state machine inside this module.

`CircuitBreaker` is the narrow execution port:

```go
type CircuitBreaker interface {
	Execute(
		context.Context,
		func(context.Context) (*http.Response, error),
	) (*http.Response, error)
}
```

The middleware wraps retry. One admitted breaker execution can contain several
bounded physical attempts, but records only the final logical outcome. This
matches endpoint semantics and prevents retry count from inflating breaker
throughput or failure ratios.

## First-party `circuit-breaker` adapter

The maintained integration delegates admission, rolling windows, opening,
half-open probes, administrative modes, observers, and shutdown to
`circuit-breaker`:

```go
classifier, err := httpclient.NewGoCircuitBreakerClassifier(nil)
circuit, err := breaker.New(breaker.Config{
	Name:              "widgets",
	MinimumThroughput: 10,
	Opening: &breaker.OpeningRules{
		FailureRatio: 0.5,
	},
	Classifier: classifier,
})
adapter, err := httpclient.NewGoCircuitBreakerAdapter(circuit)
middleware, err := httpclient.NewCircuitBreakerMiddleware(
	httpclient.CircuitBreakerOptions{
		Name:    "widgets-circuit",
		Layer:   httpclient.MiddlewareClient,
		Breaker: adapter,
	},
)
```

The adapter borrows the breaker. Its owner retains responsibility for
`Shutdown` and observer lifecycle. The HTTP client neither copies nor mirrors
breaker state.

## Default HTTP outcome policy

`DefaultCircuitOutcomeClassifier` applies after the complete retry policy:

- final `5xx` responses are failures;
- transport, retry-exhaustion, deadline, and other operation errors are
  failures;
- cancellation is ignored only when the caller context is canceled; a
  dependency-produced cancellation-shaped error is a failure;
- local limiter capacity and maximum-wait rejection are ignored;
- `429 Too Many Requests` is ignored because admission policy owns it; and
- other HTTP responses are successes, independent of body decoding.

Use `CircuitOutcomeClassifierFunc` for a provider contract that treats specific
statuses or errors differently. Implement `ContextCircuitOutcomeClassifier`
when custom policy also needs caller-context state. The callback receives the
final response and error but must not retain the context, mutate, consume, or
close the response. Invalid outcomes are passed to `circuit-breaker` as
invalid classifications so its permit safety and error contract remain
authoritative.

## Ordering and traffic bounds

The default operation order is:

1. identity, request policy, and initial limiter admission;
2. circuit admission at transport priority `-600`;
3. bounded retry at `-500`; and
4. per-attempt limiter, authentication, signing, telemetry, and transport.

A local limiter rejection happens before circuit admission. An open-circuit
rejection happens after the initial rate reservation but before network I/O.
Every later retry or redirect must reserve another limiter slot.

A half-open permit protects one logical probe. That probe can use only the
endpoint's already-bounded retry policy; it cannot create an unbounded attempt
loop. Configure conservative retry attempts for probe-sensitive endpoints.

Do not configure another breaker inside generated clients or transports. A
nested breaker would classify different boundaries and make half-open capacity,
rejection, and recovery behavior ambiguous.

## Rejection and ownership

First-party `RejectionError` values are mapped to `CircuitBreakerError` and the
stable `ErrCircuitRejected` sentinel. The wrapper preserves the underlying
cause for `errors.Is` and `errors.As` without rendering breaker identity,
state details, retry timestamps, or custom causes.

Rejected operations do not reach the attempt pipeline or network. A hostile
custom breaker that returns a response with a rejection has that body closed
before the typed error is returned. Ordinary operation failures remain their
original typed errors and final successful responses remain caller-owned.
