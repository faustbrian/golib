# Migration guide

## Locale-keyed Go maps

1. Inventory whether keys contain underscores, aliases, invalid tags, or
   canonical duplicates.
2. Decide duplicate and `und`/`mul`/private-use policy explicitly.
3. Construct with `TextFromMap` or `NewTextWithOptions` at ingress.
4. Replace direct indexing with `Get` and preserve the presence boolean.
5. Add matching or fallback only at call sites that previously promised it.
6. Persist `EncodeJSON` output and compare semantic round trips before deleting
   legacy code.

## Spatie Translatable JSON

Spatie objects map locale strings to strings and commonly contain
present-empty values. The fixture `postgres/testdata/spatie-translatable.json`
is accepted without semantic drift. PHP locale aliases or underscore keys MUST
be audited; strict v1 decode rejects underscores, while `PermissiveJSON` can be
used only as an explicit migration bridge before canonical re-encoding.

## Track

Treat Track display text as a domain value, not a translation catalog. Decode
the existing JSON column strictly, report canonical duplicates, write canonical
objects in a shadow or audited migration, and switch readers only after
round-trip comparison. The representative `track.json` fixture includes a
regional Swedish tag.

## Postal

Postal names are content; postal-code and locality lookup behavior remains in
Postal. The `postal.json` fixture proves Finnish and Swedish text. Do not infer a
fallback from country or postal code in this package.

## Location

Location pickup-point names may contain regional tags. The `location.json`
fixture preserves `fi-FI`. Coordinate, carrier, and nearest-location selection
remain outside this package.

## Normalized rows

Use `postgres.Rows` and `postgres.FromRows` to bridge `(entity_id, locale,
text)` schemas. They do not create tables or run migrations. Order rows by
entity and canonical locale for reproducible comparisons. Example extraction
SQL is in `postgres/testdata/normalized-rows.sql`.

## Rollout checks

- compare locale count, canonical key set, present-empty set, and hash;
- classify every rejected key before changing data;
- dual-read without silently preferring one representation;
- never materialize fallback results into stored translations;
- retain rollback data until semantic counts match.
