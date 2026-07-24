# API reference

## Schema declarations

`SchemaConfig` declares `Resource`, semantic `Revision`, fields, filters, sorts,
relationships, default sorts, allowed logical operators, pagination, and hard
`Bounds`. `NewSchema` validates and defensively copies it. Caller mutation after
construction cannot alter the schema.

`FieldDefinition` controls name, type, default response membership, required
execution membership, deprecation, and cost. Required fields may be omitted
from `Plan.ResponseFields` but always remain in `Plan.ExecutionFields`.

`RelationshipDefinition` is an explicit resource edge with nested edges and
cost. Paths use dots. Schema construction rejects duplicate names and resource
cycles; compilation enforces depth, count, and authorization on every traversed
edge.

`FilterDefinition` declares one type and exact operator set plus protected,
deprecated, nullable, empty-string, and cost behavior. Supported operators are
`eq`, `neq`, `lt`, `lte`, `gt`, `gte`, `in`, `not_in`, `between`, `is_null`,
`contains`, `starts_with`, and `ends_with`. Operators remain type-checked.
Logical `and`, `or`, and unary `not` work only when declared.

`SortDefinition` declares type, fixed null placement, cost, and whether it is
the single stable tie-breaker. Cursor schemas require exactly one tie-breaker.
Every cursor plan appends it when absent, after authorization.

`PaginationDefinition` independently enables cursor and offset modes, requires
a bounded default page size, and caps offsets. Offset mode has no cursor
stability claim.

Zero `Bounds` receive conservative nonzero defaults. Positive declarations can
only narrow them; negative bounds invalidate the schema. `Schema.Bounds`
returns the normalized copy so transport adapters can use `MaxRequestBytes`.
Cost is a deterministic schema weight with overflow-safe aggregation, not a
database optimizer estimate.

## Requests and values

`Request` preserves absent versus explicit empty fields, includes, sorts, and
schema revision with `Optional[T]`, constructed by `Present`. `FilterExpr` is
either one `Predicate` or one bounded logical group. `PageRequest` selects
`none`, `cursor`, or `offset`; `after` and `before` conflict.

`Value` is a closed canonical type. Constructors are `StringValue`, `IntValue`,
`UintValue`, finite `FloatValue`, `BoolValue`, UTC `TimeValue`, `BytesValue`, and
`NullValue`. Null is valid for cursor positions or mandatory values but is not a
schema field type or typed filter value. JSON contains an explicit type and
canonical string representation.

## Compilation and plans

`Compile(ctx, schema, request, options)` returns either a reviewed immutable
`Plan` or `*Violations`. `CompileOptions.Authorize` receives only a declared
`Capability{Kind, Name}`. `MandatoryConstraints` remain a separate server-owned
list. `CursorDecoder` is required whenever an after/before token is present; the
core verifies returned direction, position count/types, and bounds. A nil
authorizer allows declared capabilities; applications needing authorization
should always provide one.

Plan accessors return defensive copies: resource and revision, response and
execution fields, includes, deep-cloned filter, mandatory constraints, ordered
sorts, page request, and cost. `Canonical` returns deterministic bounded JSON
for cache keys, signatures, conformance, or equality. Randomized raw cursor
tokens never enter a successful plan or canonical output; canonical pages use a
digest of authenticated `CursorState`. `Plan.Cursor` returns a defensive copy
for seek compilation. It is not an execution format and contains protected
constraints; store it accordingly.

## Errors

`Violations.Items` returns defensive copies with stable `Code`, `Path`, and safe
`Message`. Codes are `invalid_element`, `unsupported_operation`, `conflict`,
`authorization_rejected`, `cost_limit`, `cursor_failure`, `version_mismatch`,
and `limit_exceeded`. Paths identify request structure, never raw values.
Collectors stop at `Bounds.MaxErrors`.

Transport parsers expose one sanitized sentinel error. Cursor decoding exposes
`ErrInvalid`, `ErrExpired`, `ErrVersion`, `ErrSchema`, `ErrSort`, and `ErrReplay`
without payload content. PostgreSQL and JSON:API bridges fail with sanitized
adapter errors. `apiqueryvalidation.Report` converts query violations into an
immutable `validation` report and sanitizes unrelated errors.

## Adapter packages

- `apiqueryhttp.Parse` strictly parses one bounded raw query string.
- `apiqueryrpc.Parse` strictly parses bounded parameters; `Params.Request`
  returns a defensive request; `OpenRPCContentDescriptor` returns fresh maps.
- `apiqueryjsonapi.FromQuery` consumes a query parsed by `jsonapi`; callbacks
  explicitly own filter and pagination profile semantics.
- `apiquerypgx.NewCompiler` snapshots allowlisted mappings. `Compile` returns
  projection, where, order, and typed positional arguments, never execution.
- `cursor.NewKeyring`, `Rotate`, `NewCodec`, `Encode`, `Decode`, and `BuildPage`
  implement the cursor protocol and response envelope.
- `apiquerytest` provides schema/request builders, `MustCompile`, violation and
  canonical assertions, an order fixture, and cross-decoder conformance suite.
