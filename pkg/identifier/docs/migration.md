# Migration

## Laravel and Postal ULIDs

Laravel ULIDs and Postal persistence use the canonical 26-character uppercase
ULID representation. Migrate values as text without lowercasing, decoding,
re-encoding, or generating replacements. Before cutover, stream every stored
value through `ulid.Parse`, assert `parsed.String() == stored`, and compare the
old and new bytewise sort order. Reject rows that depend on a
case-insensitive or locale-specific collation.

For a zero-downtime migration, dual-read the old column, validate in shadow,
then dual-write the identical canonical string. Backfill with an equality
check, add the new unique constraint, switch reads, and retain rollback until
counts, extrema, and ordered samples match. The official ULID vector
`01ARZ3NDEKTSV4RRFFQ69G5FAV` is covered by the compatibility suite.

## Cline Mint and strongly typed IDs

Treat a Mint or `cline/strongly-typed-id` value as an existing wire contract,
not as permission to infer a new family. Export the exact canonical string,
identify whether its payload is UUID, ULID, TypeID, KSUID, or a project-specific
format, and validate it with that parser. Use `identifier.ID[DomainTag]` to add
compile-time domain separation without changing stored text.

Do not convert ULID to UUIDv7 or add a TypeID prefix in place: those create new
identifiers. If a new representation is required, add a new column and retain
an explicit old-to-new mapping. Compare null behavior, JSON shape, SQL type,
case, collation, timestamp extraction, and ordering before switching readers.

## Rollback

Rollback restores readers to the old column; it never attempts to reconstruct
old values from newly generated IDs. Keep the old unique index until every
writer and asynchronous consumer has moved and reconciliation is complete.
