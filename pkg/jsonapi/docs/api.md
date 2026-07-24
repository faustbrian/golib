# Public API reference

This guide groups the complete exported surface by purpose. Symbol comments in
the source are the canonical field-level reference and are rendered by
`go doc github.com/faustbrian/golib/pkg/jsonapi`.

## Core documents

| API | Purpose |
| --- | --- |
| `Document` | Top-level core document with `jsonapi`, links, data, included, errors, meta, and registered members |
| `JSONAPI` | Version plus applied extension/profile URIs and meta |
| `ResourceObject` | Resource identity, attributes, relationships, links, meta, and registered members |
| `Identifier` | Resource linkage identity using `type` plus `id` or `lid` |
| `Relationship`, `Relationships` | Relationship links, data, meta, and registered members |
| `Attributes`, `Meta`, `Members` | Application values and registered semantic values |
| `ErrorObject`, `ErrorSource` | JSON:API error representation, valid HTTP status, and pointer/parameter/header source |

Primary-data constructors:

- `NullData()` creates an explicit top-level `data: null`;
- `ResourceData(resource)` creates to-one primary data;
- `ResourceCollection(resources...)` creates collection primary data;
- `NullRelationship()` creates explicit null linkage;
- `ToOne(identifier)` and `ToMany(identifiers...)` create relationship linkage.

Non-empty exported string fields are present automatically. For a valid empty
string that must remain present on the wire, use the corresponding `With...`
method. This applies to resource and identifier `id`/`lid`, JSON:API `version`,
error strings and sources, link object strings, and Atomic targets. The
presence-aware methods avoid treating a Go empty string as an omitted member.

## Core codec and validation

| API | Purpose |
| --- | --- |
| `Marshal`, `Unmarshal` | Strict generic document boundary |
| `MarshalWith`, `UnmarshalWith` | Boundary with `ValidationOptions` |
| `Document.Validate`, `Document.ValidateWith` | Validate a constructed document without encoding |
| `ValidationContext` | Generic, response, create, update, or relationship mutation context |
| `DecodeError` | One parsing/shape failure with JSON pointer and code |
| `ValidationError` | Ordered collection of `Violation` values |
| `Violation` | Path, stable machine code, and human-readable message |
| `CallbackError` | Redacted extension/profile/cursor/sort callback failure with an inspectable phase, cause, or panic value |
| `DecodeLimits`, `DefaultDecodeLimits` | Bounded byte, depth, member, item, and total-value policy |
| `UnmarshalWithLimits` | Core decoding with explicit limits and validation context |

All marshal entry points validate before emitting bytes. All unmarshal entry
points reject duplicate JSON members and validate before returning a document.
`ValidationOptions.ExpectedType` and `ExpectedID` enforce endpoint identity.
Set `ExpectedIDPresent` when the expected ID is the valid empty string;
non-empty expected IDs enable matching without the flag.
Unknown validation contexts are rejected instead of falling back to generic
document validation.

Valid @-Members in an object's `AdditionalMembers` are accepted without
extension registration and emitted after core members. Decoding ignores
@-Members as required by JSON:API. They never satisfy required structural
content, and malformed @-Member names are rejected during strict marshal.

## Links

`Links` maps relation names to the opaque `Link` sum type. Construct links with:

- `URI(href)` for a JSON string;
- `NullLink()` for JSON `null`;
- `ObjectLink(href, meta)` for the common object form;
- `LinkFromObject(LinkObject{...})` for the full JSON:API 1.1 link object;
- `ExtensionLinkValue(value)` for a registered namespaced member directly in
  a `links` object;
- `LanguageTag(tag)` or `LanguageTags(tags...)` for `hreflang`.

Link strings and object `href` values are URI-references, so an empty relative
reference is valid. Values are validated as RFC 3986 wire text: percent-encode
spaces, Unicode, brackets used as path data, and other characters outside the
component grammar. `WithRel`, `WithTitle`, and `WithType` preserve explicitly
empty optional link-object members when their individual grammar allows it.
Constructed recursive `describedby` chains use the package nesting limit and
reject pointer cycles before encoding.

