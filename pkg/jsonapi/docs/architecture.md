# Architecture

## Layers

The package separates protocol responsibilities into five explicit layers.

| Layer | Responsibility | Main API |
| --- | --- | --- |
| Model | Represent JSON:API values without domain reflection | `Document`, `ResourceObject`, `Relationship`, `Link` |
| Codec | Strict deterministic JSON boundaries | `Marshal`, `Unmarshal`, `Codec` |
| Validation | Enforce structural and contextual invariants | `Validate`, `ValidationOptions`, typed errors |
| Request protocol | Parse query families and negotiate media types | `QueryParser`, `Negotiator` |
| Official semantics | Atomic execution and Cursor Pagination policy | `ExecuteAtomic`, `CursorPagination`, profile metadata |

The core performs no network, database, router, ORM, or background work.

## Explicit model instead of domain reflection

Callers map domain values to `ResourceObject` themselves. This keeps resource
type, identity, linkage, omission, and null semantics visible in code. Pointer
wrappers such as `PrimaryData` and `RelationshipData` preserve the difference
between an absent member, JSON `null`, one resource, and a collection.

Custom `MarshalJSON` methods preserve explicitly empty arrays and objects while
still producing stable object-member ordering. Arbitrary attribute and meta
values are supported without using reflection to infer JSON:API structure.

## Decode pipeline

Core decoding proceeds in this order:

1. verify valid JSON and reject duplicate object members;
2. decode JSON:API-defined objects while rejecting unknown members;
3. ignore `@`-members where forward-compatible processing requires it;
4. preserve JSON numbers with `json.Number`;
5. validate document shape, identities, linkage, links, and context;
6. return typed `DecodeError` or `ValidationError` values.

A configured `Codec` first extracts registered extension members from their
declared scopes, sanitizes the core document, runs the same core decoder, then
reattaches semantic extension values. Profile validators run after core
validation.

## Validation contexts

The same resource shape has different requirements in create, update,
relationship, and generic documents. `ValidationOptions` makes that context an
explicit input rather than silently guessing from an HTTP method or URL.

Atomic validation follows the same pattern with `AtomicValidationOptions` and
request, response, and generic contexts.

## Extension boundary

JSON:API extensions may add namespaced members; profiles may add
implementation semantics but cannot alter base semantics. `NewCodec` encodes
that distinction:

- `ExtensionDefinition` registers an absolute URI, namespace, member scopes,
  and optional value validators;
- `ProfileDefinition` registers an absolute URI and optional whole-document
  semantic validator;
- unknown extension members remain invalid core members;
- application behavior outside these seams remains the caller's concern.

## Atomic execution boundary

`ExecuteAtomic` owns ordering, result correspondence, rollback, and commit
behavior. The application provides `AtomicTransactionBeginner` and
`AtomicTransaction`, which map each operation to its persistence semantics.
This is intentionally narrower than an ORM adapter and makes transactional
ownership explicit.

## Cursor Pagination boundary

The profile helpers validate query values, link presence, metadata shapes,
error contracts, and endpoint policy. The application still owns cursor
encoding, stable database ordering, data retrieval, and deciding whether a
next or previous page exists.

## Error model

- `DecodeError` identifies JSON pointer, failure code, message, and optional
  underlying parser error.
- `ValidationError` aggregates deterministic `Violation` values.
- `QueryError`, `NegotiationError`, and `CursorPaginationError` include the
  protocol HTTP status expected at their boundary.
- `AtomicExecutionError` preserves operation index, source pointer, cause, and
  rollback failure when both fail.

The package does not write HTTP responses because applications differ in
logging, localization, error detail, and status policy.
