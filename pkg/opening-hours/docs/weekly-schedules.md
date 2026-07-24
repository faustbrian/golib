# Weekly schedules in five minutes

A schedule has one IANA timezone and seven independent weekly rule slots. A
missing map key is inherited. At the root schedule, inherited resolves closed;
inside overlay it means “leave the lower-precedence schedule unchanged.”

| State | Constructor | Meaning |
| --- | --- | --- |
| inherited | `Inherited()` or missing | Delegate to lower precedence |
| ranges | `OpenRanges` | Open only inside canonical ranges |
| all day | `OpenAllDay()` | Open for the complete civil date |
| closed | `Closed()` | Explicitly unavailable |

```go
morning, _ := openinghours.NewRange(at(9, 0), at(12, 0))
afternoon, _ := openinghours.NewRange(at(13, 0), at(17, 0))
monday, _ := openinghours.OpenRanges(
    []openinghours.Range{afternoon, morning},
    openinghours.RejectOverlapAndAdjacent,
)
schedule, _ := openinghours.NewSchedule(openinghours.Config{
    Timezone: "Europe/Helsinki",
    Weekly: map[time.Weekday]openinghours.DayRule{time.Monday: monday},
})
```

Construction copies maps and slices. Query results return detached slices.
Metadata is copied and has no effect on interval semantics. Use
`SemanticallyEqual` to ignore metadata and `Equal` for full source equality.

For logs, consoles, and administrative display, `HumanSummary` returns bounded
deterministic presentation text. It is deliberately not a wire encoding, omits
labels and exception provenance, and cannot be passed to `UnmarshalText`.
