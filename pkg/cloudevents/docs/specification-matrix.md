# Specification matrix

This module targets the stable CloudEvents 1.0 line. A row is a support claim
only after its implementation, normative cases, official fixtures, and
independent interoperability columns are all complete in the release report.

## Pinned authorities

| Authority | Revision | Role |
| --- | --- | --- |
| `cloudevents/spec` | tag `v1.0.2`, commit `fc1f6f31f5f011a72183f1bcea20c987cb683ade` | stable core, formats, bindings, extensions, and registry |
| `cloudevents/conformance` | tag `v0.4.1`, commit `7a8ee0ac0e782bba1ba30e58c62d24d2e6c337e5` | official receiver-oriented HTTP and Kafka feature scenarios |
| `cloudevents/sdk-go` | tag `v2.16.2`, commit `af3e8599b3316ab6b4b73ff69aa8ec0efddbb5bb` | independent Go interoperability implementation |
| `cloudevents/sdk-javascript` | tag `v10.0.0`, commit `c23895145a2f055e8e90714401fc67252a0edf21` | independent non-Go JSON, HTTP, Kafka, batch, and extension implementation |
| `cloudevents/spec` errata review | `main` commit `c2845a49bc9831be02f305a4a792401b932d77d4` | review ceiling for post-1.0.2 clarifications; not a claim of 1.0.3-wip support |

The requested `cloudevents/sdk-conformance` repository did not exist at
implementation time. The official organization exposes the archived
`cloudevents/conformance` tool and feature files instead. The release evidence
must state their limited scenario coverage rather than treating the tool as a
complete conformance oracle.

## Normative coverage

| Area | Pinned document | Intended support | Required evidence before claim |
| --- | --- | --- | --- |
| Core information model and type system | `cloudevents/spec.md`, 1.0.2 | complete stable 1.0 context attributes, unknown valid extensions, data presence, validation, and size semantics | attribute/type decision cases, hostile-input tests, fuzzing, 100% coverage and mutation |
| JSON event format | `cloudevents/formats/json-format.md`, 1.0.2 | structured JSON including `data`, `data_base64`, null, absent, empty, and deterministic package serialization | official schema cases and bidirectional sdk-go and sdk-javascript round trips |
| JSON batch format | JSON format section 4, 1.0.2 | empty and non-empty batches with one spec version | batch fixtures, limits, and sdk-javascript parsing |
| HTTP binding | `cloudevents/bindings/http-protocol-binding.md`, 1.0.2 | binary, structured JSON, and JSON batch modes for requests and responses | official feature scenarios, sdk-go and sdk-javascript records, cancellation, duplicates, conflicts, limits, ownership |
| Kafka binding | `cloudevents/bindings/kafka-protocol-binding.md`, 1.0.2 | binary and structured JSON; no Kafka batch claim | official feature scenarios, sdk-go and sdk-javascript records, duplicate headers, tombstones, keys, limits |
| Distributed tracing extension | `cloudevents/extensions/distributed-tracing.md` at the 1.0.2 commit | `traceparent` and `tracestate` only | W3C Trace Context validation and protocol-copy conflict tests |
| Partitioning extension | `cloudevents/extensions/partitioning.md` at the 1.0.2 commit | `partitionkey` and opt-in Kafka key mapping | immutable mapping and key-preservation tests |
| Queue, outbox, event-sourcing, workflow, tenancy, correlation, telemetry, and audit conversion | Golib-owned mapping documents | explicit adapters only; none is an official CloudEvents binding | collision/loss matrices and round-trip tests against each owned contract |
| Schema validation and registry lookup | Golib `json-schema` and `schema-registry` adapters | opt-in only | no-I/O core tests, bounded caller-supplied resolver tests, SSRF review |

Kafka batch mode is not supported because the pinned official Kafka binding does
not support it. Queue and outbox encodings must be named Golib mappings, never
CloudEvents protocol bindings.

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
