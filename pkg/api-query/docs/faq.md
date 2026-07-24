# Frequently asked questions

## Is this an ORM or query builder?

No. It validates an API query contract. Applications still own repositories,
SQL/SQLC variants, execution, transactions, joins, and scanning.

## Does it implement JSON:API query semantics?

No. `jsonapi` is authoritative. The bridge only composes its parsed result
with an explicitly configured general application contract.

## Can clients send arbitrary fields, SQL, regex, or functions?

No. Only declared names, types, operators, and bounded logical composition can
enter a plan. PostgreSQL identifiers come solely from server mappings.

## Why are execution and response fields different?

An adapter may need keys or cursor positions that must not be serialized.
Required execution fields preserve correctness without authorizing disclosure.

## Are costs database estimates?

No. They are conservative stable API weights used to reject known expensive
shapes before persistence.

## Are cursor pages snapshot-consistent?

Not by default. Seek pagination has the consistency behavior documented in
`SECURITY.md`. Add a protected snapshot constraint when fixed results are
required.

## Can offset and cursor pagination coexist?

Yes, as separate declared capabilities. Offset limits and consistency are
documented independently; offset state cannot be mixed with cursors.
