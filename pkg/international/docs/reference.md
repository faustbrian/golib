# API and standards reference

| Package | Identity/standard | Default acceptance |
|---|---|---|
| `country` | ISO 3166-1 alpha-2/alpha-3/numeric | distinct current types |
| `subdivision` | ISO 3166-2-derived CLDR identifiers | current records |
| `language` | ISO 639 plus IANA BCP 47 data | canonical ISO 639-1 or ISO 639-3-only identifiers |
| `locale` | BCP 47 | bounded registry-aware tags |
| `currency` | ISO 4217 alphabetic/numeric | distinct active types |
| `phone` | E.164 and libphonenumber metadata | bounded parseable numbers |
| `postal` | country-contextual opaque value | bounded printable UTF-8 |

`Parse` validates a representation without changing it. APIs named
`Canonicalize`, `Canonical`, or `Normalize` perform explicit transformations.
Display names and phone formatting are presentation metadata, never identity.

Every scalar implements strict text, JSON, `database/sql.Scanner`, and
`driver.Valuer` contracts. Zero means absent: text marshal fails, JSON emits
`null`, SQL emits `NULL`, and decoding null resets the destination. Failed
decodes leave it unchanged. Nullable database columns may therefore use either
the zero value or an application-owned presence wrapper when absent and
explicit zero must be distinguished.

`Status` distinguishes unknown, official, reserved, transitional, deleted,
user-assigned, and historic records. Non-current acceptance is always opt-in.
Country alpha-2, alpha-3, and numeric parsers each provide an options-bearing
form so an accepted historic representation retains its authoritative status.
Options-bearing text, JSON, and SQL decode methods preserve that opt-in policy
when loading historic country, subdivision, or currency values. Reassigned
numeric identifiers retain their authoritative alphabetic mapping in memory;
an options-bearing numeric parse rejects the value when the selected current
and historic statuses would make that mapping ambiguous.
`DatasetDiff` classifies additions, removals, aliases, status changes, and
metadata changes for update review.

All exported declarations are documented in Go doc. Core deliberately excludes
message catalogs, content negotiation, money arithmetic, address validation,
geocoding, tax, sanctions, delivery, and locale detection.

`internationaltest` exports immutable copies of governed country, subdivision,
language, locale, currency, phone, and postal vectors. Each vector family has a
source constant, and the package tests evaluate frozen expected results rather
than deriving expectations from the generated tables. Country mappings are
also compared across the complete official set with `x/text`; stable currency
and locale behavior and all public phone vectors have differential checks.
