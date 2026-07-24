# OpenRPC and JSON-RPC

`apiqueryrpc.Parse` accepts one strict bounded JSON object, normally using
`schema.Bounds().MaxRequestBytes`. Unknown or duplicate
members, trailing data, malformed UTF-8/JSON, excess nesting, and excess bytes
fail with `ErrInvalid`. `Params` uses pointers so absent values remain distinct
from explicit zero or empty values; `Params.Request` deep-copies slices and
filters.

`OpenRPCContentDescriptor` returns a minimal optional content descriptor named
`query`, with `additionalProperties: false`. The returned map is caller-owned.
Applications should enrich its filter, sort, and page schemas from their static
published contract, not by reflecting domain models or exposing authorization-
specific hidden capabilities.

An RPC method should parse bounded transport input, compile with request policy,
translate `Violations` to its stable application error envelope, execute an
application-owned adapter, and use `cursor.Page` for response boundaries.
OpenRPC generation must not compile or execute a request.
