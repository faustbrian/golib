# Verification provenance

Provider evidence is pinned independently of provider identity:

| Input | Version | Immutable identity |
|---|---:|---|
| Confluent Platform Kafka | 8.3.1 | `sha256:0ad069035863aa1b090f4d9af47bfd2c08dc32864f3575d7d8579e3155c2586d` |
| Confluent Platform Schema Registry | 8.3.1 | `sha256:f0cfd047a839c1ace54d93b92e3459f0d03dc3b5c9db1192a2246fd79b4f44c4` |
| Confluent Java schema-ID serializer | 8.3.1 | `sha256:b9cb84802804bb7feff2bc998b386ccd3e6b1a2342d168b21e792cfb62dabdcb` |
| franz-go Schema Registry client | 1.8.0 | Git commit `f9ccda5bd05883e50d9885ecff0c45f509efb045` |
| AWS Glue Schema Registry Java SerDe | 1.1.27 | Git commit `b280404e615b4e63e2fb33b1aedc228e039fbf31` |
| AWS SDK for Go v2 Glue | 1.152.0 | Go module checksum in `providers/glue/go.sum` |
| AWS Smithy Go | 1.27.7 | Go module checksum in `providers/glue/go.sum` |
| Maven reference runtime | 3.9.11, Temurin 21 | `sha256:6fdc855a6ed81d288ca7ca37ac6ff5e9308b612485c0801d70b25a858c83d237` |

This matrix was refreshed on 2026-08-10. `make provenance` verifies remote tag
and image identities and then confirms the integration scripts still select
them. `make sbom` creates and validates a
temporary CycloneDX document covering the core and both provider modules. No
generated SBOM, dependency cache, Maven repository, or Go build cache is left
in the source tree after either gate.

`go.uber.org/goleak` v1.3.0 is a test-only release dependency in the core and
both provider modules. The `leak`, `fault-injection`, `stress`, and `soak`
targets are bounded and package-local. Stress uses the race detector; soak uses
deterministic repeated cache, graph, migration, retry, malformed-response, and
provider concurrency scenarios. These gates do not replace the pinned service
and independent-client integrations listed above.

Glue conformance drives the pinned AWS SDK v2 client through its actual Smithy
JSON serializer, SigV4 signer, HTTP transport, error decoder, and retryer against
a faithful local service. This credential-free integration is a release gate.
The caller-selected live AWS target is read-only, optional, and reported
separately; its absence does not weaken or skip the faithful gate.

The Glue interoperability gate publishes equivalent 1,024-byte framing latency
for the official Java SerDe and Go framer. The Confluent gate does the same for
classic and Protobuf framing with the official Java `PrefixSchemaIdSerializer`,
and also retains the franz-go comparison. Java reports the arithmetic mean over
100,000 operations after 10,000 warmups; Go uses the standard `testing.B`
calibrator for one second and reports allocations. Every comparison uses the
same 1,024-byte payload and provider identity.
