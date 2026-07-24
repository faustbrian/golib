# Migration guide

## From `cline/intl`

Replace universal string codes with the matching package type. Move implicit
case repair to explicit `Canonicalize`/`Canonical` calls. Replace locale
truncation with a selected fallback policy. Audit zero and null behavior before
changing database columns.

## From country-list packages

Persist `country.Code`, not a display name. Replace hand-maintained maps with
`Alpha3`, `Numeric`, `Name`, and `DatasetProvenance`. Decide explicitly whether
historic, reserved, or user-assigned values are accepted. When importing such
values from JSON or SQL, use the options-bearing decode methods; the default
interfaces intentionally continue to accept current identifiers only.

## From Brick PhoneNumber or wrappers

Parse using `phone.Parse` with an explicit region hint for national input.
Persist E.164 plus the separate extension through the built-in codecs. Replace
single “valid” booleans with `Possible` and `Valid`, and remove any ownership,
SMS capability, or reachability inference.

## Rollout

Inventory stored values, run a dry parse that records only aggregate counts,
classify rejected obsolete aliases, choose opt-in policies, backfill canonical
representations transactionally, and dual-read before switching writes. Never
log rejected phone or postal values. Pin the package and dataset versions in
the migration record.
