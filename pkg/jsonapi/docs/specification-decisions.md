# JSON:API specification decisions

This register records observable choices where JSON:API 1.1, its official
extensions, profiles, recommendations, or referenced standards permit or
appear to permit more than one implementation. The
[JSON:API 1.1 format](https://jsonapi.org/format/) is authoritative for the
base protocol. Extension, profile, and recommendation authority is stated per
decision rather than merged into the base specification.

Statuses are `resolved`, `unresolved`, or `superseded`. Resolved decisions are
part of the compatibility contract. A change requires specification review,
executable evidence, and a changelog entry.

## JSONAPI-DEC-001: Non-compliant and unrecognized members

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonapi` maintainers |
| Source | JSON:API 1.1 [document structure](https://jsonapi.org/format/#document-structure) and [@-Members](https://jsonapi.org/format/#document-at-members) |
| Classification | Normative requirement and forward-compatibility policy |
| Issue | JSON:API-defined objects MUST NOT contain additional members, but implementations MUST ignore non-compliant members. Rejecting an unknown member is therefore stricter than the protocol and prevents forward compatibility. |
| Credible interpretations | Reject any member not defined for the object; retain unknown members opaquely; or ignore them while continuing to validate all recognized members. |
| Known peer behavior | No maintained peer fixture is pinned yet. The base specification explicitly requires ignore behavior, so peer disagreement cannot override it. |
| Selected behavior | Core and Atomic decoders discard unrecognized and unapplied extension members, including nested object members, and continue validating recognized content. `@`-Members are likewise ignored recursively. Duplicate names, malformed JSON, resource-limit violations, invalid recognized values, and explicitly forbidden known members remain errors. |
| Consequences | New or non-compliant members cannot break an otherwise valid document and are not reproduced on marshal. Applied registered extension members are extracted before the core decoder and retain their defined semantics. |
| Executable evidence | `TestDecodersIgnoreNonCompliantMembers`, `TestAtAndNonCompliantMembersAreIgnored`, and the `TestCoreCodecIgnores*` extension-member cases |
| Public surface | `Unmarshal`, `UnmarshalWith`, `Codec.Unmarshal`, `UnmarshalAtomic` |
| Upstream record | The normative requirement is in the base format; no erratum is needed. |
| Reconsider when | A future JSON:API version changes ignore semantics or defines preservation of unknown members. |

## JSONAPI-DEC-002: Extension namespaces and profile authority

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonapi` maintainers |
| Source | JSON:API 1.1 [Extensions](https://jsonapi.org/format/#extensions), [Extension Members](https://jsonapi.org/format/#extension-members), [Profiles](https://jsonapi.org/format/#profiles), and [Content Negotiation](https://jsonapi.org/format/#content-negotiation) |
| Classification | Normative interpretation and package policy |
| Issue | Extensions may add namespaced semantics, while profiles may define implementation semantics but cannot alter or remove base rules. Codec registration and HTTP declaration are separate boundaries. |
| Credible interpretations | Infer semantics from any colon member; apply every declared URI dynamically; or require explicit immutable registration for semantic processing while ignoring unapplied content. |
| Known peer behavior | No maintained peer matrix is pinned across object scopes. Official Atomic and Cursor artifacts are the current independent authorities. |
| Selected behavior | An extension URI, namespace, object scope, member name, and optional validator must be registered before the member becomes semantic content. Profiles use document-level validators only after base validation and cannot mutate or weaken the validated document. Codec and negotiator registration are explicit and must use the same supported URI policy. |
| Consequences | Unknown extension members are ignored under JSONAPI-DEC-001. Registered members round-trip by exact scope. Profile callbacks cannot convert an invalid core document into a valid one. Unsupported request extensions fail negotiation; unknown profiles remain declarations without package-owned semantics. |
| Executable evidence | `TestNewCodecRejectsInvalidExtensionDefinitions`, `TestCodecRoundTripsRegisteredResourceExtensionMember`, scope-specific member codec tests, `TestCodecAppliesRegisteredProfileDocumentValidation`, `TestProfileValidatorCannotMutateValidatedDocument`, negotiation tests |
| Public surface | `NewCodec`, `ExtensionDefinition`, `MemberDefinition`, `ProfileDefinition`, `NewNegotiator` |
| Upstream record | Official extension and profile registries are linked from `docs/extensions-and-profiles.md`. |
| Reconsider when | JSON:API changes extension/profile authority or introduces a discovery mechanism that can be applied safely without implicit semantics. |

## JSONAPI-DEC-003: Query parameter families

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonapi` maintainers |
| Source | JSON:API 1.1 [Fetching Data](https://jsonapi.org/format/#fetching), [Implementation-Specific Query Parameters](https://jsonapi.org/format/#query-parameters-custom), and [Extension Query Parameters](https://jsonapi.org/format/#query-parameters-custom) |
| Classification | Normative interpretation and application seam |
| Issue | The specification defines `include`, `fields`, `sort`, `page`, and `filter` families but leaves most filter and pagination semantics implementation-owned. Unknown families must not silently acquire meaning. |
| Credible interpretations | Preserve every query parameter; reject everything outside core names; or accept explicitly registered implementation and extension families. |
| Known peer behavior | No maintained router or server peer is pinned. The package compares behavior to the authoritative query grammar rather than one framework's routing conventions. |
| Selected behavior | Parse core families with presence and ordering preserved. Accept implementation-specific and extension families only through explicit registration and valid name grammar. Reject malformed, unregistered, excessive, or semantically invalid parameters with a structured query error. Filtering, URL length, authorization, and backend execution remain application-owned. |
| Consequences | Applications can support custom operators without making them implicit global behavior. Explicit empty values remain distinguishable from absence. Parser resource use is bounded. |
| Executable evidence | `TestParseQueryParameters`, `TestParseQueryPreservesExplicitEmptyIncludeAndSort`, `TestQueryParserAcceptsRegisteredCustomAndExtensionFamilies`, `TestParseQueryRejectsMalformedOrUnknownParameters`, query-limit tests, `FuzzParseQuery` |
| Public surface | `ParseQuery`, `NewQueryParser`, `QueryParser.Parse` |
| Upstream record | No standard filter operator vocabulary exists in JSON:API 1.1. |
| Reconsider when | A base revision or applied extension standardizes a currently application-owned family. |

## JSONAPI-DEC-004: Relationship objects and linkage

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonapi` maintainers |
| Source | JSON:API 1.1 [Relationships](https://jsonapi.org/format/#document-resource-object-relationships), [Resource Linkage](https://jsonapi.org/format/#document-resource-object-linkage), and CRUD relationship rules |
| Classification | Normative interpretation |
| Issue | Relationship objects can carry links, data, meta, and applied extension members. Linkage has null, to-one, and to-many shapes, but does not itself authorize fetching or mutation. |
| Credible interpretations | Require `data` for every relationship; treat an empty object as meaningful; or accept any qualifying normative member while preserving exact linkage shape. |
| Known peer behavior | No maintained peer fixture is pinned across every create, update, and relationship endpoint context. |
| Selected behavior | A relationship must contain at least one qualifying recognized or applied extension member. When `data` is present, preserve null, one identifier, or identifier collection exactly. Context-specific validators enforce create, update, and relationship endpoint shapes; the package does not infer authorization or persistence behavior. |
| Consequences | An object containing only ignored non-compliant members remains invalid after those members are discarded. Explicit empty to-many linkage remains distinct from absent linkage. |
| Executable evidence | `TestMarshalRelationshipDataShapes`, `TestValidateRelationshipRequestContexts`, `TestRelationshipIdentifierTraversalClassifiesEveryShape`, `TestValidateAcceptsToManyCompoundLinkage` |
| Public surface | `Relationship`, `RelationshipData`, validation contexts |
| Upstream record | No known erratum changes the relationship object minimum-member rule. |
| Reconsider when | JSON:API changes relationship object membership or endpoint validation requirements. |

## JSONAPI-DEC-005: Included-resource uniqueness and full linkage

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonapi` maintainers |
| Source | JSON:API 1.1 [Compound Documents](https://jsonapi.org/format/#document-compound-documents) and [Sparse Fieldsets](https://jsonapi.org/format/#fetching-sparse-fieldsets) |
| Classification | Normative interpretation |
| Issue | Included resources must be unique and fully linked, but identity may use `id` or local `lid`, and sparse fieldsets may remove relationship fields that would otherwise prove linkage. |
| Credible interpretations | Compare only type plus server ID; treat local identities separately; or build one alias-aware identity graph and apply the specified sparse-fieldset exception. |
| Known peer behavior | No maintained peer graph implementation is pinned. Official examples and the package's identity fixtures are the current evidence. |
| Selected behavior | Build an alias-aware identity index from type plus `id` or `lid`, reject duplicate represented resources, and traverse primary and included relationship linkage. Apply the full-linkage exception only when the relevant relationship field was excluded by a sparse fieldset. |
| Consequences | A resource cannot appear twice through alternate identity aliases. Unreachable included resources fail unless the precise sparse-fieldset exception applies. |
| Executable evidence | `TestValidateLinksIncludedResourceThroughItsLocalIdentity`, `TestIncludedIdentityFollowsValidationContext`, `TestValidateWithAllowsSparseFieldsetFullLinkageException`, identity and validation tests |
| Public surface | `Document.ValidateWith`, `ValidationOptions` |
| Upstream record | No known erratum resolves aliasing beyond the base `id` and `lid` rules. |
| Reconsider when | A future revision changes local identity or compound-document linkage semantics. |

## JSONAPI-DEC-006: Sparse fieldsets

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonapi` maintainers |
| Source | JSON:API 1.1 [Sparse Fieldsets](https://jsonapi.org/format/#fetching-sparse-fieldsets) |
| Classification | Normative parsing and application policy |
| Issue | The query grammar is standardized, but selecting fields from application resources and enforcing authorization require schema knowledge outside the document codec. |
| Credible interpretations | Apply fieldsets directly to generic maps; parse only and leave projection to applications; or reject fieldsets without a registered schema. |
| Known peer behavior | Framework peers commonly combine parsing and serialization, but no equivalent-work peer fixture is pinned for this transport-neutral package. |
| Selected behavior | Parse `fields[type]` into ordered explicit field names and preserve empty presence. Applications own schema validation, authorization, and projection. Validation receives fieldset context only for the normative full-linkage exception. |
| Consequences | The package does not expose fields accidentally or invent resource schemas. Callers must apply the parsed fieldset before constructing a response. |
| Executable evidence | `TestParseQueryParameters`, `TestParseQueryPreservesExplicitEmptyIncludeAndSort`, `TestValidateWithAllowsSparseFieldsetFullLinkageException` |
| Public surface | `Query.Fieldsets`, `QueryParser`, `ValidationOptions` |
| Upstream record | JSON:API does not define an application schema registry. |
| Reconsider when | A separate typed schema integration supplies a generic authorization-safe projection contract. |

## JSONAPI-DEC-007: Pagination parameters and links

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonapi` maintainers |
| Source | JSON:API 1.1 [Pagination](https://jsonapi.org/format/#fetching-pagination) and the [Cursor Pagination profile](http://jsonapi.org/profiles/ethanresnick/cursor-pagination/) |
| Classification | Base implementation policy plus profile-specific normative behavior |
| Issue | Base JSON:API reserves the `page` family but leaves strategy and parameter names implementation-owned. The Cursor profile defines a concrete strategy, links, metadata, and errors. |
| Credible interpretations | Impose offset pagination globally; preserve opaque page parameters; or activate concrete rules only under an applied profile. |
| Known peer behavior | No maintained peer fixture is pinned. The official Cursor profile examples and URI are treated as the profile authority. |
| Selected behavior | Preserve generic base `page[...]` parameters without inventing offset semantics. Apply `page[size]`, `page[after]`, `page[before]`, stable sort, directional links, metadata, and error rules only through the Cursor profile API. Pagination links are valid only for collection primary data, and URL construction remains caller-owned. |
| Consequences | Base users can choose a pagination model. Cursor users receive strict bounded profile behavior without changing base conformance. |
| Executable evidence | `TestParseQueryParameters`, `TestPaginationLinksRequireCollectionData`, `TestCursorPaginationParsesProfileParameters`, `TestValidateCursorPaginationLinks`, cursor page and metadata tests, `FuzzCursorPaginationQuery` |
| Public surface | `Query.Page`, cursor parser, page validation, cursor metadata helpers |
| Upstream record | The profile URI intentionally uses HTTP because that is the URI published by the profile. |
| Reconsider when | JSON:API standardizes base pagination semantics or the Cursor profile publishes a compatible revision. |

## JSONAPI-DEC-008: Atomic operation order and rollback

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonapi` maintainers |
| Source | Atomic Operations [Processing](https://jsonapi.org/ext/atomic/#processing) and [Operation Objects](https://jsonapi.org/ext/atomic/#operation-objects) |
| Classification | Extension requirement and transaction-boundary policy |
| Issue | Operations must be processed in order and as one atomic unit, but a reusable package cannot create an application database transaction itself. Commit ambiguity, callback panic, cancellation, and invalid results must not leak partial success. |
| Credible interpretations | Validate document shape only; execute callbacks without transaction control; or require an explicit caller-supplied transaction lifecycle. |
| Known peer behavior | No maintained cross-language executor fixture is pinned; the extension's processing rules are the authoritative behavior. |
| Selected behavior | Validate before beginning. Use a caller-supplied transaction boundary, apply operations sequentially, validate positional results before commit, stop at the first failure or cancellation, and attempt rollback exactly once after any post-begin failure. Convert callback panics to bounded errors and preserve rollback failure without hiding the primary failure. |
| Consequences | The package owns orchestration semantics but not database durability. Applications must provide transaction methods whose commit and rollback contracts match their datastore. |
| Executable evidence | `TestExecuteAtomicAppliesInOrderAndCommits`, `TestExecuteAtomicRollsBackAtFirstOperationFailure`, commit/rollback failure tests, panic and cancellation tests, invalid-result rollback tests |
| Public surface | `ExecuteAtomic`, Atomic transaction and operation callback interfaces |
| Upstream record | Atomic Operations remains an official JSON:API extension rather than base format text. |
| Reconsider when | The extension revises processing order, result correspondence, or atomicity requirements. |

## JSONAPI-DEC-009: Recommendation status

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonapi` maintainers |
| Source | Official [JSON:API Recommendations](https://jsonapi.org/recommendations/) |
| Classification | Non-normative guidance |
| Issue | Recommendations describe useful naming, URL, filtering, date/time, asynchronous, and method-override conventions but are not conformance requirements. |
| Credible interpretations | Enforce recommendations as protocol rules; ignore them entirely; or document adoption guidance while keeping validation normative. |
| Known peer behavior | Frameworks commonly enforce local URL and naming conventions, but no such convention is portable JSON:API conformance evidence. |
| Selected behavior | Document and use recommendations in examples where useful, but never reject a base-compliant document solely for violating a recommendation. Routing, pluralization, filters, timestamps, asynchronous resources, and method override remain application or middleware policy. |
| Consequences | Conformance claims do not inflate SHOULD-like advice into MUST behavior. Applications can adopt stricter conventions explicitly. |
| Executable evidence | `docs/recommendations.md`, query parser tests proving filter-family preservation, document models supporting recommended links without requiring them |
| Public surface | Documentation and examples; no recommendation-only validator |
| Upstream record | The recommendations page explicitly remains separate from the normative format. |
| Reconsider when | A recommendation is promoted into normative base, extension, or profile text. |

## JSONAPI-DEC-010: Conflicts between authorities

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonapi` maintainers |
| Source | JSON:API 1.1 [Extensions](https://jsonapi.org/format/#extensions), [Profiles](https://jsonapi.org/format/#profiles), Atomic Operations, Cursor Pagination, and Recommendations |
| Classification | Normative source hierarchy and compatibility policy |
| Issue | Base text, an extension, a profile, recommendations, examples, and application conventions have different authority. Merging them can silently weaken base rules or mislabel policy as conformance. |
| Credible interpretations | Let the most specific artifact override base behavior; treat all official pages as equally normative; or apply each artifact only within its declared authority. |
| Known peer behavior | No peer vote determines authority. Differential disagreement must be classified against the governing artifact. |
| Selected behavior | Base normative text always applies. An applied extension may add specification semantics only within its namespace and declared scope. A profile may add implementation semantics but cannot alter base or extension rules. Recommendations and examples are informative. Application conventions cannot be reported as JSON:API requirements. A direct unresolved contradiction blocks release rather than being silently prioritized. |
| Consequences | Conformance matrices label base, extension, profile, recommendation, referenced-standard, package-policy, and application-policy evidence separately. Compatibility review is required when a decision moves between categories. |
| Executable evidence | `docs/conformance.md`, `docs/extensions-and-profiles.md`, `docs/recommendations.md`, profile-after-core validation tests, Atomic and Cursor conformance tests |
| Public surface | All parsing, validation, negotiation, extension, profile, and documentation claims |
| Upstream record | No unresolved direct contradiction is currently recorded. |
| Reconsider when | A governing artifact publishes an erratum or revision that changes its authority or conflicts with another applied artifact. |

## Unresolved decisions

No known normative interpretation is unresolved at this revision. Independent
peer fixtures remain incomplete where noted and must be added before
repository-wide interoperability completion can be claimed. New ambiguities
remain unresolved until they receive a stable identifier, authority analysis,
executable evidence, and maintainer disposition here.
