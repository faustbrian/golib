# Specification matrix

This module targets the stable CloudEvents 1.0 line. `Supported` means the
listed surface has an owned implementation and executable artifact. It does not
mean that the archived upstream conformance program has certified the module.
`Unsupported` means the package neither parses nor emits that format or binding,
or does not implement the registered extension's specialized semantics. Unknown
valid extension attributes can still round-trip through supported formats as
generic CloudEvents attributes.

Observable interpretations and accepted errata are governed by the
[specification decision register](specification-decisions.md). The exact scope
and limits of the official scenarios are recorded in the
[conformance report](conformance-report.md).

The machine-readable
[normative requirement matrix](../specification/normative-requirements.json)
enumerates all 140 BCP 14 statements in the pinned supported documents, with
source anchors, implementation surfaces, executable artifacts, and explicit
unsupported or caller-owned limitations. It also records Avro, Protobuf, AMQP,
MQTT, NATS, WebSockets, dataref, sampledrate, and sequence only as intentionally
unsupported surfaces.

Of the 140 requirement records, 104 are mapped as supported, 29 are
specification-, producer-, deployment-, or application-owned and therefore not
applicable to this codec, and 7 remain explicitly unsupported. The unsupported
set covers the 20-character attribute-name recommendation, complete public
canonical-string constructors, the public timestamp string setter, uniform
non-CloudEvents metadata forwarding across HTTP and Kafka, generic sensitive
context enforcement, and the two distributed-tracing multi-hop lifecycle
requirements.

## Pinned authorities

| Authority | Revision | Role |
| --- | --- | --- |
| `cloudevents/spec` | tag `v1.0.2`, commit `fc1f6f31f5f011a72183f1bcea20c987cb683ade` | stable core plus the complete format, binding, extension, and registry inventory below |
| `cloudevents/conformance` | tag `v0.4.1`, commit `7a8ee0ac0e782bba1ba30e58c62d24d2e6c337e5` | archived receiver-oriented HTTP and Kafka feature scenarios |
| `cloudevents/sdk-go/v2` | tag `v2.16.2`, commit `af3e8599b3316ab6b4b73ff69aa8ec0efddbb5bb` | independent Go JSON, JSON batch, and HTTP interoperability implementation |
| `cloudevents/sdk-go/protocol/kafka_sarama/v2` | tag `protocol/kafka_sarama/v2.16.2`, commit `bee0ebe38fde4cecb92dc51aab7acddc951cbd70` | independently versioned Go Kafka interoperability module |
| `cloudevents/sdk-javascript` | tag `v10.0.0`, commit `c23895145a2f055e8e90714401fc67252a0edf21` | independent non-Go interoperability implementation |
| `cloudevents/spec` errata review | `main` commit `c2845a49bc9831be02f305a4a792401b932d77d4` | review ceiling for selected post-1.0.2 clarifications; not a 1.0.3-wip support claim |

Remote refs were refreshed on 2026-08-11. The four pinned release tags above
were the latest release tags on their respective repositories. The archived
conformance repository's `main` branch was four archive and dependency-cleanup
commits ahead of v0.4.1 and contained no newer release tag.

## Core and JSON support

| Surface | Status | Observable scope | Executable artifacts | Independent/official boundary |
| --- | --- | --- | --- | --- |
| Core information model and type system | Supported with declared limitations | CloudEvents 1.0 required and optional context attributes, typed attributes, generic valid extensions, data presence, validation, and configured resource limits; public canonical-string construction gaps remain listed in the requirement matrix | `event_test.go`, `extensions_test.go`, `boundary_test.go`, `fuzz_test.go` | SDK behavior is comparative evidence only; normative prose controls |
| JSON event format | Supported | Structured JSON; `data`, `data_base64`, absent, null, text, JSON, and binary data | `json_test.go`, `json_boundary_test.go`, `interoperability_test.go`, `javascript_interop_test.go` | Go and JavaScript selected JSON cases run in both directions |
| JSON batch format | Supported | Empty and non-empty JSON batches with one specification version and configured limits | `json_test.go`, `json_boundary_test.go`, `interoperability_test.go`, `javascript_interop_test.go` | Golib and the Go SDK consume each other's two-event batches with exact context, payload, selected-extension, and opaque-extension checks; Golib and JavaScript consume each other's batches with complete declared-context, selected-extension, opaque-extension, and semantic-payload assertions |
| JSON event schema | Validation aid | The pinned schema is provenance input, not the normative authority and not a complete conformance oracle | `scripts/check-specification-evidence.sh` validates the pin; decoder behavior is tested by the JSON artifacts above | No claim that every schema case is an official certification case |

## Complete v1.0.2 format inventory

| Pinned format | Status | Reason / evidence |
| --- | --- | --- |
| JSON event and batch (`formats/json-format.md`, `formats/cloudevents.json`) | Supported | Implemented by `json.go`; artifacts listed above |
| Avro (`formats/avro-format.md`, `formats/cloudevents.avsc`) | Unsupported | No Avro parser, serializer, or media-type registration is exposed |
| Protobuf (`formats/protobuf-format.md`, `formats/cloudevents.proto`) | Unsupported | No Protobuf parser, serializer, or media-type registration is exposed |

