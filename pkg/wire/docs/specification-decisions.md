# Wire Specification Decisions

This register records observable choices where the supported wire-format
specifications, Go codecs, dependency codecs, and package policy permit
different outcomes. Each format remains an explicit package; this module does
not claim one lossless data model across all eight formats.

Each resolved entry names executable evidence. Changing one requires
compatibility, security, resource, API, conformance, dependency, and changelog
review. Superseded decisions remain linked from their replacements.

## WIRE-DEC-001: Specification editions and codec delegation

**Authoritative reference:** [Go 1.26.6 encoding source](https://cs.opensource.google/go/go/+/refs/tags/go1.26.6:src/encoding/).

- **Status, owner, and classification:** `resolved`; wire maintainers;
  normative-source and interoperability policy.
- **Source and issue:** The pinned
  [source manifest](../specification/manifest.tsv) identifies the exact JSON,
  XML 1.0, SOAP 1.1/1.2, YAML 1.2.2, TOML 1.1.0, MessagePack, CBOR, CTAP2, and
  BSON 1.1 sources. They define distinct data models, while Go and third-party
  codecs implement overlapping but not identical subsets and defaults.
- **Interpretations and peer behavior:** Reimplement every grammar, expose
  dependency defaults unchanged, normalize all formats through one generic
  tree, or wrap reviewed codecs with explicit package policy. Codec behavior
  differs on duplicate names, numeric conversion, tags, aliases, and output
  ordering.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  JSON and XML delegate syntax and Go
  value mapping to Go 1.26.6; SOAP builds on that XML boundary. YAML, TOML,
  MessagePack, CBOR, and BSON use the exact modules in `go.mod`, with package
  preflight, limits, options, and error classification defining the public
  contract. Dependency behavior is never promoted to a broader conformance
  claim without package evidence.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSharedRepositoryContract`, `TestCodecDocumentationUsesRealAPIsAndSemantics`,
  and `TestPublicAPIReferenceInventoriesEveryExport` cover all format packages
  and documentation. Reconsider every source or codec upgrade and any new
  format.

## WIRE-DEC-002: Complete input units and destination mutation

**Authoritative reference:** [RFC 8259](https://www.rfc-editor.org/rfc/rfc8259.html).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  application-boundary policy.
- **Source and issue:** Format specifications define streams, documents, or
  data items, while decoder APIs may accept prefixes, multiple units, empty
  documents, or mutate destinations before a later failure.
- **Interpretations and peer behavior:** Decode the first value, decode a
  stream implicitly, require exactly one unit, or expose per-format behavior.
  Codec destinations commonly become partially assigned on errors.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  JSON, XML, SOAP, YAML, MessagePack,
  CBOR, and BSON require exactly one non-empty complete value, document,
  envelope, or item. TOML permits its specified empty document. YAML multi-
  document input is opt-in and requires a slice destination. All decoders
  reject nil and non-pointer targets before reading; successful reuse is
  supported, but a destination is explicitly indeterminate after any decode
  error.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestEveryDecoderDefinesEmptyWhitespaceTruncatedAndConcatenatedInput`,
  `TestEveryDecoderClassifiesInvalidTargets`,
  `TestEveryDecoderSupportsSuccessfulTargetReuse`, and
  `TestEveryDecoderTreatsFailedTargetAsIndeterminate` cover every decode API.
  Reconsider only with an explicit streaming API that has separate ownership.

## WIRE-DEC-003: Input, structural, graph, and output limits

**Authoritative reference:** [RFC 8949](https://www.rfc-editor.org/rfc/rfc8949.html).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  resource policy.
- **Source and issue:** The format specifications permit documents and values
  far larger or deeper than a general service can safely process, and encoded
  length fields can request disproportionate allocation.
- **Interpretations and peer behavior:** Leave limits to callers or codecs,
  impose one byte limit only, stream unbounded output, or enforce finite
  format-aware limits. Codec defaults and allocation behavior vary.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Zero selects a 1 MiB input and
  output default. Negative limits fail validation. Readers consume at most one
  detection byte beyond the limit, with `MaxInt64` handled without overflow.
  XML/SOAP depth, YAML alias/depth, MessagePack depth/collection, and CBOR
  dependency limits remain explicit. Every typed or raw encoder completes
  within a quota before writing, so limit failures emit no partial output.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestEveryReaderStopsAtOneByteBeyondLimit`,
  `TestEveryEncoderHonorsExactNegativeAndMaximumOutputLimits`,
  `TestEncodeWriterDoesNotExceedOutputLimit`, and
  `TestForgedBinaryLengthsHaveBoundedAllocationCounts` cover all formats.
  Reconsider defaults only with compatibility and measured resource evidence.

## WIRE-DEC-004: Conservative format detection

**Authoritative reference:** [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  application policy.
- **Source and issue:** No supported specification defines reliable automatic
  discrimination among JSON, XML, YAML, TOML, MessagePack, CBOR, and BSON.
  Several valid inputs are lexically ambiguous.
- **Interpretations and peer behavior:** Guess from extensions, MIME types,
  complete parsing, or leading bytes; try codecs in order; or require callers
  to select a format. Generic serializers commonly overstate detection.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  `DetectFormat` recognizes only a
  leading JSON object or array and a leading XML `<` after whitespace. It does
  not validate the payload, reports SOAP as XML, and never guesses scalar JSON,
  YAML, TOML, MessagePack, CBOR, or BSON. Unknown and empty input remain
  `ErrUnsupportedFormat`; explicit format selection is the normal API.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestDetectFormat` and `TestDetectFormatRejectsUnknownAndEmptyPayloads` cover
  `DetectFormat`. No upstream standard exists; reconsider only for a separate
  evidence-backed media-type boundary.

## WIRE-DEC-005: JSON names, numbers, Unicode, and normalization

**Authoritative reference:** [RFC 8259](https://www.rfc-editor.org/rfc/rfc8259.html).

- **Status, owner, and classification:** `resolved`; maintainers; RFC 8259
  interoperability policy with explicit compatibility mode.
- **Source and issue:** RFC 8259 [objects](https://www.rfc-editor.org/rfc/rfc8259.html#section-4)
  says names SHOULD be unique and permits implementations to differ for
  duplicates and number range; its encoding rules require UTF-8 on open
  ecosystems. Go's decoder accepts duplicate names and invalid UTF-8.
- **Interpretations and peer behavior:** Reject duplicates unconditionally,
  preserve first or last values, expose every pair, replace malformed Unicode,
  or follow Go defaults. Implementations differ on number precision and BOMs.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Decode requires valid UTF-8 and one
  value. Duplicate-name rejection is explicit and recursive through
  `DisallowDuplicateNames`; compatibility default retains Go's last-value
  behavior. Typed decoding follows Go numeric conversion. `Normalize` alone
  strips an initial UTF-8 BOM, preserves number lexemes, compacts whitespace,
  orders object keys, and does not silently enable duplicate rejection.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestDuplicateNameValidationDefinesEveryJSONShape`,
  `TestJSONRejectsInvalidUTF8AcceptedByStandardLibrary`,
  `TestDecodeRejectsMalformedAndTrailingValues`, and
  `TestNormalizeMakesVendorJSONCanonical` cover `Decode` and `Normalize`.
  Reconsider duplicate defaults only as a compatibility-governed change.

## WIRE-DEC-006: XML strictness, namespaces, entities, and charsets

**Authoritative reference:** [XML 1.0 Fifth Edition](https://www.w3.org/TR/2008/REC-xml-20081126/).

- **Status, owner, and classification:** `resolved`; maintainers; XML 1.0
  interoperability and defensive profile.
- **Source and issue:** XML 1.0 Fifth Edition
  [defines document syntax](https://www.w3.org/TR/2008/REC-xml-20081126/) and
  Namespaces in XML 1.0 Third Edition
  [defines resolved names](https://www.w3.org/TR/2009/REC-xml-names-20091208/).
  Go exposes strict recovery and caller-provided charset conversion without
  canonical XML.
- **Interpretations and peer behavior:** Recover malformed markup by default,
  compare lexical prefixes, expand declarations, guess encodings, or require
  strict resolved names and declared conversion. XML stacks vary materially on
  DTD and entity behavior.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Strict parsing, one root, resolved
  namespace URL plus local-name comparison, finite token depth, and no declared
  entity expansion are defaults. Non-strict recovery is opt-in. Built-in
  charset conversion is limited to UTF-8, ASCII, ISO-8859-1, and Windows-1252;
  other declared encodings require an explicit reader. Output follows
  `encoding/xml` struct traversal and is deterministic for that value, but is
  not XML canonicalization and does not preserve lexical prefixes.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestDecodeNamespaceAwareFixture`,
  `TestDecodeCanExplicitlyRecoverNonStrictXML`,
  `TestDecodeDoesNotExpandDeclaredEntities`, `TestCharsetReaderContract`, and
  `TestEncodeIsDeterministicAndConfigurable` cover `xmlwire`. Reconsider when
  Go's XML contract or a required vendor charset changes.

## WIRE-DEC-007: SOAP versions, envelope profile, and faults

**Authoritative reference:** [SOAP 1.2 Part 1 Second Edition](https://www.w3.org/TR/2007/REC-soap12-part1-20070427/).

- **Status, owner, and classification:** `resolved`; maintainers; SOAP 1.1/1.2
  transport-neutral profile.
- **Source and issue:** SOAP 1.1
  [section 4](https://www.w3.org/TR/2000/NOTE-SOAP-20000508/#_Toc478383494)
  and SOAP 1.2 Part 1 Second Edition
  [section 5](https://www.w3.org/TR/2007/REC-soap12-part1-20070427/#soapfault)
  define different envelope and fault structures, while actor/role, headers,
  body content, HTTP action, and processing models can involve transport and
  application policy.
- **Interpretations and peer behavior:** Coerce both versions into one loose
  XML shape, preserve arbitrary envelope children, bind HTTP behavior, or
  expose a strict request/response profile with raw escape hatches.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  The namespace selects exactly SOAP
  1.1 or 1.2. Header is optional and precedes one required Body; unexpected
  envelope children or text fail. Typed body decoding requires exactly one
  child; raw sections remain copied bytes with inherited namespaces preserved
  for decoding. A valid fault returns both the envelope and `*FaultError`, with
  version-specific code, reasons, subcodes, actor/node/role, and Detail. HTTP,
  SOAPAction, header processing, WSDL, WS-* and application mapping are outside
  this package.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestParseSOAP11EnvelopePreservesRawSections`,
  `TestParseSOAP12FaultReturnsTypedErrorAndEnvelope`,
  `TestParseRejectsInvalidEnvelopeStructure`,
  `TestDecodeBodyRetainsInheritedNamespaces`, and
  `TestMarshalFaultRoundTripsBothVersions` cover `soap`. Reconsider for a new
  separately specified processing profile, not by weakening this one.

## WIRE-DEC-008: YAML schema, aliases, merges, duplicates, and streams

**Authoritative reference:** [YAML 1.2.2](https://yaml.org/spec/1.2.2/).

- **Status, owner, and classification:** `resolved`; maintainers; YAML 1.2.2
  interoperability and defensive policy.
- **Source and issue:** YAML 1.2.2
  [chapters 3 and 10](https://yaml.org/spec/1.2.2/) define representation,
  serialization, presentation, and recommended schema models with tags,
  anchors, aliases, and multi-document streams. Native Go mapping cannot
  preserve every YAML graph or key shape uniformly; merge keys are a codec
  interoperability feature rather than YAML 1.2.2 core syntax.
- **Interpretations and peer behavior:** Use permissive YAML 1.1 typing, reject
  every advanced feature, expose dependency defaults, or select explicit safe
  v4 behavior and options. YAML codecs differ on duplicate keys and alias
  limits.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Use `go.yaml.in/yaml/v4` v4 defaults,
  one document and unique keys by default, sorted mapping output, and codec
  depth/alias protections. Multiple documents require a slice and opt-in;
  aliases and merge keys may be independently rejected, and caller limits may
  be stricter. Tags, core-schema implicit values, and typed non-string map keys
  remain dependency-governed and documented; no JSON-compatible-subset claim
  is made.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestDecodeRejectsMalformedDuplicateAndMultipleDocuments`,
  `TestDecodeDefinesAliasAnchorAndMergeBehavior`,
  `TestDecodeDefinesTagsImplicitTypesAndNonJSONKeys`,
  `TestDecodeClassifiesBuiltInResourceProtectionAsSizeLimit`, and
  `TestYAMLRepairsDependencyBlockIndentDifferential` cover `yamlwire`.
  Reconsider every YAML dependency or default-schema change.

## WIRE-DEC-009: TOML document model and conversion

**Authoritative reference:** [TOML 1.1.0](https://toml.io/en/v1.1.0).

- **Status, owner, and classification:** `resolved`; maintainers; TOML 1.1.0
  interoperability policy.
- **Source and issue:** TOML 1.1.0
  [defines](https://toml.io/en/v1.1.0) one UTF-8 configuration document with
  unique keys, tables, date/time values, and bounded numeric grammar, while Go
  target conversion and unknown-field handling remain application choices.
- **Interpretations and peer behavior:** Treat TOML as generic maps, coerce
  out-of-range values, ignore unknown keys, or preserve native TOML types and
  expose strict target validation.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  `BurntSushi/toml` v1.6.0 parses one
  complete document, including the valid empty document, and rejects duplicate
  or conflicting keys. Native date/time and numeric types are preserved;
  narrowing or incompatible target conversion is validation failure. Unknown
  fields are optional strictness. Encoding sorts map keys and supports explicit
  whitespace-only indentation, but does not promise source formatting or
  comments round-trip.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestDecodeFixturePreservesDatetimeAndNumericTypes`,
  `TestDecodeRejectsMalformedDuplicateAndTrailingData`,
  `TestDecodeRejectsUnknownFieldsAndNumericLossWhenRequested`, and
  `TestEncodeIsDeterministicAndPreservesNativeTypes` cover `tomlwire`.
  Reconsider on TOML edition or codec changes.

## WIRE-DEC-010: MessagePack maps, extensions, numeric fit, and structure

**Authoritative reference:** [MessagePack format specification](https://github.com/msgpack/msgpack/blob/8aa09e2a6a9180a49fc62ecfefe149f063cc5e4b/spec.md).

- **Status, owner, and classification:** `resolved`; maintainers; MessagePack
  interoperability and defensive policy.
- **Source and issue:** The pinned MessagePack
  [format specification](https://github.com/msgpack/msgpack/blob/8aa09e2a6a9180a49fc62ecfefe149f063cc5e4b/spec.md)
  defines compact values, arbitrary map keys, extension identifiers, and
  multiple integer widths, but not duplicate-key resolution, Go assignment
  narrowing, application extension registries, or resource limits.
- **Interpretations and peer behavior:** Accept last-key-wins maps, coerce
  numbers, decode all keys into interfaces, trust declared lengths, or validate
  structure and target fit before assignment.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Exactly one object is required.
  Recursive duplicates fail by default with opt-in compatibility; untyped maps
  require string keys while typed comparable keys remain available. Unknown
  extensions are unsupported, registered timestamp behavior is retained, and
  numeric overflow or precision loss into typed destinations fails before the
  main assignment. Explicit finite nesting and collection limits preflight
  forged lengths. Encoders sort map keys; compact integer, float, and array-
  struct modes are explicit.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestDecodeRejectsMalformedTrailingAndUnknownExtensions`,
  `TestDecodeRejectsDuplicateMapKeysByDefault`,
  `TestDecodeValidatesNestedNumericAssignments`,
  `TestDecodeEnforcesDefaultStructuralLimits`, and
  `TestEncodeIsDeterministicAndConfigurable` cover `msgpackwire`. Reconsider
  when extension ownership or a codec with native safe limits is adopted.

## WIRE-DEC-011: CBOR deterministic profiles and accepted data items

**Authoritative reference:** [RFC 8949](https://www.rfc-editor.org/rfc/rfc8949.html).

- **Status, owner, and classification:** `resolved`; maintainers; RFC 7049,
  RFC 8949, and CTAP2 profile policy.
- **Source and issue:** RFC 7049
  [section 3.9](https://www.rfc-editor.org/rfc/rfc7049.html#section-3.9), RFC
  8949 [section 4.2](https://www.rfc-editor.org/rfc/rfc8949.html#section-4.2),
  and CTAP 2.2 [section 8](https://fidoalliance.org/specs/fido-v2.2-ps-20250714/fido-client-to-authenticator-protocol-v2.2-ps-20250714.html#sctn-encoded-message)
  define different deterministic profiles. CBOR tags, indefinite lengths,
  duplicate keys, simple values, bignums, and preferred serialization are
  separately configurable.
- **Interpretations and peer behavior:** Call every stable encoding canonical,
  accept all valid CBOR by default, silently normalize profiles, or expose the
  exact three profiles and independent decode policy.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  `Canonical` means RFC 7049 section
  3.9, `CoreDeterministic` means RFC 8949 core deterministic encoding, and
  `CTAP2Deterministic` means the pinned CTAP2 profile. Decode always rejects
  duplicate keys; tags and indefinite lengths are independently opt-in, while
  resource bounds remain finite. Encode defaults to legacy `Canonical`; tags
  require explicit permission and time tags require tags. Valid simple values
  and tagged bignums retain codec-defined Go representations. No decoder claim
  is made that incoming bytes already satisfy an encode profile.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestEncodeUsesExplicitDeterministicProfiles`,
  `TestDecodeDefinesTagsAndIndefiniteLengthBehavior`,
  `TestDecodePreservesSimpleValuesAndBignums`, and
  `TestDecodeEnforcesUnknownFieldsNumericAndResourceLimits` cover `cborwire`.
  Reconsider if profile naming or minimum-version defaults change.

## WIRE-DEC-012: BSON document identity, order, duplicates, and conversion

**Authoritative reference:** [BSON Specification Version 1.1](https://bsonspec.org/spec.html).

- **Status, owner, and classification:** `resolved`; maintainers; BSON 1.1 and
  official-driver interoperability policy.
- **Source and issue:** BSON
  [Specification Version 1.1](https://bsonspec.org/spec.html) defines an
  ordered document with length prefixes and typed elements, while generic Go
  maps lose ordering and the specification does not select duplicate-name
  resolution or lossy target conversion.
- **Interpretations and peer behavior:** Accept scalars, ignore trailing bytes,
  accept duplicate names, claim map determinism, create local BSON types, or
  retain the official driver model with explicit options.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  APIs require exactly one complete
  top-level document whose declared length and terminator validate. Recursive
  duplicate names fail by default with explicit compatibility opt-in.
  Official driver types are re-exported rather than copied. Struct, `D`, and
  raw order are stable; `M` map order is explicitly not deterministic.
  ObjectID-as-string and truncating-double conversion, integer-width
  minimization, and JSON struct tags are explicit options rather than defaults.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestDecodeRejectsMalformedTrailingDuplicateAndScalarData`,
  `TestDecodeRejectsNestedDuplicateKeys`,
  `TestDecodeProvidesExplicitInteroperabilityOptions`,
  `TestRoundTripPreservesDecimalBinarySubtypeAndRegex`, and
  `TestEncodeOrderedDocumentsAreDeterministic` cover `bsonwire`. Reconsider on
  BSON edition or official driver type changes.

## WIRE-DEC-013: Deterministic output is not universal canonicalization

**Authoritative reference:** [RFC 8949](https://www.rfc-editor.org/rfc/rfc8949.html).

- **Status, owner, and classification:** `resolved`; maintainers; wire-format
  compatibility policy.
- **Source and issue:** Some formats define deterministic or canonical
  encodings, while others permit many semantically equivalent serializations.
  Stable current bytes can be mistaken for a standards-level canonical form.
- **Interpretations and peer behavior:** Claim every encoder is canonical,
  promise only semantic round-trip, or describe determinism per format and
  value shape.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  JSON, YAML, TOML, and MessagePack
  sort supported map keys; XML follows stable struct traversal but is not C14N;
  SOAP uses fixed package-owned envelope emission; CBOR names its exact
  profile; BSON guarantees order only for ordered shapes. Formatting options,
  codec upgrades, semantically unordered maps, and dependency representations
  remain compatibility-relevant. Stable bytes are claimed only where the
  format matrix says so.
- **Evidence, public surface, upstream, and reconsideration:** Per-format
  `TestEncodeIsDeterministicAndConfigurable`,
  `TestEncodeIsDeterministicAndPreservesNativeTypes`,
  `TestEncodeUsesExplicitDeterministicProfiles`, and
  `TestEncodeOrderedDocumentsAreDeterministic` cover encoder options. Reconsider
  every codec upgrade or proposed signing/hash use.

## WIRE-DEC-014: Error classification, causes, and disclosure

**Authoritative reference:** [Go errors package](https://pkg.go.dev/errors).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  API and privacy policy.
- **Source and issue:** Codec errors mix syntax, type conversion, unsupported
  features, limits, destination failures, and valid SOAP faults. Raw errors can
  expose input fragments, while flattening loses actionable causes.
- **Interpretations and peer behavior:** Return codec errors directly, expose
  only sentinels, redact every diagnostic, or wrap bounded causes in a stable
  cross-format taxonomy.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  `wire.Error` classifies parse,
  validation, target, unsupported, envelope, SOAP fault, size, encode, and
  write outcomes with format and operation. `errors.Is` and `errors.As` retain
  stable classification and useful causes. The package does not echo complete
  payloads or the tested sensitive values; callers must still treat field names
  and small offending lexemes as potentially sensitive. Valid SOAP faults are
  protocol outcomes, not malformed input.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestErrorKindsMatchTheirSentinels`,
  `TestErrorSupportsClassificationAndWrapping`,
  `TestDecodeErrorsDoNotEchoSensitiveValues`, and SOAP fault tests cover
  `Error`, sentinels, and `FaultError`. Reconsider whenever a dependency error
  shape or disclosure boundary changes.

## WIRE-DEC-015: Encode graph validation and dependency differentials

**Authoritative reference:** [Go language specification](https://go.dev/ref/spec).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  Go-value and dependency-boundary policy.
- **Source and issue:** Recursive Go values can cycle or exceed stack-safe
  depth even where the wire format itself has no references. Mature codecs can
  also accept or emit behavior that conflicts with this package's guarantees.
- **Interpretations and peer behavior:** Trust codecs, recover panics, reject
  every shared reference, or preflight only active recursion paths and retain
  differential tests for known policy gaps.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Every typed encoder rejects cycles
  on the active path and traversal beyond 1,000 levels before recursive codec
  work. Reused acyclic references remain valid. Package policy deliberately
  differs from dependencies for invalid JSON UTF-8, MessagePack, CBOR and BSON
  duplicate handling, and YAML block-scalar emission. Those differences are
  executable compatibility evidence, not hidden patches.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestAllEncodePathsRejectCyclicValues`,
  `TestValidateAcceptsAcyclicValues`,
  `TestValidateRejectsCyclesAndDepth`, and all tests in
  `dependency_differential_test.go` cover shared validation and codec seams.
  Reconsider on any dependency upgrade or new recursive value support.

## WIRE-DEC-016: Transport, schema, and application boundaries

**Authoritative reference:** [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html).

- **Status, owner, and classification:** `resolved`; maintainers; scope and
  ownership policy.
- **Source and issue:** Wire formats are commonly coupled to media types, HTTP,
  RPC, schemas, WSDL, vendor mapping, persistence, signing, or content
  negotiation, none of which is defined uniformly by the format grammars.
- **Interpretations and peer behavior:** Grow one serialization framework that
  owns all surrounding policy, infer transport behavior, or remain a narrow
  format boundary composed by higher layers.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  `wire` owns bounded byte and
  reader/writer format handling only. It does not select HTTP content types or
  status codes, SOAPAction, JSON-RPC/JSON:API behavior, WSDL/XSD validation,
  schema registries, compression, signatures, persistence, or business
  normalization. Callers must select and validate those policies before or
  after this boundary. No known material ambiguity inside the current public
  surface remains unresolved.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSharedDocumentationConventions`, the format matrix, architecture docs,
  and package dependency graph cover the boundary. No single upstream source
  owns it; reconsider only through a separately specified additive API and a
  new decision entry.

## Unresolved and excluded behavior

No known material ambiguity in the current public surface is unresolved.
Format auto-negotiation, arbitrary charset guessing, XML canonicalization,
DTD/entity expansion, general YAML graph preservation, MessagePack extension
registration, validation of incoming CBOR deterministic form, BSON scalar
encoding, HTTP policy, WSDL/XSD, schema validation, signatures, compression,
and application mapping are outside the current claim. Adding one requires a
new decision before runtime implementation.
