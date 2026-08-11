# Conformance report

## Claim boundary

This is a coverage and provenance report for the executable artifacts shipped
with the module. It is not a certification from the CloudEvents project and is
not proof that the archived `cloudevents/conformance` command was executed
against a live sender or receiver.

The upstream conformance repository is archived. At pinned tag v0.4.1 it
provides one HTTP feature and one Kafka feature. Those features are
receiver-oriented happy paths, not a requirement-by-requirement oracle for the
CloudEvents core, JSON format, HTTP binding, or Kafka binding. Current pass/fail
results belong to the repository gate evidence; this document deliberately does
not preserve a stale local run as a permanent result.

Requirement-level traceability is maintained in the
[normative requirement matrix](../specification/normative-requirements.json).
Its source keyword-line inventory is checked against the 19 pinned v1.0.2 files;
an artifact link is traceability, not a recorded pass. Observable
interpretations and accepted errata are governed by the
[specification decision register](specification-decisions.md).

## Official fixture mapping

| Official feature scenario | Local executable mapping | Covered assertion | Not covered by that scenario |
| --- | --- | --- | --- |
| HTTP binary, `application/json` | `TestOfficialConformanceHTTPBindingScenarios` | Required context, time, content type, and JSON payload decode | encode, failures, limits, cancellation, ownership, other data forms |
| HTTP binary, `application/json; charset=utf-8` | `TestOfficialConformanceHTTPBindingScenarios` | Same receiver mapping with a media-type parameter | parameter conflicts and non-JSON content |
| HTTP structured, `application/cloudevents+json` | `TestOfficialConformanceHTTPBindingScenarios` | Structured JSON receiver mapping | batch, encode, redundant metadata, unsupported formats |
| HTTP structured, media-type parameter | `TestOfficialConformanceHTTPBindingScenarios` | Structured receiver mapping with `charset` | arbitrary parameters and duplicate content type |
| Kafka binary | `TestOfficialConformanceKafkaBindingScenarios` | Required headers, time, content type, and JSON value decode | key, tombstone, duplicate headers, limits, ownership, encode |
| Kafka structured, `application/cloudevents+json` | `TestOfficialConformanceKafkaBindingScenarios` | Structured JSON record decode | encode, conflicts, unsupported formats, batch |
| Kafka structured, media-type parameter | `TestOfficialConformanceKafkaBindingScenarios` | Structured record decode with `charset` | arbitrary parameters and duplicate content type |

`TestPinnedOfficialConformanceFixtures` verifies the exact vendored fixture
digests before the transcribed mappings are exercised. `make conformance` first
runs `scripts/check-specification-evidence.sh`, which validates the manifest and
all local fixture and lockfile digests, and then runs the mapped Go scenarios.

## Independent SDK interoperability

| SDK | Direction and mode currently exercised | Explicit omissions |
| --- | --- | --- |
| Go SDK core v2.16.2 | JSON, JSON batch, and HTTP binary and structured modes in both directions, with exact context, payload, selected-extension, and opaque-extension checks; JSON edge cases cover absent, explicit null, empty text, empty binary, and parameterized JSON | `SetData(nil)` normalizes null to absent; explicit `json.RawMessage("null")` preserves null; JSON batch uses slices because sdk-go exposes no separate batch transport abstraction; no official certification claim |
| Go SDK Kafka Sarama v2.16.2 | Kafka binary and structured modes in both directions, with selected and opaque extensions and exact payload checks | The SDK consumer adapter does not expose the inbound Kafka key; no Kafka batch claim |
| JavaScript SDK v10.0.0 | JSON and batch in both directions; an SDK-produced edge batch covers absent, null, empty text, empty binary, and parameterized JSON; canonical non-empty HTTP binary/structured and Kafka records assert the complete context, selected and opaque extensions, and semantic payload in both implemented directions | The SDK HTTP helpers normalize timestamps to millisecond form; structured HTTP collapses null and empty data to absent; binary HTTP materializes a default content type for absent data, renders JSON null as text, and collapses empty binary data to absent. JavaScript-produced binary Kafka is deliberately rejected because it redundantly emits `ce_datacontenttype`; no Kafka batch claim |

These examples establish the listed wire and edge-case compatibility only.
Duplicate or conflicting metadata and malformed inputs are exercised as local
hostile-input contracts because conforming producer SDK APIs do not originate
those invalid messages. Broker retries, compression, partial writes, and
shutdown are not binding-record operations owned by this module and are not
claimed as SDK interoperability evidence.

## Owned evidence outside the official features

| Contract | Executable artifacts |
| --- | --- |
| JSON representations, duplicates, Unicode, depth, and limits | `json_test.go`, `json_boundary_test.go`, `fuzz_test.go` |
| HTTP conflicts, casing, short reads, cancellation, body ownership, and limits | `http_test.go`, `http_boundary_test.go`, `stress_test.go` |
| Kafka conflicts, keys, tombstones, headers, copied metadata, and limits | `kafka_test.go`, `kafka_boundary_test.go`, `stress_test.go` |
| Extension validation and generic unknown-extension retention | `extensions_test.go`, `json_test.go`, `interoperability_test.go`, `javascript_interop_test.go` |
| Explicit schema validation with no implicit lookup | `schema_test.go`, `adapters/golib/schema_hardening_test.go` |

The existence of an artifact is a traceability link, not a recorded pass. Race,
fuzz, leak, stress, soak, coverage, mutation, benchmark, clean-consumer, and
release results must be taken from fresh repository gate evidence and are not
claimed by this report.

## Unsupported specification surfaces

Avro, Protobuf, AMQP, MQTT, NATS, and WebSockets are not implemented. The
data-reference, sampling-rate, and sequence extensions have no specialized
implementation. Their pinned documents remain in the manifest so omission is
explicit rather than silently presented as complete CloudEvents support. See
the [specification matrix](specification-matrix.md) for the full inventory.
