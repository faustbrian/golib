# Specification decisions

This document records package decisions where the pinned OpenAPI prose,
published artifacts, or accepted errata need interpretation. The order of
authority is normative prose, accepted OpenAPI Initiative errata, published
schemas and registries, then local package policy. A published schema never
silently overrides normative prose.

Statuses are `resolved`, `unresolved`, or `superseded`. Resolved decisions are
part of the compatibility contract and require specification, security,
resource, wire, executable-evidence, and changelog review when changed.

## OPENAPI-DEC-001: OpenAPI 3.2 default Schema Object dialect

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openapi` maintainers |
| Source | OpenAPI Initiative [accepted correction #4994](https://github.com/OAI/OpenAPI-Specification/pull/4994) and the pinned [OpenAPI 3.2 dialect artifact](https://spec.openapis.org/oas/3.2/dialect/2025-09-17) |
| Classification | Accepted erratum and version-specific Schema Object dialect selection |
| Issue | OpenAPI 3.2.0 prose named a 3.1 dialect URI while the published 3.2 schema defaulted `jsonSchemaDialect` to the dated 3.2 dialect. Using the prose literally would reject valid 3.2 vocabulary and disagree with the accepted correction. |
| Credible interpretations | Preserve the erroneous prose forever; follow the published schema silently; require every caller to declare a dialect; or apply the accepted erratum while retaining the original pinned source unchanged. |
| Known peer behavior | Generic JSON Schema engines select their own default dialect unless a caller registers OpenAPI resources explicitly. OpenAPI tools may therefore disagree on omitted 3.2 dialect declarations. |
| Selected behavior | For a 3.2 document with neither root `jsonSchemaDialect` nor resource-root `$schema`, Schema Objects use `https://spec.openapis.org/oas/3.2/dialect/2025-09-17`. An explicit resource declaration overrides the document declaration, which overrides the default. The released 3.2.0 Markdown remains byte-identical and the erratum is recorded separately in the provenance manifest. |
| Security and resource consequences | Only checksum-pinned dialect resources are registered; selection performs no network access and remains under compiler depth, node, operation, and cancellation limits. An unrecognized explicit dialect fails rather than silently falling back. |
| Compatibility and wire consequences | Omitted 3.2 dialect declarations gain the corrected 3.2 vocabulary semantics, including XML `nodeType` and discriminator `defaultMapping`. Explicit dialect bytes and precedence remain unchanged. |
| Executable evidence | `TestDocumentCompilerUsesNormativeBaseDialect`, `TestSchemaDeclarationOverridesCompilerDefault`, `TestCompilerAppliesOpenAPIDialectAndPreservesAnnotations`, and `TestVerifyAcceptsEveryManifestArtifact` |
| Public surface | `jsonschema.NewCompilerForDocument`, `jsonschema.Compiler`, OpenAPI 3.2 document validation, and `specification/manifest.json` |
| Upstream record | Correction #4994 merged as `0f65e951d63d34c207cd79081cff4743c0d763fb` for the 3.2.1 patch line; the manifest binds that decision without rewriting the pinned 3.2.0 publication. |
| Reconsider when | A released OpenAPI patch changes the 3.2 dialect URI, precedence rules, or accepted erratum. |

