# Migration from faustbrian/temporal

The Go API is deliberately not a transliteration. Start from represented-set
behavior, then select the type whose semantics match.

| PHP concept | Go replacement | Deliberate change |
|---|---|---|
| `Period\Bounds` | `temporal.Bounds` | immutable enum; closed-open is zero/default |
| `DatePoint` | `time.Time` or `calendar.Date` | instant and civil date are distinct |
| `Period\Duration` | `time.Duration` / `timeofday.Duration` | no implicit calendar months |
| `Period` | `instant.Period` or `dateperiod.Period` | typed immutable values |
| `Sequence` | normalized `instant.Set` / `dateperiod.Set` | no mutation collection API |
| `Time\Time` | `timeofday.Time` | nanoseconds; strict ASCII ISO text |
| `Time\Duration` | `timeofday.Duration` | checked `time.Duration` interoperability |
| `Time\Interval` | `timeofday.Interval` | all four bounds; explicit collapsed/full |
| `Time\IntervalSet` | `timeofday.IntervalSet` | normalized disjoint immutable segments |

PHP accepts whitespace in mathematical notation; Go's strict codecs do not.
Trim only at a trusted application boundary if the protocol permits it. PHP's
local-time precision is microseconds; Go supports nanoseconds and rejects
lossy PostgreSQL writes.

PHP `Time::endOfDay()` denotes the last microsecond
(`23:59:59.999999`). Go `EndOfDay()` is the distinct boundary `24:00`. Use the
PHP last-microsecond value only when reproducing legacy sampled membership; use
`24:00` for interval boundaries.

PHP represents collapsed and full-day intervals with equal formatted endpoints
and distinguishes them through duration/type. Go rejects equal `Between`
endpoints and requires `Collapsed(anchor)` or `FullDay()`.

Date factories move to `dateperiod` and delegate civil arithmetic to
`calendar`. End-of-day conversion becomes a next-boundary exclusive instant
range, preserving DST behavior.

PHP snap helpers become `instant.Snap` or `Period.SnapOutward`; callers provide
the unit, direction, location, and `calendar` gap/fold policy. PHP
`Time::applyTo` becomes `Time.Apply`, and circular `Interval::toNative` becomes
`Interval.ToInstant`; both require the same explicit civil context.

Unversioned `jsonSerialize` payloads become `temporalwire.Document` for scalar
values and `CollectionDocument` for normalized sets. Decoders reject unknown
fields and trailing data instead of accepting partially understood payloads.

All variable-output operations accept `temporal.Limits` and may return
`LimitError`. All arithmetic and parsing errors are typed and compatible with
`errors.Is`/`errors.As`.

## Unsupported charting gap

`Period\Chart` has no v1 Go implementation. This includes `Chart`, `Data`,
`Dataset`, `GanttChart`, `GanttChartConfig`, `Output`, `StreamOutput`, terminal
capabilities, colors, alignments, affix/reverse/generated labels, decimal,
Latin-letter and Roman-number labels, chart errors, and every rendering
fixture. The detailed inventory is in `docs/compatibility.md`.

Do not mark a migration fully compatible if it uses PHP charting. Keep core
period/set data and introduce an application renderer, or wait for a separately
versioned future `temporalchart` package.
