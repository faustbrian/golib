# Formal normalization and boundary contract

All intervals use `[start,end)`. Local times have nanosecond precision and lie
in `[00:00,24:00)`. `00:00-00:00` is invalid; full-day opening is a state.

## Range-state table

| Input state | Constructor or policy | Canonical result |
| --- | --- | --- |
| inherited or omitted weekday | `Inherited` or zero `DayRule` | inherited; closed without a lower source |
| empty ranged input | `OpenRanges([]Range{}, ...)` | `CodeLimitExceeded`; never all-day |
| explicit closure | `Closed` | closed, even when lower-precedence spill exists |
| full day | `OpenAllDay` | one explicit all-day state, not `00:00-00:00` |
| single ordinary range | any valid overlap policy | one `[start,end)` range |
| multiple disjoint ranges | any valid overlap policy | sorted by start then end |
| adjacent ranges | reject, preserve, or merge by named policy | never silently merged |
| overlapping ranges | reject or merge by named policy | never silently accepted |
| duplicate ranges | overlap policy applies | rejected or collapsed explicitly |
| ranges collapsing to `00:00-24:00` | `MergeAdjacent` | `DayOpenAllDay` |
| non-midnight 24-hour collapse | any merge policy | `CodeDayBoundaryOverflow` |
| overnight range | end earlier than start | owner-day linear range through next date |

Caller slices are cloned before sorting or merging. Returned range slices are
also detached, so canonicalization cannot mutate the source rule or schedule.

| Policy | Overlap | Adjacency |
| --- | --- | --- |
| `RejectOverlap` | error | preserve separately |
| `RejectOverlapAndAdjacent` | error | error |
| `MergeOverlap` | merge | preserve separately |
| `MergeAdjacent` | merge | merge |

Input is copied, sorted by start/end, and normalized in an owner-day linear
coordinate. Duplicate ranges count as overlap. A merge spanning exactly the
civil day from midnight becomes `DayOpenAllDay`; any ambiguous or longer wrap
returns `CodeDayBoundaryOverflow`.

Normalization is idempotent. Canonical JSON orders Sunday through Saturday,
then exceptions by date/precedence. Algebra results are immutable expression
trees capped by `MaxCompositionDepth`; query fragments are capped at 8,192.
`Schedule.Compare` orders complete schedule values by those canonical bytes, so
its ordering includes provenance and composition shape just like `Equal`.

`TestAlgebraPropertiesAgreeWithPointSet` exhausts pairwise union,
intersection, and subtraction over a deterministic interval fixture set and
checks the conservation identity `|A union B| + |A intersection B| = |A| +
|B|`. `TestNormalizationIsIdempotentAndCanonical` proves repeated
normalization and caller ordering produce the same canonical value.
