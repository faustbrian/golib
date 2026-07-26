# PHP compatibility matrix

This matrix audits `faustbrian/temporal` at commit
`469603239dbe700739c29b4c532a90382b6cbedf` (2026-07-16). It is the contract
for the Go successor, not a claim that the current implementation is complete.
Every public PHP method and enum case is accounted for below. Rows containing
several method names group aliases that share one mathematical contract.
The generated compatibility fixture also pins a sorted inventory of all 412
non-chart public PHP types, methods, enum cases, constants, and properties.
Each symbol has an adjacent machine-checked status, contract family, Go test or
divergence evidence, and migration pointer. The generator rejects an unknown
type instead of assigning a catch-all classification. This prevents a source
behavior from disappearing between grouped matrix rows.

Status values are **supported**, **diverges**, and **deferred**. **Supported**
means the Go API, tests, and migration guidance exist; **diverges** identifies
an intentional Go-native replacement or omission.

## Compatibility policy

- Go values are immutable. PHP collection mutators become copy-returning
  operations or standard iteration helpers.
- Instant, civil-date, local-time, and elapsed-duration values never implicitly
  convert into one another.
- PHP's microsecond model is accepted by parsers, while Go retains nanosecond
  precision and rejects precision beyond nine fractional digits.
- PHP relative-date strings are not accepted. Only strict documented notation
  is parsed.
- Operations which are partial in PHP return typed errors in Go. Expected empty
  set results are values, not errors.
- All variable-output operations accept limits and fail before exceeding them.
- Charting remains deferred; therefore v1 does not claim complete package
  compatibility.

## Period namespace

| PHP type and behavior | Go destination | Status and deliberate difference |
|---|---|---|
| `Bounds`: four cases | `temporal.Bounds` | supported; same four modes, half-open default |
| `parseIso80000`, `buildIso80000` | `notation` bound codec | supported; strict ASCII and trailing-data rejection |
| `parseBourbaki`, `buildBourbaki` | `notation` bound codec | supported; French reversed brackets retained |
| `isStartIncluded`, `isEndIncluded` | `IncludesStart`, `IncludesEnd` | supported |
| `equalsStart`, `equalsEnd` | bound-side comparison | diverges; explicit side values replace boolean pair tricks |
| `includeStart`, `includeEnd`, `excludeStart`, `excludeEnd` | immutable bound helpers | supported |
| `replaceStart`, `replaceEnd` | immutable side replacement | supported |
| `DatePoint::fromDate`, `fromDateString`, `fromTimestamp`, `fromFormat` | instant/date constructors | diverges; no relative strings and no mixed date/instant type |
| `DatePoint` relation methods | period relation predicates | supported; formal Allen relation is primary API |
| `DatePoint::second`, `minute`, `hour` | `instant.Snap` | diverges; supported replacement requires location and DST policy |
| `DatePoint::day`, `isoWeek`, `month`, `quarter`, `semester`, `year`, `isoYear` | `dateperiod` constructors | supported |
| `Period\\Duration` factories and `adjustedTo` | fixed duration or calendar adapter | diverges; calendar components require a civil reference |
| `Period::fromIso8601`, `fromBourbaki`, `fromIso80000` | `notation` plus `instant`/`dateperiod` | supported; strict and typed by value domain |
| `fromDate`, `fromTimestamp` | `time.Unix` plus `instant.New` | diverges; Go's standard instant constructors remain explicit and monotonic readings are stripped |
| `after`, `around`, `before` | `instant` constructors | supported; checked duration arithmetic |
| `fromRange` | explicit iterator/range conversion | diverges; no implicit PHP `DatePeriod` semantics |
| `fromYear`, `fromIsoYear`, `fromSemester`, `fromQuarter`, `fromMonth`, `fromIsoWeek`, `fromDay` | `dateperiod` factories | supported; calendar arithmetic delegated |
| `toIso80000`, `toBourbaki`, `toIso8601` | text codecs | supported; stable canonical output |
| `jsonSerialize` | `temporalwire.Document` | supported; versioned strict JSON replaces unversioned magic serialization |
| `timeDuration` | `instant.Period.Duration` | supported; elapsed `time.Duration` |
| `dateInterval` | calendar difference adapter | diverges; never returned for an instant period |
| duration comparison methods | `Duration`, `CompareDuration` | supported; one comparison primitive plus predicates |
| `isBefore`, `bordersOnStart`, `meetsOnStart`, `isStartedBy` | Allen relation and predicates | supported; bound-sensitive truth table |
| `isDuring`, `contains`, `equals`, `isEndedBy` | Allen relation and predicates | supported; interval equality and set equality are separate |
| `meetsOnEnd`, `bordersOnEnd`, `isAfter`, `abuts`, `meets`, `overlaps` | Allen relation and predicates | supported; `abuts` is boundary-aware adjacency |
| `timeDurationDiff`, `dateIntervalDiff` | typed duration differences | diverges; no mixed fixed/calendar result |
| `rangeForward`, `rangeBackwards` | `SplitForward`, `SplitBackward`, set `All` | diverges; Go exposes bounded pieces or standard iterators rather than PHP `DatePeriod` values |
| `splitForward`, `splitBackwards` | bounded split | supported; positive progress required |
| `diff`, `subtract` | normalized period set difference | supported; empty output is not exceptional |
| `gap` | optional gap result | diverges; overlap returns `(zero, false)`, not exception |
| `intersect` | intersection | diverges; disjoint result is explicit absence, not exception |
| `union`, `merge` | normalized set union / hull | supported; hull and exact set union use distinct names |
| `startingOn`, `endingOn`, `boundedBy` | immutable endpoint/bounds replacement | supported |
| `withDurationAfterStart`, `withDurationBeforeEnd` | checked resize | supported |
| `moveStartDate`, `moveEndDate`, `move`, `expand` | checked immutable movement | supported |
| all `snapTo*` methods | calendar snapping adapter | diverges; explicit location, calendar, and DST policy |
| `Sequence` construction, `length`, `count`, `isEmpty` | `instant.Set` / `dateperiod.Set` | supported; normalized immutable collection |
| `totalTimeDuration`, `gaps`, `intersections`, `unions`, `subtract` | set algebra | supported; deterministic and cardinality-bounded |
| `some`, `every`, `contains`, `indexOf`, `get` | `Includes`, `Search`, `All`, copied slices | diverges; `some`/`every` use ordinary Go iterator loops and negative indexing is omitted |
| `toList`, `jsonSerialize`, `getIterator` | copied slice, JSON, Go iterator | supported; caller cannot mutate internal storage |
| `offsetExists`, `offsetGet`, `offsetSet`, `offsetUnset` | none | diverges; PHP array mutation is intentionally omitted |
| `sort`, `unshift`, `push`, `insert`, `set`, `remove`, `clear` | copy-returning set operations | diverges; normalized order cannot be mutated |
| `filter`, `sorted`, `map`, `reduce` | copy/iterator helpers | supported; callbacks cannot bypass limits |

