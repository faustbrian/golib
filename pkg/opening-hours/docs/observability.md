# Observability

`ObserveIsOpen` and `ObserveNextTransition` execute normal queries and call an
optional observer afterward. They accept a separate `clock` `ElapsedClock`
for monotonic duration measurement. Passing a nil elapsed clock explicitly
disables measurement and reports a zero duration; the package never reads the
process clock implicitly. Observation fields are limited to operation, outcome,
range count, bounded search steps, and duration.

No label, source, revision, date, timezone, customer identifier, or schedule
content is present. Callbacks run without locks. A callback panic is recovered
and cannot change the returned query value or error. There is no exporter,
registry, background refresh, or hidden goroutine.

Applications may translate observations into their own metrics, but should keep
dimensions low-cardinality and must not attach schedule or customer data.
