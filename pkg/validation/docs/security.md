# Security model

## Trust boundaries

Input values, object graphs, map keys, tags, custom validators, translation
catalogs, and transport consumers may be hostile. Applications choose which
parameters and causes are safe; core rules add bounds only and never rejected
values. Paths are locations and can contain caller field/key text, so
`validationobserve` deliberately excludes them from labels.

## Threats and controls

| Threat | Control |
| --- | --- |
| Secret disclosure | values are absent from violations/default formatting; projections omit causes |
| Validation bypass | explicit typed rules, strict tags, mutation-tested branches |
| Path confusion | typed segments, deterministic rendering, RFC 6901 escaping |
| Rule-code collision | bounded machine-safe codes participate in structural dedup identity; invalid custom diagnostics fail closed |
| Log/metric injection | observation labels exclude paths, values, parameters, and causes; invalid custom labels are replaced |
| CPU or memory denial | string/collection/depth/field/tag/path/violation/regex/cache/concurrency limits |
| Regex denial | startup compilation with Go RE2 and pattern-length bound |
| Reflection panic/recursion | startup kind checks, inaccessible-field errors, cycle/depth detection |
| Custom panic | function adapters contain panics; sync/async wrappers protect arbitrary implementations; payloads are discarded |
| Hidden I/O/deadlock | I/O uses separate context-aware async contract |

## Resource budgets

| Limit | Default | Enforcement |
| --- | ---: | --- |
| Depth | 32 | reflective compilation |
| Collection size | 10,000 | item/key/unique traversal before work |
| String size | 65,536 bytes | typed and reflective string rules before parsing or comparison |
| Violations | 100 | report add/merge |
| Path length | 1,024 bytes | every report addition |
| Metadata entries | 16 | context construction |
| Metadata key/value | 64/256 bytes | context construction |
| Violation parameters | 16 entries, 64/256-byte key/value | construction and report insertion |
| Locale and operation | 256 bytes each | context construction |
| Regex pattern | 1,024 bytes | pattern construction |
| Custom concurrency | 8 | `AsyncAll` worker pool |
| Struct fields | 256 | typed fields and every reflectively visited field |
| Tag length | 1,024 bytes | tag compilation |
| Cached plans | 256 | instance cache |

Applications should lower limits for smaller protocols. Limits must be
positive. The zero-value `Context` safely uses default limits. Caller maps,
slices, pointers, and objects are read but not mutated.
Custom validators remain application code and can violate the non-mutation
rule; isolate them by ownership and review, not by assuming Go can enforce
purity. `ValidatorFunc` and `AsyncValidatorFunc` contain panics automatically.
Wrap other interface implementations with `IsolatePanics` before direct use.

## Reporting vulnerabilities

Do not include a real secret or customer payload in a report. Provide a minimal
synthetic reproducer describing the affected rule, path, and version.
Custom parameter values are bounded safe text but remain application-asserted
message data; the package cannot infer whether arbitrary text is confidential.
Default error formatting omits them and escapes control-bearing paths, while
transport encoders perform their normal JSON escaping.
Translation lookup is panic-safe and cannot alter machine path, code,
severity, ordering, or blocking state. Catalog text exceeding the string
budget, containing invalid UTF-8 or controls, is omitted; accepted text is
HTML-escaped before return.
