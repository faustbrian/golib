# AWS Glue Schema Registry adapter

This independently releasable module preserves AWS Glue registry/schema names,
numeric versions, UUID schema-version IDs, lifecycle state, service errors, and
wire framing. UUIDs remain scoped provider identities, never portable schema
fingerprints.

`Config` receives a narrow AWS SDK v2 Glue client already configured with
region, endpoint, credentials, and SDK retry policy. The adapter adds one total
deadline and a concurrency bound without adding a second retry loop. Use
least-privilege IAM permissions for the exact registry and schema operations.
The caller-provided scope should identify region, account, and registry.

Glue supports Avro, JSON Schema drafts 4/6/7, and proto2/proto3 within service
limits; configure a matching local canonicalizer. Unknown formats are rejected
during construction. The service applies its
configured compatibility policy during registration and does not expose an
equivalent candidate dry-run, so `CheckCompatibility` is explicitly unsupported.
The focused adapter does not advertise schema references, listing, or deletion.

Registration first resolves the exact definition. After registration succeeds,
creation outcome is unknown because duplicate/concurrent calls can return the
same UUID. Resolve exposes pending, available, deleting, failed, or unknown
lifecycle state. The service schema-definition limit is enforced at 170,000
bytes.

`UncompressedFramer` implements AWS header version 3, compression byte 0, and a
16-byte UUID. ZLIB byte 5 is recognized and explicitly unsupported. See the
official [Glue Schema Registry guide](https://docs.aws.amazon.com/glue/latest/dg/schema-registry.html).

## Integration verification

`make interoperability` compares framing with the pinned official AWS Glue
Schema Registry Java SerDe v1.1.27 in an isolated Maven container. `make
integration` performs read-only resolution and idempotent-registration checks
against a caller-selected existing AVRO schema using the default AWS credential
chain. It requires `SCHEMA_REGISTRY_GLUE_INTEGRATION_REGION`,
`SCHEMA_REGISTRY_GLUE_INTEGRATION_REGISTRY`, and
`SCHEMA_REGISTRY_GLUE_INTEGRATION_SCHEMA`; it refuses to create a version if
the service cannot find the latest definition. Both checks remove their
disposable Go caches after execution.
