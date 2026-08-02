# Goal Harden: `schema-registry`

## Mission

Prove schema identity, compatibility, provider interoperability, cache safety,
wire correctness, outage behavior, and hostile-schema resistance across every
supported format and registry adapter.

## Required Audit

1. Refresh provider versions, APIs, compatibility semantics, quotas, wire
   formats, SDKs, security advisories, and supported schema specifications.
2. Inventory every format, identity, fingerprint, subject rule, operation,
   cache, offline bundle, provider adapter, codec integration, and diagnostic.
3. Prove canonical fingerprints across equivalent/non-equivalent schemas and
   test collision handling without trusting provider IDs.
4. Exhaustively test backward, forward, full, transitive, reference, deletion,
   latest-version, and unsupported compatibility semantics per provider.
5. Exercise concurrent registration, eventual consistency, duplicate schemas,
   throttling, quotas, timeout, cancellation, failover, malformed responses,
   ambiguous outcomes, and reconciliation.
6. Verify positive/negative cache bounds, stale policy, single flight, eviction,
   invalidation, poisoning resistance, offline bundles, and startup behavior.
7. Differentially test wire framing and codecs with provider SDKs and
   independent clients for JSON Schema, Avro, and Protobuf where supported.
8. Attack endpoints, credentials, redirects, schema size/depth, recursive
   references, decompression, metadata leakage, and incompatible downgrade.
9. Run fuzz, race, leak, stress, soak, fault injection, and strict resource
   limits against real or faithful provider environments.
10. Verify migration, backup/export, provider switch, dual-read/dual-register,
    rollback, and disaster-recovery procedures.

## Required Evidence

- per-provider semantic and interoperability matrices;
- exact 100% meaningful statement coverage and 100% viable mutation kills;
- differential codec/wire fixtures and compatibility corpora;
- outage, failover, cache, offline, migration, and rollback exercises;
- race, fuzz, leak, stress, soak, security, and resource-bound results;
- equivalent benchmarks against official provider clients;
- complete docs, examples, notices, and clean-consumer proof.

No generic core claim may erase a provider-specific incompatibility or turn an
unknown registration outcome into success.
