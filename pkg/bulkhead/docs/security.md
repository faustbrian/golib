# Security

## Threat model

Untrusted callers may attempt to create high-cardinality resource identities,
fill queues, request pathological weights, hold operations indefinitely,
trigger observer failures, or cause retry amplification.

## Controls

- resource and revision labels are length- and character-bounded;
- registry creation is explicit and has a fixed maximum;
- lookup never creates and automatic eviction does not exist;
- queue cardinality and wait time are mandatory for waiting;
- weight must be positive and no greater than capacity;
- events do not retain contexts, operation results, or arbitrary errors;
- observers run outside locks and cannot alter admission by failing or
  panicking;
- no finalizer or lease falsely reclaims capacity from possibly live work;
- no package-global state or environment-derived configuration exists.

Resource names and policy revisions still belong to the caller. Use stable
non-secret classes such as `inventory-db`, not raw hostnames with credentials,
customer IDs, request paths, tokens, or payload data.

## Denial of service

A bounded queue can still retain expensive caller state outside this package.
Choose the smallest useful queue and a short maximum wait. Do not respond to
saturation by adding retries, hedges, or unbounded partitions. Operations that
ignore cancellation can exhaust capacity; enforce transport deadlines and
audit callback cancellation.

## Supply chain

Production permit accounting uses `golang.org/x/sync` v0.22.0. Failsafe-Go
v0.9.6 and goleak v1.3.0 are test/benchmark dependencies. Exact checksums are
in `go.sum`; repository gates run vulnerability, license, SBOM, secret, and
provenance checks.
