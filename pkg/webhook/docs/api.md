# API and compatibility

The root package contains protocol primitives. `Signer` emits signatures for
all active keys. `Verifier` authenticates a bounded timestamp and optionally
records a replay key. `CaptureBody`, `SignRequest`, `VerifyRequest`, and
`Middleware` provide the `net/http` boundary. `Envelope` is deterministic
event data. `Deliverer`, `RetryPolicy`, `SSRFPolicy`, `FanOut`, and `Replay`
provide bounded outbound work. `Observer` receives privacy-safe lifecycle
records. Adapter packages integrate durable replay, logging, telemetry, queue,
and outbox seams. `webhooktest` provides deterministic consumer fixtures.

All exported declarations have Go documentation; run `go doc -all .` and
`go doc -all ./adapters/goidempotency` (similarly for other adapters) for the
complete generated reference.

`NonceGenerator`, clocks, delivery IDs, sleeps, and resolvers are injectable.
Compatibility is checked on Go 1.26. Canonical bytes, signature header syntax,
envelope JSON, exported error identity, replay key derivation, retry
classification, and provider preset behavior are SemVer-governed wire or
behavioral contracts. Adding a field whose zero value preserves behavior may
be minor; changing signed bytes or existing classifications is major.

The core uses narrow standard interfaces (`HTTPDoer`, `ReplayStore`, hooks),
so callers can integrate without importing optional adapters.
