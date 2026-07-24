# Hardening report

This report records the evidence-driven security, correctness,
interoperability, and operational audit of `jsonrpc`. It is a living audit
artifact: an `Open` disposition is a release blocker at the stated severity,
not an implied acceptance of the risk.

## Method and severity

Protocol conclusions use the official
[JSON-RPC 2.0 specification](https://www.jsonrpc.org/specification). JSON
interoperability conclusions use [RFC 8259](https://www.rfc-editor.org/rfc/rfc8259).
Runtime conclusions use the documented contracts of Go's standard library and
the source shipped with the tested Go toolchain.

- **High**: remotely triggerable loss of availability, integrity, or protocol
  correlation under a normal deployment boundary.
- **Medium**: malformed or adversarial peers can cause ambiguous behavior,
  incorrect protocol results, or a bounded but material operational failure.
- **Low**: defensive API, diagnostics, or misuse resistance gap with limited
  direct impact.
- **Informational**: verified behavior, documentation debt, or a test-gate gap
  without a demonstrated defect.

## Baseline

The unchanged baseline was run on 2026-07-14 with Go 1.26.5 on Darwin/arm64.
All commands completed without skips:

| Command | Result |
| --- | --- |
| `test -z "$(gofmt -l .)"` | Passed |
| `go test ./...` | Passed |
| `scripts/check-coverage.sh` | Passed; 100.0% statements |
| `go vet ./...` | Passed |
| `go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 ./...` | Passed |
| `go test -race ./...` | Passed |
| `scripts/check-docs.sh` | Passed |
| `go test -run='^$' -bench=. -benchmem ./...` | Passed |
| `go test -run='^$' -fuzz='^FuzzDispatcher$' -fuzztime=30s .` | Passed |
| `go test -run='^$' -fuzz='^FuzzRequestUnmarshal$' -fuzztime=30s .` | Passed |
| `go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...` | Passed; no vulnerabilities found |
| `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7` | Passed |

The module has no runtime dependency outside the Go standard library. The
baseline proved the existing gate was healthy; it did not prove the hostile
input, correlation, and resource-bound claims below.

## Threat model

| Actor or failure | Entry point | Protected asset | Required control | Current disposition |
| --- | --- | --- | --- | --- |
| Malicious client | Dispatcher or HTTP request bytes | Availability and correct server dispatch | Strict envelopes, size and batch limits, panic containment | Parser and input limits verified |
| Malicious server | Client transport reply | Client integrity, memory, and correlation | Strict response shape, IDs, membership, and reply limits | Validation and parsing limits verified; custom acquisition is adopter-owned |
| Buggy handler | Handler result, error, panic, or blocking work | Response confidentiality and server availability | Safe error mapping, panic recovery, context propagation, explicit trust boundary | Mapping, recovery, and propagation verified; handler allocation and blocking remain adopter-owned |
| Oversized batch | Dispatcher batch array | CPU, memory, downstream capacity | Payload and member limits with deterministic rejection | Fixed and verified |
| Slow HTTP peer | HTTP body read or response wait | Connections and goroutines | Context, adopter timeouts, bounded bodies, cleanup | Body bounds verified; timeout policy is adopter-owned |
| Concurrent adopter | Registry, dispatcher, client, custom options | Race freedom and deterministic ownership | Immutable configuration and synchronized mutation | Registry and default ID generator have race-detector-backed concurrent tests; custom dependencies retain documented contracts |
| Ambiguous JSON peer | Duplicate names, case variants, malformed UTF-8 | Cross-parser agreement | Reject ambiguous protocol text before interpretation | Fixed and verified |

Authentication, authorization, TLS policy, rate limiting, and application data
validation remain adopter responsibilities. The library must still make its
own parsing, correlation, allocation, and goroutine behavior safe when those
outer controls are absent.

## Public surface inventory

The audited surface includes:

- protocol types `ID`, `Request`, `Response`, and `Error`, their custom JSON
  methods, constructors, validators, standard errors, and raw data ownership;
- `Registry`, `Dispatcher`, `Handler`, `Middleware`, `ErrorMapper`, `Hooks`,
  `DecodeParams`, and request context propagation;
- `Client`, `BatchCall`, `Transport`, `IDGenerator`, typed calls, notifications,
  batch correlation, and all exported client sentinels;
- `HTTPHandler`, `HTTPTransport`, body limits, media types, headers, methods,
  status errors, URL validation, caller clients, and body cleanup;
- sequential batch execution, registry locking, atomic ID generation, panic
  recovery, hook recovery, and every package goroutine boundary; and
- repository workflows, release tooling, fuzz targets, benchmarks,
  documentation generation, and the standard-library-only dependency graph.

Exact signatures and compatibility commitments are maintained in the
[public API reference](api.md) and [compatibility policy](compatibility.md).

## Findings

| ID | Severity | Finding and impact | Affected surface | Disposition and evidence |
| --- | --- | --- | --- | --- |
| GJ-001 | Medium | Duplicate protocol members used Go's last-value-wins behavior, allowing peers to disagree on an envelope's meaning. | Request, response, and error decoding | **Fixed.** Unique-name scanning precedes decoding; `TestProtocolDecodersRejectDuplicateMembers` covers every reserved member. This is defensive policy under [RFC 8259 section 4](https://www.rfc-editor.org/rfc/rfc8259#section-4). |
| GJ-002 | Medium | A custom client ID generator could produce duplicate IDs in one batch, overwriting pending correlation before transport I/O. | `Client.Batch` | **Fixed.** `ErrDuplicateRequestID` is returned before transport; covered by `TestClientBatchRejectsDuplicateRequestIDsBeforeTransport`. |
| GJ-003 | Medium | Go's case-insensitive struct matching accepted case variants of reserved names despite JSON-RPC's case-sensitive convention. | Request, response, and error decoding | **Fixed.** Reserved variants are rejected while unrelated extensions remain allowed; covered by `TestProtocolDecodersRejectCaseVariantsOfReservedMembers`. |
| GJ-004 | Medium | Invalid UTF-8 was silently replaced, changing methods, IDs, params, results, or error data before validation. | All protocol decoders and `Dispatcher.Dispatch` | **Fixed.** Invalid UTF-8 is rejected and server input becomes a parse error; covered by `TestProtocolRejectsInvalidUTF8` and fuzz seeds. |
| GJ-005 | High | The transport-neutral dispatcher had no payload or batch-member limit. Input size therefore controlled validation work, allocations, handler count, and downstream calls. | `Dispatcher.Dispatch` and batch dispatch | **Fixed.** Four-MiB and 1,024-member defaults are enforced before parsing or handler execution; exact-boundary and zero-side-effect behavior is covered by `TestDispatcherRejectsResourceLimitViolationsBeforeDispatch`. |
| GJ-006 | Medium | Numeric ID canonicalization parsed attacker-controlled exponent digits with `math/big.Int`, causing allocation count and memory to scale poorly. | `ID.UnmarshalJSON`, `ID.Equal`, client correlation | **Fixed.** Linear decimal-string arithmetic preserves mathematical equality; `TestIDCanonicalizationAllocationsDoNotScaleWithExponentDigits` caps a 64-KiB exponent at 50 allocations and long carry/borrow equivalence is covered by `TestDecimalExponentArithmetic`. |
| GJ-007 | Medium | Named parameter decoding inherited duplicate-name behavior inside the params object. | `DecodeParams` | **Fixed.** Unique-name validation now runs before exact-name and typed decoding; covered by `TestDecodeParams`. |
| GJ-008 | Medium | Generic transports could return arbitrarily large client replies and the client parsed them without its own configured limit. | `Client.Call`, `Client.Batch`, `Client.Notify` | **Fixed.** A four-MiB client default rejects replies before JSON parsing and is configurable through `WithMaxClientResponseBytes`; covered by `TestClientRejectsOversizedGenericTransportResponse`. Custom transports remain responsible for bounding their own acquisition. |
| GJ-009 | Low | The zero value of `Registry` panicked on registration because its method map was nil. | `Registry.Register` | **Fixed.** The map is initialized under the registry write lock and zero-value registration and lookup are covered by `TestRegistryZeroValueIsUsable`. |
| GJ-010 | Low | Nil functional options and a nil context passed directly to HTTP transport could panic at API boundaries. | Constructors and `HTTPTransport.RoundTrip` | **Fixed.** Constructors consistently ignore nil options and HTTP request construction errors return before network I/O; covered by `TestConstructorsIgnoreNilOptions` and `TestHTTPTransportRejectsNilContextWithoutNetworkIO`. |
| GJ-011 | Informational | Fuzzing covered dispatcher and request decoding only; response, error, ID, client correlation, and round-trip properties lacked dedicated fuzz targets. | Test and CI gates | **Fixed.** Dedicated targets now exercise every listed boundary locally, in pull requests, on the weekly schedule, and during releases. Seeds include the checked-in official corpus, invalid UTF-8, duplicate members, deep JSON, large batches, and hostile exponent IDs. |
| GJ-012 | Informational | Existing benchmarks covered representative dispatch and one hostile exponent but omitted maximum payload and malicious-server reply boundaries. | Benchmark gate | **Fixed.** Accepted and rejected maximum payloads, accepted and rejected maximum batches, oversized client replies, and hostile numeric IDs now have reproducible benchmarks. Slow network timing remains transport- and deployment-owned rather than assigned a nonportable library budget. |
| GJ-013 | Medium | The default HTTP client followed redirects, allowing arbitrary configured headers such as API keys to be forwarded to another origin. | `HTTPTransport` default client | **Fixed.** The default client returns redirects as bounded `HTTPStatusError` values without a second request; covered by `TestHTTPTransportDoesNotFollowRedirectsByDefault`. A caller may explicitly supply a client with its own redirect policy. |
| GJ-014 | Low | Direct calls to `ID.UnmarshalJSON` accepted a valid ID followed by another JSON value because the streaming decoder did not require EOF. | `ID.UnmarshalJSON` | **Fixed.** A second decode must reach EOF before state is committed; covered by `TestProtocolDefensivePaths` and `FuzzIDRoundTrip`. |
| GJ-015 | Low | `StringID` encoded invalid UTF-8 with Go's replacement behavior but retained the original invalid bytes for equality, so an echoed response could never correlate. | `StringID`, client response matching | **Fixed.** The canonical value is decoded from the exact generated JSON; covered by `TestStringIDCorrelationMatchesItsJSONEncoding`. Malformed UTF-8 received directly from a peer remains rejected. |

## Verified defensive properties

- Handler and middleware panics are contained as internal errors and their
  causes are not serialized.
- Hook panics cannot alter protocol output; hook-observed response data is
  copied before invocation.
- Registry lookup and registration use an `RWMutex`; the default client ID
  generator uses `atomic.Int64`.
- Server batches execute sequentially, so input cannot create goroutine fan-out.
- Direct dispatch rejects more than four MiB or 1,024 batch members before any
  handler call. The limits remain enabled when nonpositive options are supplied.
- HTTP request and response bodies default to four MiB; request overflow is
  `413`, response overflow is `ErrResponseTooLarge`, and bodies are closed.
- Go 1.26.5's `encoding/json` scanner enforces a nesting ceiling of 10,000.
  A library-level shallower limit has not yet been justified by evidence.
- Internal handler errors, panic values, and stack traces are retained only as
  local causes; the wire message remains `Internal error`.
- Cancellation reaches client transports, middleware, and handlers. HTTP body
  lifetimes are closed on success and read failure, and one-byte reads exercise
  the same bounded path as ordinary request bodies.

Handlers, middleware, hooks, custom transports, custom ID generators, and
caller-supplied HTTP clients are trusted extension points. The package cannot
bound allocations performed inside adopter code or interrupt code that ignores
context; deployment HTTP timeouts and application output policy remain required.

On the baseline Apple M4 Max, the new hostile-boundary benchmarks measured:

| Benchmark | Time | Bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Maximum 1,024-member batch | 2.28 ms | 2,661,007 | 47,133 |
| Rejected 1,025-member batch | 0.26 ms | 3,543 | 26 |
| Maximum four-MiB payload | 1.33 ms | 6,346 | 83 |
| Rejected four-MiB-plus-one payload | 1.52 us | 1,025 | 17 |
| Rejected four-MiB-plus-one client reply | 0.97 us | 873 | 18 |

These measurements justify pre-dispatch rejection and establish a local
comparison point; they are not portable latency budgets.

`BenchmarkDispatchMaximumPayload`, `BenchmarkDispatchRejectedPayload`, and
`BenchmarkClientRejectedResponse` provide the equivalent byte-limit comparison
points. Network deadlines are deliberately not benchmarked: custom transports,
HTTP client timeouts, server timeouts, and request contexts own those policies.

The corrected 64-KiB exponent benchmark improved from approximately 5.14 ms,
11.0 MB, and 1,009 allocations to 0.39 ms, 0.48 MB, and 17 allocations on the
same machine. The regression asserts allocation count rather than timing.

## Release readiness

**Final verdict: release-ready for the first `v1.0.0` tag.** Every finding is
fixed or assigned to an explicit trusted extension boundary, every normative
conformance row has direct passing evidence, and the final gate completed
without skips on 2026-07-14 using Go 1.26.5 on Darwin/arm64.

| Final command | Result |
| --- | --- |
| `test -z "$(gofmt -l .)"` | Passed |
| `go test ./...` | Passed |
| `scripts/check-coverage.sh` | Passed; 100.0% production statements |
| `go vet ./...` | Passed |
| `go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 ./...` | Passed |
| `go test -race ./...` | Passed |
| `scripts/check-docs.sh` | Passed |
| `go test -run='^$' -bench=. -benchmem ./...` | Passed |
| All seven `go test -run='^$' -fuzz='^TARGET$' -fuzztime=30s .` commands | Passed |
| `go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...` | Passed; no vulnerabilities found |
| `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7` | Passed |

The semantic-version recommendation is `v1.0.0`: the remote has no published
tags, the stable API and compatibility contract are now documented, and the
full hardening set is included in the dated `1.0.0` changelog section. Trusted
handlers and custom transports still require application output policy,
timeouts, cancellation cooperation, authentication, authorization, TLS, and
rate limiting as documented above; these are deployment boundaries, not open
library findings.
