# Normative semantics

## Lookup and resolution

| Operation | Input | May choose another tag | Uses app default | Mutates value |
|---|---|---:|---:|---:|
| `Get`, `Has`, `Require` | one tag | no | no | no |
| `match.Best` | weighted preferences | yes, BCP 47 matcher | no | no |
| `FallbackPlan.Resolve` | fixed exact chain | yes, configured only | optional | no |
| `Plan.Resolve` | requested tag and graph | yes, exact/parent graph | optional | no |
| `http.Select` | `Accept-Language` | yes, including wildcard | no | no |

Result kinds are `Exact`, `Matched`, `Fallback`, `Default`, and `Missing`.
`Present` is true for all successful kinds. `Empty` is true only when the
selected entry is present with an empty string.

## Special locale classes

| Locale class | Construction default | Matching |
|---|---|---|
| `und` | accepted; optionally rejected | exact when present, otherwise locale matcher policy |
| `mul` | accepted; optionally rejected | exact when present, otherwise locale matcher policy |
| private use | accepted; optionally rejected | exact identity; no invented relationship |
| zero/absent tag | rejected | invalid preference or missing plan request |
| unknown but registry-valid | accepted | pinned locale matcher policy |

HTTP `*` is represented only inside the HTTP adapter and selects the first
canonical stored locale after quality ordering. It is not stored as `und` and
does not change `match.Best` semantics.

## Missing, empty, and invalid

| State | Stored entry | `Get` text | `Get` present | Encoding |
|---|---:|---|---:|---|
| Missing | no | `""` | false | key absent |
| Present-empty | yes | `""` | true | `"tag":""` |
| Whitespace-only | yes | unchanged | true | unchanged string |
| Invalid UTF-8 | never | n/a | n/a | typed rejection |
| SQL NULL | nullable wrapper invalid | n/a | n/a | database NULL |
| Zero `Text` | no entries | `""` | false | `{}` |

Whitespace-only is valid core text. Applications opt into
`localizedvalidation.RequireNonWhitespace`.

## Merge conflicts

| Left | Right | `LeftWins` | `RightWins` | `RejectConflict` | Resolver |
|---|---|---|---|---|---|
| absent | absent | absent | absent | absent | not called |
| value | absent | left | left | left | not called |
| absent | value | right | right | right | not called |
| value | value | left | right | error | callback |
| empty | value, `EmptyIsValue` | empty | right | error | callback |
| empty | value, `EmptyIsAbsent` | right | right | right | not called |
| value | empty, `EmptyIsAbsent` | left | left | left | not called |
| empty | empty, `EmptyIsAbsent` | absent | absent | absent | not called |

Conflict identity is computed after tag canonicalization. Merge output is
deterministically ordered and revalidated against output limits.

## Duplicate construction

| Policy | Canonical duplicate result |
|---|---|
| `RejectDuplicates` | `ErrDuplicateLocale` |
| `FirstWins` | first input occurrence retained |
| `LastWins` | last input occurrence retained |

Input cardinality and bytes are checked before duplicate reduction so duplicate
floods cannot bypass work limits.

## Encoding

| Form | Order | Duplicate visibility | Null | Empty |
|---|---|---|---|---|
| Canonical JSON object | canonical lexical keys | detected while tokenizing | strict reject | `{}` |
| Permissive JSON object | canonical lexical keys | detected | accepted as zero | `{}` |
| Entry-array JSON | canonical lexical entries | detected after parsing | reject | `[]` |
| SQL/pgx JSONB | semantic object | PostgreSQL/object rules | explicit wrapper | `{}` |
| wire adapters | format deterministic rules | adapter and core rules | format-specific | map/object |

Canonicalization changes tag spelling and preferred aliases only. It never
creates, copies, translates, or removes a localized string.