`LinkObject` supports `href`, `rel`, `describedby`, `title`, `type`, `hreflang`,
`meta`, and registered extension members.

`Link.ExtensionValue` distinguishes an extension-defined links-object value
from string, object, and null link values. Such values marshal only through a
`Codec` that registers the member at `LinksObjectMemberScope`.

## Query parsing

Query parsers accept already-decoded `url.Values`. `net/url.ParseQuery` or the
HTTP server owns percent-decoding and rejects malformed escapes before this
package is called. `QueryLimits` bounds decoded names and values; the HTTP
layer must separately bound the encoded request target.

| API | Purpose |
| --- | --- |
| `ParseQuery(url.Values)` | Parse core query parameters with page/filter hooks |
| `NewQueryParser(custom, namespaces)` | Register implementation and extension families |
| `QueryParser.Parse` | Parse with the configured family registry |
| `Query` | Parsed include paths, fieldsets, sort, page, filter, custom, and extension families |
| `SortField` | One ordered field with descending flag |
| `ParameterFamily` | Original decoded names and values for application processing |
| `QueryError` | HTTP 400 query error with parameter and stable code |
| `QueryLimits`, `DefaultQueryLimits` | Bounds decoded names, values, selectors, lists, and aggregate size |
| `NewQueryParserWithLimits`, `ParseQueryWithLimits` | Query parsing with explicit resource limits |

## Content negotiation

| API | Purpose |
| --- | --- |
| `MediaTypeJSONAPI` | `application/vnd.api+json` constant |
| `MediaType` | Canonical extension and profile parameter representation |
| `MediaType.String` | Stable formatted content type |
| `NewNegotiator` | Register supported extension/profile URIs |
| `Negotiator.CheckContentType` | Validate request content type; returns 415 failures |
| `Negotiator.NegotiateAccept` | Select by HTTP quality and media-range precedence; returns 406 failures |
| `NegotiatedMedia` | Selected media type, content type, and `Vary: Accept` requirement |
| `NegotiationError` | Status, code, and message for protocol mapping |
| `NegotiationLimits`, `DefaultNegotiationLimits` | Bounds header, candidate, URI-list, and configuration work |
| `NewNegotiatorWithLimits` | Negotiator construction with explicit limits |

## Registered extensions and profiles

| API | Purpose |
| --- | --- |
| `CodecOptions` | Validation plus extension/profile definitions |
| `NewCodec` | Validate registrations and construct a strict configured codec |
| `Codec.Marshal`, `Codec.Unmarshal` | Encode/decode registered semantic members |
| `ExtensionDefinition` | Absolute URI, namespace, and member definitions |
| `MemberDefinition` | Object scope, namespaced name, and optional value validator |
| `MemberScope` | Top-level, resource, relationship, identifier, JSON:API, error, error source, links object, or link object |
| `ProfileDefinition` | Absolute URI and optional document semantic validator |

Constructors copy registration slices and definitions, so later mutation of
the supplied slices does not alter a codec, negotiator, or query parser.
Constructed instances are safe for concurrent use when application callbacks
and documents are not concurrently mutated.

Extension, profile, cursor, and sort callbacks are invoked through a panic
boundary. Their returned errors and panic values are retained for explicit
`errors.Is`/`errors.As` diagnostics but omitted from public error strings.
Profile validators are observational: a successful callback that mutates its
document view is rejected before marshal or unmarshal can succeed.

## Atomic Operations

