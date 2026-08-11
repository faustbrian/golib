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
| IDs, subjects, numeric versions, references | focused adapter tests and pinned Confluent Platform 8.3.1 integration |
| Backward, forward, full, transitive, none | REST mode matrix plus 48-case live Avro, JSON Schema, and Protobuf compatibility corpora, including two-version non-transitive/transitive distinctions |
| Retry, throttling, authentication, malformed and oversized responses | faithful HTTP transport fault matrix |
| Listing and soft/permanent deletion | bounded HTTP contract tests; live subject listing and cleanup |
| Classic and Protobuf wire version 0 | fuzz tests plus Avro, JSON Schema, and Protobuf byte differentials against franz-go v1.8.0 and byte/benchmark comparisons with Confluent's official Java schema-ID serializer 8.3.1 |
| JSON Schema value codec | bounded validation plus JSON payload differential composed with a live registered schema ID |
| GUID header wire version 1 | explicitly unsupported; a version-0 parser never accepts it as a classic frame |
| Quotas and product-specific extensions | caller policy; not generalized across compatible products |

## AWS Glue Schema Registry adapter

| Boundary | Evidence and scope |
|---|---|
| UUID IDs, registry/schema names, versions, lifecycle | real AWS SDK v2 client against a faithful local Smithy JSON service; optional caller-selected live-service integration |
| Compatibility | explicitly unsupported as a candidate dry-run; Glue enforces configured policy during registration |
| Registration, duplicates, eventual lifecycle, ambiguous outcomes | faithful serialized service exchanges and exact-definition reconciliation |
| Throttling, quotas, deadlines, cancellation, malformed responses | faithful SDK retry/transport/error exchanges plus bounded adapter fault matrices |
| References, listing, deletion | explicitly unsupported by the focused adapter |
| Header version 3, uncompressed payloads | fuzz tests and byte differential against AWS Glue Java SerDe v1.1.27 |
| Header version 3, ZLIB compression byte 5 | recognized and explicitly unsupported |
| Live service | optional `live-integration` target for read-only latest/by-ID resolution and idempotent registration against an existing AVRO schema |

Glue `integration`, `conformance`, and `check-release` require no AWS account or
credentials and do not skip any faithful exchange. The separate live target
requires caller-selected non-production identifiers and an AWS credential
source; it is additional evidence, not a prerequisite for local conformance.