## OPENAPI-DEC-002: HTTP reference representation selection

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openapi` maintainers |
| Source | RFC 9110 [Content-Type semantics](https://www.rfc-editor.org/rfc/rfc9110.html#section-8.3) and [representation metadata risks](https://www.rfc-editor.org/rfc/rfc9110.html#section-8.3-5) |
| Classification | HTTP representation selection and media-type confusion policy |
| Issue | A remote server can return an OpenAPI-looking body whose path extension conflicts with its explicit `Content-Type`. Trusting the suffix over the response metadata can parse attacker-controlled bytes under the wrong format. |
| Credible interpretations | Always trust the path suffix; always sniff body bytes; accept every textual media type; reject responses without `Content-Type`; or treat explicit supported media types as authoritative and use extensions only when metadata is absent. |
| Known peer behavior | Generic OpenAPI loaders commonly infer JSON or YAML from file names or content. RFC 9110 permits inspection when metadata is absent but warns against overriding a received type. |
| Selected behavior | The HTTP resolver accepts JSON, structured-syntax `+json`, and documented YAML media types. When `Content-Type` is present, any unsupported or malformed type is rejected even if the path ends in `.json`, `.yaml`, or `.yml`. A recognized extension selects the parser only when the field is absent. No body sniffing occurs. |
| Security and resource consequences | Explicit metadata cannot be downgraded by a misleading path. Header and body handling remain bounded by response, decompression, redirect, concurrency, destination, timeout, and cancellation policy. |
| Compatibility and wire consequences | Servers that send a conflicting or generic explicit media type are rejected and must correct or omit it. Responses without metadata retain deterministic extension-based compatibility. |
| Executable evidence | `TestHTTPResponseFormatUsesAuthoritativeMediaTypeOrExtension`, `TestHTTPResolverEnforcesResponsePolicyAndLimits`, `TestHTTPResolverReadsAuthorizedJSONAndYAML`, and `FuzzHTTPResolverResponseBoundary` |
| Public surface | `reference.HTTPResolver`, HTTP resolver options, `ErrUnsupportedResourceFormat`, JSON parsing, and YAML parsing |
| Upstream record | This selection policy applies RFC 9110 to the package's supported OpenAPI representations; OpenAPI does not define a separate remote-fetch algorithm. |
| Reconsider when | OpenAPI publishes reference retrieval media-type rules or a registered OpenAPI media type changes the supported representation set. |

## OPENAPI-DEC-003: Unpaired JSON surrogate escapes

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openapi` maintainers |
| Source | RFC 8259 [Unicode interoperability](https://www.rfc-editor.org/rfc/rfc8259.html#section-8.2) and [parser allowances](https://www.rfc-editor.org/rfc/rfc8259.html#section-9) |
| Classification | Intentional strict-JSON compatibility and identity policy |
| Issue | Go's standard JSON decoder replaces unpaired UTF-16 surrogate escapes with U+FFFD, so the decoded member name or string no longer represents the received bytes and can collide with another value. |
| Credible interpretations | Accept replacement characters silently; preserve invalid escape bytes in a parallel representation; accept only member names; reject every surrogate escape; or reject unpaired surrogates while accepting valid escaped pairs. |
| Known peer behavior | RFC 8259 notes unpredictable receiver behavior for non-Unicode bit sequences. JSON implementations vary between rejection, replacement, and preservation. |
| Selected behavior | Strict JSON input rejects an unpaired high or low surrogate before semantic decoding. Valid escaped surrogate pairs remain accepted and decode normally. No invalid sequence is normalized to U+FFFD. |
| Security and resource consequences | Rejection prevents ambiguous identifiers and replacement-character collisions. The pre-scan is linear, allocation-free, bounded by the parser byte limit, and subject to the surrounding cancellation and depth controls. |
| Compatibility and wire consequences | JSON documents containing unpaired surrogate escapes are intentionally rejected even where another parser accepts them. Valid Unicode and paired escaped scalar values remain wire-compatible. |
| Executable evidence | `TestJSONRejectsUnpairedUnicodeSurrogateEscapes`, `TestUnpairedSurrogateScannerCoversEscapeBoundaries`, `TestJSONRejectsMalformedRepresentations`, and `FuzzJSONParserDeterminism` |
| Public surface | `parse.JSON`, strict JSON document parsing, member-name identity, string values, and `parse.ErrInvalidJSON` |
| Upstream record | RFC 8259 explicitly permits parsers to constrain accepted character contents and warns that unpaired surrogate behavior is unpredictable. |
| Reconsider when | Go exposes a lossless standard JSON representation for invalid surrogate sequences and the package defines a compatible identity model for it. |

## OPENAPI-DEC-004: Version-specific dialects and prose authority

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openapi` maintainers |
| Source | OpenAPI [2.0 Schema Object](https://spec.openapis.org/oas/v2.0.html#schema-object), [3.0.4 Schema Object](https://spec.openapis.org/oas/v3.0.4.html#schema-object), [3.1.2 Schema Object](https://spec.openapis.org/oas/v3.1.2.html#schema-object), and [3.2.0 Schema Object](https://spec.openapis.org/oas/v3.2.0.html#schema-object); pinned prose, schemas, dialects, and registries are listed in `specification/manifest.json` |
| Classification | Version-specific normative behavior, prose-versus-schema conflict policy, and implementation-defined dialect selection |
| Issue | Swagger 2.0, OpenAPI 3.0, 3.1, and 3.2 use materially different Schema Object dialects and object vocabularies. Published schemas are useful executable artifacts but cannot express every prose rule and have historically disagreed with released prose. Treating the newest schema or one generic JSON Schema dialect as authority for every document would silently change older wire contracts. |
| Credible interpretations | Validate every version with its published schema only; apply the newest rules to all compatible-looking documents; normalize every Schema Object to JSON Schema 2020-12; accept unknown patch versions optimistically; or dispatch exact supported versions and layer explicit prose rules over pinned artifacts. |
| Known peer behavior | Compared OpenAPI libraries differ on OpenAPI 3.1 boolean schemas, OpenAPI 3.2 fields, validation depth, and whether published schemas or hand-written rules are decisive; the pinned observations are in `docs/interoperability.md`. |
| Selected behavior | The declared document version selects an exact supported model, validator rule set, conversion path, and Schema Object dialect. Swagger 2.0 uses its extended draft-04 subset, OpenAPI 3.0 uses the OpenAPI 3.0 schema subset, and OpenAPI 3.1/3.2 use their declared or version-default JSON Schema 2020-12 dialect. Unknown versions fail explicitly. Normative prose and accepted errata outrank published schemas; schema-only acceptance never suppresses an implemented prose violation. Cross-version conversion is explicit, bounded, and reports semantic loss rather than pretending incompatible fields are equivalent. |
| Security and resource consequences | Exact dispatch prevents permissive newer vocabularies from bypassing older constraints. All dialect artifacts are checksum-pinned and loaded without ambient network access; compilation and conversion retain depth, node, operation, and cancellation bounds. |
| Compatibility and wire consequences | Documents retain the semantics of their declared version. Patch conversion preserves values where rules are equivalent; line upgrades and downgrades can return explicit loss records. Applications cannot rely on an informational published schema to override normative prose. |
| Executable evidence | `TestDocumentCompilerSelectsSwagger20SchemaSubset`, `TestDocumentCompilerSelectsOpenAPI30SchemaSubset`, `TestDocumentCompilerUsesNormativeBaseDialect`, `TestSchemaProseRemainsAuthoritativeWhenInformationalSchemaPasses`, `TestConvertPatchVersionPreservesDocumentValues`, and `TestConvertOpenAPI31To30ReportsLosses` |
| Public surface | document parsing and model construction, `validate.DocumentWithOptions`, `jsonschema.NewCompilerForDocument`, `convert.Convert`, generated Swagger 2.0 and OpenAPI 3.x models, and conformance ledgers |
| Upstream record | Pinned revisions and accepted corrections are recorded in `specification/manifest.json`; local prose rules map to exact requirement rows in `specification/conformance/`. |
| Reconsider when | A supported OpenAPI patch, accepted erratum, or incorporated JSON Schema dialect changes a version boundary or resolves a recorded prose/schema conflict. |

## OPENAPI-DEC-005: Reference siblings, external resources, and cycles

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openapi` maintainers |
| Source | OpenAPI [3.0.4 Reference Object](https://spec.openapis.org/oas/v3.0.4.html#reference-object), [3.1.2 Reference Object](https://spec.openapis.org/oas/v3.1.2.html#reference-object), [3.2.0 Reference Object](https://spec.openapis.org/oas/v3.2.0.html#reference-object), [3.2.0 relative references](https://spec.openapis.org/oas/v3.2.0.html#relative-references-in-api-description-uris), RFC 3986, and RFC 6901 |
| Classification | Version-specific reference semantics, optional external-resource policy, and defensive cycle policy |
| Issue | Reference Object siblings differ by OpenAPI version and object position, while Schema Object `$ref` follows JSON Schema rules. External retrieval needs explicit authority and stable base identity. Cycles may be legal graph structure, malformed recursive overlays, or infinite work if a resolver treats every visit as new. |
| Credible interpretations | Discard all siblings; merge every sibling; apply JSON Schema `$ref` semantics everywhere; load external files and URLs implicitly; reject every cycle; or distinguish Reference Objects, Schema Objects, versions, target types, and graph identity. |
| Known peer behavior | OpenAPI libraries differ on sibling overlays, implicit external loading, canonical resource identity, and cycle handling. Generic JSON Reference and JSON Schema resolvers also do not enforce OpenAPI object-position rules. |
| Selected behavior | Parsing preserves Reference Object siblings losslessly. Dereferencing ignores undefined siblings and applies only the `summary` and `description` overlays allowed by OpenAPI 3.1/3.2 and only where the target type permits them; OpenAPI 3.0 siblings have no effect. Schema Object `$ref` remains governed by the selected JSON Schema dialect. External resources are never fetched without an explicit caller resolver and base resource identity. Resolution uses canonical resource URI plus fragment identity, terminates legal cycles with an explicit cycle result, and rejects malformed targets, exhausted limits, or unresolved required references without recursive expansion. |
| Security and resource consequences | No reference grants ambient filesystem or network authority. Caller-enabled resolvers enforce allowlists, canonical containment, response limits, document budgets, redirect policy, concurrency, depth, node counts, reference counts, cancellation, and redacted errors. Canonical cycle detection prevents unbounded recursion and alias-driven repeated work. |
| Compatibility and wire consequences | Unknown Reference Object siblings round-trip but do not alter dereferenced semantics. Legal cyclic documents remain representable and diagnosable; APIs that require an acyclic inlined value receive a classified cycle error instead of partial output. Relative references resolve against the correct retrieval or OpenAPI 3.2 `$self` identity. |
| Executable evidence | `TestDereferenceObjectsAppliesReferenceSiblingsAndRejectsCycles`, `TestDereferenceObjectsIgnoresReferenceExtensionsAcrossVersions`, `TestResolveChainReportsLegalReferenceCycle`, `TestDocumentDistinguishesReferenceCycles`, `TestDocumentResolvesExternalReferencesOnlyWhenAuthorized`, and `TestDocumentUsesOpenAPI32SelfAsReferenceBase` |
| Public surface | `reference.Object`, `reference.Resolve`, `reference.ResolveChain`, `reference.DereferenceObjects`, `reference.Resolver`, file and HTTP resolvers, validation options, and conversion APIs |
| Upstream record | The conformance ledger records the applicable Reference Object and relative-reference requirements for every supported version; retrieval remains package policy because OpenAPI does not define a transport client. |
| Reconsider when | OpenAPI changes Reference Object sibling overlays, base-URI rules, target-position rules, or publishes a retrieval and cycle-processing algorithm. |

## OPENAPI-DEC-006: Paths, callbacks, webhooks, and extensions

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openapi` maintainers |
| Source | OpenAPI [3.2.0 Path Templating](https://spec.openapis.org/oas/v3.2.0.html#path-templating), [3.2.0 Callback Object](https://spec.openapis.org/oas/v3.2.0.html#callback-object), [3.2.0 `webhooks`](https://spec.openapis.org/oas/v3.2.0.html#oas-webhooks), and [Specification Extensions](https://spec.openapis.org/oas/v3.2.0.html#specification-extensions), with corresponding pinned 2.0, 3.0, and 3.1 sections |
| Classification | Version-specific path semantics, runtime-expression policy, extension policy, and cross-document operation inventory |
| Issue | Path templates can be textually different but operationally ambiguous, and OpenAPI 3.2 tightens repeated-expression and additional-operation rules. Callbacks and webhooks contain Path Item Objects outside the root `paths` map and can be referenced externally. Extension values may contain `$ref`-shaped or operation-shaped data that is application data rather than normative OpenAPI structure. |
| Credible interpretations | Compare path keys literally only; reject every similar template; traverse root paths only; treat callbacks and webhooks as opaque; recursively interpret extension values as OpenAPI; or apply each version's exact operation surfaces while preserving extensions as data. |
| Known peer behavior | Tooling varies in ambiguous-template detection, callback and webhook traversal, custom-method support, runtime-expression validation, and whether extension contents are interpreted recursively. |
| Selected behavior | Path keys and template parameters are validated under the declared version. Operationally equivalent templates are diagnosed; OpenAPI 3.2 additionally rejects repeated template expressions and fixed HTTP methods duplicated through `additionalOperations`, while earlier versions do not inherit those rules. Operation inventories include root paths, webhooks, callbacks, authorized external Path Items, and OpenAPI 3.2 additional operations, with canonical reference-cycle deduplication. Callback keys use the normative runtime-expression grammar. `x-` members are preserved losslessly but ignored by normative traversal unless a specifically documented extension handler opts in; `$ref`-shaped extension data never grants reference semantics. |
| Security and resource consequences | Traversal across nested and external operation surfaces shares reference, node, depth, operation, diagnostic, and cancellation budgets. Treating extensions as opaque prevents untrusted extension data from triggering network access or hidden validation work. Runtime-expression parsing and evaluation are bounded and do not execute code. |
| Compatibility and wire consequences | Callback, webhook, custom-operation, and extension bytes round-trip in source order. Diagnostics are version-specific, so valid earlier documents are not retroactively rejected by 3.2-only rules. Operation identifiers and path-parameter requirements are checked across every effective operation surface. |
| Executable evidence | `TestDocumentValidatesPathTemplatesAndParameters`, `TestOpenAPI32RejectsRepeatedPathTemplateExpressions`, `TestAdditionalOperationRuleDoesNotApplyBeforeOpenAPI32`, `TestDocumentRequiresOperationIDsUniqueAcrossWebhooks`, `TestDocumentCollectsOperationsFromExternalCallbacks`, `TestParseRejectsExpressionsOutsideNormativeGrammar`, and `TestDocumentAcceptsLegalComponentNamesAndIgnoresExtensions` |
| Public surface | `validate.DocumentWithOptions`, `expression.Parse`, `expression.ParseTemplate`, generated Path Item, Callback, Webhook, and extension fields, operation filtering, diff, composition, and serialization APIs |
| Upstream record | Exact version requirements and evidence are mapped in `specification/conformance/`; registered extension artifacts are pinned in `specification/manifest.json`. |
| Reconsider when | OpenAPI changes path equivalence, callback or webhook operation scope, custom methods, runtime-expression grammar, or normative interpretation of extension contents. |

## OPENAPI-DEC-007: Parameter serialization and ambiguous values

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openapi` maintainers |
| Source | OpenAPI [2.0 Parameter Object](https://spec.openapis.org/oas/v2.0.html#parameter-object), [3.0.4 Parameter Object](https://spec.openapis.org/oas/v3.0.4.html#parameter-object), [3.1.2 Parameter Object](https://spec.openapis.org/oas/v3.1.2.html#parameter-object), [3.2.0 Parameter Object](https://spec.openapis.org/oas/v3.2.0.html#parameter-object), RFC 3986, and RFC 6570 |
| Classification | Version-specific serialization, explicitly implementation-defined empty-value behavior, and defensive ambiguity policy |
| Issue | Defaults depend on version, location, style, shape, and `explode`; Swagger 2.0 uses `collectionFormat`; OpenAPI leaves nested `deepObject`, some empty values, cookie/header quoting, and several delimiter collisions undefined or implementation-defined. A decoder cannot recover object and array boundaries when raw data contains the same delimiters unless an additional convention is used. |
| Credible interpretations | Accept every style/shape combination heuristically; split all delimiters; return the first plausible shape; prohibit delimiter-bearing values; preserve raw text only; or support normative combinations and require an explicit reversible convention for otherwise ambiguous values. |
| Known peer behavior | Framework URL codecs commonly apply form decoding too early, collapse repeated keys, normalize percent escapes, or quote headers. OpenAPI implementations differ on undefined `deepObject`, empty-value, and delimiter-collision behavior. |
| Selected behavior | Swagger 2.0 and each OpenAPI line use their own declared defaults and allowed location/style/shape combinations. Header values pass through unchanged where the specification forbids automatic URI encoding. Query form handling applies its defined plus and percent-decoding rules at the correct stage. Unsupported nested values and undefined combinations fail explicitly. Empty query values require the caller to select the documented policy. Ambiguous array/object delimiters are reversible only through the package's explicit percent-escape convention; decoding does not guess among multiple shapes. Limits are checked before splitting or allocating items. |
| Security and resource consequences | Strict shape and delimiter handling prevents parameter smuggling through parser differentials. Byte and item limits are enforced before amplification, malformed escapes are rejected, and integer-bound calculations avoid overflow. The package does not quote security-sensitive headers on a caller's behalf. |
| Compatibility and wire consequences | Normative examples and supported Swagger `collectionFormat` values round-trip deterministically. Undefined combinations return classified errors rather than unstable values. The delimiter-escape convention is a documented package interoperability policy and must be shared by both producer and consumer when delimiter-bearing values occur. |
| Executable evidence | `TestOptionsForAppliesParameterSerializationDefaults`, `TestSwagger20CollectionFormatsRoundTrip`, `TestEncodeMatchesOpenAPI32StyleExamples`, `TestDecodeOpenAPI32StyleExamples`, `TestDecodeRequiresExplicitEmptyValuePolicy`, `TestDecodeRejectsMalformedOrAmbiguousSerializations`, and `TestEscapeAmbiguousDelimitersDefinesReversibleConvention` |
| Public surface | `parameter.OptionsFor`, `parameter.Encode`, `parameter.Decode`, Swagger 2.0 codecs, empty-value policy, limits, shapes, styles, and parameter example validation |
| Upstream record | The conformance ledger maps each supported version's defaults and restrictions; the reversible delimiter convention is local policy for behavior the specification does not define. |
| Reconsider when | OpenAPI defines currently undefined nested, empty, cookie, header, or delimiter behavior, or a broadly adopted interoperable convention supersedes the package policy. |

## OPENAPI-DEC-008: Security Requirement composition

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openapi` maintainers |
| Source | OpenAPI [2.0 Security Requirement Object](https://spec.openapis.org/oas/v2.0.html#security-requirement-object), [3.0.4 Security Requirement Object](https://spec.openapis.org/oas/v3.0.4.html#security-requirement-object), [3.1.2 Security Requirement Object](https://spec.openapis.org/oas/v3.1.2.html#security-requirement-object), and [3.2.0 Security Requirement Object](https://spec.openapis.org/oas/v3.2.0.html#security-requirement-object) |
| Classification | Normative composition semantics and version-specific scope/role interpretation |
| Issue | Security Requirement arrays express alternatives while names inside one object are combined requirements. Empty arrays, empty objects, operation overrides, non-OAuth values, and OpenAPI 3.2 URI-named schemes are often flattened incorrectly, which can turn AND into OR or accidentally require authentication for an anonymous alternative. |
| Credible interpretations | Require every array entry; accept any named scheme in one object; treat `{}` as malformed; infer missing credentials; ignore required scopes; or evaluate OR across objects, AND within each object, and preserve version-specific naming and role rules. |
| Known peer behavior | HTTP middleware frequently models one scheme at a time and cannot represent compound requirements without application code. OpenAPI tools also differ on the role-label allowance introduced after 3.0 and URI-based scheme names introduced in 3.2. |
| Selected behavior | One Security Requirement Object is satisfied only when every named scheme and required scope or role in that object is available. Array entries are alternatives, and an empty object permits anonymous access. An explicitly empty security array disables inherited requirements. Document validation resolves names and applies Swagger 2.0, OpenAPI 3.0, 3.1, and 3.2 scheme-value and URI-precedence rules exactly; runtime evaluation does not perform authentication or authorization itself. |
| Security and resource consequences | Preserving AND/OR boundaries avoids authorization weakening. Malformed arrays, objects, or scope values fail closed with stable errors. Alternative, scheme, and scope counts are bounded before evaluation; credential data is caller-owned and no secret material is logged or retained. |
| Compatibility and wire consequences | Compound and alternative requirements produce deterministic results, including anonymous alternatives and explicit disablement. OpenAPI 3.1 role labels and 3.2 URI scheme names are not incorrectly rejected by older-version rules, while older documents retain their stricter value constraints. |
| Executable evidence | `TestSatisfiedAppliesAlternativeAndCombinedRequirements`, `TestSatisfiedHandlesOptionalAndDisabledSecurity`, `TestSatisfiedEvaluatesRequiredRoleLabels`, `TestDocumentValidatesSecurityRequirementConnections`, `TestOpenAPI31AllowsNonOAuthRoleRequirements`, and `TestOpenAPI32SecurityRequirementNamesPreferComponentsAndValidateURIs` |
| Public surface | `security.Satisfied`, `security.Credentials`, `security.Limits`, document and operation validation, operation filtering, generated Security Requirement models, and conversion loss reports |
| Upstream record | Each supported version's composition and scheme-value requirements are represented in the normative and evidence ledgers. |
| Reconsider when | OpenAPI changes security override, anonymous-access, role-label, URI-name precedence, or compound-scheme semantics. |

## OPENAPI-DEC-009: Server URL template expansion

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openapi` maintainers |
| Source | OpenAPI [3.0.4 Server Object](https://spec.openapis.org/oas/v3.0.4.html#server-object), [3.1.2 Server Object](https://spec.openapis.org/oas/v3.1.2.html#server-object), [3.2.0 Server Object](https://spec.openapis.org/oas/v3.2.0.html#server-object), and RFC 3986 |
| Classification | Normative substitution with explicit transport-specific escaping policy |
| Issue | The specification requires variable substitution but does not define a general automatic escaping step for values used in different URL components. Applying generic percent-encoding can double-encode caller data or change delimiters; applying none without saying so can surprise callers. Missing declarations, missing defaults, unused overrides, malformed braces, and repeated variables also need deterministic handling. |
| Credible interpretations | Percent-encode every replacement; infer escaping from the template position; insert values literally; silently retain unknown placeholders; ignore unused overrides; or require valid declarations and make escaping caller-owned. |
| Known peer behavior | URI-template libraries often implement RFC 6570 expansion, but OpenAPI Server Object templates are a narrower brace substitution mechanism. Tooling varies on enum enforcement, undeclared variables, and whether replacement values are escaped. |
| Selected behavior | Every template occurrence must name a declared Server Variable with a string default. A caller override replaces that default. Substitution inserts bytes exactly as supplied; the caller owns any component-specific percent-encoding before expansion. Malformed or nested braces, undeclared variables, non-string defaults, invalid UTF-8, and unused overrides fail explicitly. Validation separately applies version-specific URL, enum, and server-surface rules. Expansion neither mutates caller maps nor resolves the resulting URL; `server.ResolveReference` performs explicit RFC 3986 resolution against an expanded server URL. |
| Security and resource consequences | Explicit escaping ownership avoids hidden double-decoding and delimiter transformations. Output bytes, declarations, overrides, and occurrences are bounded; invalid inputs fail before use as a network destination. Expansion grants no network authority. |
| Compatibility and wire consequences | Defaults and overrides produce stable literal substitutions, including repeated variables. Applications that require encoded values must provide encoded bytes deliberately. Swagger 2.0 host/basePath/schemes conversion remains a separate, loss-reporting operation rather than being treated as a Server Object. |
| Executable evidence | `TestExpandUsesDefaultsAndCallerOverrides`, `TestExpandRejectsInvalidServerAndVariableInputs`, `TestExpandEnforcesOutputAndVariableBounds`, `TestResolveReferenceUsesServerURLAsRFC3986Base`, `TestDocumentValidatesServerVariablesAndNames`, and `TestServerDialectDecisionsAreExact` |
| Public surface | `server.Expand`, `server.Options`, `server.ResolveReference`, server validation, generated Server Object models, and cross-version conversion |
| Upstream record | Server Object requirements for each 3.x release are mapped in the conformance ledger; literal replacement and caller-owned escaping are explicit package policy. |
| Reconsider when | OpenAPI defines component-aware encoding, adopts RFC 6570 expansion semantics, or changes declaration, default, or URL resolution requirements. |

## OPENAPI-DEC-010: JSON-equivalent YAML input

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openapi` maintainers |
| Source | OpenAPI [3.0.4 format](https://spec.openapis.org/oas/v3.0.4.html#format), [3.1.2 format](https://spec.openapis.org/oas/v3.1.2.html#format), [3.2.0 format](https://spec.openapis.org/oas/v3.2.0.html#format), YAML 1.2, and RFC 8259 |
| Classification | Deliberately strict representation equivalence and defensive parser policy |
| Issue | OpenAPI permits JSON or YAML while defining a JSON data model. General YAML supports aliases, merge keys, non-string keys, tags, non-JSON scalars, duplicate mappings, and multiple documents that have no single portable JSON meaning. Accepting those features makes behavior parser-dependent and can produce semantics that cannot round-trip through JSON. |
| Credible interpretations | Accept the full YAML language; expand aliases and merges; coerce keys and scalars to JSON; preserve YAML-only metadata; or restrict YAML input to one unambiguous JSON-equivalent document. |
| Known peer behavior | The pinned interoperability comparison shows independent YAML loaders accepting aliases and independent JSON loaders accepting duplicate members. YAML libraries differ in scalar inference, merge processing, duplicate handling, and alias expansion. |
| Selected behavior | JSON and YAML parse into the same ordered immutable JSON semantic model with exact number spellings. YAML accepts one document with string mapping keys and JSON-compatible scalar tags only. Duplicate keys, anchors, aliases, merge keys, custom tags, non-JSON numeric forms, multiple documents, invalid UTF-8, and other non-equivalent forms are rejected. Serialization emits deterministic JSON or a YAML representation of the same semantics; it does not preserve YAML presentation details. |
| Security and resource consequences | Rejecting aliases and merges removes alias-expansion and hidden-key amplification. Both parsers enforce independent byte, token/value, scalar, width, depth, cancellation, and diagnostic bounds before semantic use. Duplicate rejection prevents key-smuggling differentials. |
| Compatibility and wire consequences | A supported YAML document has the same member identity, ordering, scalar kinds, and numeric values as its JSON rendering. YAML-specific comments, anchors, tags, quoting choices, and document boundaries are intentionally not round-tripped. Inputs relying on general YAML features must be normalized by the producer. |
| Executable evidence | `TestYAMLUsesTheJSONSemanticModel`, `TestYAMLRejectsAmbiguousOrNonJSONRepresentations`, `TestJSONRejectsDuplicateObjectMembers`, `TestJSONPreservesSemanticOrderAndExactNumbers`, `FuzzYAMLParserProducesJSONSemantics`, and `TestYAMLRoundTripPreservesJSONSemantics` |
| Public surface | `parse.JSON`, `parse.YAML`, `jsonvalue.Value`, JSON and YAML serialization, document model construction, and interoperability fixtures |
| Upstream record | The strict subset is package policy layered on the OpenAPI JSON data-model requirement and is explicitly compared in `docs/interoperability.md`. |
| Reconsider when | OpenAPI standardizes portable semantics for a currently rejected YAML feature or the package adds a separate lossless YAML representation with an explicit non-JSON contract. |

## Unresolved decisions

None for the interpretations currently recorded here. New accepted errata,
retrieval semantics, or parser compatibility exceptions require a stable
decision before implementation.
