# Clock and temporal composition

`calendarclock.Today` accepts only `Now() time.Time` and an explicit location.
That structural interface is satisfied by `clock.Clock` without importing
timer, ticker, or sleep ownership.

`calendartemporal.Sequence` returns a bounded inclusive sequence of civil dates.
`InclusiveDates` returns start-inclusive/end-exclusive instant boundaries.
Pass those endpoints to `temporal/instant.Range`; interval relations and set
algebra remain owned by temporal.

Schedulers may consume these calculations for due dates, but dispatch,
retries, cron, clocks, and ownership remain scheduler concerns.
