# Production Hardening Audit

This document records the hostile-network audit for the current pre-v1 tree.
It is a release artifact, not a claim that arbitrary caller callbacks, custom
transports, or downstream vendor code are safe without their own review.

## Threat model

Protected assets are credentials, request and response bodies, tenant and
account identity, connection capacity, memory, goroutines, file integrity,
cache isolation, fixture data, and telemetry cardinality. Adversaries can
control URLs, DNS answers, proxy destinations, redirects, headers, status
codes, compressed and partial bodies, retry hints, pagination cursors, timing,
connection closure, and recorded fixture input.

The package trusts Go's standard library and explicitly configured roots. It
also trusts caller implementations of documented interfaces to honor context,
concurrency, ownership, and secrecy contracts. Package-owned callback seams
that run inside lifecycle machinery contain panics where documented. Egress
and TLS guarantees require the package-owned transport. No production service,
database, proxy, or credential was used during this audit.

Primary protocol references are the Go [`net/http`](https://pkg.go.dev/net/http)
and [`net/url`](https://pkg.go.dev/net/url) contracts, HTTP semantics
[RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html), HTTP caching
[RFC 9111](https://www.rfc-editor.org/rfc/rfc9111.html), OAuth 2.0
[RFC 6749](https://www.rfc-editor.org/rfc/rfc6749), bearer-token usage
[RFC 6750](https://www.rfc-editor.org/rfc/rfc6750), OAuth security best current
practice [RFC 9700](https://www.rfc-editor.org/rfc/rfc9700.html), and W3C
[Trace Context](https://www.w3.org/TR/trace-context/).

## Lifecycle map

1. `RequestSpec` resolves and validates an HTTP(S) URL, snapshots metadata,
   and opens an owned request body.
2. `Client.Do` resolves a finite policy profile, assigns operation identity,
   and runs operation middleware once.
3. The standard client applies redirect and cookie-jar behavior. Every
   physical `RoundTrip`, including redirects, enters attempt middleware.
4. Attempt admission, session, idempotency, authentication, signing,
   telemetry, compression, egress, DNS, connect, proxy, TLS, write, and header
   waits run in deterministic order.
5. A middleware short circuit that never reaches its scope terminal closes the
   current request body; a reached physical transport retains standard
   `RoundTripper` ownership.
6. Retry drains and closes discarded responses before replaying an eligible
   body. The final response remains caller-owned unless a consuming helper is
   selected.
7. Status, decode, drain, transfer, cache, fixture, and resume helpers apply
   their own documented bounds and ownership transitions.
8. `Client.Close` cancels client-owned operation contexts, closes tracked
   response bodies, saves an owned session, and closes owned idle transports.

## Audited surface inventory

The review covered client construction and shutdown; standard and custom
transports; request specs, query encoders, headers, trailers, byte, form,
streaming, and multipart bodies; middleware stages and inspection; Basic,
bearer, API-key, HMAC, OAuth2, cookies, sessions, and redirects; operation
identity, idempotency, retry, rate limiters, circuit breakers, and profiles;
status classification, bounded decode, drain, compression, transfer, range,
resume, and atomic files; page-number, offset, cursor, Link, and custom
pagination; slice, generator, and channel pools; private/shared cache,
revalidation, stale policy, coalescing, and stores; policy scopes; egress, DNS,
proxy, TLS, and pins; telemetry, trace context, baggage, metrics, and logging;
strict replay, bounded recording, persistence, migration, and fixture expiry;
all examples, docs, scripts, fuzz targets, benchmarks, and CI/release gates.

Public extension ports were reviewed at their invocation seams: transports,
body openers, request editors and signers, token sources, retry clocks/jitter
and policies, limiters and observers, breakers/classifiers, decoders,
redactors/mappers, progress observers, pagination fetch/key functions, pool
selectors/sources/keys/executors, cache stores/keys/schedulers, scope values,
cookie jars, telemetry observers/propagators, fixture redactors/migrators, and
file-system ports.

## HTTP and security policy matrix

| Surface | Default policy | Explicit opt-in or caller duty | Evidence |
| --- | --- | --- | --- |
| URL and egress | Absolute HTTP(S), no userinfo; public HTTPS:443 egress | Additional schemes, hosts, ports, origins, or address classes | URL and egress fuzz/tests |
| TLS | TLS 1.2+, platform roots, hostname verification | Custom roots, fixed name, client certs, additive SPKI pins | real TLS tests |
| Authentication | HTTPS and initial-origin trust only | `AllowInsecure` for local tests; exact extra origins | auth redirect/TLS tests and fuzz |
| Redirects | standard method/body rules; attempt policy reruns | caller redirect callback; exact trusted origins | real redirects and redirect fuzz |
| Retry | off unless registered; safe method and replayable body | endpoint idempotency policy for unsafe methods | retry tests and retry fuzz |
| Timeout | finite client total plus phase timeouts | caller deadline and operation profile | cancellation integration tests |
| Bodies | final raw response is caller-owned | consuming helpers close; every buffer has a limit | lifecycle, adversarial body tests |
| Cache | private, bounded, hashed keys, credential isolation | shared storage and bounded stale directives | cache tests and five benchmarks |
| Pagination/pool | finite pages, items, bytes, elapsed, pending work | caller callbacks honor context and size metadata | tests, race, benchmarks |
| Telemetry | closed labels; no URL, body, header, or error text | allowlisted baggage and safe adapters | telemetry tests |
| Fixtures | strict ordered replay; recording omits bodies by default | bounded redactor and explicit persistence | hostile fixture tests/benchmarks |
| Direct `HTTPClient` | standard transport, timeout, and jar only | caller accepts complete pipeline bypass | bypass tests and integration docs |

`RetryOptions.MaximumElapsed` bounds whether another delay/attempt may start;
it is not an in-progress attempt deadline. The client total timeout or caller
context is the hard operation deadline. `DrainResponse` and similar helpers
depend on the response body's context-aware transport when a hostile peer can
stall reads.

## Findings and dispositions

| ID | Severity | Reproduction and impact | Disposition |
| --- | --- | --- | --- |
| HTTP-01 | High | Authentication middleware sent credentials to a trusted plain-HTTP origin | Fixed: HTTPS default plus explicit local-test opt-in |
| HTTP-02 | High | A direct request with URL userinfo or a malformed port entered origin-bound credential/session policy | Fixed: shared strict authority validation |
| HTTP-03 | Medium | A caller decoder panic crossed `DecodeResponse`; cleanup ran but the API panicked | Fixed: typed secret-safe error and close-once regression |
| HTTP-04 | Medium | A transfer or resume progress observer panic crossed the API | Fixed: typed secret-safe error and close-once regression |
| HTTP-05 | High | Attempt middleware could short circuit after request gzip started but before a transport owned the transformed body, leaving the compressor worker blocked | Fixed: scope-terminal ownership tracking, deterministic worker-exit regression, and aggregate leak gate |
| DOC-01 | Medium | Generated-client docs implied `HTTPClient()` retained middleware hardening | Fixed: bypass and reduced-guarantee wording made explicit |
| EVD-01 | Medium | Redirect and retry fuzz targets were absent | Fixed: both are in the smoke gate with retained corpus |
| EVD-02 | Medium | Allocation evidence omitted request construction and serialization workloads | Fixed: request build, query-style, and form benchmarks added to the maintained matrix |
| EVD-03 | Medium | Vendor examples compiled but had no output contract, so `go test` did not execute them | Fixed: deterministic local-TLS output examples run in the normal CI suite |
| EVD-04 | Medium | Lifecycle probes existed, but no aggregate process-exit goroutine check covered the full root suite | Fixed: `goleak` root `TestMain` plus an uncached `test-leak` gate without broad ignores |
| CI-01 | Medium | The release workflow ran `make check` without installing its workflow-lint dependencies | Fixed: pull-request and tag workflows install the same pinned Go tools and ShellCheck |
| CI-02 | Medium | The safety script relied on unquoted command substitution to pass a file list | Fixed: `find -exec` streams matched files without shell word splitting |
| API-01 | Informational | Direct `HTTPClient()` calls bypass middleware and operation identity by design | Retained compatibility surface; documented caller responsibility |
| TIME-01 | Informational | Retry elapsed policy does not interrupt an active attempt | Retained layered timeout model; documented precisely |
| CACHE-01 | Informational | Qualified `no-cache` is handled conservatively as unqualified | Retained stricter revalidation behavior |

No finding was hidden with a lint suppression, unbounded allowance, disabled
certificate verification, ignored error, or reduced assertion.

## Maintained evidence

`make check` is the release gate and runs format, vet, lint, unit/integration
tests, race detection, an uncached aggregate leak pass, 100% production-line
coverage, all seven fuzz smoke targets, the full allocation benchmark matrix,
docs and workflow checks, module integrity, `govulncheck`, and `GO-SAFETY-1`.

Real-network coverage includes HTTP/1.1, HTTP/2, TLS verification and pinning,
connection reuse, proxy routing, cancellation, retries, and redirects.
Adversarial tests cover partial and oversized bodies, content-length mismatch,
compression expansion, close failures, callback panic containment, cache
isolation and stampede behavior, fixture corruption, and lifecycle shutdown.

The benchmark gate reports allocations for direct, instrumented,
authenticated, and actually retried requests; pagination and request pools;
cache hit, miss, revalidation, stale, and concurrent stampede; limiter/breaker
composition; request construction, query and form serialization; multipart,
transfer, decode, and decompression; scope resolution; and large fixture
replay/record. Results are machine-specific and carry no noisy latency
threshold.

### Requirement evidence map

| Required behavior | Executable evidence |
| --- | --- |
| Request construction, serialization, identity, trailers, streams, multipart, and compression | `request_test.go`, `form_test.go`, `idempotency_test.go`, `trailer_test.go`, `multipart*_test.go`, `compression_test.go`, URL/header fuzz targets, and construction/body benchmarks |
| Authentication, cookies, OAuth2, redirects, retry, limiter, and breaker order | `auth_test.go`, `oauth2_test.go`, `session_test.go`, `client_test.go`, `retry_test.go`, `rate_limit_test.go`, `circuit_breaker*_test.go`, and challenge/redirect/retry fuzz targets |
| Response ownership, decode, status, transfer, range, resume, panic, and cancellation | `response*_test.go`, `status_test.go`, `transfer*_test.go`, `range_test.go`, `resume_test.go`, `lifecycle_leak_test.go`, controlled-server integration tests, and error-payload fuzzing |
| Pagination, request pools, cache states, scopes, profiles, and bounded concurrency | `pagination*_test.go`, `pool_test.go`, `cache_test.go`, `scope_test.go`, and `profile_test.go` under normal and race execution plus maintained throughput/state benchmarks |
| HTTP/1.1, HTTP/2, TLS, proxy, egress, DNS rebinding, connection reuse, and shutdown | `transport_integration_test.go`, `tls_policy_test.go`, `egress*_test.go`, `client_test.go`, and `lifecycle_leak_test.go` using controlled servers and dialers |
| Telemetry, trace/baggage isolation, fixtures, migration, corruption, and large replay/record | `observability_test.go`, `fixture_replay_test.go`, `fixture_record_test.go`, `fixture_persistence_test.go`, hostile fixture cases, and large-fixture benchmarks |
| Coverage, race, leaks, fuzz, examples, workflows, dependencies, and vulnerabilities | `make check`, root `goleak` `TestMain`, executable output examples, `actionlint`, `shellcheck`, `go mod verify`, and `govulncheck` |

### Final command evidence

On 2026-07-16, Go 1.26.5 on darwin/arm64 produced:

- `go test ./... -count=10`: passed all packages ten times with no failure;
- `go test -json ./... -count=1`: passed with zero skipped tests;
- `go list -m -u -mod=readonly all`: direct dependencies were current after
  refreshing `circuit-breaker`; and
- `make check`: passed format, vet, lint with zero issues, normal/race/fresh
  leak tests, 100.0% production coverage, seven fuzz targets, every benchmark,
  docs, `actionlint`, ShellCheck, module verification, vulnerability scanning
  with no findings, and `GO-SAFETY-1`.

## Release verdict

**Ready for pre-v1 release as audited on 2026-07-16.** The complete
`make check` gate and requirement-by-requirement completion pass succeeded on
the audited implementation. No open finding meets a release-blocker
criterion. The explicit caller duties in the policy matrix remain part of the
public contract; using a custom transport, direct `HTTPClient()`, insecure auth
opt-in, or caller extension port transfers the documented guarantee to that
caller.

Remaining risks are caller-provided transport and extension correctness,
vendor-specific policy choices, environment-specific proxy/root configuration,
and workload tuning. Fuzz runs are bounded smoke evidence rather than a proof
over every input, and benchmark values are comparative only; neither weakens
the finite runtime policies enforced by the package.

Any future credential leak, unsafe retry, unbounded work, body/goroutine leak,
SSRF bypass, cache isolation failure, race, panic from hostile input, flaky
network proof, misleading timeout statement, vulnerability, or incomplete
required evidence blocks release again.