Typed PHP errors (`InvalidInterval`, `InaccessibleInterval`, and
`UnprocessableInterval`) map to errors discoverable with `errors.Is` and
`errors.As`; error strings are not compatibility contracts.

## Time namespace

| PHP type and behavior | Go destination | Status and deliberate difference |
|---|---|---|
| `Bound::Start`, `Bound::End` | `temporal.Side` | supported |
| `Unit` cases and conversion methods | `time.Duration` helpers | supported; nanoseconds are canonical |
| `RoundingMode` cases | `timeofday.RoundingMode` | supported |
| `Duration::of`, `zero`, `min`, `max`, `minOf`, `maxOf` | `NewDuration`, `ZeroDuration`, `Compare` | diverges; package-level extrema helpers are replaced by comparison and ordinary Go selection |
| duration `fromDateInterval` | calendar adapter | diverges; rejects months/years without reference |
| duration `fromFormat`, `format`, JSON | strict fixed ISO codec and `temporalwire` | diverges; PHP format tokens are not parsed and fixed ISO text is canonical |
| `toDateInterval`, `total` | standard duration conversion | diverges; fractional totals use explicit quotient/remainder |
| `isZero`, `negated`, `abs`, `sum`, `increase`, `decrease` | checked arithmetic | supported; overflow is typed |
| `roundTo`, comparisons, `clamp`, `multipliedBy`, `dividedBy` | checked arithmetic | supported; division documents truncation and remainder |
| `Time::at`, `fromOffset`, `midnight`, `noon`, `endOfDay` | `timeofday.Time` | supported; `24:00` is a distinct end boundary |
| `fromDate`, `applyTo` | `timeofday.FromInstant`, `Time.Apply` | diverges; supported replacement requires explicit location, precision, and DST resolution |
| `fromFormat`, `format`, JSON | strict text/JSON codec | supported; nanosecond precision |
| `now` | none | diverges; clock ownership remains in `clock` |
| `minOf`, `maxOf`, comparison predicates, `clamp` | `Compare`, `Equal`, `Clamp` | diverges; package extrema helpers are replaced by explicit comparison |
| `toLocaleString` | none in core | diverges; locale formatting is presentation logic |
| `shift`, `with`, `roundTo`, `diff`, `distance` | `Shift`, `New`, `Round`, `Difference`, `CircularDistance` | diverges; component replacement reconstructs a validated value, wrapping policy is explicit, and rounding upward to the day boundary yields distinct `24:00` rather than PHP `00:00` |
| `Interval::since`, `until`, `around`, `between` | daily interval constructors | supported; negative durations rejected |
| `fromFormat`, `format`, JSON | daily interval codecs | supported |
| `fromLinearSpan`, `fullDay`, `circular`, `collapsed` | explicit interval kinds | supported; collapsed and full-day never alias |
| `toNative` | `Interval.ToInstant` | diverges; supported replacement requires explicit date, location, and DST policy |
| `startingOn`, `endingOn`, `shift`, `shiftBound`, `lasting`, `expand`, `roundTo` | immutable daily operations | diverges; endpoint/bound replacement, fixed constructors, shift, and expand are supported, while callers round endpoints explicitly before reconstruction |
| `complement`, `intersect`, `gap`, `union`, `difference` | circular set algebra | supported; universe is explicit |
| `steps`, `splitBy`, `splitAt` | `Steps`, `Split` | diverges; fixed stepping is bounded, while arbitrary split points require explicit caller ordering around midnight |
| duration comparisons and `equals` | daily interval comparison | supported; structural equality separate from set equality |
| `includes`, `contains`, `overlaps`, `abuts` | boundary-aware predicates | supported |
| `IntervalSet` construction, serialization, `all`, `duration`, formatting | normalized daily set | supported; always stable and disjoint |
| `get`, `nth`, `has`, index/search and matching methods | `Intervals`, `Search`, `Includes`, `All` | diverges; copied slices and standard iterators replace collection-specific lookup variants |
| `push`, `unshift`, `replace`, `remove` | copy-returning set updates | diverges; no mutation-shaped names in core |
| `any`, `every`, `map`, `transform`, `filter`, `reduce`, `each` | `All` plus ordinary Go iterator loops | diverges; normalized reconstruction must pass through `NewIntervalSet` and its limits |
| `gaps`, `difference`, `intersect`, `complement`, `union` | normalized algebra | supported; output limit enforced |
| `sortedUsing`, `sorted` | canonical order | diverges; normalized sets expose one stable order |
| format/type enums | typed codec/kind enums | supported; unknown values rejected |

