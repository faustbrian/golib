# Provider matrix

Provider and specification facts in this document were refreshed on
2026-08-10. The executable service baseline is Confluent Platform 8.3.1;
Confluent-compatible products remain separate compatibility targets.

| Contract | Confluent-compatible | AWS Glue Schema Registry |
|---|---|---|
| Provider ID | Cluster-scoped positive integer | Region/account/registry-scoped schema-version UUID |
| Name model | Subject | Registry plus schema name |
| Version | Positive subject version | Positive schema version plus UUID identity |
| Formats | Avro, JSON Schema, and Protobuf subject types accepted by the configured deployment; dialect support is product/version-specific | Avro 1.11.4; JSON Schema drafts 4, 6, and 7; proto2 and proto3 without extensions or groups |
| References | Provider supports named subject/version references; traversal is bounded and explicit | Adapter reports unsupported |
| Compatibility | Subject/global configured BACKWARD, FORWARD, FULL, transitive, or NONE | NONE, DISABLED, BACKWARD, BACKWARD_ALL, FORWARD, FORWARD_ALL, FULL, or FULL_ALL is enforced during registration; no equivalent candidate dry-run |
| Registration creation result | Cannot safely distinguish a concurrent creator; returns unknown after create API success | Duplicate calls can return the same UUID; returns unknown after registration API success |
| Listing | Bounded deterministic subject pages; list responses do not contain versions, fingerprints, or lifecycle evidence, so lifecycle is unknown | Not advertised by the focused adapter |
| Deletion | Soft and explicit permanent version deletion after fingerprint confirmation | Not advertised by the focused adapter |
| Consistency | Subject/version reads may lag deployment topology | Version status can be pending before available |
| Service limits | Deployment- or Confluent Cloud plan-specific; no numeric limit is portable across products | Hard limits include 100 registries and 10,000 total schema versions per account per Region; schema definitions are at most 170,000 bytes |
| Implemented wire scope | Legacy magic-byte/schema-ID header version 0; classic and Protobuf message-index variants | Header version 3 with compression byte 0 |
| Explicitly unsupported wire scope | GUID header version 1 | ZLIB compression byte 5 |

## Version, API, and specification refresh

| Surface | Reviewed version or specification | Authority |
|---|---|---|
| Confluent service | Platform 8.3.1 container integration | [Schema Registry REST API](https://docs.confluent.io/platform/current/schema-registry/develop/api.html) |
| Confluent official wire client | `kafka-schema-serializer` 8.3.1 | [Confluent schema-ID serializer source](https://github.com/confluentinc/schema-registry/tree/v8.3.1/schema-serializer) |
| Confluent independent client | `franz-go/pkg/sr` 1.8.0 | [franz-go Schema Registry package](https://pkg.go.dev/github.com/twmb/franz-go/pkg/sr) |
| AWS Glue API | AWS SDK for Go v2 Glue 1.152.0; Smithy Go 1.27.7 | [AWS Glue Schema Registry operations](https://docs.aws.amazon.com/glue/latest/webapi/API_Operations_AWS_Glue_Schema_Registry.html) |
| AWS Glue formats | Avro 1.11.4; JSON Schema drafts 4/6/7; Protobuf 2/3 without extensions or groups | [AWS Glue schema format documentation](https://docs.aws.amazon.com/glue/latest/dg/schema-registry.html) |
| AWS wire reference | AWS Glue Schema Registry Java SerDe 1.1.27 | [official SerDe repository](https://github.com/awslabs/aws-glue-schema-registry) |
| Avro | Apache Avro 1.12.0; `goavro/v2` 2.15.0 | [Avro 1.12.0 specification](https://avro.apache.org/docs/1.12.0/specification/) |
| JSON Schema | Draft 2020-12 is the newest portable local dialect; providers apply product-specific subsets and compatibility rules | [JSON Schema Draft 2020-12](https://json-schema.org/draft/2020-12) |
| Protobuf | proto2, proto3, and current Editions syntax are specification inputs; provider support remains narrower | [Protocol Buffers guides](https://protobuf.dev/programming-guides/) |

Confluent non-transitive modes compare the latest version, while transitive
modes compare the complete subject history. Glue `_ALL` modes use its
checkpoint/history semantics; `DISABLED` prevents new versions and `NONE`
accepts without compatibility checks. Similar names do not establish identical
behavior.

Confluent behavior is evaluated against the official [Schema Registry
documentation](https://docs.confluent.io/platform/current/schema-registry/index.html)
and [REST API](https://docs.confluent.io/platform/current/schema-registry/develop/api.html).
AWS behavior is evaluated against the official [Glue Schema Registry
guide](https://docs.aws.amazon.com/glue/latest/dg/schema-registry.html) and API.

Provider modules document their endpoint, credentials, quotas, retries, and wire
scope independently. Compatibility with one Confluent-compatible product does
not imply identical extensions, limits, or deletion behavior in another.
