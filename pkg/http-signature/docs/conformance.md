# Normative conformance matrix

The [specification decision register](specification-decisions.md) records the
ambiguities, alternatives, selected behavior, consequences, and reconsideration
conditions behind this matrix.

[`spec/normative-requirements.json`](../spec/normative-requirements.json) is
the exhaustive machine-checked inventory for three precisely defined source
surfaces: every BCP 14 keyword occurrence outside conventions boilerplate,
every locally declared ABNF production, and every numbered main-body prose
section in the checksum-pinned RFC 8941, RFC 9421, and RFC 9530 texts. Each item
maps through a named group to implementation, executable tests, and
documentation. This section-level prose inventory covers normative algorithms,
definitions, and security requirements that do not use BCP 14 keywords; it
does not misleadingly recast every prose sentence as a separate requirement.
The tables below summarize those mappings by protocol topic. `make conformance`
verifies that no inventoried keyword occurrence, ABNF production, or prose
section was added, omitted, duplicated, or mapped to a missing artifact.
This is exhaustive source traceability, not a substitute for behavioral proof:
the structural gate validates group mappings and artifact existence but does
not infer that every referenced test asserts every requirement in its group.
Corpus execution, differential results, coverage, and mutation evidence are
reported by their own gates.

This matrix is tied to the source digests in
[`spec/sources.lock.json`](../spec/sources.lock.json). Normative RFC prose
controls over examples; RFC 8941 parsing algorithms control over its
illustrative ABNF if they disagree. `Application` means the RFC explicitly
assigns the choice or enforcement to an application profile; the package
exposes the required decision point and does not invent a default. IANA
registration procedure requirements are recorded as `Registry`, not runtime
behavior.

## Authoritative source inventory

`make spec-sources` validates the byte identity of the three RFC texts, all
three RFC Editor errata searches, the IANA XML registries, the HTTPWG RFC 8941
Structured Fields corpus snapshot, and the NIST CAVP ECDSA corpus. It also
compares the following machine-readable inventories in `sources.lock.json`
against the fetched sources:

- RFC 8941 has no recorded errata; RFC 9421 has verified errata 8102 and 8103;
  RFC 9530 has verified errata 8158 and 8273 plus reported erratum 8890.
- The complete IANA Signature Algorithms (6), Signature Metadata Parameters
  (6), Derived Component Names (10), Component Parameters (6), and HTTP Digest
  Algorithms (8) registries are recorded with security-relevant status and
  target fields. The deprecated legacy HTTP Digest Algorithm Values registry
  (8) is separately pinned with its direct RFC 3230 relationship and all three
  RFC 3230, RFC 9530, and replacement-registry deprecation-note references.
- The nine package-relevant HTTP Field Name records are pinned, including
  `Digest` and `Want-Digest` as obsoleted, their direct RFC 3230 references,
  and their RFC 9530 Section 1.3 obsoletion comments plus each active field's
  permanent status and Structured Field type where IANA publishes one.
- The HTTPWG corpus is pinned at revision
  `faed1f92942abd4fb5d61b1f9f0dc359f499f1d7`, the last revision explicitly
  targeting RFC 8941 before later RFC 9651-only types. `make conformance`
  checksum-validates and safely extracts that archive, then executes all 1,526
  parsing cases and all 544 serialization cases through the package's strict
  Structured Fields boundary. Source locking proves provenance and identity;
  the executed case counts are enforced separately by the corpus test.

## RFC 8941 dependency

RFC 9421 and RFC 9530 normatively depend on RFC 8941 Structured Fields. The
matrix therefore treats RFC 8941 as part of the acceptance boundary instead of
an untracked parser-library detail.