| API | Purpose |
| --- | --- |
| `AtomicExtensionURI` | Official extension URI |
| `AtomicDocument` | Operations, results, errors, links, meta, and JSON:API object |
| `AtomicOperation`, `AtomicOperationCode` | Add, update, or remove operation |
| `AtomicReference` | Resource or relationship target by `id`/`lid` |
| `AtomicResult` | Positional operation result |
| `MarshalAtomic`, `UnmarshalAtomic` | Strict generic Atomic codec |
| `MarshalAtomicWith`, `UnmarshalAtomicWith` | Codec with `AtomicValidationOptions` |
| `UnmarshalAtomicWithLimits` | Atomic decoding with explicit resource limits |
| `AtomicDocument.Validate`, `ValidateWith` | Validate constructed Atomic documents |
| `AtomicValidationContext` | Generic, operations request, or results response |
| `ExecuteAtomic` | Ordered transaction execution with rollback/commit guarantees |
| `AtomicTransactionBeginner` | Begins an application transaction |
| `AtomicTransaction` | Applies operations and commits or rolls back |
| `AtomicExecutionError` | Operation and rollback failure context |
| `AtomicPanicError` | Redacted transaction callback panic with inspectable value |

`AtomicOperation.WithHref` and the `AtomicReference.WithID`, `WithLID`, and
`WithRelationship` methods preserve target-member presence independently of
the string value. Atomic validation rejects unknown contexts and negative
expected-result counts as configuration errors.

Set `AtomicValidationOptions.ExpectedOperations` when validating a response
against its request. This enforces positional result count and operation-aware
data rules: removal and recognized relationship results contain no `data`,
while resource result data is a single identified resource object. A create
without a client-generated ID must return that created resource as `data`; a
create with a client-generated ID may omit unchanged representation data.
`ExecuteAtomic` applies this validation before commit. Applications remain
responsible for enforcing the same no-data rule when an href-targeted, to-one
update is only identifiable as a relationship after URI routing.

Because an Atomic `href` may identify either a resource or a relationship,
validation uses unambiguous data shapes: arrays on add/remove and null or
arrays on update are relationship operations; a single update object remains
valid for either a resource or to-one relationship target. The application
still verifies that the URI resolves to the matching endpoint kind.

## Cursor Pagination

| API | Purpose |
| --- | --- |
| `CursorPaginationProfileURI` | Official profile URI |
| `NewCursorPagination` | Validate endpoint pagination policy |
| `CursorPagination.Parse`, `ParseQuery` | Validate page family and optional sort policy |
| `CursorPaginationConfig` | Default/max size, range support, cursor/sort validators |
| `CursorPageRequest` | Parsed size, before/after values, presence, range flag, and metadata member alias |
| `CursorPaginationError` | Profile error with `ErrorObject` conversion |
| `ValidateCursorPaginationLinks` | Require and validate `prev` and `next` links |
| `CursorPage` | Data/links/meta instance validation |
| `CursorPageMeta`, `CursorEstimatedTotal` | Exact/estimated total metadata representation |
| `CursorPageMeta.Meta`, `ParseCursorPageMeta` | Encode/decode pagination metadata using the default `page` element |
| `CursorPageMeta.MetaAs`, `ParseCursorPageMetaAs` | Encode/decode pagination metadata using a profile element alias |
| `CursorItemMeta`, `ParseCursorItemMeta` | Encode/decode optional per-item cursor metadata using `page` |
| `CursorItemMetaAs`, `ParseCursorItemMetaAs` | Encode/decode item metadata using the same alias |

The exported error type-link URI constants are
`CursorUnsupportedSortTypeURI`, `CursorMaxSizeExceededTypeURI`, and
`CursorRangeNotSupportedTypeURI`.

Set `CursorPaginationConfig.PageMember` when the profile's `page` element must
be aliased. The alias is carried by `CursorPageRequest`, used by `CursorPage`
validation, and emitted in max-size error metadata. `CursorPage.HasPrevious`
and `HasNext` provide the boundary evidence needed to enforce required null or
non-null `prev` and `next` links; `HasMore` separately means matching results
were omitted because the used page size was reached.
