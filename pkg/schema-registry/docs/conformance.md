# Conformance and hardening matrix

Conformance is recorded per provider and per semantic boundary. A passing core
test never upgrades an unsupported provider capability, and a wire fixture does
not prove service behavior.

## Portable core

| Contract | Executable evidence |
|---|---|
| Canonical and non-equivalent fingerprints, collision rejection | format canonicalizer tests; bundle collision and round-trip tests |
| Registration outcomes and concurrent idempotency | client outcome matrix and concurrent registration tests |
| Lookup identity, lifecycle, compatibility, listing, deletion | client contract-validation and administrative tests |
| References, cycles, missing nodes, graph limits | reference graph, bundle, and reference fuzz tests |
| Positive/negative cache, stale policy, invalidation, poisoning, offline mode | cache behavior, cancellation, and fault-injection tests |
| Hidden-I/O prohibition and bounded codecs | codec composition tests, immutable bundle tests, clean-consumer proof |
| Provider migration, failover, and rollback | explicit dual-registration/cutover/failover/rollback exercise |
| Leaks, races, sustained concurrency and failures | `leak`, `race`, `stress`, `soak`, and `fault-injection` targets |

## Confluent-compatible adapter

| Boundary | Evidence and scope |
|---|---|
| IDs, subjects, numeric versions, references | focused adapter tests and pinned Confluent Platform 8.2.0 integration |
| Backward, forward, full, transitive, none | REST mode matrix; live integration verifies configured backward behavior |
| Retry, throttling, authentication, malformed and oversized responses | faithful HTTP transport fault matrix |
| Listing and soft/permanent deletion | bounded HTTP contract tests; live subject listing and cleanup |
| Classic and Protobuf wire version 0 | fuzz tests plus byte differential against franz-go v1.8.0 |
| Quotas and product-specific extensions | caller policy; not generalized across compatible products |

## AWS Glue Schema Registry adapter

| Boundary | Evidence and scope |
|---|---|
| UUID IDs, registry/schema names, versions, lifecycle | faithful AWS SDK API tests and caller-selected live-service integration |
| Compatibility | explicitly unsupported as a candidate dry-run; Glue enforces configured policy during registration |
| Throttling, limits, cancellation, malformed successes | Smithy error and bounded API fault matrices |
| References, listing, deletion | explicitly unsupported by the focused adapter |
| Header version 3, uncompressed payloads | fuzz tests and byte differential against AWS Glue Java SerDe v1.1.27 |
| Live service | read-only latest/by-ID resolution and idempotent registration against an existing AVRO schema |

The live Glue row requires caller-selected non-production service identifiers
and AWS credentials. Missing credentials or identifiers fail conformance rather
than silently skipping it.
