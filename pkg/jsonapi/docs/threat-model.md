# Threat model

## Scope and trust boundaries

The package accepts attacker-controlled JSON bytes, decoded URL query values,
and media type header strings. It also calls application-controlled extension
validators, profile validators, cursor/sort callbacks, and Atomic transaction
implementations.

The package does not read request bodies, perform network requests, authorize
users, query databases, log values, or write HTTP responses. Those are caller
trust boundaries and are not implied by protocol conformance.

## Protected properties

- Availability: bounded parsing and validation work without panics or runaway
  allocation.
- Integrity: strict document shapes, deterministic output, full linkage, and
  ordered all-or-fail Atomic execution.
- Confidentiality: public errors do not echo payloads, cursors, URLs,
  extension values, callback text, or panic values by default.
- Interoperability: accepted and emitted data follows JSON:API, its claimed
  official extension/profile, and applicable HTTP selection semantics.

## Threats and controls

| Threat | Entry point | Primary controls | Caller responsibility |
| --- | --- | --- | --- |
| Oversized or deeply nested JSON | Core, configured, and Atomic unmarshal | byte, depth, member, item, and total-value limits; duplicate and UTF-8 preflight | limit transport body size before allocation |
| Huge decoded query families | `QueryParser.Parse` | name, value, total-byte, selector, list, parameter, and value counts | limit encoded request-target size |
| Header and candidate explosion | `Negotiator` | header, candidate, URI count/length, and configured URI limits | configure server header limits |
| Ambiguous or duplicate input | JSON codecs | duplicate-member rejection, strict unknown-member rules, presence-aware models | map typed failures without leaking request bodies |
| Numeric precision loss | attributes, meta, extension values | `json.Number` preservation | convert numbers with domain-specific range checks |
| Extension/profile confusion | configured `Codec`, `Negotiator` | unique absolute URIs, unique namespaces, scope-specific registration, core validation before profile semantics | keep codec and negotiator URI sets aligned |
| Malicious validator or callback | extension, profile, cursor, sort seams | bounded invocation count; panic containment; redacted wrapper errors; profile mutation detection | make callbacks bounded, pure, deterministic, and thread-safe; never expose retained causes to clients |
| Partial Atomic success | `ExecuteAtomic` | ordered apply, response validation before commit, rollback on every post-begin failure | implement a real transaction and never commit from `ApplyAtomic` |
| Transaction panic or cancellation | `ExecuteAtomic` | panic conversion, context checks, non-canceled exactly-once rollback attempt | honor context and make rollback idempotent |
| Error-data disclosure | public errors | stable generic messages with inspectable causes | do not serialize `Cause`, panic `Value`, or raw validation errors directly |
| SSRF through links or operation hrefs | URI-reference values | syntax validation only | treat URLs as data; apply allowlists before dereferencing |
| Constructed recursive links | `Document.Validate`, marshal entry points | `describedby` depth bound and pointer-cycle detection | avoid cycles in all caller-owned application values |
| Mutable data races and aliasing | maps, slices, callbacks | registries copied at construction; validation and marshal do not mutate documents; concurrent codec regression | do not mutate documents or callback state concurrently; copy application-owned maps when sharing |

## Abuse cases

Adversarial tests and fuzz seeds cover invalid UTF-8, duplicate members,
trailing JSON values, deep composites, collection explosions, malformed media
types and qvalues, media-range precedence, query-family explosions, cursor
boundaries, callback failures, panics, cancellation, rollback failures,
registry mutation, cursor metadata, and constructed round trips.

Failures are expected to be typed and deterministic. Resource-limit failures
are package policy rather than JSON:API validation failures. Applications may
lower or deliberately raise limits, but should document that choice as an API
boundary decision.

## Residual risk review

Review this model when a new decoder, query family, extension/profile scope,
callback, transaction phase, network helper, reflection-based mapper, or code
generator is introduced. Any new untrusted loop or recursion requires an
explicit bound and an adversarial regression before release.
