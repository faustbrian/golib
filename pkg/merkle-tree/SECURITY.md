# Security

Report suspected vulnerabilities privately to the repository maintainers
before public disclosure. Do not attach confidential leaves, persisted trees,
roots from private systems, or proof corpora to a public report.

## Trust model

A proof establishes only that supplied leaf bytes participate in a particular
cryptographic relationship under a trusted complete root identity:

```text
(profile, profile version, hash algorithm, tree size, root digest)
```

It does not establish truth, freshness, authorization, uniqueness, ordering
policy outside the selected profile, or semantic validity. Applications must
authenticate the complete identity and its source. Digest equality is not
permission to discard or convert profile identity.

Security relies on SHA-256 collision and second-preimage resistance. The
canonical and RFC 9162 profiles domain-separate leaves and branches. No
caller-supplied hash callback or algorithm registry is supported; this prevents
callback reentrancy, digest-length confusion, and algorithm downgrade through
an untrusted proof.

## Hostile input

Use the operation-specific limit type at every untrusted boundary. Limits
cover leaf count, per-leaf and total bytes, indexes, proof elements, selected
leaf cardinality, traversal depth, retained nodes, encoded bytes, node reads,
and temporary bytes. Use a deadline or cancellable `context.Context` for
meaningful work. A default is a compatibility ceiling, not an application
resource budget.

Decoders reject unknown identities, impossible counts, arithmetic overflow,
non-canonical shape, missing or surplus nodes, truncation, and trailing data
before performing derived allocation or traversal. Verification distinguishes:

- unsupported profile, algorithm, or encoding version;
- malformed proof or encoding;
- configured resource exhaustion; and
- a well-formed proof that fails cryptographic authentication.

Do not turn any of these classes into an acceptance path. Error values and
`ResourceError` contain metadata only and never leaf bytes.

## Ownership and concurrency

`RawLeaf` copies caller bytes. Roots, digests, proofs, and snapshots return
copies at byte-slice boundaries. Immutable snapshots and proof values are safe
for concurrent read-only use. `Builder` and `RootBuilder` are mutable,
caller-owned, and require external synchronization. Do not race append,
snapshot, root, marshal, or resume operations.

The package starts no hidden goroutines. A storage implementation or
application that adds parallelism must bound fan-out, propagate cancellation,
wait for shutdown, and avoid holding locks while invoking untrusted storage or
hashing code.

## Persisted snapshots

The package has no database, filesystem, object-store, or publication adapter.
`MarshalBinary` returns one canonical snapshot object and `ParseSnapshot`
validates its entire postorder node graph, shape, branch digests, root, and
limits before returning. Shared, cyclic, reordered, corrupt, missing, and
surplus nodes are rejected.

The persisted cumulative raw-leaf byte count is not authenticated by the
Merkle root because raw leaf lengths are not committed separately.
`ResumeBuilder` therefore requires the expected count from independently
trusted caller state. Never derive that trusted argument from the same
untrusted snapshot.

Durability remains the caller's responsibility. Write new immutable bytes and
all required metadata durably before atomically publishing the new root
identity. A successful publication must not point to missing data. Protect
against stale writers with transactional compare-and-swap or equivalent
serialization. Retain the prior valid snapshot until the new publication is
recoverable, and validate again after every read.

## Operational and supply-chain risks

- Reject a proof whose operation, profile, version, algorithm, size, or root is
  not the one the application requested; successful parsing is not
  verification.
- Keep proof and snapshot size ceilings well below process memory limits.
  Parsing or hashing attacker-selected data can otherwise be a CPU or
  allocation denial of service.
- Never log raw leaves or serialized snapshots by default. Scrub fuzz failures,
  crash artifacts, benchmark corpora, and error wrappers before sharing them.
- Pin conformance fixtures and dependencies. Review provenance, checksum,
  license, and semantic changes before updating them.
- Rebuild trusted roots from source leaves when corruption, stale publication,
  or dependency compromise is suspected. Compare the complete identity, not
  only digest bytes.

There is no encryption or confidentiality property. A proof discloses the
caller-supplied leaf and sibling digests needed for its operation.
