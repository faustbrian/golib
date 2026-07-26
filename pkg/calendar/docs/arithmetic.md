# Calendar arithmetic

`AddDays(1)` means the following civil date. It never means 24 elapsed hours.

| Source | Operation | Clamp | Reject | Overflow |
| --- | --- | --- | --- | --- |
| 2023-01-31 | +1 month | 2023-02-28 | error | 2023-03-03 |
| 2024-02-29 | +1 year | 2025-02-28 | error | 2025-03-01 |
| 2024-03-31 | -1 month | 2024-02-29 | error | 2024-03-02 |

Offsets are checked before native integer addition. Movement outside years
1–9999 returns `ErrArithmetic`. `DaysUntil` uses Gregorian ordinals, so the
entire supported range is exact and does not saturate like `time.Duration`.

`ComponentsUntil` is signed and policy-driven. It applies years, then months,
then exact calendar days. Under `Reject`, a decomposition can fail when an
intermediate anniversary or month lacks the source day; it never switches to
clamping silently.

The exhaustive suite checks all 3,652,059 supported dates and differentially
compares weekday, ordinal, ISO week, leap, and month-length behavior with Go's
Gregorian implementation.
