# Troubleshooting

**A schedule never appears:** compile errors report duplicate names, invalid
cron, or unavailable time zones. Run CLI `validate` and `list` during startup.

**A local time did not run:** check the IANA zone and whether the time was in a
DST spring gap. Use `next` around the transition.

**Repeated dispatch:** inspect occurrence ownership, pod owner uniqueness,
backend clock/configuration, idempotency records, and queue acknowledgements.
Duplicates remain possible across process crashes.

**No takeover:** compare backend server time with lease expiry. Do not force
unlock until the old owner is isolated.

**Valkey startup fails:** use Valkey 9 or later and set
`maxmemory-policy noeviction`.

**Catch-up fails with a limit error:** downtime crossed the bounded scan budget.
Choose an operational recovery window instead of unbounded replay.
