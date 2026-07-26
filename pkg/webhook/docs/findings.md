# Hardening findings

| Severity | Exploitability | Finding | Evidence | Disposition |
| --- | --- | --- | --- | --- |
| Critical | Remote endpoint or DNS attacker could redirect delivery to internal services | URL-only policy permitted redirect or DNS-change bypass | controlled resolver and redirect tests | Resolved: redirects refused; every dial re-resolves and validates all addresses |
| High | Concurrent duplicate requests could both execute | Replay acceptance required an atomic store contract | 64-way race test | Resolved: atomic check-and-record, hashed tenant namespace, fail closed |
| Critical | The same event could be accepted once per overlapping key | Replay digest included verification key ID | two-key rotation replay regression | Resolved: replay identity is protocol domain, namespace, and event ID only |
| High | Receiver ambiguity could multiply side effects | Retries without endpoint idempotency | delivery and adapter attempt tests | Resolved: retries require an idempotency key; durable consumers perform one attempt |
| High | Signed requests lacked the explicitly required nonce | Repeated equal canonical inputs produced equal signatures | nonce injection and mutation tests; independent vectors | Resolved: signed bounded nonce with `crypto/rand` default and injectable generator |
| Medium | Direct caller timestamps with nanoseconds failed against second-precision wire values | Any direct caller using nonzero nanoseconds | protocol-precision regression | Resolved: caller timestamps compare by Unix second |
| Medium | Signer selected keys at wall-clock time instead of an explicit signed timestamp | Historical or delayed signing could emit a signature the verifier rejects | rotation timestamp regression | Resolved: select keys at normalized signed time and reject impossible windows |
| Medium | Malformed or duplicate headers could parse ambiguously | Remote unauthenticated input | strict parser, mutation tests, fuzzer | Resolved: fixed field order, count/byte bounds, canonical encodings |
| High | Content type and idempotency semantics were not authenticated | An intermediary could change decoding or deduplication behavior without changing the body | fixed-header mutation and pre-body duplicate tests | Resolved: both fixed headers are bounded, canonicalized, and covered by v1 |
| High | Duplicate query values were sorted before signing | Reordering values could change `Query().Get` while retaining a valid MAC | duplicate-order regression and independent vectors | Resolved: keys are sorted but duplicate value order is bound |
| High | HTTP method case was normalized before signing | Changing a case-sensitive extension method could retain a valid MAC | method-case mutation regression and independent vectors | Resolved: the exact non-empty method is covered by v1 |
| Medium | Body, response, DNS, retry, or fan-out work could exhaust resources | Remote sender, endpoint, or operator input | limit and fault tests | Resolved: hard byte, address, attempt, delay, and worker bounds |
| Medium | Diagnostics could expose attacker-controlled secrets | Logging raw verification or delivery input | middleware and adapter privacy tests | Resolved: safe typed response plus closed observation/log schemas |
| Medium | Telemetry claim lacked propagation evidence | Integration could silently omit trace context | compiled telemetry transport test | Resolved: explicit secure-client wrapper injects W3C context and preserves policy |
| Medium | Linux safety scanning included cgo-enabled standard-library variants | CI contradicted `GO-SAFETY-1` even though module sources contained no cgo | failing Linux quality job and pure-Go dependency scan | Resolved: the safety scan enforces `CGO_ENABLED=0` without disabling cgo required by the Linux race detector |
| Low | An injected clock moving backward emitted negative latency | Test clocks and wall-clock adjustments could corrupt metrics | regressing-clock observation regression | Resolved: observed durations saturate at zero |
| Low | A body consumed before verification cannot be reconstructed | Misordered trusted middleware | prior-read characterization test and inbound guide | Accepted: verification must be first; authenticated context exposes only remaining exact bytes |
| Low | Named `http-client` integration is unavailable | External repository has no published Go module or API | module lookup documented in integration guide | Accepted: `HTTPDoer` is the compile-time seam until an API exists |

No open critical, high, or medium finding remains. Provider support remains
intentionally empty rather than making a claim without authoritative vectors.
