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

## Unresolved decisions

None for the interpretations currently recorded here. New accepted errata,
retrieval semantics, or parser compatibility exceptions require a stable
decision before implementation.
