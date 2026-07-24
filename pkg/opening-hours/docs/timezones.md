# Timezones and DST in five minutes

Every non-zero schedule loads an explicit IANA location. `IsOpen(time.Time)`
starts from an instant and is unambiguous. `IsOpenLocal` and `ResolveLocal`
require a policy and resolve civil fields to an instant before evaluation.

| Local condition | `RejectDST` | `PreferEarlier` | `PreferLater` | `ShiftForward` |
| --- | --- | --- | --- | --- |
| exact | exact instant | exact | exact | exact |
| spring gap | error | error | error | shift by gap |
| autumn fold | ambiguous error | first instant | second instant | ambiguous error |

Range expansion uses earlier resolution for an ambiguous opening and later
resolution for an ambiguous closing, so `01:30-02:30` across a fall fold covers
both repeated occurrences. Gap boundaries shift forward. A skipped civil date
therefore contributes no elapsed interval when both boundaries collapse.

The package delegates bounded zone loading and exact/fold occurrence resolution
to `calendar/timezone`, which in turn uses Go's installed IANA database.
Opening-hours adds only the explicit `ShiftForward` schedule policy for gaps.
Canonical encoding stores the zone identity, not a frozen rule snapshot.
