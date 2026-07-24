# Adoption from Laravel and cline/struct

## Laravel validation

Map request binding and validation into separate steps. Preserve presence while
decoding with `Value[T]`; do not collapse missing, null, and zero into one Go
field before validation.

| Laravel concept | validation |
| --- | --- |
| `required`, `present`, `prohibited` | rules on `Value[T]` |
| `min`, `max`, `between` | typed length/range constructors |
| `in`, `regex` | `OneOf`, precompiled `Pattern` |
| `array`, `distinct`, `*` | typed slice/map, `Unique`, `Items` |
| dependent rules | `When`, `Dependent`, typed cross-field accessors |
| validation exception | `Report`, `InvalidError`, transport projection |
| translated messages | application `validationtext.Catalog` |

There is intentionally no Laravel rule-string parser, implicit coercion,
database `exists`/`unique`, request binder, or global validator.

## cline/struct and attribute DTO validation

Prefer a typed `structplan.Builder[T]` when fields can be selected in code. Use
`CompileTags` only at startup for simple `required`, `email`, `min`, and `max`
convenience. Unknown tags are errors, not ignored metadata. Replace custom tag
callbacks with typed validators and explicit accessors.

## Incremental rollout

1. Inventory existing rule identity, missing/null behavior, and error paths.
2. Decode into presence-aware boundary types without normalization.
3. Reproduce truth tables using typed rules and report codes.
4. Add transport projection conformance tests before switching responses.
5. Move database/network checks to `AsyncValidator`.
6. Keep domain invariants in constructors after input validation.
