# Conformance evidence

Each row links a primary section and executable evidence. The rows group
closely related normative statements; they do not infer server routing,
persistence, authorization, or business semantics from package tests.

## JSON:API 1.1 core

| Classification | Requirement and primary section | Implementation | Executable evidence |
| --- | --- | --- | --- |
| Core | [Top-level document shape and member exclusivity](https://jsonapi.org/format/#document-top-level) | strict root codec and validator | `codec_test.go`, `validation_test.go`, `presence_test.go` |
| Core | [Resource objects and identity](https://jsonapi.org/format/#document-resource-objects) | presence-aware resources and contextual identity rules | `document_test.go`, `identity_validation_test.go`, `context_validation_test.go` |
| Core | [Resource fields and namespace constraints](https://jsonapi.org/format/#document-resource-object-fields) | field/member validator | `validation_edge_test.go`, `ownership_test.go` |
| Core | [Resource identifier objects](https://jsonapi.org/format/#document-resource-identifier-objects) | identifier codec and validator | `identity_validation_test.go`, `validation_edge_test.go` |
| Core | [Attributes](https://jsonapi.org/format/#document-resource-object-attributes) | exact JSON value codec | `codec_test.go`, `presence_test.go` |
| Core | [Relationships and linkage shapes](https://jsonapi.org/format/#document-resource-object-relationships) | presence-aware relationship sum types | `document_test.go`, `validation_test.go`, `validation_edge_test.go` |
| Core | [Links and link objects](https://jsonapi.org/format/#document-links) | scoped link codec, URI-reference and relation validation | `link_test.go`, `presence_test.go`, `validation_edge_test.go` |
| Core | [Error objects and sources](https://jsonapi.org/format/#error-objects) | error/source codec and HTTP status validation | `document_test.go`, `validation_edge_test.go`, `error_contract_test.go` |
| Core | [JSON:API object](https://jsonapi.org/format/#document-jsonapi-object) | version/ext/profile presence and URI validation | `identity_validation_test.go`, `profile_codec_test.go` |
| Core | [Member-name grammar](https://jsonapi.org/format/#document-member-names) | Unicode-aware semantic and implementation grammar | `validation_edge_test.go`, `query_test.go`, registry fuzz corpus |
| Core | [Forward-compatible @-Members](https://jsonapi.org/format/#document-at-members) | recursive ignore-on-decode and structural invisibility | `codec_defense_test.go`, `member_codec_defense_test.go`, `validation_edge_test.go` |
| Core | [Compound full linkage and uniqueness](https://jsonapi.org/format/#document-compound-documents) | alias-aware identity index and graph traversal | `validation_test.go`, `identity_validation_test.go` |
| Core | [Include paths and response member presence](https://jsonapi.org/format/#fetching-includes) | parsed paths plus `IncludePresent`; response expansion remains application-owned | `query_test.go`, `docs/adoption.md` |
| Core | [Sparse fieldsets](https://jsonapi.org/format/#fetching-sparse-fieldsets) | fieldset parser | `query_test.go` |
| Core | [Sorting](https://jsonapi.org/format/#fetching-sorting) | ordered sort parser; support policy hook | `query_test.go`, `cursor_test.go` |
| Core | [Pagination](https://jsonapi.org/format/#fetching-pagination) | parameter preservation and collection-only link validation | `query_test.go`, `validation_edge_test.go` |
| Core | [Filtering](https://jsonapi.org/format/#fetching-filtering) | family preservation; filter semantics remain application-owned | `query_test.go`, `docs/adoption.md` |
| Core | [Creating resources](https://jsonapi.org/format/#crud-creating) | create validation context | `context_validation_test.go` |
| Core | [Updating resources](https://jsonapi.org/format/#crud-updating) | update and endpoint-identity context | `context_validation_test.go` |
| Core | [Updating relationships](https://jsonapi.org/format/#crud-updating-relationships) | to-one/to-many mutation contexts | `context_validation_test.go` |
| Core | [Media type parameters](https://jsonapi.org/format/#media-type-parameters) | exact `ext`/`profile` parsing and canonical formatting | `negotiation_test.go` |
| Core | [Server content-negotiation responsibilities](https://jsonapi.org/format/#content-negotiation-server-responsibilities) | 415/406 selection with HTTP precedence | `negotiation_test.go`, `negotiation_limits_test.go` |
| Referenced standard | [RFC 8288 relation-type grammar](https://www.rfc-editor.org/rfc/rfc8288.html#section-3.3) | registered token or absolute URI validation | `link_test.go` |
| Referenced standard | [HTTP media-range precedence](https://www.rfc-editor.org/rfc/rfc9110.html#section-12.5.1) | representation-specific quality selection | `negotiation_test.go` |

## Registered extensions and profiles

| Classification | Requirement and primary section | Implementation | Executable evidence |
| --- | --- | --- | --- |
| Core extension mechanism | [Extension rules](https://jsonapi.org/format/#extensions) | absolute unique URIs and namespaces | `member_codec_defense_test.go`, `profile_codec_test.go` |
| Core extension mechanism | [Extension members](https://jsonapi.org/format/#extension-members) | immutable scope-specific registry, including links and link objects | `member_codec_test.go`, `ownership_test.go`, `fuzz_test.go` |
| Core extension mechanism | [Extension media-type negotiation](https://jsonapi.org/format/#content-negotiation-server-responsibilities) | unsupported extensions rejected or ignored by request/response role | `negotiation_test.go` |
| Core profile mechanism | [Profile rules](https://jsonapi.org/format/#profiles) | declaration validation and guarded document semantics | `profile_codec_test.go`, `callback_test.go` |
| Core profile mechanism | [Profile media-type negotiation](https://jsonapi.org/format/#content-negotiation-server-responsibilities) | unknown profiles retained for content type and ignored for Accept selection as required | `negotiation_test.go` |
| Package policy | Registry immutability, callback invocation count, panic containment, profile purity, and redacted causes | copied definitions, representation fingerprinting, and `CallbackError` | `ownership_test.go`, `member_codec_defense_test.go`, `profile_codec_test.go`, `callback_test.go` |

## Atomic Operations extension

| Classification | Requirement and primary section | Implementation | Executable evidence |
| --- | --- | --- | --- |
| Extension | [Document structure](https://jsonapi.org/ext/atomic/#document-structure) | strict operations/results/errors exclusivity | `atomic_codec_test.go`, `atomic_test.go` |
| Extension | [Operation objects](https://jsonapi.org/ext/atomic/#operation-objects) | op, target, data, meta, and member validation | `atomic_test.go` |
| Extension | [Adding resources](https://jsonapi.org/ext/atomic/#adding-resources) | resource-add shape and local identity validation | `atomic_test.go` |
| Extension | [Updating resources](https://jsonapi.org/ext/atomic/#updating-resources) | target/data identity consistency | `atomic_test.go` |
| Extension | [Removing resources](https://jsonapi.org/ext/atomic/#removing-resources) | target-required/data-forbidden validation | `atomic_test.go` |
| Extension | [Adding to relationships](https://jsonapi.org/ext/atomic/#adding-to-relationships) | ref/href to-many shape validation | `atomic_test.go` |
| Extension | [Updating relationships](https://jsonapi.org/ext/atomic/#updating-relationships) | to-one/to-many replacement shapes | `atomic_test.go` |
| Extension | [Removing from relationships](https://jsonapi.org/ext/atomic/#removing-from-relationships) | relationship target and identifier data | `atomic_test.go` |
| Extension | [Local identities](https://jsonapi.org/ext/atomic/#operation-objects) | current/prior add resolution for targets and linkage | `atomic_test.go` |
| Extension | [Result objects](https://jsonapi.org/ext/atomic/#result-objects) | positional, operation-aware result validation including server-generated creates | `atomic_test.go`, `atomic_execute_test.go` |
| Package policy | Transaction orchestration | ordered callbacks, pre-commit result validation, cancellation, panic containment, exactly-one rollback attempt | `atomic_execute_test.go`, `error_contract_test.go` |

## Cursor Pagination profile

| Classification | Requirement and primary section | Implementation | Executable evidence |
| --- | --- | --- | --- |
| Profile | [Query parameters](https://jsonapi.org/profiles/ethanresnick/cursor-pagination/#query-parameters) | `page[size]`, `page[after]`, `page[before]` parser | `cursor_test.go` |
| Profile | [Page size](https://jsonapi.org/profiles/ethanresnick/cursor-pagination/#page-size) | positive/default/maximum policy | `cursor_test.go` |
| Profile | [Cursors](https://jsonapi.org/profiles/ethanresnick/cursor-pagination/#cursors) | opaque values and guarded application validation | `cursor_test.go`, `callback_test.go` |
| Profile | [Sorting requirement](https://jsonapi.org/profiles/ethanresnick/cursor-pagination/#sorting-requirement) | guarded stable/supported-sort hook | `cursor_test.go`, `callback_test.go` |
| Profile | [Links](https://jsonapi.org/profiles/ethanresnick/cursor-pagination/#links) | required prev/next presence, boundary nulls, range state | `cursor_page_test.go`, `cursor_test.go` |
| Profile | [Page metadata](https://jsonapi.org/profiles/ethanresnick/cursor-pagination/#page-meta-object-members) and [item cursors](https://jsonapi.org/profiles/ethanresnick/cursor-pagination/#item-cursors) | alias-aware metadata helpers | `cursor_meta_test.go`, `cursor_page_test.go`, `fuzz_test.go` |
| Profile | [Collection sizes](https://jsonapi.org/profiles/ethanresnick/cursor-pagination/#collection-sizes) | exact total/estimate integer handling | `cursor_meta_test.go` |
| Profile | [Errors](https://jsonapi.org/profiles/ethanresnick/cursor-pagination/#errors) | profile type links, metadata, and redacted causes | `cursor_test.go`, `error_contract_test.go` |

## Package and application policy

| Classification | Policy | Evidence or owner |
| --- | --- | --- |
| Package policy | Reject duplicate members, invalid UTF-8 bytes, trailing JSON, recursive-link cycles, and configured resource-limit excess | `duplicate_member_test.go`, `codec_defense_test.go`, `resource_limits_test.go`, `link_test.go` |
| Package policy | Preserve exact numbers, explicit empty members, deterministic output, and caller-owned data | `codec_test.go`, `presence_test.go`, `ownership_test.go` |
| Package policy | Copy registries at construction and support concurrent codec use | `ownership_test.go` |
| Package policy | Bound decoded queries and negotiation work | `query_limits_test.go`, `negotiation_limits_test.go` |
| Application policy | Bound encoded request targets and request bodies before constructing `url.Values` or `[]byte` | HTTP integration; see `docs/adoption.md` |
| Application policy | Implement persistence, authorization, filter semantics, cursor encoding, URL generation, and transaction correctness | application integration; see `docs/adoption.md` |

## Recommendations

The official [JSON:API recommendations](https://jsonapi.org/recommendations/)
are non-normative. Naming, URL, filtering-strategy, and date/time advice is
tracked separately in [recommendations.md](recommendations.md) and is never a
condition for codec conformance.

## Resilience evidence

- Exact production statement coverage is checked from the raw profile rather
  than a rounded display.
- Nine fuzz targets cover core and Atomic decode, constructed validation,
  member registries, queries, negotiation, Cursor query/metadata, and
  marshal/unmarshal round trips.
- Benchmarks cover representative and adversarial compound documents, Atomic
  batches, queries, negotiation, and pagination metadata.
- Short local fuzz runs are regression evidence, not proof that no parser bug
  exists; scheduled CI continues each target independently.