| Section | Normative requirement | Decision and implementation | Executable evidence | Documentation |
|---|---|---|---|---|
| 1.1–1.2 | Parsing is intentionally strict; implementations must behave indistinguishably from the algorithms, which take precedence over ABNF. | All package Structured Field entry points use the same strict parser boundary and fail closed on malformed input. | `httpwg_rfc8941_corpus_test.go` (1,526 parsing cases), `structured_field_safe_test.go`, malformed cases in parser tests and fuzzing | [Security](security.md) |
| 2 | Structured Field definitions must identify their field location, top-level type, semantics, constraints, and failure handling. BCP 14 keywords inside the `Foo-Example` template apply to that illustrative field, not to this package at runtime. | Package field definitions are inventoried through RFC 9421/RFC 9530 and IANA evidence; the illustrative field is retained in the keyword inventory as specification-author guidance. | `scripts/check-conformance.sh`, `scripts/check-spec-sources.sh` | This matrix |
| 3 | Lists, Dictionaries, Items, Inner Lists, and Parameters are ordered models with unique keys; locally declared ABNF defines their wire syntax. | The matrix inventories all 26 RFC 8941 ABNF productions and maps package-used types to the parser/serializer boundary. | `httpwg_rfc8941_corpus_test.go`, `message_test.go`, `signature_fields_test.go`, `digest_test.go`, `digest_preference_test.go` | This matrix |
| 3.1–3.3 | Generic parsers must support the RFC minimum collection, key, String, Token, and Byte Sequence sizes; field specifications may constrain cardinality and value types. | These field-specific parsers deliberately impose explicit application bounds before and after dependency parsing instead of advertising themselves as an unrestricted general RFC 8941 parser. | `syntax_limits_test.go`, `fuzz_test.go` | [Security](security.md) |
| 4.1 | Serialization follows the ordered algorithms, including Boolean omission, strict String escaping, canonical numbers, and padded Byte Sequences. | Package serializers use one canonical Structured Fields implementation and preserve ordered extension parameters. | `httpwg_rfc8941_corpus_test.go` (544 serialization cases), signature, digest, preference, and component serialization tests | This matrix |
| 4.2 | Field lines combine before parsing; any parse error fails the complete value; invalid base64 alphabet and line feeds fail while absent padding and non-zero pad bits should be tolerated. | Header instances are passed as one field value set and malformed complete values fail closed. | HTTPWG corpus parsing cases, combined-field tests, and parser boundary tests | [Security](security.md) |
| 5–6 | Field-definition administration and strict parsing have interoperability and resource-exhaustion consequences. | Registry/source administration is separated from runtime behavior; input limits and panic containment bound hostile parsing. | `scripts/check-spec-sources.sh`, `scripts/check-conformance.sh`, `structured_field_safe_test.go` | [Security](security.md) |

## RFC 9421

