# Confluent-compatible schema registry adapter

This independently releasable module preserves Confluent subject, integer ID,
version, reference, compatibility, and deletion semantics. It does not claim
that every Confluent-compatible service has identical extensions or quotas.

`Config` requires one HTTPS endpoint, provider scope, injected transport,
request deadline, response/retry/concurrency/reference bounds, and explicit
format canonicalizers. HTTP is available only through the test flag. URL
userinfo, query, fragments, redirects, and implicit credential forwarding are
not accepted. Unknown canonicalizer formats are rejected during construction.

The adapter retries transport failures, throttling, and server errors within one
total deadline. Registration performs an exact-content lookup first. A
successful create call reports an unknown creation outcome because a concurrent
caller may have created the version. Compatibility is checked only when the
effective subject or global mode matches the requested mode.

`ClassicFramer` implements version-0 Avro/JSON framing. `ProtobufFramer`
implements the version-0 message-index vector. IDs are scoped to the configured
cluster and are not portable fingerprints. Listing returns bounded subject
descriptors with unknown lifecycle because the response does not distinguish
active and soft-deleted state. Soft or permanent version deletion requires an
exact fingerprint confirmation.

Authentication is supplied by `CredentialProvider` for the configured endpoint.
Use least-privilege credentials, service-specific rate limits, and an endpoint
allowlist. See the upstream [REST API](https://docs.confluent.io/platform/current/schema-registry/develop/api.html)
and [wire format](https://docs.confluent.io/platform/current/schema-registry/fundamentals/serdes-develop/overview.html).

## Integration verification

`make integration` starts pinned Confluent Platform 8.3.1 Kafka and Schema
Registry images, runs the adapter against the real REST service, and compares
registration, lookup, listing, references, all compatibility modes across Avro,
JSON Schema, and Protobuf, and classic/Protobuf wire framing with
`franz-go/pkg/sr` v1.8.0 as an independent client. The JSON Schema fixture
also exercises its bounded value codec through a registered schema. Containers,
subjects, and the disposable Go build cache are removed after the run.

`make interoperability` compares classic and Protobuf framing byte-for-byte
with Confluent's official Java `PrefixSchemaIdSerializer` from
`kafka-schema-serializer` 8.3.1. It also publishes equivalent 1,024-byte
framing benchmarks for the official serializer and the Go framers. The Maven
runtime, primary Confluent artifact checksum, Maven cache, and Go build cache
are isolated and verified by the gate.

## Ecosystem

Use the [Golib documentation portal](https://github.com/faustbrian/golib/blob/main/docs/index.md)
to choose companion packages, supported stacks, recipes, and operations guidance.
