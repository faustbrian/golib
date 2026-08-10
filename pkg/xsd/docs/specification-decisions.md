# XML Schema specification decisions

This register records observable choices where XML Schema 1.0 Second Edition,
its errata, XSTS, XML parsing, or the Go host environment leave more than one
credible implementation. Normative Structures and Datatypes prose outranks
the primer, schema-for-schemas, examples, XSTS metadata, and peer behavior.
Exact source versions and digests are pinned in
[`specification/manifest.tsv`](../specification/manifest.tsv).

Statuses are `resolved`, `unresolved`, or `superseded`. Resolved decisions are
part of the compatibility contract and require specification, security,
resource, wire, executable-evidence, and changelog review when changed.

## XSD-DEC-001: Stable scope is XML Schema 1.0 Second Edition

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `xsd` maintainers |
| Source | W3C XML Schema [Structures](https://www.w3.org/TR/2004/REC-xmlschema-1-20041028/), [Datatypes](https://www.w3.org/TR/2004/REC-xmlschema-2-20041028/), and the pinned [Second Edition errata](https://www.w3.org/2004/03/xmlschema-errata.html) |
| Classification | Version-selection and feature-line policy |
| Issue | XML Schema 1.1 adds assertions, alternatives, open content, and datatype changes that cannot be accepted partially without changing parsing, compilation, validation, and interoperability behavior. |
| Credible interpretations | Track the latest W3C schema vocabulary; accept recognizable 1.1 syntax as foreign markup; implement isolated 1.1 features under the 1.0 claim; or close the stable language to 1.0 Second Edition plus reviewed errata. |
| Known peer behavior | JAXP and other validators expose different 1.0 and 1.1 support levels. Peer acceptance therefore does not identify this package's selected feature line. |
| Selected behavior | Stable support is exactly XML Schema 1.0 Second Edition plus the pinned errata snapshot. XML Schema 1.1 syntax and semantics are rejected or retained only where 1.0 explicitly permits foreign annotation content; no partial 1.1 conformance is advertised. |
| Security and resource consequences | A closed grammar prevents unreviewed assertions or alternative-selection languages from introducing executable or amplified work. Existing parser, compiler, and validator limits cover the selected 1.0 model. |
| Compatibility and wire consequences | XML Schema 1.0 documents have a stable interpretation. A 1.1 document cannot silently downgrade over the wire and requires a future explicit feature-line API and migration review. |
| Executable evidence | `TestOfficialXSTS`, `TestParseRejectsUnknownSchemaComponents`, and `TestSchemaAttributesRejectInvalidLexicalValues` |
| Public surface | `Parse`, `Document`, `compile.Compiler`, `validate.Validator`, builders, and serializers |
| Upstream record | Exact W3C publications, errata snapshot, schema resources, and XSTS archive are pinned in the [manifest](../specification/manifest.tsv). |
| Reconsider when | XML Schema 1.1 is selected as a separately complete, tested, and documented feature line. |

## XSD-DEC-002: Parsing performs no external resolution

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `xsd` maintainers |
| Source | XML Schema Structures [schema document access](https://www.w3.org/TR/2004/REC-xmlschema-1-20041028/#schema-repr) and Go [`context.Context`](https://pkg.go.dev/context) |
| Classification | Host integration, ownership, and secure-default policy |
| Issue | Includes, imports, and redefines name external resources, but the specification does not define application trust, filesystem roots, network policy, credentials, redirects, or cancellation. Implicit parser I/O would embed unsafe deployment policy. |
| Credible interpretations | Resolve locations while parsing; allow file and HTTP URLs by default; ignore composition references; use a process-global catalog; or preserve references and require an injected bounded resolver during compilation. |
| Known peer behavior | Validators often expose parser-specific entity and schema-resolution callbacks, with defaults varying by runtime and deployment. Those defaults are not portable security policy. |
| Selected behavior | Parsing consumes only caller-supplied bytes and retains composition references. Compilation resolves them only through an injected `resolve.Resolver`; the default deny resolver permits no file or network access. The opt-in file resolver is root-confined and byte-bounded. |
| Security and resource consequences | Default parsing and compilation have no SSRF, credential forwarding, DNS, redirect, or path-traversal side effects. Injected resolvers remain caller-owned trust boundaries subject to graph, byte, depth, reference, and context limits. |
| Compatibility and wire consequences | Resource identities remain explicit URIs in the model; bytes are never fetched merely because a schema arrived over a wire. Callers must provide identical resolver inputs for reproducible compilation. |
| Executable evidence | `TestCompileUsesDenyResolverByDefault`, `TestFileResolverRejectsUnsafeAndOversizedResources`, `TestFileResolverRejectsSymlinkEscape`, and `TestResolversRejectInvalidConfigurationAndHonorCancellation` |
| Public surface | `Parse`, `compile.New`, `compile.Options.Resolver`, and the `resolve` package |
| Upstream record | Resolution requirements are mapped as `DOC-003`, `DOC-004`, and `RES-001` in `specification/requirements/xsd-1.0.tsv`. |
| Reconsider when | A new explicit resolver adapter is added; parsing itself remains I/O-free unless the public ownership contract is deliberately redesigned. |

## XSD-DEC-003: Conformance claims require matrix evidence

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `xsd` maintainers |
| Source | W3C [XML Schema Test Suite 2007-06-20](https://www.w3.org/XML/2004/xml-schema-test-suite/xmlschema2006-11-06/xsts-2007-06-20.tar.gz) and XML Schema 1.0 recommendations |
| Classification | Official-fixture applicability and feature-claim policy |
| Issue | A green unit suite or raw XSTS percentage does not prove every broad feature claim. XSTS also contains `queried` expectations that are not accepted pass/fail requirements. |
| Credible interpretations | Claim support from implementation presence; ignore XSTS; count queried expectations as passes or failures; use only aggregate percentages; or require each normative matrix row to name focused evidence and applicable official expectations. |
| Known peer behavior | Validators report XSTS with different harnesses, exclusions, and feature lines. Counts are comparable only when the archive, expectation policy, and denominator match. |
| Selected behavior | A feature is supported only when its versioned requirement row names the normative source and executable evidence covering the stated scope. Official fixtures supplement that mapping; a green unit suite or aggregate percentage cannot replace it. |
| Security and resource consequences | The harness confines resources to the extracted suite root and applies normal compiler and validator limits. A fixture cannot authorize network or filesystem access outside that root. |
| Compatibility and wire consequences | Conformance claims are attributable to exact schema and instance fixtures rather than an opaque percentage. A newly discovered semantic gap reopens its row instead of narrowing prose or accepting divergent wire behavior. |
| Executable evidence | `TestOfficialXSTS`, `TestRunExcludesQueriedExpectations`, and `TestRunReportsMetadataAndRequiredCaseFailures` |
| Public surface | Published XML Schema 1.0 support claims and all APIs covered by `specification/requirements/xsd-1.0.tsv` |
| Upstream record | Requirement status and evidence remain in `specification/requirements/xsd-1.0.tsv`; the measured corpus result is retained in [`xsts-baseline.md`](xsts-baseline.md). |
| Reconsider when | A requirement row, corpus pin, or accepted-expectation policy changes. |

## XSD-DEC-004: XML Schema 1.0 support matrix is complete

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `xsd` maintainers |
| Source | W3C [XML Schema Test Suite 2007-06-20](https://www.w3.org/XML/2004/xml-schema-test-suite/xmlschema2006-11-06/xsts-2007-06-20.tar.gz) and XML Schema 1.0 recommendations |
| Classification | Current support-baseline and release-gate decision |
| Issue | Completing a support matrix is a point-in-time evidence claim. Treating it as permanently complete would allow later regressions, fixture changes, or discovered semantic gaps to remain hidden behind historical status. |
| Credible interpretations | Freeze the completed labels; weaken broad claims when a test fails; exclude newly failing fixtures; treat the unit suite as sufficient; or reopen every affected row whenever its evidence no longer proves the requirement. |
| Known peer behavior | Validators publish support matrices with different update and regression policies. A historical peer claim does not prove current behavior. |
| Selected behavior | Every current XML Schema 1.0 support and release-quality row is implemented, all 24,696 accepted XSTS expectations pass without skips, and 90 upstream `queried` expectations remain separately reported. Any semantic gap or regression reopens its affected row and blocks the broad claim until fixed. |
| Security and resource consequences | Fail-closed reopening prevents security and resource regressions from being relabeled as unsupported edge cases. Official execution remains root-confined and bounded. |
| Compatibility and wire consequences | Accepted schema and instance wire behavior cannot be narrowed silently to preserve a green status. A changed support row requires explicit compatibility and changelog review. |
| Executable evidence | `TestOfficialXSTS`, `TestRunExcludesQueriedExpectations`, and `TestRunReportsMetadataAndRequiredCaseFailures` |
| Public surface | Complete XML Schema 1.0 support and release-readiness claims |
| Upstream record | Current row status is retained in `specification/requirements/xsd-1.0.tsv`; exact corpus counts are retained in [`xsts-baseline.md`](xsts-baseline.md). |
| Reconsider when | Any requirement evidence, accepted fixture, implementation behavior, or corpus pin changes. |

## XSD-DEC-005: Normative-source precedence and errata

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `xsd` maintainers |
| Source | W3C XML Schema [Structures](https://www.w3.org/TR/2004/REC-xmlschema-1-20041028/), [Datatypes](https://www.w3.org/TR/2004/REC-xmlschema-2-20041028/), [primer](https://www.w3.org/TR/2004/REC-xmlschema-0-20041028/), and [errata](https://www.w3.org/2004/03/xmlschema-errata.html) |
| Classification | Normative-source and contradiction-resolution policy |
| Issue | Prose, schema-for-schemas, primer examples, errata, XSTS expectations, and implementations can disagree. Treating all artifacts as equally normative creates circular or fixture-driven behavior. |
| Credible interpretations | Follow the schema-for-schemas mechanically; use XSTS as the specification; copy JAXP behavior; ignore errata; or apply normative prose plus accepted errata and classify every lower-authority disagreement. |
| Known peer behavior | Independent validators differ around errata and under-specified edge cases. A peer majority is evidence to investigate, not authority over W3C normative text. |
| Selected behavior | Structures and Datatypes normative prose, as corrected by the pinned Second Edition errata, controls. The schema-for-schemas constrains syntax only where the recommendation assigns that role. Primer, examples, XSTS, and peer output are evidence and cannot silently override prose. |
| Security and resource consequences | Explicit precedence prevents permissive fixtures or generated schemas from weakening validation. All accepted interpretations still execute under package resource limits. |
| Compatibility and wire consequences | Contradictions resolve deterministically and are documented before changing accepted schema or instance wire data. A changed erratum interpretation receives compatibility review. |
| Executable evidence | `TestOfficialXSTS`, `TestReferenceDifferentialCorpus`, and `TestCompileRejectsInvalidAndUnresolvedComponentContracts` |
| Public surface | All parsing, compilation, validation, datatype, and serialization behavior |
| Upstream record | Source role, version, status, digest, size, and URL are recorded independently in the [manifest](../specification/manifest.tsv). |
| Reconsider when | W3C publishes a superseding correction or a minimized fixture proves the selected reading conflicts with normative prose. |

## XSD-DEC-006: DTD and entity processing is forbidden

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `xsd` maintainers |
| Source | XML 1.0 Fifth Edition [document type definition](https://www.w3.org/TR/2008/REC-xml-20081126/#sec-prolog-dtd) and XML Schema Structures schema-document rules |
| Classification | Defensive XML parsing policy |
| Issue | XML permits DTD declarations and entities, but XML Schema processing does not require this package to expose entity expansion. Accepting them introduces external-resource, amplification, and parser-differential risks. |
| Credible interpretations | Delegate all XML behavior to the host decoder; allow internal entities only; expand with limits; ignore declarations; or reject every DTD before schema or instance processing. |
| Known peer behavior | XML parsers and schema validators differ in entity defaults and hardening flags. Secure deployment guidance commonly disables DTDs, but defaults remain runtime-specific. |
| Selected behavior | Schema parsing and instance validation reject any DTD directive before component or instance processing and never expand external entities. This is a deliberate secure profile layered on XML Schema 1.0. |
| Security and resource consequences | Rejection removes XXE, external fetch, and entity-expansion amplification from owned parsing paths. Input bytes, element depth, nodes, text, and diagnostics remain independently bounded. |
| Compatibility and wire consequences | Schema or instance wire documents requiring DTD entities are rejected rather than normalized. Callers must resolve trusted entities before this boundary if their application deliberately supports them. |
| Executable evidence | `TestParseRejectsDTDWithoutResolvingEntities` and `TestValidateRejectsDTDBeforeInstanceProcessing` |
| Public surface | `Parse`, `validate.Validator`, byte, reader, and tree validation entry points |
| Upstream record | The secure profile is mapped as `SEC-001` and `SEC-002` in the requirement matrix. |
| Reconsider when | No implicit DTD mode is planned; any future opt-in would require a separate threat model and feature-line contract. |

## XSD-DEC-007: Expanded names own identity

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `xsd` maintainers |
| Source | XML Namespaces 1.0 Third Edition [expanded names](https://www.w3.org/TR/2009/REC-xml-names-20091208/#dt-expname) and XML Schema Structures [schema composition](https://www.w3.org/TR/2004/REC-xmlschema-1-20041028/#composition-schemaImport) |
| Classification | Namespace identity, chameleon include, and serialization policy |
| Issue | Prefixes are lexical aliases, while component identity uses namespace URI plus local name. Includes without a target namespace acquire the including schema's namespace, and serialization may need to allocate new prefixes. |
| Credible interpretations | Treat prefixes as identity; preserve only lexical QName text; reject chameleon includes; use one process-global prefix map; or resolve expanded names in scope and allocate deterministic serialization prefixes. |
| Known peer behavior | Validators agree on expanded-name identity but may preserve or regenerate prefixes differently. Byte-identical prefix output is not a normative validation requirement. |
| Selected behavior | Parsing resolves QName-valued attributes against their local namespace scope. Compilation keys components by expanded name and applies chameleon namespaces to included no-namespace schemas. Serialization preserves semantic expanded names and allocates deterministic prefixes when needed. |
| Security and resource consequences | Namespace and component counts are bounded, unknown prefixes fail closed, and no prefix text can shadow a different namespace identity. Serialization work and output remain bounded. |
| Compatibility and wire consequences | Different valid prefixes compile to the same component identity. Serialized bytes may use deterministic package-selected prefixes while preserving equivalent namespace wire semantics. |
| Executable evidence | `TestCompileResolvesCyclesOnceAndAppliesChameleonNamespace`, `TestParseResolvesQNameAttributesInTheirNamespaceScope`, `TestQNameValueNamespaceScopeSurvivesSerialization`, and `TestMarshalAllocatesPrefixesForProgrammaticQNames` |
| Public surface | `QName`, parsed `Document`, `compile.Set`, component lookup, builders, and serialization |
| Upstream record | Namespace, composition, and chameleon obligations are mapped under `DOC-004`, `COMP-002`, and `COMP-003`. |
| Reconsider when | A lexical-preservation mode is introduced; semantic identity remains expanded-name based. |

## XSD-DEC-008: XML Schema regular expressions are a distinct dialect

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `xsd` maintainers |
| Source | XML Schema Datatypes [regular expressions](https://www.w3.org/TR/2004/REC-xmlschema-2-20041028/#regexs) |
| Classification | Normative datatype-dialect and translation policy |
| Issue | XML Schema regexes are implicitly unanchored in their own grammar and differ from Go RE2, PCRE, and ECMAScript in subtraction, escapes, character classes, and matching semantics. Direct host-regex compilation changes accepted values. |
| Credible interpretations | Pass patterns directly to Go `regexp`; use the ECMAScript package; support a convenient subset silently; or parse and translate the exact XML Schema 1.0 dialect with bounded expansion and explicit rejection. |
| Known peer behavior | General-purpose regex engines expose different syntax and anchoring. XML Schema validators implement a dedicated dialect or translation layer. |
| Selected behavior | Pattern facets use the XML Schema 1.0 regex grammar and matching semantics. Translation to the execution engine is bounded, class subtraction and escapes retain XSD meaning, derived pattern facets remain cumulative, and invalid expressions fail compilation. |
| Security and resource consequences | Translation width, range expansion, pattern size, and execution work are bounded before amplification. Unsupported or malformed syntax fails closed rather than reaching a permissive host engine. |
| Compatibility and wire consequences | Pattern facet wire text is interpreted as XSD regex, never Go, PCRE, or ECMAScript. Equivalent host-engine strings are not a compatibility guarantee. |
| Executable evidence | `TestCompilePatternUsesXMLSchemaSemantics`, `TestCompilePatternBoundsTranslationWork`, `TestCompilePatternRejectsInvalidExpressions`, and `TestValidatePatternUsesXMLSchemaMatchingSemantics` |
| Public surface | Pattern facets, datatype compilation, schema compilation, and instance validation |
| Upstream record | Pattern support is mapped under `TYPE-002` in the requirement matrix. |
| Reconsider when | XML Schema 1.1 is implemented or the execution backend changes; the public dialect remains versioned XSD syntax. |

## XSD-DEC-009: Identity constraints use only the XSD XPath subset

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `xsd` maintainers |
| Source | XML Schema Structures [identity-constraint XPath subset](https://www.w3.org/TR/2004/REC-xmlschema-1-20041028/#coss-identity-constraint) |
| Classification | Normative restricted-language and evaluation policy |
| Issue | `xs:selector` and `xs:field` use a restricted XPath grammar, not general XPath. Delegating to a full engine accepts functions, axes, and expressions the schema language forbids and can add unbounded work. |
| Credible interpretations | Run XPath 1.0; accept any host-engine expression; implement string matching heuristics; or parse only the specification subset and estimate every evaluation step. |
| Known peer behavior | Schema validators implement the restricted selector and field grammar, though diagnostics and extension behavior differ. General XPath libraries intentionally accept much more. |
| Selected behavior | The compiler accepts exactly the XML Schema 1.0 selector and field subset, including namespace-qualified names and permitted descendant forms. Validation evaluates that compiled subset with explicit node, identity-value, step, and diagnostic limits. |
| Security and resource consequences | No XPath functions, arbitrary axes, extension callbacks, or implicit I/O execute. Step accounting bounds amplification across large trees and key tables. |
| Compatibility and wire consequences | Unsupported general XPath wire expressions are schema errors rather than implementation extensions. Valid selectors compare expanded names using in-scope namespaces. |
| Executable evidence | `TestIdentityXPathGrammarDecisionTables`, `TestValidateIdentityPrefixWildcardsAndDescendantFields`, and `TestValidateIdentityConstraintsStopsAtEveryLimitBoundary` |
| Public surface | Parsed identity constraints, compiler plans, and validation of `unique`, `key`, and `keyref` |
| Upstream record | Identity requirements are mapped as `IDENT-001` and `IDENT-002`. |
| Reconsider when | A future XML Schema feature line defines a different selector language. |

## XSD-DEC-010: Datatype comparisons use value spaces

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `xsd` maintainers |
| Source | XML Schema Datatypes [value spaces and lexical representations](https://www.w3.org/TR/2004/REC-xmlschema-2-20041028/#value-space) |
| Classification | Normative datatype normalization and equality policy |
| Issue | Lexically different values can be equal after whitespace, numeric, QName, calendar, duration, list, union, or derived-type processing. Byte-string equality gives incorrect fixed values, identity constraints, facets, and canonical forms. |
| Credible interpretations | Compare source text; normalize all values as strings; use host floating-point and time types; or parse each built-in and derived datatype into its XML Schema value space before equality and ordering. |
| Known peer behavior | Conforming validators compare value spaces, but host numeric, calendar, timezone, and QName representations can create implementation-specific edge cases. |
| Selected behavior | Whitespace applies before other facets, arbitrary-precision decimal and integer values remain exact, QName values include namespace context, list and union members retain selected types, and fixed values and identity keys compare in the declared value space. Partial calendar ordering remains explicit rather than coerced to host time. |
| Security and resource consequences | Lexical bytes, numeric digits, list members, union candidates, and comparison work are bounded. Exact arithmetic avoids overflow and lossy security decisions. |
| Compatibility and wire consequences | Canonically different XML lexical forms may validate as equal; namespace context can make identical QName text unequal. Diagnostics preserve source locations without redefining wire values. |
| Executable evidence | `TestCompileComparesValueConstraintsInTheDeclaredValueSpace`, `TestValidateAppliesDerivedWhitespaceBeforeOtherFacets`, `TestValidateFixedConstraintsUseBuiltInValueSpaces`, and `TestCalendarComparisonUsesXMLSchemaValueSpace` |
| Public surface | Datatype parsing, facets, fixed/default constraints, identity constraints, and validation diagnostics |
| Upstream record | Datatype obligations are mapped under `TYPE-001` through `TYPE-003`. |
| Reconsider when | A datatype defect, erratum, or future feature line changes value-space semantics. |

## XSD-DEC-011: Deterministic serialization preserves schema semantics

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `xsd` maintainers |
| Source | XML Schema Structures [XML representation of schema components](https://www.w3.org/TR/2004/REC-xmlschema-1-20041028/#schema-repr) and XML Namespaces expanded-name rules |
| Classification | Package-defined deterministic wire profile |
| Issue | XML Schema defines semantic XML, not one byte serialization. Namespace declaration order, generated prefixes, attribute order, annotation markup, and empty values can vary or be lost during model round trips. |
| Credible interpretations | Preserve original bytes; emit host encoder output; discard annotation markup; claim canonical XML; or define a deterministic package profile that preserves represented semantics without claiming W3C canonicalization. |
| Known peer behavior | Validators commonly regenerate semantically equivalent schemas with different prefixes and formatting. Byte identity is not required for schema validity. |
| Selected behavior | Serialization preflights the complete model, emits deterministic UTF-8 XML, sorts namespace declarations, allocates stable prefixes, preserves empty constraints, IDs, annotations, and supported component families, and rejects unknown QName namespaces or cyclic models. It is deterministic package output, not Canonical XML. |
| Security and resource consequences | Preflight bounds depth, components, retained memory, and output bytes before partial writes. Unsafe annotation fragments, cycles, unknown namespaces, and write failures fail explicitly. |
| Compatibility and wire consequences | Parse-marshal round trips preserve represented schema semantics but not original whitespace or prefix spelling. Repeated serialization of the same model produces stable package bytes. |
| Executable evidence | `TestMarshalIsDeterministicAndRoundTripsSchemaComponents`, `TestQNameValueNamespaceScopeSurvivesSerialization`, `TestParseAndMarshalPreserveComponentAnnotations`, and `TestMarshalWithOptionsEnforcesResourceLimits` |
| Public surface | `Marshal`, `MarshalWithOptions`, builders, `Document`, and annotation models |
| Upstream record | Serialization policy is mapped as `SER-001` and `SER-002`. |
| Reconsider when | A separate lexical-preserving or standards-defined canonicalization mode is introduced. |

## XSD-DEC-012: Finite limits and structured diagnostics are observable policy

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `xsd` maintainers |
| Source | XML Schema Structures [schema assessment](https://www.w3.org/TR/2004/REC-xmlschema-1-20041028/#validation_outcome) and Go [`context.Context`](https://pkg.go.dev/context) |
| Classification | Defensive resource, cancellation, and diagnostic policy |
| Issue | The specifications define validity but not finite Go process budgets, cancellation granularity, diagnostic caps, or ownership. Recursive schemas, particles, XPath selectors, and invalid instances can amplify work and errors. |
| Credible interpretations | Apply no limits; return only the first error; collect every diagnostic; use hidden worker goroutines for deadlines; or expose finite limits and deterministic typed failures at every stage. |
| Known peer behavior | Validators apply implementation-specific recursion, memory, and error-count limits. Their thresholds and diagnostic wording are not normative. |
| Selected behavior | Parse, compile, validate, serialize, resolver, regex, identity, and diagnostic work is bounded by explicit options with conservative defaults. Context cancellation is checked synchronously. Validation distinguishes structured invalidity diagnostics from parser, resource, cancellation, and I/O errors. |
| Security and resource consequences | Every untrusted amplification dimension has checked finite accounting; diagnostic growth cannot consume unbounded memory. No hidden worker is required for owned cancellation. |
| Compatibility and wire consequences | A valid schema or instance may return a resource error under a selected policy even when an unconstrained validator would finish. Remote callers requiring identical outcomes must carry equivalent limit configuration explicitly. |
| Executable evidence | `TestValidateReaderEnforcesInputBoundaries`, `TestMarshalWithOptionsEnforcesResourceLimits`, `TestCompileBoundsSchemaCount`, and `TestValidateIdentityConstraintsStopsAtEveryDiagnosticBoundary` |
| Public surface | All options and limits, typed errors, `Result.Diagnostics`, resolvers, parsers, compilers, validators, and serializers |
| Upstream record | Defensive requirements are mapped as `SEC-003`, `SEC-004`, `VAL-003`, and `SER-002`. |
| Reconsider when | A stage gains new recursive, allocating, I/O, or diagnostic behavior. |

## XSD-DEC-013: Byte, reader, and tree validation share one semantic engine

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `xsd` maintainers |
| Source | XML Schema Structures [schema-validity assessment](https://www.w3.org/TR/2004/REC-xmlschema-1-20041028/#validation_outcome) and Go [`io.Reader`](https://pkg.go.dev/io#Reader) ownership contract |
| Classification | Host API parity and ownership policy |
| Issue | Separate byte, streaming-reader, and caller-owned tree validators can diverge in namespace context, DTD rejection, limits, diagnostics, or mutation ownership. |
| Credible interpretations | Maintain independent implementations; normalize every input to bytes; mutate caller trees for annotations; give streaming weaker limits; or route all forms through one validation plan with representation-specific bounded ingestion. |
| Known peer behavior | Validation libraries expose different combinations of DOM and streaming APIs, often with distinct diagnostics and ownership rules. The specification does not choose a Go representation. |
| Selected behavior | Byte, incremental reader, and caller-owned tree entry points share the same compiled validation engine and semantic outcomes. Each path enforces owned input bounds, rejects DTDs, preserves namespace context, does not mutate caller trees, and returns the same structured diagnostic model. |
| Security and resource consequences | Reader bytes, tree nodes, attributes, text, namespace data, and semantic work are bounded before retention or traversal. Caller resources are not closed or mutated implicitly. |
| Compatibility and wire consequences | Equivalent XML information produces equivalent validation regardless of entry point. Reader chunking and in-memory tree layout do not become wire-visible semantics. |
| Executable evidence | `TestValidateReaderMatchesByteValidation`, `TestValidateTreeMatchesByteValidation`, `TestValidateTreeEnforcesOwnedInputBounds`, and `TestReferenceDifferentialCorpus` |
| Public surface | `Validate`, `ValidateReader`, `ValidateTree`, validation options, results, and diagnostics |
| Upstream record | Validation-mode parity is mapped as `VAL-001`, `VAL-002`, and `QUAL-004`. |
| Reconsider when | A new input representation or truly incremental output API is added. |

## Unresolved decisions

None. New errata conflicts, XSTS applicability disputes, XML Schema feature
lines, resolver policies, datatype ambiguities, or serialization profiles MUST
be registered before observable behavior is selected. An unresolved
validation, security, resource, or wire decision blocks a release claim for
the affected surface.
