# Troubleshooting

## A request fails after a schema change

Send the expected schema revision and inspect stable violation codes/paths. A
changed field, sort, bound, default, or cursor contract should use a new
revision. Old cursors must fail rather than be reinterpreted.

## Cursor decoding returns invalid

Check the configured protocol version, retained key ID, 32-byte secret, maximum
encoded bytes, clock, TTL, exact schema revision, exact sort terms including
null placement, traversal direction, `CompileOptions.CursorDecoder`, and replay
policy. Do not log the token or plaintext positions.

## Pagination duplicates or omits rows

Confirm one stable unique tie-breaker, exact mixed-direction seek comparisons,
fixed null ordering, canonical response order for backward pages, and the
documented non-snapshot consistency model. Mutable sort keys can move rows.

## A required field appears in execution but not the response

That is intentional. Required server fields support joins, hydration, cursor
positions, or policy checks. Serialize only `ResponseFields`; adapters load
`ExecutionFields`.

## PostgreSQL compilation rejects a plan

A reviewed mapping is missing or unsafe. Add the declared capability to the
application mapping only after reviewing joins and exposure. Never fall back to
the public name as a database identifier.

## HTTP and RPC produce different plans

Check absent versus explicit empty components, field/include separators, sort
directions, filter JSON types, page defaults, and schema revision. Add both
decoders to `apiquerytest.RunCanonicalConformance`.

## Local `validation` replacement

Before the dependency's implementation commit is published, this repository
uses `replace github.com/faustbrian/golib/pkg/validation => ../validation` for fully
offline development. Release order is: publish `validation`, pin its public
pseudo-version or tag here, remove the replacement, run `make ci`, then tag v1.
