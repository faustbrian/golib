# Overnight ranges in five minutes

An end earlier than the start is an overnight range owned by the start date:
`22:00-02:00` on Monday represents Monday 22:00 through Tuesday 02:00.

| Query | Result |
| --- | --- |
| Monday 21:59 | closed |
| Monday 22:00 | open |
| Tuesday 01:59 | open from weekly spill |
| Tuesday 02:00 | closed (end-exclusive) |

The following date then applies its own exception operations to the union of
incoming spill and its weekly rule. A following closure or replacement can
remove the spill; addition and subtraction modify it.

Normalization compares owner-day coordinates linearly. `22:00-02:00` overlaps
`23:00-03:00`. A merge that would create a continuous interval longer than one
day returns `CodeDayBoundaryOverflow` rather than silently wrapping twice.
