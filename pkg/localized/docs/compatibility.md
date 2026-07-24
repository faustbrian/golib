# Compatibility and provenance

## Toolchain and dependencies

| Component | Pinned baseline | Role |
|---|---|---|
| Go | 1.26.5 | minimum language and iterator contract |
| `international/locale` | `v0.0.0-20260717012043-f6e9bbc622bd` | public BCP 47 identity and provenance |
| `golang.org/x/text` | v0.40.0 | private CLDR matching and Unicode normalization |
| pgx | v5.10.0 | JSONB and PostgreSQL integration |
| wire | pinned pseudo-version in `go.mod` | bounded format adapters |
| config | pinned pseudo-version in `go.mod` | configuration hook conformance |
| PostgreSQL | 14–18 | JSONB integration matrix |

`locale.DatasetProvenance` records the IANA Language Subtag Registry retrieved
2026-07-16, upstream registry version 2026-06-14, x/text v0.40.0, and SHA-256
`be1fad86a99e3a932d07b80c9b3c271ec2381a5909ce22420144e5077ab0a43a`.
Releases MUST state changes to either locale dependency because preferred-value
canonicalization or matching can change.

## Locale classes

Language, script, region, variant, extension, private-use, grandfathered, and
deprecated tags follow the pinned locale dependency. Default construction
accepts valid `und`, `mul`, and private-use tags; `LocalePolicy` can reject each
class. Strict string boundaries reject underscores and whitespace.

Unknown-but-well-formed tags follow the pinned locale registry contract. The
package does not expose registry enumeration or mutable registry state.

## Stability

Canonical JSON, exact presence, missing/present-empty distinction, merge policy,
and result kinds are v1 compatibility commitments. Matcher choices may change
only with a documented locale-data dependency update and compatibility vectors.
