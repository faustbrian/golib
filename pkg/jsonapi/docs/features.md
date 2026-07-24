# Supported features

Status meanings:

- **Implemented**: modeled, validated, encoded/decoded, and covered where the
  concern belongs to this package.
- **Integration responsibility**: required server behavior exposed through an
  explicit primitive; the application must connect it to HTTP or persistence.
- **Planned after v1**: intentionally outside the first stable core.
- **Out of scope**: intentionally not a protocol-core responsibility.

## JSON:API 1.1 core

| Capability | Status | Package surface |
| --- | --- | --- |
| Top-level document and member exclusivity | Implemented | `Document`, validation |
| Resource objects and identifiers | Implemented | `ResourceObject`, `Identifier` |
| Attributes, relationships, links, meta | Implemented | Typed models and strict codecs |
| Error documents and sources | Implemented | `ErrorObject`, `ErrorSource` |
| Compound documents and full linkage | Implemented | included-resource validation |
| Local identifiers | Implemented | `LID`, identity validation |
| Null, to-one, and to-many data presence | Implemented | explicit constructors and round trips |
| Duplicate and unknown member rejection | Implemented | strict decoder |
| `@`-member forward compatibility | Implemented | recursive stripping in defined containers |
| JSON:API 1.1 link objects | Implemented | `LinkObject`, nested `describedby`, `hreflang` |
| Extension/profile declarations | Implemented | `JSONAPI.Ext`, `JSONAPI.Profile`, URI validation |
| Extension-defined members | Implemented | registered `Codec` scopes, including links and link objects |
| Profile implementation semantics | Implemented | `ProfileDefinition.ValidateDocument` |
| Callback panic containment and redaction | Implemented | `CallbackError` and guarded validators/hooks |
| Sparse fieldset parsing | Implemented | `Query.Fields` |
| Include path parsing | Implemented | `Query.Include` |
| Sort parsing | Implemented | `Query.Sort` |
| Page/filter parameter hooks | Implemented | `ParameterFamily` |
| Implementation query families | Implemented | `NewQueryParser` |
| Extension query namespaces | Implemented | `NewQueryParser` |
| Request content type rules | Implemented | `Negotiator.CheckContentType` |
| `Accept` selection and quality values | Implemented | `Negotiator.NegotiateAccept` |
| HTTP method/status and endpoint routing | Integration responsibility | caller maps typed results to `net/http` |
| Fetching, mutation, and relationship persistence | Integration responsibility | caller owns handlers and storage |
| Filtering semantics | Integration responsibility | raw family deliberately preserved |

## Official Atomic Operations extension

| Capability | Status |
| --- | --- |
| Official URI and namespaced document members | Implemented |
| Operations/results/errors document exclusivity | Implemented |
| Add, update, and remove operation shapes | Implemented |
| `ref`/`href` targeting and local identities | Implemented |
| Resource and relationship operation data | Implemented |
| Positional result validation | Implemented |
| Ordered execution | Implemented |
| All-or-nothing transaction orchestration | Implemented |
| Commit and rollback error preservation | Implemented |
| HTTP `POST`, 200/204, and application status policy | Integration responsibility |
| Mapping operations to storage behavior | Integration responsibility |

## Official Cursor Pagination profile

| Capability | Status |
| --- | --- |
| Official profile URI | Implemented |
| `page[size]`, `page[after]`, `page[before]` | Implemented |
| Positive/default/maximum size rules | Implemented |
| Range pagination policy | Implemented |
| Cursor validation hook | Implemented |
| Stable/unsupported sort hook | Implemented |
| Required `prev` and `next` links | Implemented |
| Per-item cursor metadata | Implemented |
| Exact total and estimated total metadata | Implemented |
| Required profile error objects and type links | Implemented |
| Cursor encoding and database keyset query | Integration responsibility |
| Deciding page existence and building URLs | Integration responsibility |

## Optional future packages

| Capability | Status |
| --- | --- |
| OpenAPI/schema generation | Planned after v1 |
| Client helpers | Planned after v1 |
| Router middleware | Planned after v1, outside core package |
| Resource code generation | Planned after v1 |
| ORM integration | Out of scope |
| Authentication and authorization | Out of scope |
| Project-specific filtering language | Out of scope |
