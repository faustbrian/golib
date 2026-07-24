# Testing Integrations

Use deterministic seams for clocks, jitter, identifiers, continuation keys,
transports, and fixture migration. Avoid real sleeps and public network calls.

Contract tests should combine strict scripted transports, real
`httptest.Server` framing, and sanitized representative fixtures. Cover success,
vendor errors, timeout, cancellation, truncation, malformed responses,
redirects, retries, connection reuse, and every response ownership exit. Use
TLS and HTTP/2 test servers where those behaviors matter.

Race-sensitive tests should share token sources, caches, coalescing state,
limiters, pagination, pools, breaker adapters, and transports under
`go test -race`. Leak regressions should prove that cancellation joins pending
operations and that discarded bodies, timers, workers, files, and connections
are closed through deterministic probes rather than timing-only assertions.

The repository gate is:

```console
make check
```

It runs normal, race, and uncached aggregate goroutine-leak tests; exact
production coverage; seven bounded fuzz smoke targets; allocation-reporting
benchmarks; lint; vet; docs; workflow and shell-script lint; module integrity;
vulnerability scanning; and `GO-SAFETY-1`. The root `TestMain` applies
`goleak` to every root-package test exit without broad ignore rules. For
fixture construction, recording, persistence, migration, and hostile-input
rules, see `testing-fixtures.md`.
