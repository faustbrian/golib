# Troubleshooting

## `ErrDuplicateLocale`

Two raw keys canonicalized to the same tag, such as `EN-us` and `en-US`.
Inventory and choose an explicit migration winner; do not silently discard one
in strict decode.

## `ErrInvalidLocale`

Strict string boundaries reject malformed tags, underscores, and whitespace.
Use permissive JSON only for an audited legacy migration, then store canonical
output.

## `ErrLimitExceeded`

Identify whether input bytes, locale count, tag bytes, per-text bytes, total
bytes, or merge output exceeded the configured budget. Package errors omit the
content deliberately.

## A match selected an unexpected English region

Matcher behavior comes from the pinned locale dependency and supported locale
set. Assert result kind and selected canonical tag. Use configured fallback when
the application requires a specific regional chain.

## JSON `null` fails

Strict localized JSON requires an object. Use a nullable SQL/config wrapper for
absence, or explicit `PermissiveJSON` only at a legacy boundary.

## PostgreSQL scan fails

Register `postgres.JSONBCodec()` on the pgx connection type map and confirm the
column is JSON/JSONB text containing only string values. Use `postgres.Text` to
distinguish SQL NULL.

## Hosted CI is unavailable

Continue locally. `make check`, `make mutation`, and a disposable PostgreSQL
matrix provide the implementation evidence. Hosted CI is a final publication
verification, not a development blocker.
