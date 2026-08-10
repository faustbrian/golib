# Provider matrix

| Contract | Confluent-compatible | AWS Glue Schema Registry |
|---|---|---|
| Provider ID | Cluster-scoped positive integer | Region/account/registry-scoped schema-version UUID |
| Name model | Subject | Registry plus schema name |
| Version | Positive subject version | Positive schema version plus UUID identity |
| Formats | Avro, JSON Schema, Protobuf as configured | Avro, JSON, Protobuf as configured by the schema |
| References | Provider supports named subject/version references; traversal is bounded and explicit | Adapter reports unsupported |
| Compatibility | Subject/global configured BACKWARD, FORWARD, FULL, transitive, or NONE | Enforced during registration; no equivalent candidate dry-run |
| Registration creation result | Cannot safely distinguish a concurrent creator; returns unknown after create API success | Duplicate calls can return the same UUID; returns unknown after registration API success |
| Listing | Bounded deterministic subject pages; list responses do not contain versions, fingerprints, or lifecycle evidence, so lifecycle is unknown | Not advertised by the focused adapter |
| Deletion | Soft and explicit permanent version deletion after fingerprint confirmation | Not advertised by the focused adapter |
| Consistency | Subject/version reads may lag deployment topology | Version status can be pending before available |

Confluent behavior is evaluated against the official [Schema Registry
documentation](https://docs.confluent.io/platform/current/schema-registry/index.html)
and [REST API](https://docs.confluent.io/platform/current/schema-registry/develop/api.html).
AWS behavior is evaluated against the official [Glue Schema Registry
guide](https://docs.aws.amazon.com/glue/latest/dg/schema-registry.html) and API.

Provider modules document their endpoint, credentials, quotas, retries, and wire
scope independently. Compatibility with one Confluent-compatible product does
not imply identical extensions, limits, or deletion behavior in another.