| Section | Normative requirement | Decision and implementation | Executable evidence | Documentation |
|---|---|---|---|---|
| 1.1 | An application profile **MUST** specify covered components and parameters, Structured Field types, key retrieval, algorithm choice, signature verification, and result use. | `SigningProfileConfig`, `VerificationProfileConfig`, `MessageContext.StructuredFields`, resolvers, signer/verifier, and adapter callbacks make these decisions explicit. No authorization result is produced. | `signer_test.go`, `verifier_test.go`, `http_integration_test.go` | [Security](security.md) |
| 2 | Signer and verifier component context **MUST** be available, identical for all components, and derived consistently. | One immutable `MessageContext` is passed through complete base construction; external target context is one complete value. | `TestCreateSignatureBaseUsesExplicitExternalRequestTargetThroughout`, profile external-context tests | [Integration](integration.md) |
| 2 | Each component identifier **MUST** occur once; parameter order is preserved but ignored for equality; parameterized variants **MAY** coexist. | Parsing, profiles, and base creation compare sorted parameter identities while retaining received order for serialization. | `TestParseSignatureInputsRejectsAmbiguityAndWrongStructuredTypes` | This matrix |
| 2 | Component values **MUST NOT** contain newline. | Derived and field validation rejects CR/LF outside permitted obsolete folding. | `message_test.go` malformed component cases | [Security](security.md) |
| 2.1 | HTTP field component names **MUST** be lowercase. | Syntax accepts only lowercase field-name characters. | `TestParseSignatureInputsAllowsLowercaseHTTPFieldNameCharacters`, uppercase rejection | This matrix |
| 2.1 | Field values **MUST** come from the target header unless `tr` or `req` changes the source; non-ASCII **MUST** be encoded before base inclusion. | Header/trailer/related-request sources are separate. Ordinary mode rejects non-ASCII; `bs` encodes raw octets. | `TestCreateSignatureBaseCombinesFieldInstancesAndSeparatesTrailers`, `TestCreateSignatureBaseBinaryWrapsNonASCIIFieldOctets` | [Integration](integration.md) |
| 2.1 | Multiple instances **MUST** combine with comma and space; robust single-instance or strict list/dictionary processing is **RECOMMENDED**. | Ordinary unambiguous fields normalize each instance then join with `, `; `sf` uses strict Structured Fields serialization. Known transformation hazards fail closed: outbound Cookie requires one canonical value and multi-line Set-Cookie requires `bs`. | `message_test.go` field combination tests, `TestSignatureBaseRejectsAmbiguousCookieFieldInstances` | [Security](security.md) |
| 2.1 | Implementations **MUST** derive the same target field value after HTTP-version and framework transformations. | Host, content length, transfer encoding, trailer declarations, connection close, and explicit User-Agent use deterministic `net/http` wire state. `MessageContext.ResponseTransport` selects preserved received identity or `Response.Write` identity; its zero value fails when those models differ. Stale outbound map aliases are ignored; unavailable inbound trailer order, impossible received transfer coding, implicit User-Agent, and Unicode Host punycode fail closed. | `TestSignatureBaseUsesNetHTTPTransportManagedRequestFields`, `TestSignatureBaseUsesNetHTTPTransportManagedResponseFields`, response transport-mode and invalid Host tests | [Specification decisions](specification-decisions.md) |
| 2.1 | Applications **MUST** account for semantically equivalent field serializations. | Profiles explicitly identify known Structured Field types and component parameters; no unknown semantic normalization occurs. | strict Structured Fields tests | [Security](security.md) |
| 2.1 | Field component parameters **MAY** be combined only where compatible. | `sf`, `key`, `bs`, `tr`, and `req` are parsed; `bs` with `sf`/`key` and parameters on invalid targets fail base creation. | `TestCreateSignatureBaseRejectsUnresolvableOrIncompatibleComponents` | This matrix |
| 2.1.1 | With `sf`, value **MUST** use formal serialization; multiple List/Dictionary lines **MUST** combine before serialization. | The declared field type selects RFC 8941 parse and canonical marshal across all field instances. | Structured Fields cases in `message_test.go` | This matrix |
| 2.1.2 | Dictionary lines **MUST** combine; a parameterized key **MUST NOT** appear twice; a missing key **MUST** fail; keys **MAY** be covered in any order. | `key` parses the combined dictionary, selects exactly one member, preserves base order, and missing/duplicate component identity fails. | dictionary-member tests in `message_test.go` and signature parser tests | This matrix |
| 2.1.3 | Problematic fields **SHOULD** use `bs`; `bs` component value **MUST** trim OWS, unfold obs-fold, encode each raw value, then serialize a Byte Sequence list. | Raw octets are normalized per instance, base64 encoded, and joined without combining instance semantics. | `TestCreateSignatureBaseBinaryWrapsNonASCIIFieldOctets` and multi-instance tests | [Security](security.md) |
| 2.1.4 | Trailer coverage **MUST** use `tr`; header and trailer values **MUST NOT** combine and **MAY** both be signed. | `tr` selects only `Trailer`; distinct identifiers can cover both. Request and response adapters declare, finalize, wait for, and authenticate trailer fields explicitly. | `TestCreateSignatureBaseCombinesFieldInstancesAndSeparatesTrailers`, request and response trailer integration tests | [Integration](integration.md) |
| 2.2 | Derived names **MUST** start `@` and processors **MUST** distinguish them from fields. Definitions **MUST** declare targets. | All active IANA derived names have explicit request/response handling; unknown names fail. | derived-component table tests in `message_test.go` | This matrix |
| 2.2 | Derived values **MUST** be printable, contain no newline, and have no boundary whitespace. | `validDerivedValue` enforces all byte and boundary rules. | `TestCreateSignatureBaseRejectsDerivedWhitespaceBoundaries` | This matrix |
| 2.2.3 | `@authority` **MUST** lowercase host and remove the default port; it **SHOULD** replace direct `Host` signing. | Authority normalization implements RFC 9110 rules; profile coverage remains application-owned. | authority/default-port and proxy tests | [Security](security.md) |
| 2.2.4 | `@scheme` **MUST** be lowercase. | Only HTTP(S) schemes are accepted and lowercased. | request-origin normalization tests | This matrix |
| 2.2.5 | Request target forms are origin, absolute, authority, or asterisk; non-HTTP/1.1 use is **NOT RECOMMENDED**. | Raw `RequestURI` or explicit external target is preserved across all four forms. Profiles decide whether the component is allowed for the HTTP versions deployed. | `TestCreateSignatureBaseUsesAbsoluteFormRequestTargetOrigin`, `TestCreateSignatureBasePreservesAuthorityAndAsteriskRequestTargets` | [Integration](integration.md) |
| 2.2.8 | `@query-param` **MUST** follow HTML form parsing; `name` is **REQUIRED**; missing or repeated names **MUST** fail; distinct names **MAY** appear in any order; whole query signing is **RECOMMENDED** for repeats. | Percent decoding, plus-to-space, UTF-8 replacement, form re-encoding, exact-name selection, and repeated-name rejection are implemented. | query parameter tests including UTF-8 replacement and duplicates | [Security](security.md) |
| 2.2.9 | `@status` **MUST NOT** target a request. | Request bases reject it; valid response status is required. | `TestCreateSignatureBaseRejectsUnresolvableOrIncompatibleComponents`, response integration tests | This matrix |
| 2.3 | `@signature-params` is **REQUIRED** last and **MUST NOT** appear in covered components; covered-component order **MUST** match base order. | Base construction appends it exactly once and rejects explicit coverage. | RFC request example and rejection tests | This matrix |
| 2.4 | `req` **MAY** coexist with valid parameters; it **MUST NOT** target a request; requester **MUST** retain original request values. | Responses require an explicit `RelatedRequest`; request targets reject `req`. Adapters retain/bind the exact request. | response signing/verifying tests | [Integration](integration.md) |
| 2.4 | Response signatures over signed requests **SHOULD** cover all request-signature components. | Application profile decision; required components can enumerate the request fields and signature dictionary members. No incomplete automatic expansion is performed. | request-response binding profile tests | [Security](security.md) |
| 2.5 | Component identifiers **MAY** have registered parameters; all described base-generation errors **MUST** stop immediately with no base. | Ordered RFC 8941 serialization is used and `CreateSignatureBase` returns no partial value. | malformed and unresolvable base tests | This matrix |
| 3.1 | Signer **MUST** select application-allowed algorithm and compatible key; component order **MUST NOT** change; each component **MUST** be a field or registered derived name; request control data and `created` **SHOULD** be covered/included. | Immutable signing profiles bind allowed algorithms, exact keys, ordered components, and explicit parameter policy. No universal coverage is chosen. | `signer_test.go`, `algorithm_test.go` | [Security](security.md) |
| 3.2 | Verifier **MUST** follow parsing, policy, trusted-key, algorithm, base, and cryptographic steps; unknown/untrusted key **MUST** fail; algorithms from multiple sources **MUST** agree. | `Verifier.Verify` enforces ordered selection, profile, bounded resolution, key lifecycle/freshness, exact algorithm binding, base recreation, crypto, then replay consume. | `verifier_test.go` | [Security](security.md) |
| 3.2.1 | Applications **MAY** add requirements, **MUST** enforce them, and **MUST NOT** accept nonconforming signatures. | Profiles are mandatory and invalid/insufficient coverage fails before key use. | policy failure tests | [Security](security.md) |
| 3.3 | Signature method **MUST** be appropriate; registered `alg` values **MUST** be used when explicit; JWS `none` and prohibited JOSE algorithms **MUST NOT** be used. | Only six active native registry algorithms are accepted. JWS is not implemented and no `none` path exists. | RFC 9421 Appendix B fixed vectors for five algorithms, a pinned NIST CAVP P-384/SHA-384 vector, unsupported-algorithm tests, and strict-key tests | [Security](security.md) |
| 4 | Signature labels **MUST** be unique and shared by both fields; both fields **MUST** exist with identical label sets; each field **MAY** carry multiple labels/lines. | Parsers reject duplicates and wrong member types; verifier requires equal sets; ordered values avoid map-order output. | `signature_fields_test.go`, verifier label-set tests | This matrix |
| 4.1 | `Signature-Input` **MUST** preserve the exact serialized metadata used in the base; trailer use **MAY** occur but header use is **RECOMMENDED**; labels across lines **MUST** be unique. | Parsed order is preserved and canonical serialization drives the base. Header and explicit request/response trailer adapters are separate. | parser round trips and request/response trailer integration tests | [Integration](integration.md) |
| 4.2 | `Signature` trailer use **MAY** occur but header use is **RECOMMENDED**; labels across lines **MUST** be unique. | Same as 4.1; RFC 8941 Item parameters are preserved as extension data; trailer loss is documented and fail-closed at recipients. | signature parameter round-trip and trailer integration tests | [Integration](integration.md) |
| 4.3 | Multiple signatures **MAY** occur and each **MUST** have a unique label. | `CombineSignedFields` preserves caller order and rejects duplicates. | signer combination tests | This matrix |
| 5 | `Accept-Signature` response variation **SHOULD** be uncacheable or list the field in `Vary`. | Application cache policy; parser exposes ordered requests and integration documentation mandates cache handling. | `accept_signature_test.go` parses all negotiation data | [Integration](integration.md) |
| 5 | Target **MUST** contain requested labels/components, sender **MUST** request only target-valid identifiers, and fulfilled signature **MUST** keep label/components/process parameters; extra parameters/signatures **MAY** occur; receiver **MAY** reject profile-incompatible requests. | Parser/serializer preserves requests. Fulfillment is deliberately an application signing-profile operation: applications validate target type, construct an exact profile, and sign or reject. No unsafe auto-accept helper exists. | `accept_signature_test.go`, signer profile tests | [Security](security.md) |
| 6.2–6.5 | Registry identifiers **MUST** follow syntax/uniqueness/status/target rules and **SHOULD** remain short; component parameter definitions **MUST** declare targets. | `Registry`: applies to IANA registration administration. Runtime behavior is pinned to the current registries and accepts extensions syntactically while base construction requires known semantics. | source checksum and active registry inventory | `spec/sources.lock.json` |
| Appendix B | Example private keys **MUST NOT** be used except for tests. | Official keys are confined to test fixtures; public APIs contain no embedded keys. | RFC vector tests | [Security](security.md) |

