# WSDL specification decisions

This register records observable choices where WSDL 1.1, WSDL 2.0, their
schemas and adjuncts, XML processing, or the Go host environment leave more
than one credible implementation. WSDL 1.1 and WSDL 2.0 are separate feature
lines; accepting syntax from one never silently changes the other.

Normative dated W3C prose controls over published schemas, primers, examples,
fixtures, and peer implementations. Exact source bytes and digests are pinned
in [`specification/manifest.tsv`](../specification/manifest.tsv). Statuses are
`resolved`, `unresolved`, or `superseded`. Resolved decisions are compatibility
contracts and require specification, security, resource, wire, evidence, and
changelog review when changed.

## WSDL11-DEC-001: The 15 March 2001 Note is the WSDL 1.1 baseline

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | W3C [Web Services Description Language 1.1 Note, 15 March 2001](https://www.w3.org/TR/2001/NOTE-wsdl-20010315.html), including sections 1 through 5 |
| Classification | Version-selection and normative-source policy |
| Issue | WSDL 1.1 is a W3C Note rather than a Recommendation, its namespace schemas are mutable namespace resources, and later SOAP 1.2 material does not redefine the core language. |
| Credible interpretations | Follow mutable namespace schemas; treat later WSDL versions as corrections; implement only schema-accepted syntax; or close support to the dated Note and separately identified adjunct schemas. |
| Known peer behavior | SoapUI, `dotnet-svcutil`, and Apache CXF consume WSDL 1.1 but differ in extensions and semantic validation; peer acceptance does not select the package baseline. |
| Selected behavior | The dated 15 March 2001 Note and reviewed schema snapshots define WSDL 1.1. SOAP 1.2 binding support is an explicit adjunct and does not alter core 1.1 semantics. |
| Security and resource consequences | A closed version prevents later or vendor syntax from silently expanding parser work; normal XML and component limits still apply. |
| Compatibility and wire consequences | Documents are interpreted as WSDL 1.1 only when the WSDL 1.1 namespace is selected. Unsupported future syntax is not accepted as an implicit upgrade on the wire. |
| Executable evidence | `TestParseRecognizesWSDL11Definitions`, `TestParseWSDL11CoreAndSOAPDescription`, and `TestExternalWSDL11InteroperabilityCorpus` |
| Public surface | `Parse`, `Document11`, WSDL 1.1 builders, validation, compilation, serialization, and comparison |
| Upstream record | The Note and all reviewed WSDL 1.1 schema snapshots are independently pinned in the [manifest](../specification/manifest.tsv). |
| Reconsider when | A separately complete WSDL feature line is added; the WSDL 1.1 baseline itself remains version-closed. |

## WSDL11-DEC-002: Operation child order determines operation style

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | WSDL 1.1 sections [2.4.1 through 2.4.4](https://www.w3.org/TR/2001/NOTE-wsdl-20010315.html#_porttypes) |
| Classification | Normative semantic and wire-order decision |
| Issue | The same `input` and `output` element names represent four operation styles, and their presence and order carry meaning that a schema-shaped pair can erase. |
| Credible interpretations | Normalize input before output; infer style from which children exist; preserve raw child order only; or store a typed style and serialize according to it. |
| Known peer behavior | WSDL toolchains commonly expose operation categories, but generated object models may normalize child order and lose solicit-response or notification semantics. |
| Selected behavior | Parsing records one-way, request-response, solicit-response, or notification style from child presence and order. Validation rejects inconsistent models, and serialization emits the order required by the retained style. |
| Security and resource consequences | Style detection is single-pass and bounded by operation limits; inconsistent programmatic models fail before output. |
| Compatibility and wire consequences | Solicit-response emits output before input and notification remains output-only. Canonicalization never rewrites either into request-response wire form. |
| Executable evidence | `TestOperationStyle11RecognizesEveryMessageOrder`, `TestParseWSDL11PreservesSolicitResponseOperationOrder`, and `TestWSDL11SolicitResponseSerializesOutputBeforeInput` |
| Public surface | `Operation11.Style`, parsing, validation, compilation, semantic comparison, and serialization |
| Upstream record | Requirement `WSDL11-CORE-002` maps the four normative forms to executable evidence. |
| Reconsider when | Only a new WSDL version defines different operation categories; malformed 1.1 order remains an error. |

## WSDL11-DEC-003: Binding extensions are description data, not runtime clients

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | WSDL 1.1 [SOAP binding](https://www.w3.org/TR/2001/NOTE-wsdl-20010315.html#_soap-b), [HTTP binding](https://www.w3.org/TR/2001/NOTE-wsdl-20010315.html#_http), and [MIME binding](https://www.w3.org/TR/2001/NOTE-wsdl-20010315.html#_Toc492291084) sections |
| Classification | Scope and host-integration policy |
| Issue | Binding elements describe protocol mappings but do not specify credentials, retries, network trust, request execution, or application ownership. Treating parsed bindings as active clients would invent unsafe runtime behavior. |
| Credible interpretations | Execute endpoints directly; generate and send SOAP envelopes; expose transport callbacks from the parser; ignore binding extensions; or retain and validate typed description data only. |
| Known peer behavior | Code generators often build runtime clients around WSDL models, while parser libraries generally keep model and transport lifecycles separate. |
| Selected behavior | SOAP, HTTP, and MIME extensions are parsed, preserved, validated, compiled, and serialized as description data. This package never acquires credentials, creates requests, or invokes an endpoint. |
| Security and resource consequences | Parsing untrusted WSDL cannot trigger SSRF, credential forwarding, redirects, or background network work. Description sizes remain bounded. |
| Compatibility and wire consequences | Binding XML round-trips semantically, but no transport behavior or generated SOAP envelope is part of this package's wire contract. |
| Executable evidence | `TestParseWSDL11HTTPAndMIMEBindings`, `TestValidateWSDL11BindingProtocolProperties`, and `TestSOAP12HeadersFaultsAndActionPresenceRoundTrip` |
| Public surface | WSDL 1.1 SOAP, HTTP, and MIME model types; parser; validator; compiler; serializer |
| Upstream record | Requirements `WSDL11-SOAP-001`, `WSDL11-HTTP-001`, and `WSDL11-MIME-001` separate description support from transport execution. |
| Reconsider when | Runtime generation belongs in a separate package with an explicit transport and credential threat model. |

## WSDL11-DEC-004: Overloaded operations use complete message identity

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | WSDL 1.1 section [2.4 Port Types](https://www.w3.org/TR/2001/NOTE-wsdl-20010315.html#_porttypes), including default input and output names and operation overloading |
| Classification | Normative omission-resolution and component-identity policy |
| Issue | WSDL 1.1 permits operation overloading and default message-reference names, so operation name alone cannot reliably bind a concrete operation or identify a semantic diff. |
| Credible interpretations | Reject every overload; choose the first same-named operation; key only by operation name; compare raw child order; or resolve using effective input and output names with deterministic ambiguity errors. |
| Known peer behavior | WSDL 1.1 toolchains vary in overload support and generated-language mapping, especially when default message names collide. |
| Selected behavior | Binding resolution and compiled identity use operation name plus effective input and output message-reference names. An indistinguishable overload is rejected as ambiguous instead of selected by document order. |
| Security and resource consequences | Candidate matching is bounded by operation limits and cannot trigger resolution I/O; ambiguity fails closed. |
| Compatibility and wire consequences | Valid overloads remain distinct through compilation and comparison. Reordering declarations cannot change which operation a binding selects. |
| Executable evidence | `TestValidateWSDL11ResolvesOverloadedBindingOperation`, `TestValidateWSDL11RejectsAmbiguousBindingOperation`, and `TestCompilerPreservesWSDL11OverloadedOperationIdentity` |
| Public surface | WSDL 1.1 validation, compiled operation lookup, binding resolution, and semantic diff |
| Upstream record | The selected identity implements the default-name and overloading rules represented by `WSDL11-CORE-001` and `WSDL11-CORE-003`. |
| Reconsider when | A published erratum defines a different complete identity or overloading is exposed through a versioned alternative API. |

## WSDL20-DEC-001: The 26 June 2007 Recommendations are the WSDL 2.0 baseline

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | W3C [WSDL 2.0 Core](https://www.w3.org/TR/2007/REC-wsdl20-20070626/), [Adjuncts](https://www.w3.org/TR/2007/REC-wsdl20-adjuncts-20070626/), and [Additional MEPs Note](https://www.w3.org/TR/2007/NOTE-wsdl20-additional-meps-20070626/) |
| Classification | Version-selection and normative-source policy |
| Issue | Core and Adjuncts are Recommendations, while Additional MEPs is a Note and published schemas and primer examples have different authority. |
| Credible interpretations | Treat every W3C artifact as equally normative; follow schemas over prose; omit Note-defined patterns; or preserve each artifact's status while implementing explicitly selected features. |
| Known peer behavior | Apache Woden implements WSDL 2.0 but does not make every adjunct or Note normative, and WSDL 1.1-focused peers cannot establish 2.0 semantics. |
| Selected behavior | Dated Core and Adjuncts Recommendations are normative. Additional MEPs are supported as named patterns while retaining the Note's non-normative status. The primer and examples are informative evidence only. |
| Security and resource consequences | Version closure prevents unreviewed later vocabulary from expanding work; selected patterns remain subject to component and graph limits. |
| Compatibility and wire consequences | The WSDL 2.0 namespace selects this exact feature line. Additional MEP identifiers retain their defined wire values without being represented as Core requirements. |
| Executable evidence | `TestParseRecognizesWSDL20Description`, `TestValidateWSDL20PredefinedMessageExchangePatterns`, and `TestAcceptedW3CWSDL20FixturesParseCompileAndRoundTrip` |
| Public surface | `Parse`, `Description20`, WSDL 2.0 builders, validation, compilation, serialization, and comparison |
| Upstream record | Core, Adjuncts, Additional MEPs, errata, schemas, and primer are pinned separately in the [manifest](../specification/manifest.tsv). |
| Reconsider when | W3C publishes a superseding Recommendation or a separately complete feature line is intentionally added. |

## WSDL20-DEC-002: Absent and explicitly defaulted values remain distinguishable

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | WSDL 2.0 Core section [2.5 Interface Message Reference Component](https://www.w3.org/TR/2007/REC-wsdl20-20070626/#InterfaceMessageReference), plus SOAP and HTTP defaulted properties in Adjuncts sections 5 and 6 |
| Classification | Normative defaulting and lexical-presence policy |
| Issue | A schema default gives an effective value but does not prove that the corresponding attribute appeared in the source document. Collapsing absence and explicit presence loses round-trip and compatibility information. |
| Credible interpretations | Materialize every default as explicitly present; erase explicit default values; preserve raw XML only; or retain typed effective values with independent presence flags. |
| Known peer behavior | Generated XML bindings commonly materialize schema defaults, while document models differ on whether lexical presence survives parsing. |
| Selected behavior | Defaultable SOAP and HTTP properties retain presence flags. Message content models retain whether `element` was absent even when its effective value is `#other`. |
| Security and resource consequences | Presence tracking is constant-size state and prevents malformed duplicate spellings from being normalized silently. |
| Compatibility and wire consequences | Semantic comparison can distinguish absent from explicitly supplied defaults, and serialization omits an absent attribute while preserving an explicit one. |
| Executable evidence | `TestWSDL20MessageContentModelsRoundTrip`, `TestWSDL20OperationSafetyRoundTripsWithPresence`, and `TestCompareMessageCoversPresencePropertiesAndParts` |
| Public surface | WSDL 2.0 message, SOAP, and HTTP model fields; parser; serializer; semantic diff |
| Upstream record | Requirements `WSDL20-CONTENT-001`, `WSDL20-SOAP-001`, and `WSDL20-HTTP-001` retain the relevant evidence. |
| Reconsider when | A separate normalized-view API is added; the lossless document model remains presence-aware. |

## WSDL20-DEC-003: Unknown absolute message exchange patterns remain extensible

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | WSDL 2.0 Core section [2.2.1 Interface Operation Component](https://www.w3.org/TR/2007/REC-wsdl20-20070626/#InterfaceOperation), Adjuncts section [2 Predefined MEPs](https://www.w3.org/TR/2007/REC-wsdl20-adjuncts-20070626/#meps), and the Additional MEPs Note |
| Classification | Normative extension-point policy |
| Issue | WSDL requires an absolute pattern IRI and defines known label and direction rules, but permits independently defined MEPs whose semantics are unavailable to this package. |
| Credible interpretations | Reject unknown patterns; accept any string; infer directions from child order; treat only Core patterns as valid; or validate known patterns while preserving unknown absolute identifiers. |
| Known peer behavior | WSDL 2.0 implementations vary in support for Additional MEPs and extension patterns; Woden accepts extensible pattern identifiers subject to model validation. |
| Selected behavior | Every pattern identifier must be absolute. Direction and label rules are enforced for the eight recognized patterns; an unknown absolute identifier remains valid and its explicit message references are preserved. |
| Security and resource consequences | IRI and message counts are bounded, and unknown identifiers never load code, consult a registry, or trigger network resolution. |
| Compatibility and wire consequences | Extension MEP IRIs round-trip unchanged. The package does not invent directions or labels absent from their wire representation. |
| Executable evidence | `TestValidateWSDL20PredefinedMessageExchangePatterns`, `TestValidateWSDL20CustomPatternMessageLabels`, and `TestWSDL20InitialMessageFollowsPredefinedMEPDirection` |
| Public surface | WSDL 2.0 interface operation models, validation, compilation, and serialization |
| Upstream record | Requirement `WSDL20-MEP-001` distinguishes recognized Recommendation and Note patterns from extensible absolute IRIs. |
| Reconsider when | A newly supported MEP receives an explicit identifier, semantic rule set, fixtures, and compatibility review. |

## WSDL20-DEC-004: RPC validation is split between WSDL and compiled XML Schema

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | WSDL 2.0 Adjuncts section [4.1 RPC Style](https://www.w3.org/TR/2007/REC-wsdl20-adjuncts-20070626/#RPCStyle) and XML Schema component references required by that section |
| Classification | Normative cross-specification ownership policy |
| Issue | Some RPC constraints depend only on WSDL components, while wrapper shape, particles, attributes, and parameter mappings require a compiled XML Schema graph. One validation phase cannot prove both without duplicating XSD semantics. |
| Credible interpretations | Reimplement XSD inspection in the WSDL validator; defer every RPC check to compilation; validate only lexical signature syntax; or divide checks at the component-ownership boundary. |
| Known peer behavior | Integrated WSDL toolchains often hide this phase split, while standalone model libraries may validate only shallow RPC syntax. |
| Selected behavior | Local validation owns RPC style, MEP, wrapper-reference, and signature rules requiring only WSDL. Compilation owns wrapper sequence, particle, attribute, named-type, and parameter mapping rules requiring immutable `xsd` components. |
| Security and resource consequences | Schema work occurs only through bounded injected compilation; local validation cannot trigger resolution or duplicate schema expansion. |
| Compatibility and wire consequences | A document can parse but still fail compilation when referenced schema shape violates RPC rules. Diagnostics identify the phase rather than silently accepting an incomplete wire contract. |
| Executable evidence | `TestValidateWSDL20RPCStyleRules`, `TestCompilerRejectsInvalidWSDL20RPCSchemas`, and `TestCompilerPreservesWSDL20RPCSignature` |
| Public surface | `Validate`, `compile.Compiler`, WSDL 2.0 RPC models, diagnostics, and compiled operation metadata |
| Upstream record | Requirement `WSDL20-STYLE-001` maps both local and compiled evidence to the same normative style. |
| Reconsider when | The XSD contract changes or a standalone validation API is explicitly extended to require a caller-supplied compiled schema set. |

## WSDL20-DEC-005: Operation safety accepts both published spellings and emits the normative one

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | WSDL 2.0 Adjuncts section [3.1 Operation Safety](https://www.w3.org/TR/2007/REC-wsdl20-adjuncts-20070626/#safety) and the pinned dated [WSDL 2.0 schema](https://www.w3.org/2007/06/wsdl/wsdl20.xsd) |
| Classification | Published prose/schema contradiction and canonicalization policy |
| Issue | Adjuncts defines namespaced `wsdlx:safe`, while the dated WSDL schema also declares an unqualified `safe` operation attribute. Accepting both simultaneously would create conflicting values. |
| Credible interpretations | Follow only prose; follow only schema; let the last attribute win; reject both schema and prose variants; or accept either singular spelling and choose one canonical output. |
| Known peer behavior | Schema-driven tools may accept the unqualified spelling, while prose-driven WSDL 2.0 tools use `wsdlx:safe`. |
| Selected behavior | Parsing accepts either spelling, rejects simultaneous use, records presence independently, and deterministic serialization emits the normative Adjuncts `wsdlx:safe` spelling. |
| Security and resource consequences | Duplicate semantic values fail closed and cannot smuggle conflicting safety metadata; processing remains bounded. |
| Compatibility and wire consequences | Existing schema-valid unqualified input is accepted, but canonical output changes its lexical spelling while preserving the boolean semantic value and explicit presence. |
| Executable evidence | `TestWSDL20OperationSafetyRoundTripsWithPresence`, `TestWSDL20RejectsInvalidBooleanAtEveryAdjunctBoundary`, and `TestCompareDetectsWSDL20OperationSafetyChange` |
| Public surface | WSDL 2.0 operation safety fields, parser, serializer, compiler, and semantic diff |
| Upstream record | The prose/schema discrepancy is retained in [`specification/discrepancies-and-extensions.md`](../specification/discrepancies-and-extensions.md). |
| Reconsider when | W3C publishes an erratum resolving the dated schema discrepancy. |

## WSDL20-DEC-006: Operation style validation is split between WSDL and XML Schema

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | WSDL 2.0 Adjuncts sections [4.2 IRI Style](https://www.w3.org/TR/2007/REC-wsdl20-adjuncts-20070626/#_operation_iri_style) and [4.3 Multipart Style](https://www.w3.org/TR/2007/REC-wsdl20-adjuncts-20070626/#_operation_multipart_style) |
| Classification | Normative cross-specification ownership policy |
| Issue | Style IRI, initial-message content model, and wrapper reference are visible in WSDL, while element content, local children, attributes, types, occurrence, and uniqueness require compiled XSD components. |
| Credible interpretations | Validate styles only lexically; duplicate XSD logic locally; require compilation for every shallow error; ignore schema shape; or split checks by owned model. |
| Known peer behavior | Toolchains with integrated schema processors often report one combined result; model-only libraries commonly under-validate style constraints. |
| Selected behavior | Local validation enforces style IRI, initial-message content model, and wrapper name. Compilation enforces complex sequence, local-child, attribute, simple-type, occurrence, and uniqueness requirements. |
| Security and resource consequences | Expensive schema inspection remains within bounded compilation and cannot be triggered by local validation alone. |
| Compatibility and wire consequences | Parsing preserves style declarations, while complete support claims require successful schema-aware compilation. Failures remain deterministic across both phases. |
| Executable evidence | `TestValidateWSDL20IRIAndMultipartStylesRequireElementInitialMessage`, `TestCompilerValidatesWSDL20IRIAndMultipartStyleSchemas`, and `TestIRISimpleTypeRulesCoverInlineAndBuiltInTypes` |
| Public surface | WSDL 2.0 operation style fields, validation, compilation, diagnostics, and generated models |
| Upstream record | Requirement `WSDL20-STYLE-001` binds both validation phases to the same Adjuncts sections. |
| Reconsider when | Style validation moves behind a different explicit schema-component contract without weakening either phase. |

## WSDL-DEC-001: Normative prose outranks schemas, examples, fixtures, and peers

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | Dated WSDL 1.1 [Note](https://www.w3.org/TR/2001/NOTE-wsdl-20010315.html), WSDL 2.0 [Core](https://www.w3.org/TR/2007/REC-wsdl20-20070626/), [Adjuncts](https://www.w3.org/TR/2007/REC-wsdl20-adjuncts-20070626/), and [errata](https://www.w3.org/2007/06/wsdl20-errata.html) |
| Classification | Normative-source precedence and contradiction policy |
| Issue | Prose, schemas, primer examples, W3C fixtures, and independent implementations can disagree, and WSDL 1.1 and 2.0 have different normative status. |
| Credible interpretations | Follow schemas mechanically; treat fixtures as the specification; copy majority peer behavior; merge both WSDL versions; or preserve authority and classify every disagreement. |
| Known peer behavior | Woden, SoapUI, CXF, and `dotnet-svcutil` cover different versions and extensions, so agreement can still reflect a shared omission rather than normative behavior. |
| Selected behavior | Dated normative prose plus published errata controls. Schemas constrain syntax only where assigned that role. Notes, primers, examples, fixtures, and peers are evidence and never silently override stronger text or merge version semantics. |
| Security and resource consequences | A permissive schema or peer cannot weaken secure parsing, semantic validation, or bounded execution without an explicit reviewed decision. |
| Compatibility and wire consequences | Contradictions resolve deterministically before changing accepted or emitted XML. Version-specific behavior remains separate and visible. |
| Executable evidence | `TestAcceptedW3CWSDL20FixturesParseCompileAndRoundTrip`, `TestExternalWSDL11InteroperabilityCorpus`, and `TestParseRejectsMalformedWSDL20AtEveryNestedDecoderBoundary` |
| Public surface | All parsing, validation, compilation, composition, serialization, generation, and comparison behavior |
| Upstream record | Source role, version, status, digest, size, and URL are independently recorded in the [manifest](../specification/manifest.tsv). |
| Reconsider when | A new erratum or minimized disagreement proves the selected reading conflicts with stronger normative text. |

## WSDL-DEC-002: Parsing performs no external resolution

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | WSDL 1.1 section [2.1.1 Document Naming and Linking](https://www.w3.org/TR/2001/NOTE-wsdl-20010315.html#_document-n), WSDL 2.0 Core sections [4.1 import](https://www.w3.org/TR/2007/REC-wsdl20-20070626/#imports) and [4.2 include](https://www.w3.org/TR/2007/REC-wsdl20-20070626/#includes), and Go [`context.Context`](https://pkg.go.dev/context) |
| Classification | Host integration, ownership, and defensive policy |
| Issue | Import and include locations identify resources, but WSDL does not define application trust, schemes, filesystem roots, redirects, credentials, network policy, or cancellation. |
| Credible interpretations | Fetch during parsing; allow file and HTTP URLs by default; ignore references; use a global catalog; or preserve resolved identities and require an injected bounded resolver during compilation. |
| Known peer behavior | XML and WSDL runtimes expose different resolver callbacks and defaults, including implicit filesystem or network access. Those defaults are not portable application policy. |
| Selected behavior | Parsing resolves URI references lexically against explicit base identity but loads nothing. Compilation uses only the injected resolver; the default resolver denies access, and schema resolution uses a separately injected XSD resolver. |
| Security and resource consequences | Default processing has no SSRF, path traversal, DNS, redirect, or credential side effects. Injected resolution remains context-cancellable and bounded by document, graph, reference, and cumulative-byte limits. |
| Compatibility and wire consequences | Import and include locations remain explicit in the model. Identical compilation requires identical resolver inputs; receiving XML never authorizes I/O. |
| Executable evidence | `TestParseWSDL11ImportResolvesURIWithoutLoading`, `TestParseWSDL20ImportAndIncludeResolveURIsWithoutLoading`, and `TestCompilerDefaultsToDeniedResolution` |
| Public surface | `Parse`, import/include models, `resolve.Resolver`, `compile.New`, and `compile.Options` |
| Upstream record | Requirements `WSDL11-RESOLVE-001` and `WSDL20-RESOLVE-001` distinguish lexical identity from injected resource loading. |
| Reconsider when | A resolver adapter is added; parsing remains I/O-free unless the public ownership model is deliberately redesigned. |

## WSDL-DEC-003: DTDs, directives, and custom entity processing are rejected

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | XML 1.0 Fifth Edition [document type definition](https://www.w3.org/TR/2008/REC-xml-20081126/#sec-prolog-dtd) and WSDL XML representation rules in the selected WSDL publications |
| Classification | Defensive XML parsing and extension-data policy |
| Issue | XML permits DTDs and entities, but WSDL processing does not require this package to expose entity expansion, and preserved foreign extension XML could otherwise reintroduce directives during serialization. |
| Credible interpretations | Delegate decoder defaults; permit internal entities; expand with limits; ignore directives; reject only external entities; or reject every directive before model processing and output. |
| Known peer behavior | XML runtimes vary in DTD defaults and hardening controls, so peer acceptance is deployment-dependent rather than a portable WSDL requirement. |
| Selected behavior | Parsing rejects DTDs and directives and never expands custom entities. Serialization also rejects unsafe directive-bearing raw extension XML. This is an explicit defensive profile, not a WSDL-mandated claim. |
| Security and resource consequences | The policy removes XXE, entity-expansion amplification, and hidden external fetches. Byte, depth, text, node, and extension limits remain independently enforced. |
| Compatibility and wire consequences | WSDL documents or preserved extension fragments requiring a DTD are rejected rather than normalized. Callers must preprocess trusted entities outside this boundary. |
| Executable evidence | `TestParseRejectsGeneralXMLAndResourceBoundaries`, `TestSerializationHelpersRejectUnboundNamesAndUnsafeRawXML`, and `FuzzParseRoundTrip` |
| Public surface | `Parse`, extension preservation, `Marshal`, canonicalization, and all reader-backed parsing paths |
| Upstream record | The defensive profile is documented in [`security.md`](security.md) and does not alter the pinned WSDL source bytes. |
| Reconsider when | Any opt-in entity mode receives a separate threat model, bounded resolver contract, fixtures, and wire-compatibility review. |

## WSDL-DEC-004: Expanded QNames own identity and extensions remain opaque unless understood

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | XML Namespaces 1.0 Third Edition [expanded names](https://www.w3.org/TR/2009/REC-xml-names-20091208/#dt-expname), WSDL 1.1 sections 2.1.3 and 2.1.4, and WSDL 2.0 Core section [6 Language Extensibility](https://www.w3.org/TR/2007/REC-wsdl20-20070626/#language-extensibility) |
| Classification | Namespace identity and normative extension policy |
| Issue | Prefix text is not component identity, foreign extension content must survive round trips, and required extensions cannot be treated as understood merely because they can be stored. |
| Credible interpretations | Key by prefix text; discard unknown extensions; accept all required extensions; reject every foreign element; or preserve opaque expanded-name data and require explicit understanding for required extensions. |
| Known peer behavior | Object models differ in lexical prefix retention and extension plugins; schema-valid parsing alone often does not enforce required-extension understanding. |
| Selected behavior | Component and attribute identity uses namespace URI plus local name. Unknown optional extensions remain opaque bounded data. A required extension is valid only when its expanded QName is explicitly listed as understood, including nested boundaries. |
| Security and resource consequences | Namespace, extension, attribute, and raw XML sizes are bounded. Merely parsing an extension never executes code or grants capability. |
| Compatibility and wire consequences | Prefixes may be regenerated while expanded names and extension XML semantics survive. Unknown required extensions fail validation instead of being silently ignored. |
| Executable evidence | `TestParsePreservesWSDL11ExtensionElementsAndAttributes`, `TestWSDL20ExtensionsRoundTripAcrossComponents`, and `TestValidateRequiresExplicitExtensionUnderstanding` |
| Public surface | `QName`, `Extensibility`, parser, validator options, compiler, composition, and serializer |
| Upstream record | Every modeled extension boundary is listed in [`specification/discrepancies-and-extensions.md`](../specification/discrepancies-and-extensions.md). |
| Reconsider when | A typed extension gains an explicit model and evidence; unknown extensions remain governed by this fallback contract. |

## WSDL-DEC-005: Serialization is deterministic and semantic, not lexically preserving

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | WSDL 1.1 and WSDL 2.0 XML representations, plus XML Namespaces [namespace constraints](https://www.w3.org/TR/2009/REC-xml-names-20091208/) |
| Classification | Implementation-defined canonicalization and wire-output policy |
| Issue | WSDL does not define a canonical byte serialization. Prefixes, declaration placement, insignificant formatting, and component order can vary while preserving the same model. |
| Credible interpretations | Preserve source bytes; emit Go map order; use XML canonicalization; retain arbitrary prefixes; or emit one deterministic package-defined semantic representation. |
| Known peer behavior | Toolchains commonly rewrite prefixes, formatting, and declaration order, and their generated bytes are not generally interchangeable canonical forms. |
| Selected behavior | Serialization emits stable package-selected prefixes, attributes, components, operation direction, extension payloads, and optional formatting. Parse-serialize-parse preserves model semantics; byte identity with the source is not promised. |
| Security and resource consequences | Deterministic ordering avoids hash and cache instability, and output is bounded before returning partial data. Unsafe raw extension XML fails closed. |
| Compatibility and wire consequences | Equivalent input may produce different lexical XML but stable package output. Canonical bytes are package-version behavior, while expanded names and typed semantics are the compatibility contract. |
| Executable evidence | `TestMarshalWSDL11IsDeterministicAndRoundTrips`, `TestMarshalWSDL20IsDeterministicAndRoundTrips`, and `TestSerializerAssignsTargetAndSchemaPreferredPrefixesForBothVersions` |
| Public surface | `Marshal`, marshal options, builders, composition, code-generation models, and semantic comparison |
| Upstream record | Serialization behavior is documented in [`parsing-and-generation.md`](parsing-and-generation.md); no W3C canonical-WSDL claim is made. |
| Reconsider when | A separately named XML canonicalization profile is added with exact normative rules and fixtures. |

## WSDL-DEC-006: All parsing, compilation, validation, and output work is explicitly bounded

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | Go [`context.Context`](https://pkg.go.dev/context), WSDL component models in the selected publications, and package defensive resource policy |
| Classification | Defensive host-resource and diagnostic policy |
| Issue | WSDL documents can contain deep XML, large extension payloads, broad component graphs, cyclic imports, schemas, and diagnostic amplification, while the specifications define no host resource budget. |
| Credible interpretations | Rely on memory exhaustion; expose only a byte limit; truncate silently; use process-global limits; or enforce caller-configurable finite limits at every allocation, graph, output, and diagnostic boundary. |
| Known peer behavior | General XML and WSDL libraries expose inconsistent limits and may rely on runtime defaults or deployment-level timeouts. |
| Selected behavior | Parsing, validation, compilation, code generation, and serialization enforce finite bytes, depth, nodes, attributes, text, components, references, schemas, graph depth, diagnostics, and output limits. Context cancellation is observed where work can block or traverse. |
| Security and resource consequences | Host memory, CPU, recursion, I/O, and diagnostic growth fail with typed bounded errors before oversized allocation or unbounded traversal. Temporary or background work is not retained. |
| Compatibility and wire consequences | Inputs exceeding configured or default budgets fail deterministically rather than being partially accepted. Raising a caller-owned limit does not change WSDL semantics. |
| Executable evidence | `TestParseEnforcesComponentLimitsBeforeModelConstruction`, `TestCompilerAcceptsGraphsAtEveryExactResourceLimit`, and `TestMarshalLimitAppliesAtEveryOutputBoundary` |
| Public surface | Parse, validation, compilation, resolver, code-generation, and marshal option limits; diagnostics and errors |
| Upstream record | The complete limit inventory and threat boundary are maintained in [`security.md`](security.md). |
| Reconsider when | A new processing phase or collection is added; it must receive a limit and exact-boundary evidence before release. |

## WSDL-DEC-007: Conformance and interoperability claims remain attributable

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | WSDL 1.1 and 2.0 publications, W3C WSDL 2.0 [test-suite fixtures](https://dev.w3.org/2002/ws/desc/test-suite/), and pinned independent source revisions in [`specification/interoperability.tsv`](../specification/interoperability.tsv) |
| Classification | Conformance evidence and interoperability policy |
| Issue | Passing unit tests, parsing a fixture, matching one peer, and passing a complete semantic matrix are different claims. Aggregating them can hide unsupported rows or peer-specific behavior. |
| Credible interpretations | Claim conformance from parser acceptance; report one aggregate percentage; treat Woden as normative; accept fixture skips; or bind every requirement and peer result to exact pinned evidence. |
| Known peer behavior | Woden covers WSDL 2.0, while SoapUI, `dotnet-svcutil`, and CXF primarily cover WSDL 1.1; their overlap and diagnostics differ materially. |
| Selected behavior | Versioned requirement and assertion matrices own normative claims. W3C fixtures and Woden results remain WSDL 2.0 evidence; SoapUI, .NET, CXF, and carrier fixtures remain WSDL 1.1 interoperability evidence. Missing tools or failed cases never count as passes. |
| Security and resource consequences | External corpora and tools are immutable, digest-pinned, license-recorded, isolated, and processed under package limits; fixtures never authorize network resolution. |
| Compatibility and wire consequences | Published claims identify the exact version, corpus, denominator, and peer. A disagreement is minimized and classified rather than resolved by majority behavior. |
| Executable evidence | `TestExternalWSDL11InteroperabilityCorpus`, `TestAcceptedW3CWSDL20FixturesParseCompileAndRoundTrip`, and `FuzzModelRoundTrip` |
| Public surface | Conformance documentation, requirement matrices, interoperability gate, parsing, validation, compilation, and serialization claims |
| Upstream record | Source and tool pins are maintained in `manifest.tsv`, `interoperability.tsv`, and `tooling.tsv`; Woden execution is documented in [`interoperability.md`](interoperability.md). |
| Reconsider when | A corpus, peer, tool version, applicability rule, requirement row, or source pin changes. |

## WSDL-DEC-008: The package models descriptions and never owns service execution

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `wsdl` maintainers |
| Source | WSDL 1.1 [abstract and concrete definitions](https://www.w3.org/TR/2001/NOTE-wsdl-20010315.html#_introduction) and WSDL 2.0 Core [Introduction](https://www.w3.org/TR/2007/REC-wsdl20-20070626/#intro) |
| Classification | Application-policy omission and package-boundary decision |
| Issue | WSDL describes services but does not define this library's HTTP client, SOAP envelope runtime, authentication, retry, telemetry, code-emission language, or endpoint trust policy. |
| Credible interpretations | Bundle a SOAP client and transport; expose generated network methods; select endpoints automatically; use global credentials; or stop at a deterministic description and code-generation model boundary. |
| Known peer behavior | Full SOAP stacks combine description, code generation, and transport, while focused schema/model libraries keep those lifecycles independently composable. |
| Selected behavior | The package parses, validates, resolves through injected seams, compiles, composes, compares, serializes, and produces bounded generation models. `wire` owns SOAP envelopes and callers own transport, credentials, retries, telemetry, and endpoint policy. |
| Security and resource consequences | Untrusted descriptions cannot initiate requests or access credentials. Generated-model work is bounded and contains no hidden goroutine or connection lifecycle. |
| Compatibility and wire consequences | Public APIs expose description semantics only. Runtime SOAP and HTTP behavior can evolve independently without changing the WSDL model contract. |
| Executable evidence | `TestBuildCreatesOwnedDeterministicGenerationModel`, `TestCompilerUsesOnlyInjectedSchemaResolver`, and `TestMemoryReturnsOwnedResourceCopies` |
| Public surface | Entire `wsdl` module, especially `compile`, `codegen`, SOAP and HTTP binding models, and resolver contracts |
| Upstream record | The package boundary is stated in the module README and the versioned requirement matrices intentionally contain no transport-execution claim. |
| Reconsider when | Never by silently expanding this module; a runtime client requires a separate package and explicit lifecycle and security goals. |

## Unresolved and excluded behavior

No known material WSDL interpretation is unresolved. WS-Security,
WS-Addressing, WS-Policy, transport execution, credentials, retries, and SOAP
envelope processing are excluded package concerns rather than partial WSDL
claims. Any future support requires its own versioned specification inventory,
decision entries, provenance, fixtures, and interoperability evidence.
