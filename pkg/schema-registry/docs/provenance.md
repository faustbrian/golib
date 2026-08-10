# Verification provenance

Provider evidence is pinned independently of provider identity:

| Input | Version | Immutable identity |
|---|---:|---|
| Confluent Platform Kafka | 8.2.0 | `sha256:acbbf674f2ed40e5d0a8ca51beb0f00692c866fc22b5ce06f8cadbdc54cd4436` |
| Confluent Platform Schema Registry | 8.2.0 | `sha256:7ec0b15c6d5a64aa95b4201db5231ea58952b035869b95bed45624468ce10b34` |
| franz-go Schema Registry client | 1.8.0 | Git commit `f9ccda5bd05883e50d9885ecff0c45f509efb045` |
| AWS Glue Schema Registry Java SerDe | 1.1.27 | Git commit `b280404e615b4e63e2fb33b1aedc228e039fbf31` |
| Maven reference runtime | 3.9.11, Temurin 21 | `sha256:6fdc855a6ed81d288ca7ca37ac6ff5e9308b612485c0801d70b25a858c83d237` |

`make provenance` verifies remote tag and image identities and then confirms the
integration scripts still select them. `make sbom` creates and validates a
temporary CycloneDX document covering the core and both provider modules. No
generated SBOM, dependency cache, Maven repository, or Go build cache is left
in the source tree after either gate.

`go.uber.org/goleak` v1.3.0 is a test-only release dependency in the core and
both provider modules. The `leak`, `fault-injection`, `stress`, and `soak`
targets are bounded and package-local. Stress uses the race detector; soak uses
deterministic repeated cache, graph, migration, retry, malformed-response, and
provider concurrency scenarios. These gates do not replace the pinned service
and independent-client integrations listed above.
