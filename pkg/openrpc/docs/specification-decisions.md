# OpenRPC specification decisions

This register records observable choices where OpenRPC 1.3.x or 1.4.x, its
Draft 7 meta-schema, official examples, JSON-RPC 2.0, or an adjacent standard
permits or appears to permit more than one implementation. Authoritative
inputs and checksums are pinned in
[`specification/manifest.json`](../specification/manifest.json), and normative
and object-field mappings live under
[`specification/conformance`](../specification/conformance/).

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

Statuses are `resolved`, `unresolved`, or `superseded`. Resolved decisions are
part of the compatibility contract. Changing one requires specification,
security, compatibility, executable-evidence, and changelog review. The
package-wide [evidence report](evidence.md), [compatibility contract](compatibility.md),
[specification report](specification-report.md), and
[changelog](../CHANGELOG.md) apply to every decision below.

## OPENRPC-DEC-001: Normative authority and contradictory artifacts

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openrpc` maintainers |
| Source | OpenRPC 1.4.1 [Meta JSON Schema](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/spec-template.md#meta-json-schema), [Server Object](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/spec-template.md#server-object), and the pinned [official examples](https://github.com/open-rpc/examples/tree/dce69463ba9a3ca2232506b734606fa97f25dd45/service-descriptions) |
| Classification | Normative-source precedence, correction, and provenance policy |
| Issue | The prose permits relative and variable-templated server URLs while the published meta-schema applies a generic URI format that rejects them. Pinned official examples also declare older feature lines and therefore cannot all validate against the 1.4.1 meta-schema unchanged. |
| Credible interpretations | Treat the meta-schema as infallible; treat examples as implicit amendments; silently rewrite upstream artifacts; or give normative prose precedence while retaining every divergence as pinned, executable evidence. |
| Known peer behavior | Generic Draft 7 validators enforce the published schema literally. The official examples retain their historical `openrpc` values. Neither behavior resolves the conflict in the normative prose. |
| Selected behavior | Normative prose controls. Meta-schema compilation removes only the contradictory Server URL `format` assertion and delegates that field to bounded server-expression validation. Official examples are never relabeled: the 1.3.0 metrics example is accepted by typed 1.3 semantics, and older unsupported feature lines remain explicit rejection fixtures. Every other published meta-schema constraint remains active. |
| Security and resource consequences | The narrow rewrite cannot disable unrelated structural checks. Server expressions remain parsed under finite limits, and pinned artifacts are checksum-verified before conformance claims are accepted. |
| Compatibility and wire consequences | Relative and templated server URLs remain wire-compatible with the prose. Historical example version bytes are preserved; accepting a new feature line requires a separate inventory rather than accidental fallback. |
| Executable evidence | `TestPinnedMetaSchemaAcceptsNormativeServerURLForms`, `TestPinnedOfficialExamplesRespectSupportedVersionLines`, `TestPinnedMetaSchemaAcceptsCurrentMinimalDocument`, `TestVerifyPinnedInputsChecksEveryManifestDigest`, and the [normative evidence matrix](../specification/conformance/evidence.tsv) |
| Public surface | `validate.MetaSchema`, `ParseVersion`, typed parsing, `MetaSchema`, and `JSONSchemaToolsMetaSchema`; adoption consequences are documented in the [compatibility contract](compatibility.md) |
| Upstream record | Exact upstream commits and source digests are recorded in the [manifest](../specification/manifest.json); no upstream erratum currently supersedes the pinned prose. |
| Reconsider when | OpenRPC publishes a corrected meta-schema, relabels or replaces the examples, or issues an erratum defining different precedence. |

## OPENRPC-DEC-002: Reference Object siblings and unknown members

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openrpc` maintainers |
| Source | OpenRPC 1.4.1 [Reference Object schema](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/schema.json#L148-L161) and [Specification Extensions](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/spec-template.md#specification-extensions) |
| Classification | Normative object-shape and extension interpretation |
| Issue | The Reference Object schema allows only `$ref`; it does not inherit the extension pattern. Permitting siblings, including `x-` fields, would make their evaluation and preservation semantics undefined. |
| Credible interpretations | Ignore all siblings as in older JSON Reference practice; preserve but do not evaluate them; permit `x-` siblings; or reject every sibling exactly as the published object schema requires. |
| Known peer behavior | Reference sibling handling varies across JSON Schema and OpenAPI generations. The pinned OpenRPC meta-schema is unambiguous: `additionalProperties` is false and only `$ref` is declared. |
| Selected behavior | A Reference Object contains exactly one non-empty `$ref`. Strict typed parsing rejects every sibling, including `x-` members. Preserving mode does not turn structurally invalid Reference Objects into accepted documents. |
| Security and resource consequences | Rejecting siblings avoids hidden policy data and prevents unbounded ignored values from being smuggled beside a reference. URI-reference length and syntax remain bounded separately. |
| Compatibility and wire consequences | Documents relying on Reference Object siblings are rejected rather than normalized or silently stripped. Extension data must be placed on an object that the OpenRPC schema marks extensible. |
| Executable evidence | `TestObjectFieldRequirednessAndNullabilityMatrix`, `TestDecodeAcceptsEveryReferenceUnion`, `TestDecodeRejectsMalformedReferencesInEveryUnion`, `TestDecodeUnknownFieldsUsesExplicitMode`, and the [object-field matrix](../specification/conformance/object-field-evidence.tsv) |
| Public surface | `Reference`, every `*OrReference` union, `parse.Decode`, and `parse.Options`; parser behavior is summarized in the [API guide](api.md) |
| Upstream record | The exact Reference Object definition is pinned by the [manifest](../specification/manifest.json); no sibling extension is present in OpenRPC 1.4.1. |
| Reconsider when | A released OpenRPC feature line explicitly permits Reference Object siblings or defines their processing semantics. |

## OPENRPC-DEC-003: Reference resolution and cycle boundaries

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openrpc` maintainers |
| Source | OpenRPC 1.4.1 [Schema Object and Reference Object rule](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/spec-template.md#jsonschema-object), JSON Schema Draft 7 [reference semantics](https://json-schema.org/draft-07/json-schema-core.html#rfc.section.8), and RFC 3986 [reference resolution](https://www.rfc-editor.org/rfc/rfc3986#section-5) |
| Classification | Cross-document resolution, recursion, and resource-policy decision |
| Issue | OpenRPC object references and embedded JSON Schema references share URI syntax but not recursion semantics. Following all references with one algorithm can reject valid recursive schemas or loop forever on OpenRPC object cycles. |
| Credible interpretations | Never resolve references; resolve all references identically; permit every cycle and rely on depth limits; or separate OpenRPC object traversal from Draft 7 schema resource compilation. |
| Known peer behavior | Draft 7 validators support recursive schema graphs. General JSON Reference resolvers differ on cycle handling and external loading defaults, so no peer default is imported. |
| Selected behavior | Core parsing performs no I/O. Caller-supplied stores and allowlists opt into external loading. OpenRPC object-reference aliases and cycles are followed under aggregate depth, count, byte, scheme, and host limits; cyclic object dereferencing fails. Recursive Draft 7 schema references remain intact and are compiled through the schema-resource graph instead of being dereferenced as OpenRPC objects. |
| Security and resource consequences | External access is off by default. Fetches, redirects, DNS/IP targets, bytes, fan-out, depth, aliases, and cancellation are bounded; each resource is loaded at most once per operation. |
| Compatibility and wire consequences | Valid recursive schemas remain representable. Cyclic OpenRPC object graphs produce typed failures rather than partial documents, hangs, or implicit network access. |
| Executable evidence | `TestResolverFollowsAliasesAndLoadsEachDocumentOnce`, `TestResolverRejectsCyclesAndBounds`, `TestDereferenceRejectsCyclesAndTransformLimits`, `TestResolvedDocumentPreservesRecursiveJSONSchemaReferences`, and `TestResolvedDocumentAcceptsRecursiveExternalSchemaResources` |
| Public surface | `reference.Resolver`, `reference.Store`, `reference.ResolvePolicy`, `reference.Dereference`, `reference.Bundle`, and `validate.ResolveDocument`; threats are documented in the [resolver threat model](resolver-threat-model.md) |
| Upstream record | OpenRPC and Draft 7 sources are checksum-pinned in the [manifest](../specification/manifest.json); cycle policy is package-owned because OpenRPC does not define a fetch algorithm. |
| Reconsider when | OpenRPC standardizes resolution, bundling, cycle, or retrieval semantics, or a supported JSON Schema dialect changes recursive-reference behavior. |

## OPENRPC-DEC-004: Method, descriptor, and error uniqueness

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openrpc` maintainers |
| Source | OpenRPC 1.4.1 Method Object rules for [method names](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/schema.json#L424-L438), [parameters](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/schema.json#L486-L500), and [errors](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/schema.json#L514-L528) |
| Classification | Normative cross-element semantic validation |
| Issue | Draft 7 cannot express uniqueness by method name, descriptor name, or arbitrary-precision error code, and references may hide duplicates until resolution. Optional-after-required ordering is also semantic rather than structural. |
| Credible interpretations | Validate only inline elements; compare raw JSON; let later entries overwrite earlier entries; or collect deterministic diagnostics both before and after bounded reference resolution. |
| Known peer behavior | The official meta-schema validates element shapes but cannot enforce these identity constraints. Generators commonly map names into registries, where silent overwrite would create implementation-dependent output. |
| Selected behavior | Inline validation reports duplicate method names, duplicate parameter names, optional-before-required ordering violations, and duplicate custom error codes deterministically. Resolved validation repeats identity rules across referenced elements. Error codes compare as exact integer values without floating-point conversion. No duplicate wins implicitly. |
| Security and resource consequences | Method and diagnostic counts are bounded; arbitrary-precision integer parsing is length-bounded. Deterministic collection prevents collision-driven memory growth and inconsistent registry construction. |
| Compatibility and wire consequences | Ambiguous documents fail instead of selecting first or last declarations. Reordering by-position parameters remains observable; by-name uniqueness is exact and case-sensitive. |
| Executable evidence | `TestCollectReportsDeterministicSemanticDiagnostics`, `TestResolvedDocumentAppliesSemanticRulesThroughReferences`, `TestErrorCodePreservesArbitraryPrecision`, and [ORPC-1.4-0034 through ORPC-1.4-0041](../specification/conformance/evidence.tsv) |
| Public surface | `validate.Document`, `validate.ResolvedDocument`, stable diagnostic codes, `Integer`, `Method`, and `ContentDescriptor` |
| Upstream record | Prose constraints are mapped to implementation and tests in the [normative evidence matrix](../specification/conformance/evidence.tsv). |
| Reconsider when | A released specification changes identity, case sensitivity, parameter ordering, or custom error-code rules. |

## OPENRPC-DEC-005: Example pairing validation

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openrpc` maintainers |
| Source | OpenRPC 1.4.1 Method Object [examples](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/schema.json#L546-L560), [Example Pairing Object](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/schema.json#L645-L694), and [official examples policy](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/spec-template.md#examples) |
| Classification | Ambiguous example-to-schema assertion policy |
| Issue | The prose calls examples valid params-to-result pairings but defines no algorithm for matching example names or positions to referenced descriptors, applying defaults, or treating annotations and custom formats. The meta-schema checks only shape. |
| Credible interpretations | Reject any example value not validated by a descriptor schema; validate only easily paired inline values; treat examples as documentation; or expose a separate caller-configured example assertion operation. |
| Known peer behavior | The official meta-schema and pinned examples establish structural shapes but do not provide a conformance corpus for value-to-schema assertion. Tooling may render or execute examples with application-specific semantics. |
| Selected behavior | Typed parsing preserves exact example values, explicit nulls, references, and notification result absence. Semantic document validation checks Example Pairing structure and reference syntax but does not claim that values satisfy method schemas. It never performs a partial or implicit schema assertion. Callers that execute or assert examples must do so explicitly after reference resolution with their chosen application semantics. |
| Security and resource consequences | Documentation validation cannot trigger schema callbacks, remote loading, or execution. Example JSON remains subject to parser byte, depth, value, and collection limits. |
| Compatibility and wire consequences | Schema-incompatible example values remain round-trip-safe and do not make an otherwise valid description invalid. A future assertion API must be opt-in and cannot silently change `validate.Document`. |
| Executable evidence | `TestExamplesPreserveNullAndNotificationResultAbsence`, `TestValidationChecksExamplePairingReferenceSyntax`, `TestSemanticValidationPreservesExampleValuesWithoutSchemaAssertion`, and `TestPinnedOfficialExamplesRespectSupportedVersionLines` |
| Public surface | `Example`, `ExamplePairing`, `validate.Document`, parsing, preserving serialization, and canonical serialization; the limitation is documented in the [compatibility contract](compatibility.md) |
| Upstream record | No OpenRPC 1.4.1 algorithm or official value-validation fixture is available in the pinned [specification and examples inputs](../specification/manifest.json). |
| Reconsider when | OpenRPC defines normative example assertion semantics or publishes a value-validation conformance corpus. |

## OPENRPC-DEC-006: JSON Schema dialect and resource selection

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openrpc` maintainers |
| Source | OpenRPC 1.4.1 [Schema Object](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/spec-template.md#jsonschema-object), [Meta JSON Schema](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/spec-template.md#meta-json-schema), and JSON Schema [Draft 7](https://json-schema.org/draft-07/json-schema-release-notes.html) |
| Classification | Normative embedded-schema dialect selection |
| Issue | Schema Objects may contain arbitrary Draft 7 JSON, boolean schemas, identifiers, and external references. Inferring a newer dialect from keywords or converting values into Go-native numeric types would change semantics. |
| Credible interpretations | Use the latest JSON Schema dialect; honor embedded `$schema` declarations from any dialect; accept only the OpenRPC meta-schema companion subset; or compile every Schema Object as Draft 7 exactly as required. |
| Known peer behavior | General validators often default to newer dialects or select dialects from `$schema`. OpenRPC 1.4.1 explicitly requires Draft 7, so peer defaults are not imported. |
| Selected behavior | Every OpenRPC Schema Object is preserved as arbitrary JSON and compiled with Draft 7 semantics, including boolean schemas and exact numbers. Explicit resource maps and caller-supplied resolvers provide external schemas; no implicit loader is installed. An unsupported embedded dialect does not silently switch the OpenRPC schema contract. |
| Security and resource consequences | Compilation and validation bound schema resources, aggregate bytes, depth, operations, regular-expression work, diagnostics, and cancellation. A regexp timeout returns `ErrValidationResourceLimit` rather than a false schema mismatch. The checksum-pinned OpenRPC meta-schema uses the maximum bounded timeout because its patterns are trusted specification inputs. External retrieval remains governed by OPENRPC-DEC-003. |
| Compatibility and wire consequences | Draft 7 keywords retain Draft 7 meaning across supported OpenRPC feature lines. Newer-dialect-only behavior is not inferred and requires a future OpenRPC feature decision. |
| Executable evidence | `TestDocumentCompilesEveryDraft7SchemaObject`, `TestResolvedDocumentAcceptsRecursiveExternalSchemaResources`, `TestResolvedDocumentUsesNestedSchemaIDsAsReferenceBases`, `TestValidatorReportsRegexpTimeoutAsResourceLimit`, and [ORPC-1.4-0020](../specification/conformance/evidence.tsv) |
| Public surface | `jsonschema.Schema`, `validate.Document`, `validate.ResolveDocument`, `jsonschema.ValidationOptions`, and resource policies; limits are documented in [resource budgets](resource-budgets.md) |
| Upstream record | Draft 7 and the OpenRPC companion meta-schema are pinned with hashes in the [manifest](../specification/manifest.json). |
| Reconsider when | A supported OpenRPC feature line adopts another dialect or defines dialect negotiation for embedded schemas. |

## OPENRPC-DEC-007: `rpc.discover` transport and cache semantics

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openrpc` maintainers |
| Source | OpenRPC 1.4.1 [Service Discovery Method](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/spec-template.md#service-discovery-method) and JSON-RPC 2.0 [reserved method names](https://www.jsonrpc.org/specification#request_object) |
| Classification | Normative method identity plus package lifecycle policy |
| Issue | The specification fixes the method name and result but does not define transport registration, visibility filtering, caching, invalidation, refresh, or concurrent discovery behavior. |
| Credible interpretations | Register `rpc.discover` as an ordinary application method; expose only static bytes; use an implicit global registry/cache; or provide transport-neutral lifecycle components and explicit adapters. |
| Known peer behavior | Frameworks differ in reserved-method registration and dynamic document generation. JSON-RPC reserves the `rpc.` prefix, so ordinary application registration is not portable. |
| Selected behavior | The transport-neutral service owns provider invocation, visibility filtering, semantic validation, canonical serialization, revision, and ETag production. The JSON-RPC adapter uses the sibling registry's explicit system-method path. Caching is opt-in, process-local, bounded, concurrency-safe, and invalidated explicitly; there is no background refresh or global registry. |
| Security and resource consequences | Visibility policy runs before publication; invalid documents fail closed. Provider work, output size, waiters, retained bytes, and cancellation are bounded, and callbacks cannot leak document content through observability labels. |
| Compatibility and wire consequences | The method name is exactly `rpc.discover`; the result is canonical raw OpenRPC JSON through the JSON-RPC runtime's normal result encoding. Cache lifetime is application-owned and never hidden. |
| Executable evidence | `TestDiscoveryHandlerReturnsCanonicalRawDocument`, `TestRegisterDiscoveryUsesTypedSystemMethodContract`, `TestCacheDeduplicatesConcurrentDiscoveryAndInvalidatesExplicitly`, and [ORPC-1.4-0017](../specification/conformance/evidence.tsv) |
| Public surface | `discovery.Service`, `discovery.Provider`, `discovery.Cache`, `discovery.VisibilityPolicy`, and `jsonrpc.RegisterDiscovery`; adoption is covered by the [examples](examples.md) |
| Upstream record | The fixed method contract is pinned in the [normative matrix](../specification/conformance/normative.tsv); cache and adapter semantics are intentionally package-owned. |
| Reconsider when | OpenRPC standardizes discovery caching, transport bindings, authorization, revisioning, or conditional retrieval. |

## OPENRPC-DEC-008: Extension members and non-standard fields

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openrpc` maintainers |
| Source | OpenRPC 1.4.1 [Specification Extensions](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/spec-template.md#specification-extensions), [JSON field uniqueness](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/spec-template.md#format), and per-object declarations in the [meta-schema](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/schema.json) |
| Classification | Normative extension support and explicit parser-mode policy |
| Issue | Extension values may be any JSON, but only selected object types are extensible. Non-`x-` unknown fields may be future standard members, mistakes, or data a proxy must preserve. Duplicate keys make either interpretation ambiguous. |
| Credible interpretations | Reject every unknown field; treat every unknown field as an extension; preserve all unknown fields silently; or separate strict conformance parsing from explicit lossless preservation. |
| Known peer behavior | Validators and generators vary between strict rejection and forward-compatible retention. The OpenRPC schema marks extension points explicitly and requires `x-`, so unrestricted acceptance is not specification compliance. |
| Selected behavior | Duplicate JSON names are always rejected. Declared extensible objects accept unique `x-` fields with arbitrary exact JSON values. Strict mode rejects other unknown standard fields. Explicit preserving mode retains unknown fields and original accepted bytes for proxy/editor workflows, but does not classify those fields as standard or extension semantics. Canonical output rejects collisions between preserved and typed members. |
| Security and resource consequences | All values remain bounded by JSON parser limits. Duplicate rejection and collision checks prevent shadowed standard fields; preserving mode is explicit so callers cannot accidentally trust unrecognized policy data. |
| Compatibility and wire consequences | Strict mode exposes unsupported fields as errors. Preserving mode can round-trip future fields byte-for-byte, while canonical typed output remains deterministic and collision-free. |
| Executable evidence | `TestNewExtensionsRequiresUniquePrefixedNames`, `TestDecodeUnknownFieldsUsesExplicitMode`, `TestParseRejectsAmbiguousOrMalformedJSON`, `TestMarshalCanonicalRejectsPreservedFieldCollision`, and `TestCompleteObjectFieldRoundTrip` |
| Public surface | `Extensions`, `NewExtensions`, `parse.UnknownFieldMode`, `parse.Preserving`, `Parsed.Source`, and serialization APIs |
| Upstream record | Extensible object locations and field policy are mapped in the [object-field evidence](../specification/conformance/object-field-evidence.tsv). |
| Reconsider when | A new OpenRPC feature line adds standard members, expands extension points, or defines a general unknown-field compatibility rule. |

## OPENRPC-DEC-009: Canonical and preserving serialization

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openrpc` maintainers |
| Source | OpenRPC 1.4.1 [JSON format and field naming](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/spec-template.md#format), RFC 8785 [JSON Canonicalization Scheme](https://www.rfc-editor.org/rfc/rfc8785), and JSON-RPC 2.0 [data structures](https://www.jsonrpc.org/specification#conventions) |
| Classification | Package-defined deterministic and lossless serialization profiles |
| Issue | OpenRPC requires JSON but does not define canonical byte output. Deterministic hashing and discovery need stable bytes, while editors and proxies may need exact source preservation, including whitespace and unknown fields. One serializer cannot satisfy both contracts implicitly. |
| Credible interpretations | Preserve input bytes everywhere; use ordinary map iteration; claim RFC 8785 conformance; or expose separate canonical typed and exact preserving operations. |
| Known peer behavior | Generic JSON encoders differ in whitespace, escaping, number handling, and key order. RFC 8785 is a distinct profile and is not silently claimed by OpenRPC. |
| Selected behavior | `MarshalCanonical` emits the package's deterministic typed representation with sorted object keys, exact standard field names, exact number lexemes, and no insignificant whitespace. It is not advertised as RFC 8785. Preserving parses retain owned original bytes for exact re-emission. Canonicalization validates collisions and produces bytes that parse back to an equivalent typed document. |
| Security and resource consequences | Canonical output is bounded and detached from caller storage. Collision and malformed-value failures are explicit; no hidden normalization executes extensions or external references. |
| Compatibility and wire consequences | Canonical bytes are stable for semantically identical typed documents under this package version and support ETags. Preserving bytes may differ semantically only insofar as the accepted source contained unknown fields; callers choose the profile explicitly. |
| Executable evidence | `TestMarshalCanonicalIsStableAcrossObjectOrderAndWhitespace`, `TestCanonicalDocumentParsesAgain`, `TestMarshalCanonicalRejectsPreservedFieldCollision`, parser round-trip fuzzing, and [ORPC-1.4-0012 through ORPC-1.4-0014](../specification/conformance/evidence.tsv) |
| Public surface | `MarshalCanonical`, `Parsed.Source`, `parse.Preserving`, discovery `Snapshot.Bytes`, and `Snapshot.ETag` |
| Upstream record | OpenRPC defines JSON shape but no canonical-byte profile; this package profile is documented in the [compatibility contract](compatibility.md) and [API guide](api.md). |
| Reconsider when | OpenRPC adopts a canonicalization profile or interoperability evidence requires a versioned canonical-byte contract. |

## OPENRPC-DEC-010: Composition conflicts, order, and overlays

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openrpc` maintainers |
| Source | OpenRPC 1.4.1 method-name uniqueness and component registries in the [meta-schema](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/schema.json), RFC 7396 [JSON Merge Patch](https://www.rfc-editor.org/rfc/rfc7396), and RFC 6901 [JSON Pointer](https://www.rfc-editor.org/rfc/rfc6901) |
| Classification | Package-defined multi-document composition policy |
| Issue | OpenRPC does not define merging documents, resolving duplicate methods or components, overlay ordering, root metadata ownership, or reference rewriting. Silent map overwrite would make results input-order and implementation dependent. |
| Credible interpretations | Last input always wins; first input always wins; merge object fields recursively; reject every collision; or require callers to choose a finite explicit conflict policy. |
| Known peer behavior | Generic merge tools commonly use order-dependent overwrite or JSON Merge Patch. Those algorithms do not understand OpenRPC method and component identity. |
| Selected behavior | Merge requires equal supported versions, takes root metadata and non-registry fields from the first document, sorts merged methods and references deterministically, and defaults to `ConflictError`. Callers may explicitly choose `KeepFirst` or `KeepLast`. Every component registry and total input count is bounded. Ordered overlays use RFC 7396, and component renames rewrite matching references under explicit collision and traversal limits. |
| Security and resource consequences | Document, method, component, patch, traversal, and cancellation limits prevent composition amplification. Default collision failure prevents attacker-controlled overwrite of contracts. |
| Compatibility and wire consequences | No duplicate is silently selected under defaults. Choosing first/last is an explicit compatibility decision; overlays are order-sensitive by contract, while emitted collections are deterministic. |
| Executable evidence | `TestMergeAppliesExplicitConflictPolicy`, `TestMergeDetectsComponentCollisionsAndBounds`, `TestApplyOverlaysUsesOrderedRFC7396Semantics`, and `TestRenameComponentsRewritesEveryMatchingReference` |
| Public surface | `compose.Merge`, `compose.ConflictPolicy`, `ConflictError`, `KeepFirst`, `KeepLast`, `compose.ApplyOverlays`, and `compose.RenameComponents` |
| Upstream record | Composition is outside OpenRPC 1.4.1; package behavior and limits are documented in the [API guide](api.md) and [evidence report](evidence.md). |
| Reconsider when | OpenRPC standardizes overlays or multi-document composition, or a new component identity rule changes collision semantics. |

## OPENRPC-DEC-011: Semantic diff and unresolved compatibility

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `openrpc` maintainers |
| Source | OpenRPC 1.4.1 [Versions](https://github.com/open-rpc/spec/blob/3a13c7a8bad248e6edd2d48339cd1c06b57f8f22/spec/1.4/spec-template.md#versions), JSON-RPC 2.0 [request and response contracts](https://www.jsonrpc.org/specification), and Semantic Versioning 2.0.0 [compatibility model](https://semver.org/spec/v2.0.0.html) |
| Classification | Package-defined compatibility classification and fail-closed evidence policy |
| Issue | OpenRPC does not define API compatibility. Parameters differ by by-name/by-position semantics, JSON Schema implication is generally undecidable, and unresolved references can conceal contract changes. |
| Credible interpretations | Compare canonical bytes; treat every change as breaking; optimistically ignore unresolved/schema changes; or classify known structural changes while making uncertain cases explicitly conditional. |
| Known peer behavior | API diff tools use different compatibility models and schema approximations. No external classifier is treated as normative for OpenRPC or JSON-RPC consumers. |
| Selected behavior | Diff compares methods, parameter identity/order/requiredness, results, errors, servers, links, examples, components, and feature lines deterministically. Known additive, compatible, conditional, and breaking changes receive stable codes and pointers. Unresolved references and schema changes remain conditional unless `CompareResolved` can compare resolved targets. `Report.Compatible` fails closed on conditional changes, truncation, cancellation, or errors. |
| Security and resource consequences | Method, component, change, resolution, and cancellation limits bound adversarial comparisons. Diagnostics contain stable summaries rather than document payloads. |
| Compatibility and wire consequences | A positive compatibility result means no breaking or conditional finding was observed within a complete bounded comparison. It never means arbitrary JSON Schema implication was proven. Callers can inspect conditional findings rather than receiving false confidence. |
| Executable evidence | `TestCompareClassifiesMethodAndParameterCompatibility`, `TestCompareClassifiesResultsErrorsServersLinksExamplesAndComponents`, `TestCompareResolvedUsesReferenceTargetSemantics`, `TestCompareResolvedPreservesUnresolvedSourcePointers`, and `TestReportCompatibilityAndHelperFailures` |
| Public surface | `diff.Compare`, `diff.CompareResolved`, `diff.Classification`, stable `diff.Code` values, `diff.Report.Changes`, and `diff.Report.Compatible` |
| Upstream record | Compatibility classification is intentionally package-owned and documented in the [compatibility contract](compatibility.md); no OpenRPC 1.4.1 compatibility algorithm exists. |
| Reconsider when | OpenRPC publishes compatibility guidance, JSON Schema publishes a usable implication profile, or interoperability evidence exposes a misclassified consumer break. |

## Unresolved decisions

None. New ambiguities, contradictions, errata, or peer divergences MUST be
added here before behavior is selected. An unresolved wire, validation,
security, or resource-policy decision blocks a release claim for the affected
surface.