All PHP serialization magic is replaced by Go text, JSON, SQL, and pgx
interfaces. PHP locale, mutable `DateTime`, and implementation-identity behavior
is intentionally not preserved.

## Deferred chart inventory

No chart symbol is part of v1 core. The future optional `temporalchart` package
must account for every item in this inventory before the compatibility gap can
be closed.

| PHP chart surface | Observed contract / fixture | v1 status |
|---|---|---|
| `Chart::stroke` | renders a `Dataset` through an output target | deferred |
| `GanttChart`, constructor, `stroke` | empty data, periods, sequences, empty sequences, exact terminal fixtures | deferred |
| `GanttChartConfig` constructors | stream/output/random/rainbow construction | deferred |
| config glyphs | start/end included/excluded, body and space characters; one Unicode scalar, no CR/LF | deferred |
| config dimensions | width, left margin, gap size | deferred |
| config appearance | colors and label alignment; immutable copy-returning updates | deferred |
| `Terminal` | POSIX color and colorless modes | deferred |
| `Color` | reset, 8 ANSI colors, none, rainbow selection, POSIX encoding | deferred |
| `Alignment` | left, center, right and padding conversion | deferred |
| `Dataset` / `Data` | items, labels, length, max label width, append, iteration, JSON | deferred |
| `LabelGenerator` | `generate` and `format` contracts | deferred |
| `AffixLabel` / `ReverseLabel` | decoration/reversal and exact label fixtures | deferred |
| `DecimalNumber` | numeric sequence and formatting fixtures | deferred |
| `LatinLetter` / `LetterCase` | alphabetic generation, case conversion, invalid alphabet rejection | deferred |
| `RomanNumber` | Roman generation, format, invalid zero/start rejection | deferred |
| `Output`, `StreamOutput` | line output, invalid/non-stream rejection, write failure behavior | deferred |
| chart errors | invalid pattern, Unicode glyph, label, and draw failure | deferred |

Source fixtures are the PHP tests under `src/Period/Chart/*Test.php`; future
differential data must retain their exact labels, ANSI bytes, padding, glyphs,
and newlines.

## Evidence required to change a status

A row moves to **supported** only with a public Go API, package documentation,
unit or property tests, malformed/boundary tests where relevant, and a migration
example. Codec rows additionally require round-trip and fuzz evidence. Algebra
rows additionally require truth-table or set-property evidence. PostgreSQL rows
require an integration fixture proving lossless bounds and precision.
