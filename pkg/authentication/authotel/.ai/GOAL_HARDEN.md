# Goal Harden: Authentication OpenTelemetry Adapter

## Mission

Prove that telemetry cannot disclose authentication material, distort results,
or destabilize request processing.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Inventory every instrument, attribute, callback, dependency, error, and
  allocation; compare them with the documented contract.
- Test typed-nil providers, provider construction failures, canceled contexts,
  invalid events, repeated completion, callback panic, SDK errors, and
  concurrent completion under `-race`.
- Assert from captured telemetry that credentials, claims, identity values,
  arbitrary errors, and panic text cannot appear in names or attributes.
- Stress high concurrency and exporter backpressure; prove the adapter starts
  no goroutines and cannot create unbounded labels or retained request state.
- Benchmark enabled, sampled-out, and no-op providers for latency and
  allocations against direct authentication instrumentation.
- Fuzz event values and hostile provider implementations without panics,
  leaks, hangs, or behavior changes.
- Verify clean shutdown remains caller-owned and package use after SDK shutdown
  is bounded and documented.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests, plus passing race, fuzz, security, API,
documentation, and benchmark-regression gates.
