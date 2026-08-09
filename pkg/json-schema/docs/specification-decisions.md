# JSON Schema specification decisions

This register records observable choices where a supported JSON Schema
dialect, vocabulary, output specification, referenced standard, or official
conformance corpus permits or appears to permit more than one implementation.
The pinned normative and conformance inputs are listed in
[`specification/README.md`](../specification/README.md).

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

Statuses are `resolved`, `unresolved`, or `superseded`. Resolved decisions are
part of the compatibility contract. Changing one requires specification
review, executable evidence, compatibility review, and a changelog entry.

## JSONSCHEMA-DEC-001: Dialect and vocabulary selection

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | Draft 2020-12 Core [sections 8.1.1 and 8.1.2](https://json-schema.org/draft/2020-12/json-schema-core.html#section-8.1), Draft 2019-09 Core [section 8.1](https://json-schema.org/draft/2019-09/json-schema-core.html#section-8.1), and the published Draft 3, 4, 6, and 7 meta-schema identifiers listed in [`dialects.md`](dialects.md) |
| Classification | Normative dialect interpretation and package default policy |
| Issue | Schema semantics depend on the selected dialect and, for modern drafts, declared vocabularies. A schema without `$schema`, a compound document with embedded dialects, and an unknown vocabulary each require an explicit policy. |
| Credible interpretations | Require `$schema` everywhere; infer a dialect from keywords; use one caller-selected dialect for the complete compound document; or use the caller-selected dialect only as the root fallback and honor recognized embedded `$schema` declarations. |
| Known peer behavior | The Bowtie protocol supports explicit and implementation-default dialect runs. Peer defaults differ and are not interoperability evidence, so the package does not infer semantics from majority behavior. |
| Selected behavior | `WithDialect` selects the root dialect and Draft 2020-12 is the documented constructor default. A recognized embedded `$schema` starts a resource with its declared semantics. Required unknown modern vocabularies fail; optional unknown vocabularies remain inactive. Unsupported dialect identifiers fail instead of falling back. |
| Security and resource consequences | Explicit dialect boundaries prevent attacker-controlled keywords from acquiring unintended semantics. Vocabulary and resource counts remain subject to compiler limits. |
| Compatibility and wire consequences | The same schema bytes can validate differently under different dialects. Changing the default is a compatibility event; released dialect semantics are never retrofitted from a newer draft. |
| Executable evidence | `TestOfficialMetaSchemasCompileAgainstTheirDialect`, `TestOfficialVocabularyFixtures`, `TestCompoundResourcesUseTheirOwnVocabulary`, `TestCompileRejectsUnknownRequiredVocabulary`, `TestVocabularyPolicyHandlesOptionalAndPartialDeclarations`, and `TestDialectFeaturePoliciesAreExact` |
| Public surface | `NewCompiler`, `WithDialect`, `WithVocabulary`, and `Dialect` constants |
| Upstream record | Published dialect and vocabulary meta-schemas are checksum-pinned in `specification/official-meta-schemas.sources.tsv`. |
| Reconsider when | A new released dialect is deliberately added or an erratum changes vocabulary negotiation for a supported dialect. |

## JSONSCHEMA-DEC-002: Unknown keywords

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | Draft 2020-12 Core [section 6.5](https://json-schema.org/draft/2020-12/json-schema-core.html#section-6.5), Draft 2019-09 Core [section 6.5](https://json-schema.org/draft/2019-09/json-schema-core.html#section-6.5), and each supported dialect's official `unknownKeyword` fixtures |
| Classification | Normative forward-compatibility interpretation |
| Issue | Unknown keywords cannot act as assertions, but modern annotation behavior and custom vocabulary registration affect whether their values are retained or interpreted. |
| Credible interpretations | Reject every unknown keyword; silently ignore it for all purposes; retain it as an annotation where the dialect permits; or activate it only through an explicitly registered vocabulary. |
| Known peer behavior | The official suite contains cross-implementation unknown-keyword cases. Some validators expose annotations differently, so validation agreement does not imply output agreement. |
| Selected behavior | Unknown keywords never affect validity. They may contribute annotations where the active dialect's annotation model applies. A keyword gains assertion or applicator semantics only through a registered, active vocabulary; unknown required vocabularies fail under JSONSCHEMA-DEC-001. |
| Security and resource consequences | Unknown values are still parsed and bounded but cannot execute callbacks or trigger retrieval. Annotation byte and output limits apply when values are retained. |
| Compatibility and wire consequences | Adding support for a formerly unknown keyword can change validation and output only when its vocabulary becomes active. Unknown-keyword validation remains forward compatible. |
| Executable evidence | `TestOfficialOptionalCoreFixtures`, the official `unknownKeyword.json` and `refOfUnknownKeyword.json` lanes, `TestRegisteredVocabularyCompilesAndEvaluatesCustomKeyword`, and `TestOfficialAnnotationFixtures` |
| Public surface | `WithVocabulary`, `CollectAnnotations`, and validation output methods |
| Upstream record | The official suite revision and every optional fixture are pinned in `specification/official-suite-results.tsv`. |
| Reconsider when | A supported dialect changes unknown-keyword annotation rules or a new vocabulary activates a formerly unknown keyword. |

## JSONSCHEMA-DEC-003: Format assertion

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | Draft 2020-12 Validation [sections 7.2.1 and 7.2.2](https://json-schema.org/draft/2020-12/json-schema-validation.html#section-7.2), Draft 2019-09 Validation [section 7.2](https://json-schema.org/draft/2019-09/json-schema-validation.html#section-7.2), and historical dialect format definitions |
| Classification | Normative vocabulary behavior and explicit application policy |
| Issue | Modern format annotation is disabled as an assertion by default, format assertion can be required by vocabulary, and historical implementations vary in how aggressively they validate formats. |
| Credible interpretations | Always assert recognized formats; never assert them; assert only when the schema declares the assertion vocabulary; or permit an explicit application option in addition to vocabulary activation. |
| Known peer behavior | The specification documents materially varying validator support. The official optional format corpus is the pinned comparison surface rather than one peer's default. |
| Selected behavior | Recognized `format` values are annotations by default. `WithFormatAssertion` enables assertion explicitly, and the required 2020-12 format-assertion vocabulary activates it. Built-ins are dialect-scoped; unknown formats remain annotations unless a required assertion vocabulary makes support mandatory, in which case compilation fails. Custom formats are compiler-owned. |
| Security and resource consequences | Format checks receive cancellation and dedicated operation, byte, and regular-expression budgets. Panics and sensitive callback errors are contained and redacted. |
| Compatibility and wire consequences | Enabling assertion can turn previously valid instances invalid. Annotation values remain available in output where annotation collection applies. |
| Executable evidence | `TestFormatAssertionIsExplicitAndCompilerOwned`, `TestOfficialFormatAnnotationFixtures`, `TestOfficialOptionalCoreFormatFixtures`, `TestOfficialFormatAssertionVocabularyFixtures`, `TestStandardFormatsDoNotLeakAcrossDialects`, and format fuzz and limit tests |
| Public surface | `WithFormatAssertion`, `WithFormat`, `FormatChecker`, validation, and output APIs |
| Upstream record | Pinned optional format fixtures are included with zero skips in the official-suite manifest. |
| Reconsider when | A supported dialect or erratum changes format vocabulary requirements or a built-in format's normative source. |

## JSONSCHEMA-DEC-004: Content keywords

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | Draft 2020-12 Validation [section 8](https://json-schema.org/draft/2020-12/json-schema-validation.html#section-8), Draft 2019-09 Validation [section 8](https://json-schema.org/draft/2019-09/json-schema-validation.html#section-8), and Draft 7 Validation [sections 8.3 and 8.4](https://json-schema.org/draft-07/json-schema-validation.html#rfc.section.8) |
| Classification | Normative annotation behavior plus historical opt-in policy |
| Issue | Modern content keywords are annotations and automatic decoding is prohibited by default. Draft 7 permits implementation-defined validation behavior, creating a compatibility choice for callers that want historical assertion. |
| Credible interpretations | Never decode content; always decode recognized encodings and media types; or preserve annotation defaults while exposing bounded explicit assertion for dialects where that policy is permitted. |
| Known peer behavior | Official modern fixtures require malformed encoded content to remain valid. The Draft 7 optional `content.json` corpus exercises the separate assertion lane. |
| Selected behavior | Content keywords are annotations by default. `WithContentAssertion` enables bounded Draft 7-compatible assertion for RFC 4648 base64 and JSON or `+json` media types. Unknown encodings and media types remain annotations. Draft 2019-09 and Draft 2020-12 content processing never changes the enclosing validation result, even when the option is present. |
| Security and resource consequences | No implicit decoder or parser runs by default. Opt-in decoding is bounded by input, decoded-content, operation, output, and cancellation limits. |
| Compatibility and wire consequences | Default validation follows modern annotation semantics. Opt-in assertion may reject malformed encoded strings and must be selected deliberately by the caller. |
| Executable evidence | `TestContentKeywordsAreAnnotationsByDefault`, `TestContentAssertionIsLimitedToDraft7`, `TestOfficialDraft7ContentAssertionFixtures`, `TestContentValidationCoversPermissiveAndStrictBranches`, `TestContentValidationSeparatesSyntaxAndResourceFailures`, and official modern content fixtures |
| Public surface | `WithContentAssertion`, validation methods, `CollectAnnotations`, and output methods |
| Upstream record | The pinned corpus keeps modern annotation fixtures and the Draft 7 optional assertion fixture distinct. |
| Reconsider when | A later released dialect standardizes automatic content processing or the package adds a separately owned decoded-content result API. |

## JSONSCHEMA-DEC-005: Annotation collection

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | Draft 2020-12 Core [sections 7.7 and 9.2.1](https://json-schema.org/draft/2020-12/json-schema-core.html#section-7.7), Draft 2019-09 Core [section 7.7](https://json-schema.org/draft/2019-09/json-schema-core.html#section-7.7), and the official annotation corpus |
| Classification | Normative annotation interpretation and package output policy |
| Issue | Annotations may be produced on branches that are later discarded, combined across successful applicators, or needed internally by `unevaluated*` even when a caller does not request them. |
| Credible interpretations | Collect every visited annotation; retain only annotations from successful applicable branches; skip internal collection unless explicitly requested; or retain a tree identical to diagnostic output. |
| Known peer behavior | Annotation APIs and output shapes differ across validators. The official annotation fixtures define semantic expectations but not this package's flat API. |
| Selected behavior | Evaluation computes annotations needed for correct semantics regardless of requested output. `CollectAnnotations` returns a detached deterministic flat list containing only retained annotations from successful applicable paths. Standard diagnostic output remains a distinct hierarchy and may include annotations describing evaluated branches. |
| Security and resource consequences | Annotation values are copied, byte-bounded, operation-bounded, and detached from caller and schema state. Failed or inapplicable branches cannot leak retained data. |
| Compatibility and wire consequences | Ordering is deterministic but semantically secondary. The flat retained-annotation API is not interchangeable with Basic, Detailed, or Verbose output trees. |
| Executable evidence | `TestOfficialAnnotationFixtures`, `TestOfficialArrayAnnotationFixtures`, `TestOfficialAnnotationKeywordFixtures`, `TestAnnotationAndOutputTraversalSkipUnappliedSchemas`, and `TestApplicableAnnotationsContinueAfterInapplicableKeywords` |
| Public surface | `CollectAnnotations`, `Annotation`, `ValidateOutput`, and `OutputUnit` |
| Upstream record | Compatible official annotation cases run for all six supported dialects. |
| Reconsider when | The output specification standardizes a retained-annotation API or a dialect changes annotation combination semantics. |

## JSONSCHEMA-DEC-006: Unevaluated locations

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | Draft 2020-12 Core [section 11](https://json-schema.org/draft/2020-12/json-schema-core.html#section-11), Draft 2019-09 Core [section 9.3.2](https://json-schema.org/draft/2019-09/json-schema-core.html#section-9.3.2), and the official unevaluated fixtures |
| Classification | Normative applicator and annotation interpretation |
| Issue | `unevaluatedItems` and `unevaluatedProperties` depend on successful annotations across references, combinators, conditionals, contains, dependent schemas, and nested unevaluated applicators. Short-circuiting can produce the correct Boolean result while producing the wrong evaluated set. |
| Credible interpretations | Track only sibling keywords; merge every visited branch; merge only successful applicable branches; or rerun selected branches during unevaluated evaluation. |
| Known peer behavior | Unevaluated behavior is a frequent differential boundary. The complete official 2019-09 and 2020-12 fixture families are the pinned authority. |
| Selected behavior | Track evaluated item and property locations through references and all applicable successful branches, preserving the dialect-specific ordering and dependency rules. Failed or inactive branches contribute nothing. Evaluation may continue beyond a decisive Boolean result when annotations are required. |
| Security and resource consequences | Tracking sets and repeated diagnostic work are bounded by instance, operation, depth, and output limits. Tracking failures propagate rather than silently weakening validation. |
| Compatibility and wire consequences | A tracking correction can change validity and output for schemas using `unevaluated*`; it therefore requires wire-compatibility review. |
| Executable evidence | `TestOfficialUnevaluatedPropertiesFixtures`, official `unevaluatedItems` fixtures, `TestUnevaluatedAndPatternKeywordsPropagateTrackingFailures`, `TestUnevaluatedOutputContinuesAfterEvaluatedEntries`, and annotation conformance lanes |
| Public surface | Validation, annotation, and output APIs for Draft 2019-09 and Draft 2020-12 schemas |
| Upstream record | Official fixtures and their checksums are pinned without exclusions. |
| Reconsider when | An erratum changes evaluated-location propagation or a new dialect replaces these keywords. |

## JSONSCHEMA-DEC-007: Dynamic and recursive references

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | Draft 2020-12 Core [`$dynamicRef` and `$dynamicAnchor`](https://json-schema.org/draft/2020-12/json-schema-core.html#section-8.2.3.2), Draft 2019-09 Core [`$recursiveRef` and `$recursiveAnchor`](https://json-schema.org/draft/2019-09/json-schema-core.html#section-8.2.4), and the 2020-12 [release notes](https://json-schema.org/draft/2020-12/release-notes#dynamicref-and-dynamicanchor) |
| Classification | Normative dialect-specific reference interpretation |
| Issue | Dynamic scope crosses resource boundaries and differs between 2019-09 recursive references and 2020-12 named dynamic anchors. Treating them as ordinary references or leaking anchor semantics across dialects is observably wrong. |
| Credible interpretations | Resolve only the static target; search every enclosing resource; search only resources entered through reference application; or approximate both dialects with one recursive algorithm. |
| Known peer behavior | Bowtie and the official suite expose recursive and dynamic reference lanes because implementations historically disagree at scope boundaries. |
| Selected behavior | Resolve the static reference first, then apply the active dialect's dynamic-scope rules over reference-entered resources. Draft 2019-09 supports its root recursive anchor model; Draft 2020-12 supports named dynamic anchors. Those keywords remain unknown outside their defining dialects. Cycles are bounded and valid recursive schemas remain usable. |
| Security and resource consequences | Reference depth, resource count, operation count, and cancellation are enforced across static and dynamic resolution. Malformed or unresolved targets fail without unbounded recursion. |
| Compatibility and wire consequences | Dynamic scope affects validity and absolute output locations. Dialect isolation prevents the same keyword spelling from silently changing older schemas. |
| Executable evidence | `TestOfficialDynamicReferenceFixtures`, `TestBasicOutputPreservesDynamicReferenceEvaluationPath`, `TestDynamicAnchorCompilationContinuesToMatchingAnchor`, `TestReferenceDepthIsRestoredOnSuccessAndFailure`, and `TestAnchorKeywordsDoNotLeakIntoEarlierDialects` |
| Public surface | Compiler reference resolution and all validation/output methods |
| Upstream record | Pinned `recursiveRef.json` and `dynamicRef.json` fixtures cover both released models. |
| Reconsider when | Standards errata change dynamic-scope traversal or a new dialect introduces another reference model. |

## JSONSCHEMA-DEC-008: `$ref` siblings

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | Draft 7 Core [`$ref`](https://json-schema.org/draft-07/json-schema-core.html#rfc.section.8.3), Draft 2019-09 Core [`$ref`](https://json-schema.org/draft/2019-09/json-schema-core.html#section-8.2.4.1), and Draft 2020-12 Core [`$ref`](https://json-schema.org/draft/2020-12/json-schema-core.html#section-8.2.3.1) |
| Classification | Normative version-specific behavior |
| Issue | Drafts 3 through 7 define `$ref` as replacing the schema object, while Draft 2019-09 and later define it as an applicator whose siblings remain active. Applying one rule to every dialect silently changes validation. |
| Credible interpretations | Always ignore siblings; always evaluate siblings; or use the active dialect's reference model. |
| Known peer behavior | Validators that primarily target one dialect often expose one behavior globally. The official cross-draft reference fixtures are the pinned comparison surface. |
| Selected behavior | Drafts 3, 4, 6, and 7 ignore all `$ref` siblings, including sibling identifiers. Draft 2019-09 and 2020-12 evaluate `$ref` and its applicable siblings, with annotations and output preserved from both. |
| Security and resource consequences | Ignored historical siblings cannot trigger loaders, callbacks, or additional validation. Modern siblings remain subject to all normal limits. |
| Compatibility and wire consequences | Moving a schema between Draft 7 and Draft 2019-09 can activate sibling constraints and change output. This difference is explicitly documented in the dialect matrix. |
| Executable evidence | `TestReplacingReferenceIgnoresSiblingIdentifier`, `TestOfficialReferenceAndDefinitionFixtures`, `TestVerboseReferenceTraversalContinuesWithSiblingKeywords`, and cross-draft official fixtures |
| Public surface | `WithDialect`, compilation, validation, annotations, and output |
| Upstream record | The behavior change is represented separately in each pinned dialect lane. |
| Reconsider when | A supported historical-draft erratum changes replacement semantics or a new dialect changes reference application again. |

## JSONSCHEMA-DEC-009: Duplicate JSON object members

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | [RFC 8259 section 4](https://www.rfc-editor.org/rfc/rfc8259.html#section-4) and JSON Schema Core's JSON data model requirements |
| Classification | Defensive interoperability policy |
| Issue | RFC 8259 says object member names SHOULD be unique and documents unpredictable peer behavior when they are not. Last-wins parsing can hide schema constraints or instance data before validation. |
| Credible interpretations | Preserve the first value; preserve the last value; retain duplicates in an extended data model; or reject the document as ambiguous. |
| Known peer behavior | RFC 8259 explicitly records that libraries differ: some keep the last member, some report an error, and some expose all members. |
| Selected behavior | Raw schemas and instances with any duplicate object member are rejected as `ErrInvalidJSON`. `ValidateValue` operates on already-decoded Go values and therefore cannot recover duplicates discarded by an upstream decoder. |
| Security and resource consequences | Rejection prevents parser differentials, hidden constraints, signature confusion, and last-wins authorization mistakes. Duplicate detection is bounded by object and input limits. |
| Compatibility and wire consequences | Ambiguous JSON accepted by permissive decoders is rejected. Callers that need this guarantee must pass raw JSON rather than a pre-decoded map. |
| Executable evidence | `TestCompileRejectsInvalidAndAmbiguousJSON`, including the `duplicate member` case, exact parser tests, hostile-input fuzzing, and `docs/security.md` |
| Public surface | `Compile`, `Validate`, `ValidateOutput`, `CollectAnnotations`, and raw JSON parsing errors |
| Upstream record | This is a deliberate fail-closed policy over RFC 8259's interoperability warning, not a claim that JSON Schema mandates rejection. |
| Reconsider when | The underlying JSON standard mandates one duplicate-member behavior or a separate lossless duplicate-aware API is introduced. |

## JSONSCHEMA-DEC-010: Numeric precision and equality

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | Draft 2020-12 Validation [section 4.2](https://json-schema.org/draft/2020-12/json-schema-validation.html#section-4.2), Draft 2020-12 Core [section 4.2](https://json-schema.org/draft/2020-12/json-schema-core.html#section-4.2), and historical integer definitions |
| Classification | Normative data-model interpretation with dialect-specific compatibility |
| Issue | JSON allows arbitrary-precision numbers, Go `float64` does not, and integer interpretation changed from lexical-era behavior in Draft 3/4 to mathematical integer semantics in Draft 6 and later. Equality also affects `enum`, `const`, and `uniqueItems`. |
| Credible interpretations | Decode through binary floating point; impose implementation numeric bounds; preserve exact decimal text; or use exact rational comparison while retaining historical dialect integer rules. |
| Known peer behavior | The official optional `bignum`, `float-overflow`, and `zeroTerminatedFloats` fixtures exist because host-language numeric models differ. |
| Selected behavior | Raw JSON numbers retain exact decimal and exponent semantics without binary floating-point conversion. Numeric constraints and equality use exact arithmetic. Draft 3/4 integer checks retain their historical lexical behavior; Draft 6 and later use mathematical integers. `ValidateValue` preserves `json.Number` text and rejects unsupported lossy or non-finite values. |
| Security and resource consequences | Number text length and arithmetic work are bounded before allocation and comparison. Huge exponents cannot force unbounded memory. |
| Compatibility and wire consequences | Numerically equal spellings compare equal where the dialect defines mathematical equality. Historical integer differences remain dialect-specific rather than normalized away. |
| Executable evidence | `TestValidateUsesExactNumberSemantics`, `TestExactNumberComparisonSignEdges`, `TestOfficialNumericFixtures`, `TestOfficialOptionalCoreFixtures`, and official big-number and float-overflow cases |
| Public surface | `Compile`, `Validate`, `ValidateValue`, `Value`, equality-dependent keywords, and output annotations |
| Upstream record | Numeric optional fixtures are pinned and counted rather than conditionally skipped by host range. |
| Reconsider when | A supported dialect changes its numeric data model or Go exposes a new exact JSON numeric representation adopted by the public API. |

## JSONSCHEMA-DEC-011: Regular-expression semantics

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | Draft 2020-12 Core [section 6.4](https://json-schema.org/draft/2020-12/json-schema-core.html#section-6.4), Draft 2020-12 Validation [`pattern`](https://json-schema.org/draft/2020-12/json-schema-validation.html#section-6.3.3), and [ECMA-262 regular expressions](https://tc39.es/ecma262/multipage/text-processing.html#sec-regexp-regular-expression-objects) |
| Classification | Normative interoperability interpretation and defensive resource policy |
| Issue | Go's standard regular-expression engine intentionally omits ECMAScript lookaround and backreferences, while backtracking ECMAScript engines can consume unbounded time. Unicode expectations also changed across drafts. |
| Credible interpretations | Use Go regexp as an approximation; reject unsupported valid patterns; use an ECMAScript-compatible engine without limits; or provide bounded ECMAScript semantics. |
| Known peer behavior | The official optional regex and `ecmascript-regex` fixtures capture common engine differences. Peer engines also vary in Unicode and timeout behavior. |
| Selected behavior | Schema patterns and asserted `regex` formats use ECMAScript-compatible semantics, including lookaround and backreferences, without implicit anchoring. Compilation bytes, match input, backtracking duration, operations, and cancellation are bounded. Invalid active patterns fail schema compilation. |
| Security and resource consequences | Explicit byte and time limits contain catastrophic backtracking and oversized expressions. Deadline exhaustion is a typed limit error, not an invalid-instance result. |
| Compatibility and wire consequences | Valid ECMAScript patterns unsupported by Go regexp remain usable. Tightening a default regex budget is an operational compatibility change. |
| Executable evidence | `TestPatternUsesECMAScriptLookaroundAndBackreferences`, `TestPatternBacktrackingIsBounded`, `TestOfficialPatternFixtures`, `TestOfficialOptionalRegexFixtures`, `TestRegexFormatCompilationUsesConfiguredByteLimit`, and regex fuzzing |
| Public surface | Pattern-bearing keywords, asserted `regex` format, and `Limits` |
| Upstream record | Optional ECMAScript fixtures are pinned with no regex-file exclusions. |
| Reconsider when | ECMA-262 semantics, a supported dialect's regex requirements, or the selected bounded engine materially change. |

## JSONSCHEMA-DEC-012: URI identity normalization

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | Draft 2020-12 Core [sections 8.2 and 9](https://json-schema.org/draft/2020-12/json-schema-core.html#section-8.2), [RFC 3986 section 6.2.2](https://www.rfc-editor.org/rfc/rfc3986.html#section-6.2.2), and [RFC 3986 section 5](https://www.rfc-editor.org/rfc/rfc3986.html#section-5) |
| Classification | Normative reference resolution plus defensive identity policy |
| Issue | Equivalent URI spellings can otherwise create multiple resource, cache, or loader identities. Over-normalization can also change non-equivalent paths or escaped data. |
| Credible interpretations | Compare identifiers byte-for-byte; apply all syntax-based RFC normalization; normalize only scheme and host; or delegate identity entirely to loaders. |
| Known peer behavior | URI libraries and schema validators vary in normalization. Official reference fixtures cover resolution but do not exhaust equivalent identifier aliases. |
| Selected behavior | Resolve references per RFC 3986 and normalize identity through scheme and host case, default ports, dot segments, and percent-encoded unreserved characters. Preserve reserved escapes and other distinctions. Reject duplicate resources or loader keys that normalize to one identity. Custom loaders receive the normalized identifier. |
| Security and resource consequences | Canonical identity prevents alias cache bypass and ambiguous resource replacement. URI, resource, and reference-depth limits apply before indexing or loading. |
| Compatibility and wire consequences | A custom loader keyed by non-normalized spelling must index the normalized identifier. Equivalent duplicate `MapLoader` keys now fail instead of selecting by map order. |
| Executable evidence | `TestNormalizeURLAppliesRFCIdentityRules`, `TestCompileRejectsEquivalentDuplicateResourceIdentifiers`, `TestMapLoaderUsesNormalizedResourceIdentity`, `TestRemoveDotSegmentsPreservesURIPathStructure`, and reference fixtures |
| Public surface | `$id`, references, `NewMapLoader`, `NewFSLoader`, and `ResourceLoader` |
| Upstream record | The pre-v1 migration consequence is recorded in `docs/versioning.md`. |
| Reconsider when | JSON Schema or RFC 3986 errata change identity equivalence or an explicit caller-selectable normalization policy is introduced. |

## JSONSCHEMA-DEC-013: Remote resource loading

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | Draft 2020-12 Core [section 9.4](https://json-schema.org/draft/2020-12/json-schema-core.html#section-9.4), Core security considerations, and the official remote-reference corpus |
| Classification | Defensive retrieval policy and explicit application boundary |
| Issue | References identify resources but do not require a validator to perform unrestricted network retrieval. Implicit HTTP creates SSRF, credential, redirect, cache, availability, and reproducibility risks. |
| Credible interpretations | Fetch every HTTP(S) URI automatically; reject all non-local references; bundle a configurable HTTP client; or require an explicit caller-owned loader. |
| Known peer behavior | Validators differ between implicit HTTP, caller registries, and disabled remote loading. Bowtie supplies an explicit per-case registry, avoiding dependence on a peer's network policy. |
| Selected behavior | The core performs no implicit network, filesystem, or environment access. Only pinned official meta-schemas resolve internally. Every other unavailable resource returns `ErrResourceUnavailable` unless the caller supplies `WithResourceLoader`. Built-in map, filesystem, and composite loaders remain explicit and bounded; no generic HTTP loader is included. |
| Security and resource consequences | Applications own allowlists, DNS, proxies, redirects, TLS, credentials, timeouts, caching, and response limits. Package errors redact URI credentials, query, fragment, callback panic values, and callback error text. |
| Compatibility and wire consequences | Schemas relying on ambient network access do not compile without an adapter. Loader errors remain inspectable through `errors.Is` and `errors.As` while diagnostics stay redacted. |
| Executable evidence | `TestOfficialRemoteReferenceFixtures`, `TestLoaderPanicsAreContainedAndRedacted`, `TestLoaderErrorsAreRedactedAndPreserved`, `TestFSLoaderConfinesResourcesToItsBase`, `TestCompositeLoaderFallsThroughOnlyForMissingResources`, and `TestResolutionErrorsRedactURISecrets` |
| Public surface | `WithResourceLoader`, `ResourceLoader`, `NewMapLoader`, `NewFSLoader`, and `NewCompositeLoader` |
| Upstream record | The official test registry and meta-schema bundle are imported by checksum-pinned tooling, never fetched implicitly during validation. |
| Reconsider when | A separately versioned secure retrieval adapter defines a complete transport policy without changing the core default. |

## JSONSCHEMA-DEC-014: Output formats

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | Draft 2020-12 Core [section 12](https://json-schema.org/draft/2020-12/json-schema-core.html#section-12), Draft 2019-09 Core [section 10](https://json-schema.org/draft/2019-09/json-schema-core.html#section-10), and the published output schemas and examples |
| Classification | Recommended output model extended as package policy across dialects |
| Issue | Modern drafts define Flag, Basic, Detailed, and Verbose output guidance, while historical dialects do not. Output ordering, condensation, absolute locations, repeated evaluation, and retained annotations require stable package choices. |
| Credible interpretations | Expose output only for modern dialects; return one implementation-specific error tree; map every dialect into the modern location model; or expose only Boolean validity. |
| Known peer behavior | Validator diagnostics differ substantially. Official Basic output fixtures constrain modern output but do not define every historical-dialect diagnostic or deterministic ordering choice. |
| Selected behavior | Expose Flag, Basic, Detailed, and Verbose for every supported dialect using the modern location model. Flag contains only validity; Basic is flat; Detailed is condensed hierarchical output; Verbose is uncondensed. Locations use JSON Pointers and canonical absolute resource identifiers. Ordering is deterministic. Output generation may repeat bounded pure callback evaluation. |
| Security and resource consequences | Output units, annotation bytes, repeated operations, depth, callback work, and cancellation are bounded. Limit exhaustion is an error rather than a truncated success. |
| Compatibility and wire consequences | Output structure and deterministic ordering are public contracts, including for historical dialects as an explicit package extension. `CollectAnnotations` is not a standard output format. |
| Executable evidence | `TestOfficialBasicOutputFixtures`, `TestBasicOutputPreservesReferenceEvaluationPath`, `TestVerboseOutputIncludesEveryEvaluatedKeyword`, `TestVerboseOutputRetainsAnnotationResults`, `TestOutputBoundaryHelpersAreExact`, and output fuzzing |
| Public surface | `OutputFormat`, `OutputUnit`, `ValidateOutput`, and `ValidateValueOutput` |
| Upstream record | Official output artifacts are pinned with the supported meta-schema bundle; package extensions are labeled in `docs/output.md`. |
| Reconsider when | A released output specification changes field semantics or adds a format that cannot be represented compatibly. |

## JSONSCHEMA-DEC-015: Optional official-suite behavior

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `json-schema` maintainers |
| Source | The pinned [JSON Schema Test Suite](https://github.com/json-schema-org/JSON-Schema-Test-Suite/tree/c0b038ad7244712cf73650f44e90d0bc5704e8c7), its README and per-dialect `optional` trees, plus the normative sources named by each fixture family |
| Classification | Conformance and release policy |
| Issue | The upstream suite labels implementation-dependent, format, content, regex, big-number, cross-draft, and other cases optional. Silently skipping unsupported optional files can conceal material interoperability gaps while passing the mandatory suite. |
| Credible interpretations | Exclude all optional fixtures; select only convenient files; run them under one global policy; or inventory every released-dialect optional file and apply explicit dialect-appropriate options. |
| Known peer behavior | Optional support intentionally differs across implementations. Bowtie can compare portable validation outcomes, while package-specific options and outputs require local evidence. |
| Selected behavior | Discover and execute every mandatory and optional fixture for all six released dialects with no skip allowlist. Activate format or content assertion only in the explicit lanes that test those policies. Record every file, group, case, checksum, pass, skip, and failure in a generated manifest. Optional does not mean unverified. |
| Security and resource consequences | Fixture provenance and checksums prevent silent corpus reduction or replacement. Remote fixtures use an allowlisted local registry and pinned meta-schema bundle. |
| Compatibility and wire consequences | A newly added or changed optional fixture can reveal a compatibility issue but cannot silently change behavior; the pin update requires review and a changelog entry when observable behavior changes. |
| Executable evidence | `TestOfficialMandatoryFixtures`, `TestOfficialOptionalFixtures`, `TestOfficialOptionalCoreFixtures`, `TestOfficialOptionalCoreFormatFixtures`, `TestOfficialOptionalRegexFixtures`, `scripts/check-conformance-manifest.sh`, and `make provenance` |
| Public surface | Conformance claims, release gates, documented format/content options, and Bowtie harness metadata |
| Upstream record | Suite revision `c0b038ad7244712cf73650f44e90d0bc5704e8c7`, archive digest, per-file checksums, and generated results are pinned under `specification/`. |
| Reconsider when | The official suite changes its optional taxonomy, a new released dialect is added, or a fixture conflicts with normative text and is classified upstream. |

## Unresolved decisions

No known material specification decision is unresolved for the currently
supported dialects and public surface. A newly discovered contradiction,
erratum, unsupported required vocabulary, or peer disagreement that cannot be
classified against the governing specification MUST be added here and blocks
the affected compliance or release claim until resolved.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