## Complete v1.0.2 protocol-binding inventory

| Pinned binding | Status | Observable scope / evidence |
| --- | --- | --- |
| HTTP (`bindings/http-protocol-binding.md`) | Supported | Binary, structured JSON, and JSON batch mapping in `http.go`; Go SDK binary and structured modes run in both directions with selected and opaque extensions; canonical JavaScript records run in both directions with complete declared-context, extension, and semantic-payload assertions; the JavaScript SDK helper's timestamp, null, empty-data, and default-content-type normalizations are explicit in the conformance report |
| Kafka (`bindings/kafka-protocol-binding.md`) | Supported | Binary and structured JSON mapping in `kafka.go`; Go SDK binary and structured modes run in both directions with selected and opaque extensions, subject to the documented sdk-go transport-key limitation; JavaScript consumes Golib binary and structured Kafka with complete declared-context, extension, and semantic-payload assertions; Golib decodes JavaScript-produced structured Kafka, while JavaScript-produced binary Kafka is explicitly rejected because it includes the forbidden redundant `ce_datacontenttype`; no Kafka batch claim |
| AMQP (`bindings/amqp-protocol-binding.md`) | Unsupported | No AMQP binding or adapter is exposed |
| MQTT (`bindings/mqtt-protocol-binding.md`) | Unsupported | No MQTT binding or adapter is exposed |
| NATS (`bindings/nats-protocol-binding.md`) | Unsupported | No NATS binding or adapter is exposed |
| WebSockets (`bindings/websockets-protocol-binding.md`) | Unsupported | No WebSockets binding or adapter is exposed |

The HTTP and Kafka official fixtures contain receiver-oriented happy paths only.
Their exact exercised scenarios and omissions are listed in the
[conformance report](conformance-report.md). Queue, outbox, event-sourcing, and
workflow representations are Golib mappings, never CloudEvents protocol
bindings.

## Complete v1.0.2 extension inventory

| Pinned extension | Status | Observable scope / evidence |
| --- | --- | --- |
| Distributed tracing (`extensions/distributed-tracing.md`) | Supported subset | `traceparent` and `tracestate` validation plus explicit single-hop propagation-policy integration; multi-hop starting-trace retention and no-per-hop-rewrite semantics remain unsupported; `extensions_test.go`, `boundary_test.go`, and `adapters/golib/metadata_test.go` |
| Partitioning (`extensions/partitioning.md`) | Supported | `partitionkey` validation plus explicit Kafka key mapping; `extensions_test.go` and `kafka_test.go` |
| Data reference (`extensions/dataref.md`) | Unsupported specialized semantics | A valid attribute can be retained generically; the package does not dereference data or implement this extension's behavior |
| Sampling rate (`extensions/sampledrate.md`) | Unsupported specialized semantics | A valid attribute can be retained generically; the package does not implement sampling policy |
| Sequence (`extensions/sequence.md`) | Unsupported specialized semantics | Valid attributes can be retained generically; the package does not implement sequence semantics |
| Documented extensions index (`documented-extensions.md`) | Inventory only | The index is pinned for review; listing an external extension is not a support claim |

Schema lookup is not a CloudEvents format or binding. Core construction and
decode perform no lookup. `schema.go` accepts only a caller-supplied validator;
the optional Golib adapter accepts only constructor-snapshotted URI mappings
and resolves the mapped lookup through a caller-supplied bounded cache under an
explicit availability policy and required timeout. Event-controlled schema
URIs cannot select a resolver endpoint. Resolver endpoint, credential, trust,
and compiler policy remain application-owned.

## Accepted post-1.0.2 clarifications

These reviewed commits clarify the stable contract without adopting unrelated
1.0.3-wip features:

| Commit | Decision |
| --- | --- |
| `740d4665f9bffdb8350b2e4bf2099586df136d88` | reserve `data` as a context-attribute name to prevent format collisions |
| `355c85f8bb5abd4d1244e2e46430961eb6f77155` | apply the clarified JSON data, null, and base64 rules |
| `5be731125d24fe4de5ad14779c1fa5fc81569e48` | reject duplicate occurrences of a context attribute |
| `d8fe24c785838de6cbda21e4e4c9360863187523` | treat `data_base64` as a JSON member, not a context attribute |
| `1c2fa2b571950a5154124335b63a4d2a499c7be4` | allow `datacontenttype` when data is absent |
| `721fd84e49ebb690ba38685a21aafe3765286ad5` | expose the binary no-data Kafka tombstone consequence and distinguish structured records |
| `83a8bd9150de032b4788be72341c5f5f767df92e` | compare media types case-insensitively as required by MIME |
| `12336bbb48d8499ea1f35c70e9cb5a1de6a5f2fd` | apply the batch spec-version restriction independent of transport |

The 1.0.3-wip recommendation that attribute names start with a letter is not
made a 1.0.2 validity rule. Digit-leading extension names remain accepted.
