# CloudEvents specification decisions

This register records observable choices where CloudEvents 1.0.2, selected
stable-line clarifications, the JSON event format, or the HTTP and Kafka
bindings permit or appear to permit more than one implementation. Normative
prose outranks schemas, examples, conformance scenarios, and SDK behavior.
Exact source revisions and digests are pinned in
[`specification/manifest.json`](../specification/manifest.json).

Statuses are `resolved`, `unresolved`, or `superseded`. Resolved decisions are
part of the compatibility contract and require specification, security,
resource, wire, executable-evidence, and changelog review when changed.

## CLOUDEVENTS-DEC-001: Stable-line clarifications after 1.0.2

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `cloudevents` maintainers |
| Source | Pinned CloudEvents [1.0.2 specification](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/spec.md), [JSON format](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/formats/json-format.md), and reviewed [stable-line clarification ceiling](https://github.com/cloudevents/spec/tree/c2845a49bc9831be02f305a4a792401b932d77d4) |
| Classification | Accepted errata and stable-line clarification policy |
| Issue | The pinned 1.0.2 publication predates clarifications for reserved data names, JSON null and base64 handling, duplicate attributes, data-less events, Kafka tombstones, media-type comparison, and batch version consistency. Ignoring every later clarification would preserve known ambiguities; adopting the whole development branch would claim unreleased behavior. |
| Credible interpretations | Freeze every 1.0.2 ambiguity; follow current development prose wholesale; let each decoder choose independently; or adopt only reviewed clarifications that resolve the stable 1.0 contract and pin every selected commit. |
| Known peer behavior | Official SDKs evolve with stable-line clarifications at different release times, while the archived official conformance scenarios cover only selected HTTP and Kafka receiver behavior. Peer acceptance therefore does not establish which post-1.0.2 changes are normative. |
| Selected behavior | The package adopts only the eight clarification commits listed in the [specification matrix](specification-matrix.md): `data` is reserved, `data_base64` remains a JSON member, clarified JSON null/base64 rules apply, duplicate context attributes fail, `datacontenttype` is valid without data, absent Kafka binary data is a tombstone, media types compare case-insensitively, and a JSON batch uses one specification version. Other 1.0.3-wip behavior is not imported. |
| Security and resource consequences | Narrow adoption prevents duplicate or colliding metadata and ambiguous payload interpretation without widening accepted input. Every selected source is revision- and checksum-pinned; normal byte, depth, attribute, header, and batch limits still apply. |
| Compatibility and wire consequences | Stable 1.0 messages receive one deterministic interpretation across JSON, HTTP, and Kafka. Messages relying on duplicate attributes or conflicting data members are rejected; no unreleased feature is emitted. |
| Executable evidence | `TestPinnedOfficialConformanceFixtures`, `TestDecodeJSONRejectsAmbiguousAndOverLimitInput`, `TestDecodeHTTPRejectsDuplicateAndConflictingMetadata`, `TestEncodeKafkaBinaryPreservesKeyAndTombstoneSemantics`, and `TestJSONBatchRoundTripAndLimits` |
| Public surface | `NewEvent`, `EncodeJSON`, `DecodeJSON`, `EncodeJSONBatch`, `DecodeJSONBatch`, `EncodeHTTP`, `DecodeHTTP`, `EncodeKafka`, and `DecodeKafka` |
| Upstream record | Accepted commits and their exact interpretation are retained in the [specification matrix](specification-matrix.md); the manifest keeps 1.0.2 and the later review ceiling separate. |
| Reconsider when | CloudEvents publishes a stable patch release that incorporates, reverses, or supersedes one of the selected clarifications. |

## CLOUDEVENTS-DEC-002: Extension attribute-name compatibility

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `cloudevents` maintainers |
| Source | CloudEvents 1.0.2 [context attribute naming convention](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/spec.md#attribute-naming-convention) and the reviewed [development specification](https://github.com/cloudevents/spec/blob/c2845a49bc9831be02f305a4a792401b932d77d4/cloudevents/spec.md) |
| Classification | Version-specific compatibility policy for an unreleased recommendation |
| Issue | Development text recommends that attribute names start with a letter, but the pinned 1.0.2 contract permits lowercase alphanumeric names and does not impose that first-character rule. Applying the recommendation retroactively would reject valid stable-line extensions. |
| Credible interpretations | Reject digit-leading names immediately; accept them forever; warn without changing validity; or preserve 1.0.2 validity until a supported released feature line changes it. |
| Known peer behavior | SDK validation may follow the specification revision bundled with each release. No peer default can retroactively change the package's pinned 1.0.2 contract. |
| Selected behavior | Names remain non-empty lowercase ASCII alphanumeric and may begin with a digit. The 1.0.2 recommendation not to exceed twenty characters remains adoption guidance, not a hard information-model validity rule. Core names, `data`, and `data_base64` remain reserved. |
| Security and resource consequences | The restricted character set avoids Unicode, case-folding, and normalization ambiguity. Untrusted decoders apply the caller's `MaxAttributeNameBytes` limit independently from the specification's advisory twenty-character interoperability target. |
| Compatibility and wire consequences | Existing digit-leading and longer valid extensions remain accepted when transport limits permit and round-trip through supported bindings. Producers should stay within the twenty-character recommendation. A later released rule would require an explicit feature-line compatibility decision rather than silent tightening. |
| Executable evidence | `TestAttributeNamesAndCloudStringsEnforceExactCharacterBoundaries` and `TestDecodeJSONRejectsEveryContextAndExtensionBoundary` |
| Public surface | `NewEvent`, extension maps, `DecodeJSON`, `DecodeHTTP`, and `DecodeKafka` |
| Upstream record | The recommendation is above the pinned 1.0.2 support line and is intentionally excluded from the accepted-clarification table. |
| Reconsider when | A supported stable CloudEvents release makes the first-character restriction normative or defines feature-line negotiation for it. |

## CLOUDEVENTS-DEC-003: Data presence and JSON representation

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `cloudevents` maintainers |
| Source | CloudEvents 1.0.2 [event data](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/spec.md#event-data) and [JSON event-format data handling](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/formats/json-format.md#31-handling-of-data) |
| Classification | Normative data mapping plus representation-preservation policy |
| Issue | Absent data, JSON `null`, empty text, empty binary bytes, and base64-encoded bytes can collapse into the same Go zero values. JSON input also has implied JSON semantics when `datacontenttype` is absent, while a later binary binding cannot carry that implication without metadata. |
| Credible interpretations | Collapse all empty forms; infer type only from bytes; preserve presence and abstract data kind; or reject data without an explicit content type. |
| Known peer behavior | Official Go and JavaScript SDK interoperability accepts ordinary JSON and binary data, but their in-memory zero-value models are not a normative presence contract for this package. |
| Selected behavior | The package preserves absent data, JSON `null`, empty text, empty binary, non-empty binary, and JSON values distinctly. JSON-format `data` without `datacontenttype` remains JSON; conversion to binary mode materializes `application/json`. `data` and `data_base64` cannot coexist, and base64 must be canonical padded RFC 4648 encoding. |
| Security and resource consequences | Payload bytes, decoded bytes, and JSON depth are bounded before retention. Canonical base64 and explicit presence prevent alternate encodings and nil/empty confusion at authorization, hashing, or storage boundaries. |
| Compatibility and wire consequences | Round trips preserve whether data exists and which wire member represents it. Binary conversion may add `application/json` solely to preserve the JSON-format implication. |
| Executable evidence | `TestDataKindsPreserveAbsentNullEmptyAndBinaryValues`, `TestDecodeJSONPreservesUnknownExtensionsAndDataPresence`, `TestEncodeJSONCoversOptionalAttributesAndEveryDataRepresentation`, and `TestDecodeJSONRejectsEveryDataAndBatchBoundary` |
| Public surface | `Data`, `NewJSONData`, `NewTextData`, `NewBinaryData`, `Event.Data`, JSON codecs, and HTTP and Kafka binding codecs |
| Upstream record | The selected post-1.0.2 JSON clarifications are pinned separately from the base files in the [specification matrix](specification-matrix.md). |
| Reconsider when | CloudEvents changes data-presence semantics, introduces another JSON payload member, or standardizes a different binary conversion rule. |

## CLOUDEVENTS-DEC-004: Unknown extension abstract types

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `cloudevents` maintainers |
| Source | CloudEvents 1.0.2 [type system](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/spec.md#type-system) and [JSON attribute mapping](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/formats/json-format.md#22-type-system-mapping) |
| Classification | Information-loss and unknown-extension interpretation policy |
| Issue | A JSON string can represent CloudEvents String, Binary, URI, URI-reference, or Timestamp. Without the extension's specification, a decoder cannot infer the stronger abstract type safely. |
| Credible interpretations | Guess from syntax; treat every string as URI-like; reject unknown string extensions; preserve an untyped JSON token; or decode unknown JSON strings as String while retaining Boolean and Integer types exactly. |
| Known peer behavior | SDKs preserve unknown extension values through their own type models, which differ by language. Bidirectional SDK fixtures establish wire interoperability but not a portable inferred semantic type. |
| Selected behavior | Unknown JSON Boolean and Integer values preserve those abstract types. An unknown JSON string becomes String. Constructors for known extensions may retain Binary, URI, URI-reference, or Timestamp explicitly; a JSON round trip guarantees canonical value preservation, not recovery of an unregistered stronger type. |
| Security and resource consequences | No syntax-based type confusion, URI resolution, timestamp parsing, or network access occurs for unknown extensions. Attribute counts, names, values, and integer range remain bounded. |
| Compatibility and wire consequences | Valid unknown extensions round-trip predictably. Applications needing stronger semantics must register or construct that extension explicitly rather than relying on decoder heuristics. |
| Executable evidence | `TestDecodeJSONPreservesUnknownExtensionsAndDataPresence`, `TestAttributeTypesEnforceCloudEventsTypeSystem`, and `TestAttributeValidationCoversEveryAbstractTypeAndInvariant` |
| Public surface | `Attribute`, `AttributeKind`, attribute constructors, `Event.Extension`, and `DecodeJSON` |
| Upstream record | CloudEvents defines the abstract types but does not provide a registry-driven inference algorithm for unknown JSON extensions. |
| Reconsider when | CloudEvents publishes normative extension type discovery or the package introduces an explicit caller-supplied extension registry. |

## CLOUDEVENTS-DEC-005: Duplicate and null context attributes

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `cloudevents` maintainers |
| Source | CloudEvents 1.0.2 [context attributes](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/spec.md#context-attributes) and [JSON representation](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/formats/json-format.md#3-envelope) |
| Classification | Defensive duplicate, null, and metadata-identity policy |
| Issue | Generic JSON and transport APIs can expose duplicate field names or repeated singleton headers, and optional JSON attributes may be null. Last-wins parsing or equal-value deduplication hides the received representation and can make security layers disagree. |
| Credible interpretations | First value wins; last value wins; equal duplicates collapse; preserve all duplicates; or reject every duplicate while treating null optional attributes as absent and null required attributes as invalid. |
| Known peer behavior | JSON and HTTP libraries vary in duplicate handling, and SDKs may normalize values into maps before application code sees them. Their normalization is not imported as a wire acceptance rule. |
| Selected behavior | Duplicate context attributes are rejected in JSON, HTTP, and Kafka even when values match. Required attributes must be present and non-null. Optional JSON context attributes may be null and are then absent. Diagnostics identify the field and stable error category without echoing rejected values. |
| Security and resource consequences | Rejection prevents shadowing and parser differentials. Duplicate detection is bounded by the configured member, attribute, header, and input limits; diagnostics do not disclose payloads or credentials. |
| Compatibility and wire consequences | Ambiguous messages fail instead of being normalized. Canonical valid messages remain interoperable with official Go and JavaScript SDK fixtures. |
| Executable evidence | `TestDecodeJSONRejectsAmbiguousAndOverLimitInput`, `TestDecodeHTTPRejectsDuplicateAndConflictingMetadata`, `TestDecodeKafkaOfficialBinaryAndStructuredScenarios`, and `TestDecodeJSONPreservesUnknownExtensionsAndDataPresence` |
| Public surface | `DecodeJSON`, `DecodeJSONBatch`, `DecodeHTTP`, `DecodeKafka`, and package error values |
| Upstream record | Duplicate rejection incorporates the reviewed stable-line clarification identified in the [specification matrix](specification-matrix.md). |
| Reconsider when | A supported CloudEvents binding defines deterministic repeated-value semantics that cannot be represented by this singleton model. |

## CLOUDEVENTS-DEC-006: URI and URI-reference input strictness

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `cloudevents` maintainers |
| Source | CloudEvents 1.0.2 [URI-reference type](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/spec.md#type-system) and RFC 3986 [URI syntax](https://www.rfc-editor.org/rfc/rfc3986) |
| Classification | Defensive parsing and identity-preservation policy |
| Issue | Generic URI parsers may accept spaces or Unicode and silently percent-encode them. That changes the supplied identity and can make schema, source, routing, or authorization comparisons disagree. |
| Credible interpretations | Normalize invalid text automatically; apply an IRI conversion; preserve raw strings without validation; or require already-valid RFC 3986 ASCII serialization. |
| Known peer behavior | Language URI libraries differ in permissiveness and normalization. CloudEvents SDK interoperability does not define one cross-language repair algorithm. |
| Selected behavior | URI and URI-reference attribute values must already be valid RFC 3986 ASCII serialization. The package does not repair spaces, convert IRIs, resolve references, fetch resources, or claim normalized text is equivalent to the received value. |
| Security and resource consequences | Strict validation prevents Unicode and percent-encoding aliases without performing I/O. URI bytes remain subject to the ordinary attribute-value limit. |
| Compatibility and wire consequences | Invalid or non-ASCII URI text is rejected instead of rewritten. Valid relative and absolute references preserve their exact serialized identity. |
| Executable evidence | `TestAttributeValidationCoversEveryAbstractTypeAndInvariant`, `TestEventValidationRejectsCorruptedInternalDataStates`, and `TestDecodeJSONRejectsEveryContextAndExtensionBoundary` |
| Public surface | `NewURIAttribute`, `NewURIReferenceAttribute`, `NewEvent`, `DataSchema`, `Source`, and all decoders |
| Upstream record | RFC 3986 supplies syntax; automatic IRI conversion and network resolution are outside the CloudEvents core contract. |
| Reconsider when | CloudEvents adopts an IRI profile or defines a canonical normalization algorithm for context attributes. |

## CLOUDEVENTS-DEC-007: Deterministic JSON bytes

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `cloudevents` maintainers |
| Source | CloudEvents 1.0.2 [JSON event format](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/formats/json-format.md) and RFC 8259 [object-order interoperability](https://www.rfc-editor.org/rfc/rfc8259#section-4) |
| Classification | Package-defined deterministic serialization profile |
| Issue | CloudEvents requires JSON semantics but does not define object-member order, insignificant whitespace, timestamp normalization, or a general canonicalization profile. Unstable bytes undermine fixtures, hashes, caching, and reproducible transport records. |
| Credible interpretations | Preserve input bytes; use ordinary map iteration; claim RFC 8785; or define a narrow deterministic package profile without changing CloudEvents semantics. |
| Known peer behavior | Official Go and JavaScript SDKs emit semantically interoperable JSON but do not promise byte identity with this package. Their output is compared semantically in interoperability tests. |
| Selected behavior | Encoding emits members in lexicographic order, no insignificant whitespace, UTC RFC 3339 timestamps with only necessary fractional digits, base-10 integers, and padded RFC 4648 base64. This is a Golib byte-stability contract, not CloudEvents or RFC 8785 conformance. |
| Security and resource consequences | Deterministic output avoids hash and signature ambiguity. Encoding validates the event first and returns bounded owned bytes without executing extensions or resolving schemas. |
| Compatibility and wire consequences | Semantically equivalent package events produce stable package bytes. Other conforming serializers may produce different bytes and remain interoperable after decoding. |
| Executable evidence | `TestEncodeJSONIsDeterministicAndNormative`, `TestEncodeJSONCoversOptionalAttributesAndEveryDataRepresentation`, and `TestCodecSoakRoundTripsStableBytes` |
| Public surface | `EncodeJSON`, `EncodeJSONBatch`, structured HTTP encoding, and structured Kafka encoding |
| Upstream record | CloudEvents specifies the JSON mapping but no canonical-byte profile; this additional contract is package-owned. |
| Reconsider when | CloudEvents adopts a canonicalization profile or byte-level interoperability requires a versioned alternative serializer. |

## CLOUDEVENTS-DEC-008: Structured metadata conflicts and mode selection

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `cloudevents` maintainers |
| Source | CloudEvents 1.0.2 [HTTP content modes](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/bindings/http-protocol-binding.md#13-content-modes) and [Kafka content modes](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/bindings/kafka-protocol-binding.md#13-content-modes) |
| Classification | Binding mode, redundant metadata, and media-type conflict policy |
| Issue | Structured messages can carry redundant protocol metadata, singleton fields may repeat with different casing, and an unsupported structured media type could be misread as a binary event. The bindings do not define a permissive conflict-recovery algorithm. |
| Credible interpretations | Ignore all protocol metadata in structured mode; let headers override payloads; accept matching duplicates; sniff unsupported bodies; or validate redundant metadata and reject every conflict or unsupported structured format. |
| Known peer behavior | HTTP stacks normalize header names differently, and SDKs vary in whether redundant structured metadata remains visible. Official fixtures cover canonical modes, not every conflicting combination. |
| Selected behavior | Header names compare case-insensitively and singleton occurrences must be unique. In structured mode, the payload owns context attributes; redundant protocol metadata must decode to the same canonical value. Conflicts fail. Unsupported structured media types return `ErrUnsupportedMode` and are never silently reinterpreted as binary events. |
| Security and resource consequences | Fail-closed selection prevents metadata shadowing and content-type confusion. Header names, values, counts, total bytes, body bytes, and JSON depth are bounded before retained copies are created. |
| Compatibility and wire consequences | Canonical binary and structured messages interoperate normally. Ambiguous duplicates, conflicting metadata, and unsupported structured formats are rejected rather than normalized. |
| Executable evidence | `TestDecodeHTTPRejectsDuplicateAndConflictingMetadata`, `TestDecodeHTTPRejectsModeBodyAndStructuredMetadataFailures`, `TestDecodeKafkaRejectsConflictingStructuredMetadataAndMalformedRecords`, and `TestStructuredHTTPMetadataCoversOptionalFieldsAndDirectLimits` |
| Public surface | `ContentMode`, `Message`, `EncodeHTTP`, `DecodeHTTP`, `EncodeKafka`, `DecodeKafka`, and `ErrUnsupportedMode` |
| Upstream record | Binding files and official receiver scenarios are revision- and checksum-pinned in the [manifest](../specification/manifest.json). |
| Reconsider when | A supported binding defines precedence for redundant structured metadata or registers another structured event format implemented by this package. |

## CLOUDEVENTS-DEC-009: HTTP body presence, ownership, and cancellation

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `cloudevents` maintainers |
| Source | CloudEvents 1.0.2 [HTTP binary content mode](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/bindings/http-protocol-binding.md#31-binary-content-mode) and Go [`io.Reader` ownership contract](https://pkg.go.dev/io#Reader) |
| Classification | Transport-specific data-presence and resource-lifecycle policy |
| Issue | An empty HTTP body can mean absent data or present empty data, and a decoder cannot forcibly cancel an arbitrary blocking reader without starting unmanaged work or owning the transport resource. Caller-owned bodies also must not be closed unexpectedly. |
| Credible interpretations | Treat every empty body as absent; treat every empty body as present; spawn a goroutine to force cancellation; close every reader; or use content metadata for presence while preserving caller ownership and cooperative cancellation. |
| Known peer behavior | HTTP clients and SDK adapters own different body lifecycles. Their transport wrappers do not establish ownership for this transport-neutral decoder. |
| Selected behavior | In binary mode, an empty body with a non-JSON data content type is present empty binary data; with neither bytes nor a data content type, data is absent. A JSON data content type still requires a valid JSON value, so an empty body is invalid. Decoders never close caller-owned readers. Cancellation is checked before and after the bounded read; prompt interruption requires a reader whose own `Read` observes cancellation. No cancellation goroutine is started. |
| Security and resource consequences | Body size is bounded with overflow-safe accounting and cancellation cannot leak an internal goroutine. Callers remain responsible for deadlines and closing transport resources they own. |
| Compatibility and wire consequences | Empty payload presence is deterministic from received metadata and JSON cannot be represented by an empty body. Callers must provide a cancellation-aware reader when prompt interruption of a blocked read is required. |
| Executable evidence | `TestDecodeHTTPBinaryRejectsMalformedAttributesAndData`, `TestHTTPBodyLimitsCancellationAndNilOwnership`, `TestHTTPDecodePropagatesBodyFailureWithoutClosingCallerResource`, and `TestDecodeHTTPBinaryDecodesPercentEncodingAndJSONData` |
| Public surface | `DecodeHTTP`, `Limits.MaxEventBytes`, `Data.Present`, and decoder ownership documentation |
| Upstream record | CloudEvents defines binary mapping; reader ownership and forced-cancellation mechanics are package and Go runtime concerns. |
| Reconsider when | The decoder accepts a transport type whose close and cancellation lifecycle it explicitly owns. |

## CLOUDEVENTS-DEC-010: Kafka tombstones, keys, and batch scope

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `cloudevents` maintainers |
| Source | CloudEvents 1.0.2 [Kafka protocol binding](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/bindings/kafka-protocol-binding.md) and [partitioning extension](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/extensions/partitioning.md) |
| Classification | Binding-specific absent-data, transport metadata, and unsupported-mode policy |
| Issue | A nil Kafka value is a tombstone on compacted topics, while an empty non-nil value is distinct. Kafka keys and transport headers are not CloudEvents context, and the pinned binding defines no batch content mode. |
| Credible interpretations | Collapse nil and empty values; synthesize the Kafka key from `partitionkey`; flatten transport headers into extensions; invent a Kafka batch mode; or preserve each boundary explicitly. |
| Known peer behavior | Official Go and JavaScript SDK fixtures interoperate for canonical binary and structured records. Broker clients still expose keys, tombstones, headers, offsets, and settlement through transport-specific APIs. |
| Selected behavior | Absent binary event data encodes as a nil record value and therefore a tombstone; present empty data encodes as non-nil zero-length bytes. Caller keys and non-CloudEvents headers remain out of band and owned. `partitionkey` is only an opt-in mapping hint. Structured values are always non-nil. Kafka batch mode is unsupported. |
| Security and resource consequences | Key bytes, header names, values, total metadata, event bytes, and copied transport headers are bounded before retention. No broker I/O, partition selection, offset management, retry, or acknowledgement occurs. |
| Compatibility and wire consequences | Tombstone semantics remain visible rather than collapsing into empty data. Existing keys and transport headers survive decoding without becoming trusted CloudEvents attributes; batch attempts fail explicitly. |
| Executable evidence | `TestEncodeKafkaBinaryPreservesKeyAndTombstoneSemantics`, `TestDecodeKafkaOfficialBinaryAndStructuredScenarios`, `TestDecodeKafkaRejectsConflictingStructuredMetadataAndMalformedRecords`, and `TestDecodeKafkaBoundsAllCopiedRecordMetadata` |
| Public surface | `KafkaRecord`, `KafkaHeader`, `KafkaMessage`, `KafkaPartitionKey`, `EncodeKafka`, and `DecodeKafka` |
| Upstream record | The Kafka binding and selected partitioning extension are pinned in the [manifest](../specification/manifest.json); no Kafka batch binding is claimed. |
| Reconsider when | CloudEvents publishes a stable Kafka batch binding or changes absent-data and partition-key semantics. |

## CLOUDEVENTS-DEC-011: Explicit schema validation without implicit I/O

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `cloudevents` maintainers |
| Source | CloudEvents 1.0.2 [`dataschema` context attribute](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/spec.md#dataschema) and [event data schema note](https://github.com/cloudevents/spec/blob/fc1f6f31f5f011a72183f1bcea20c987cb683ade/cloudevents/spec.md#event-data) |
| Classification | Optional behavior and application-owned schema-resolution policy |
| Issue | `dataschema` identifies a schema but CloudEvents does not require receivers to fetch it or define a resolver, trust policy, dialect, cache, or SSRF boundary. Implicit validation during decoding would add hidden I/O and application policy. |
| Credible interpretations | Ignore schemas entirely; fetch every URI during decode; use a process-global registry; validate only when a caller supplies an explicit validator; or embed one schema engine and policy in the core. |
| Known peer behavior | SDKs commonly expose schema metadata without mandatory validation. Schema registries and validators differ in dialect, retrieval, caching, and trust policy. |
| Selected behavior | Constructing or decoding an event never resolves a schema or performs network I/O. `ValidateSchema` invokes only a caller-supplied `SchemaValidator` with owned event data. Nil contexts, nil validators, and typed-nil validators fail before application code is called. Resolver and registry policy stays in explicit adapters. |
| Security and resource consequences | Core decode has no SSRF, DNS, credential, cache, or remote-resource side effect. The caller owns validator limits and schema trust; the package validates callback preconditions and does not leak aliased payload storage. |
| Compatibility and wire consequences | Schema metadata round-trips whether or not validation is configured. Applications opt into validation explicitly and can change validators without changing wire decoding. |
| Executable evidence | `TestValidateSchemaIsExplicitAndOwnsValidatorInput`, `TestValidateSchemaRejectsNilContextPrecisely`, `TestValidateSchemaRejectsTypedNilValidatorWithoutPanicking`, and `TestValidateSchemaRejectsEveryPreconditionAndReturnsValidatorError` |
| Public surface | `SchemaValidator`, `ValidateSchema`, `Event.DataSchema`, and schema-related errors |
| Upstream record | CloudEvents provides schema identity but no mandatory retrieval or validation algorithm; the no-I/O default is package-owned. |
| Reconsider when | CloudEvents publishes normative schema retrieval and validation behavior or the core adopts an explicit resolver interface with equivalent fail-closed policy. |

## Unresolved decisions

None. New ambiguities, errata, binding conflicts, extension policies, or peer
divergences MUST be registered before observable behavior is selected. An
unresolved wire, validation, security, or resource-policy decision blocks a
release claim for the affected surface.
