# API reference

The authoritative symbol list is Go documentation plus `api/baseline.txt`.
This page explains the contracts that types alone cannot express.

`Middleware` is `func(http.Handler) http.Handler`. `New` accepts unnamed
middleware. `Describe` adds a stable name, explicit duplicate permission, and
`Before`/`After` constraints. `Described` validates names, duplicates, order,
nil values, and the 256-layer bound. `Handler` also rejects nil terminals and
nil results.

Constructors return typed configuration errors supporting `errors.Is` and
`errors.As`. Runtime handlers cannot return Go errors, so short circuits use
HTTP responses and injected observers. Policies are copied during construction;
function fields and state sources must be safe for concurrent calls.

Observation reports standard HTTP methods and protocol versions verbatim and
classifies every other value as `OTHER`. Route and client-class metadata are
caller-owned classifications with fixed byte bounds; they must not contain raw
request targets or identities. Negative durations from an injected clock are
clamped to zero. Metadata is extracted once after downstream routing completes,
and extractor panics produce empty metadata without corrupting the response.

`observe.RecordRoute` lets route-local middleware publish one bounded route
classification to an outer observer even when a router clones the request.
It is request-scoped, synchronized, returns false when observation is absent,
and never imports or discovers a router implementation.

`When` evaluates its predicate once per request. Predicate panic propagates.
Cancellation is available through the request context; the condition does not
invent an error channel or recover policy. Conditional decorators that resolve
to nil remain construction errors when the chain is resolved.

Each subpackage's exported policy documents its defaults and bounds in Go doc.
Configured proxy prefixes, content types, compression exclusions, deadlines,
and admission waits are validated against the same resource-budget posture as
request-controlled fields.
