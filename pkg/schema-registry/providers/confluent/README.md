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
subject's configured mode matches the requested mode.

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

`make integration` starts pinned Confluent Platform 8.2.0 Kafka and Schema
Registry images, runs the adapter against the real REST service, and compares
registration, lookup, compatibility, listing, and classic wire framing with
`franz-go/pkg/sr` v1.8.0 as an independent client. Containers, subjects, and
the disposable Go build cache are removed after the run.
