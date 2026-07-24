# Dialect selection and migration

Select a released dialect explicitly with `WithDialect`. Draft 2020-12 is the
documented constructor default, but applications should still select a dialect
at trust boundaries so a future schema cannot silently change interpretation.

| Constant | Meta-schema URI |
| --- | --- |
| `Draft3` | `http://json-schema.org/draft-03/schema#` |
| `Draft4` | `http://json-schema.org/draft-04/schema#` |
| `Draft6` | `http://json-schema.org/draft-06/schema#` |
| `Draft7` | `http://json-schema.org/draft-07/schema#` |
| `Draft201909` | `https://json-schema.org/draft/2019-09/schema` |
| `Draft202012` | `https://json-schema.org/draft/2020-12/schema` |

The selected dialect controls meta-validation and schemas without a
`$schema`. Embedded resources with released `$schema` declarations use their
declared semantics. Unsupported stable identifiers fail; unreleased dialects
are not exposed through the stable API.

Migration requires semantic review, not keyword renaming. In particular,
review `id`/`$id`, `$ref` siblings, boolean schemas, mathematical integers,
tuple `items`/`additionalItems` versus `prefixItems`/`items`, `dependencies`
versus `dependentRequired` and `dependentSchemas`, exclusive-bound forms,
contains limits, `$recursiveRef` versus `$dynamicRef`, `$defs`, unevaluated
keywords, vocabulary declarations, content, and format assertion policy. Run
both source and target official lanes plus application regressions.
