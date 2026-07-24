# Gregorian and ISO truth vectors

The calendar is proleptic Gregorian: the Gregorian rule applies before its
historical civil adoption and there is no 1582 cutover gap.

## Leap and month truth table

| Year | Divisibility | Leap | February |
| ---: | --- | --- | ---: |
| 1 | ordinary | no | 28 |
| 4 | divisible by 4 | yes | 29 |
| 100 | century, not 400 | no | 28 |
| 400 | divisible by 400 | yes | 29 |
| 1582 | ordinary | no | 28 |
| 1900 | century, not 400 | no | 28 |
| 2000 | divisible by 400 | yes | 29 |
| 9999 | ordinary | no | 28 |

Common month lengths are `31,28,31,30,31,30,31,31,30,31,30,31`; leap years
replace February 28 with 29.

## ISO week boundary vectors

| Civil date | Weekday | ISO week-year |
| --- | --- | --- |
| 2015-12-31 | Thursday | 2015-W53 |
| 2016-01-01 | Friday | 2015-W53 |
| 2016-01-04 | Monday | 2016-W01 |
| 2019-12-30 | Monday | 2020-W01 |
| 2020-12-31 | Thursday | 2020-W53 |
| 2021-01-03 | Sunday | 2020-W53 |
| 2021-01-04 | Monday | 2021-W01 |

`TestExhaustiveSupportedGregorianCalendar` goes beyond these reviewable vectors:
it constructs every supported date and differentially checks month length,
leap status, ordinal day, weekday, ISO week, quarter, and semester behavior
against Go's standard-library Gregorian implementation. A separate all-year
property matrix proves month add/subtract inverses for clamp, reject, and
overflow whenever the selected operation preserves the source day. Week 53
construction is rejected when December 28 belongs to week 52.
