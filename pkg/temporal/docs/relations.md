# Bounds and relations

## Bounds

| Go value | ISO 80000 | Start | End |
|---|---:|---:|---:|
| `ClosedOpen` | `[a,b)` | included | excluded |
| `Closed` | `[a,b]` | included | included |
| `Open` | `(a,b)` | excluded | excluded |
| `OpenClosed` | `(a,b]` | excluded | included |

For continuous instants, equal closed endpoints are a singleton and the other
three modes are empty. Civil dates are discrete, so excluding one or both ends
can also make adjacent endpoints empty. Daily equal endpoints are never
inferred: callers choose `Collapsed` or `FullDay`.

## Allen relations

Relations compare declared endpoints and require non-empty intervals.

| Relation | Endpoint truth | Converse |
|---|---|---|
| before | `a.end < b.start` | after |
| meets | `a.end = b.start` | met-by |
| overlaps | `a.start < b.start < a.end < b.end` | overlapped-by |
| starts | same start, `a.end < b.end` | started-by |
| during | `b.start < a.start` and `a.end < b.end` | contains |
| finishes | `b.start < a.start`, same end | finished-by |
| equals | same start and end | equals |
| finished-by | `a.start < b.start`, same end | finishes |
| contains | `a.start < b.start` and `b.end < a.end` | during |
| started-by | same start, `b.end < a.end` | starts |
| overlapped-by | `b.start < a.start < b.end < a.end` | overlaps |
| met-by | `a.start = b.end` | meets |
| after | `a.start > b.end` | before |

`Abuts` checks equal adjacent endpoints regardless of inclusion. `Borders`
requires both sides to include the shared endpoint. `Meets` is adjacency
without a shared represented point. Set intersection remains the authority for
actual membership.
