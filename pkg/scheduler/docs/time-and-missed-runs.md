# Missed runs, time zones, and DST

Cron expressions have five fields and use explicit IANA time zones. Spring DST
gaps omit nonexistent local times. During a fold, both physical instants of an
ambiguous local time run in chronological order. Store schedule
times as instants and display local offsets in diagnostics.

`MissedRunSkip` executes only an occurrence exactly due at the current tick.
`MissedRunOnce` chooses the latest missed occurrence. `MissedRunCatchUp` returns
at most `MaxCatchUp` latest occurrences. Scans have a hard upper bound and fail
with `ErrOccurrenceLimit` rather than replaying unbounded downtime.

Jitter is a stable per-schedule offset in `[0, maximum)`. It is identical on all
replicas and preserves occurrence ordering. Wall-clock jumps are processed from
the last runner cursor to the injected clock's current instant.
