# rule-engine/adapters/gotemporal

`gotemporal` is the optional bridge between exact `temporal/instant` periods
and the deterministic custom-operator boundary in `rule-engine`. It encodes
instants and periods as tagged strings so the core engine does not acquire a
temporal dependency.

The adapter does not read a clock, resolve named time zones, perform calendar
arithmetic, register global state, start goroutines, schedule work, or perform
I/O.

## Quick start

```go
start := time.Date(2026, time.July, 19, 10, 0, 0, 0, time.UTC)
window, err := instant.New(start, start.Add(time.Hour), temporal.ClosedOpen)
if err != nil {
    return err
}
windowValue, err := ruleenginetemporal.Period(window)
if err != nil {
    return err
}
pointValue, err := ruleenginetemporal.Instant(start.Add(30 * time.Minute))
if err != nil {
    return err
}

compiler, err := ruleengine.NewCompilerWithOperators(
    ruleengine.DefaultLimits(),
    ruleenginetemporal.Operators()...,
)
```

Registration is caller-owned. A compiler sees these operators only when its
caller explicitly supplies `Operators()`.

## Encoding

`Instant` and `Period` emit string-valued rule-engine values:

```text
instant:2026-07-19T09:30:00.123456789Z
period:2026-07-19T09:30:00Z|2026-07-19T10:30:00Z|[)
```

Endpoints use canonical UTC RFC 3339 text. Fractional seconds are omitted when
zero and otherwise contain only the digits needed to preserve up to nanosecond
precision. Periods store their start, end, and one of four bound markers:

| Marker | Start | End |
|---|---|---|
| `[)` | included | excluded |
| `[]` | included | included |
| `()` | excluded | excluded |
| `(]` | excluded | included |

Evaluation also accepts structurally valid RFC 3339 timestamps with numeric
offsets. Comparisons use the represented UTC instant, so equivalent offsets
compare identically. Encoding always writes `Z`; it never preserves or infers a
location name.

Tags and separators are part of the persisted contract. Missing or unknown
tags, extra fields, trailing data, invalid dates, leap seconds, offsets outside
RFC 3339, more than nine fractional digits, unsupported bounds, and reversed
periods are rejected. Parser work is bounded by the maximum valid encoded size.

## Interval relations

Every operator has the exact signature `(string, string) -> bool`:

| Operator | Meaning |
|---|---|
| `period_equal` | Both periods represent the same set. |
| `period_before` | The left period has the formal Allen `before` relation. |
| `period_after` | The left period has the formal Allen `after` relation. |
| `period_overlaps` | The represented sets share at least one instant. |
| `period_contains_period` | Every instant in the right period is in the left period. |
| `period_contains_instant` | The right instant is a member of the left period. |

Set equality, overlap, and containment preserve open and closed endpoints. Two
closed periods that share an endpoint overlap at that singleton; excluding the
shared endpoint can make them disjoint. Equal-endpoint `[]` is a singleton and
the other three bound modes are empty. All empty periods are set-equal, every
period contains an empty period, and empty periods contain no instant.

`period_before` and `period_after` intentionally retain the temporal package's
formal Allen endpoint relations. Adjacent equal endpoints are `meets` or
`met-by`, regardless of bound inclusion, and therefore are neither `before`
nor `after`.

## UTC and precision policy

- Values are compared as exact `time.Time` instants after parsing; monotonic
  process readings are not persisted.
- Nanoseconds are preserved. Subnanosecond persisted input is rejected instead
  of rounded or truncated.
- Leap seconds are rejected because Go's `time.Time` and RFC 3339 parser do not
  represent second 60.
- Encoding accepts only years `0000` through `9999`, the four-digit RFC 3339
  range. `Instant` and `Period` return an error outside that range.
- Named zones, `time.Local`, locale data, daylight-saving rules, and calendar
  coercion are outside this adapter. Supply an already resolved exact instant.

## API and ownership

- `Instant(time.Time) (ruleengine.Value, error)` returns a canonical tagged UTC
  instant.
- `Period(instant.Period) (ruleengine.Value, error)` returns canonical tagged
  endpoints and explicit bounds.
- `Operators() []ruleengine.Operator` returns a fresh, complete operator slice.

Operator implementations are immutable and concurrency-safe. Returned slices,
including signature slices, are caller-owned. Evaluation checks cancellation
before parsing either operand and returns the context error.

## Adoption and tradeoffs

Use this adapter when rules must persist exact instants or bounded periods and
evaluate temporal set relations without adding temporal behavior to the core
engine. Keep civil dates, recurring schedules, business calendars, named-zone
resolution, and relative expressions in their owning domain; resolve them to
exact instants before encoding.

The tagged-string boundary is portable through existing rule-engine storage,
but each evaluation parses its operands. Applications with a hot, typed
in-memory path may call `temporal/instant` directly and avoid that cost.

## Security and limitations

Treat persisted rule values as untrusted. The adapter rejects malformed and
oversized values without including their contents in errors. It performs no
network or filesystem access and has no credentials. The containing rule
engine remains responsible for its own value-size and evaluation limits.

The adapter does not model leap seconds, infinite periods, unbounded periods,
civil dates, durations with calendar units, recurrence, or probabilistic time.

## Compatibility and migration

This module is pre-v1. The `instant:` and `period:` tags, `|` separators, bound
markers, operator names, and relation meanings above form its current persisted
contract. Unknown tag versions are rejected rather than guessed.

Earlier snapshots returned a `ruleengine.Value` directly from `Instant` and
`Period`. Callers must now handle the returned error. Existing valid tagged
values remain readable; values with subnanosecond precision were previously
silently truncated by Go parsing and are now rejected.

## FAQ

### Does the adapter use the current time?

No. Every instant comes from a supplied value.

### Does an offset change equality?

No. `12:30:00+03:00` and `09:30:00Z` identify the same instant.

### Why are adjacency and overlap different?

Allen adjacency describes equal endpoint positions. Set overlap asks whether
the periods share a member, which depends on open and closed bounds.

### Can it resolve an IANA zone such as `Europe/Helsinki`?

No. Resolve that civil-time concern before calling this adapter.

## Performance and verification

`BenchmarkPeriodContainsInstant` compares tagged parse-and-evaluate work with
both equivalent direct parse/construct/membership work and already-typed direct
membership, and reports allocations. Compare results on the same machine and
Go toolchain with `benchstat`; record CPU, operating system, corpus, sample
count, and benchmark time when publishing numbers.

The repository module gate covers formatting, API compatibility, documentation
examples, tests, race detection, exact statement coverage, fuzz smoke tests,
mutation efficacy, security checks, and benchmarks.

## Release notes

See [CHANGELOG.md](CHANGELOG.md).

## License

MIT. See [LICENSE](LICENSE).