## RFC 9530

RFC 9530 defines its four fields as RFC 8941 Dictionaries and declares no
additional local ABNF productions; the machine inventory therefore requires an
empty RFC 9530 `abnf_rules` set while mapping the delegated RFC 8941 grammar.

| Section | Normative requirement | Decision and implementation | Executable evidence | Documentation |
|---|---|---|---|---|
| 2 | Recipient **MAY** ignore digests and application policy **MAY** constrain validation. Sender **MAY** send unsupported/ignored algorithms. | Parsing retains unknown keys and RFC 8941 Item parameters; `Verify` checks every explicitly required active algorithm and ignores unselected members. | digest parameter round-trip and `TestDigestFieldVerifyRequiresEverySelectedAlgorithm` | [Security](security.md) |
| 2 | `Content-Digest` **MAY** be in trailers and **MAY** be merged into headers. | Header and trailer sources are explicit. Request and response streaming generation declares trailers; verification waits for EOF. The signature profile fixes which source is authenticated. | request and response body/trailer integration tests | [Integration](integration.md) |
| 3 | The same recipient/sender **MAY** rules apply to `Repr-Digest`. | Core digest parsing/computation is field-name independent; application owns selected representation bytes and semantics. | digest tests | [FAQ](faq.md) |
| 3 | `Repr-Digest` **MAY** be in trailers and **MAY** be merged into headers. | Message canonicalization supports `tr`; application integration chooses and authenticates one source. | trailer component tests | [Integration](integration.md) |
| 3.1 | For state-changing requests not describing the target, digest **MUST** cover enclosed representation data. Status representations **MUST** cover the enclosed representation; referenced-resource responses **MUST** cover that resource's selected representation. | Application decision because `net/http` body bytes do not identify selected-representation semantics. `ComputeDigests` accepts the exact application-supplied bytes; generic adapters do not mislabel body bytes as `Repr-Digest`. | RFC sample digest vectors and application integration tests | [FAQ](faq.md) |
| 4 | Integrity preferences use integer weights 0–10; they are hints and can be ignored. | Strict ordered dictionary parser/serializer rejects other types, parameters, ranges, and duplicate keys while retaining unknown algorithms. | `digest_preference_test.go` | [FAQ](faq.md) |
| 5 | Active algorithms are **RECOMMENDED**. Deprecated algorithms **MAY** protect against accidental corruption but **MUST NOT** be used adversarially, including signed integrity fields. | Local compute/required verification supports only active SHA-256 and SHA-512. Deprecated keys remain syntax/negotiation data only. | unsupported digest tests and negotiation examples | [Security](security.md) |
| 7.1–7.3 | The four replacement fields and active hash registry are registered; `Digest`, `Want-Digest`, and their legacy algorithm registry are obsolete or deprecated. | Both active and legacy IANA sources, every registry record, the two obsolete field records, and the legacy deprecation note are machine validated. | `scripts/check-spec-sources.sh` | `spec/sources.lock.json` |

## Cross-cutting resource and errata decisions

All field parsers apply `DefaultSyntaxLimits()`; `WithLimits` variants accept
smaller application bounds. Body adapters require explicit byte limits.
Resolver and replay operations require deadlines, validity/freshness windows,
and bounded capacity/TTL. Verified RFC 9421 errata 8102 and 8103, verified RFC
9530 errata, and the reported RFC 9530 Brotli example decision are recorded in
[`spec/errata-decisions.md`](../spec/errata-decisions.md).
