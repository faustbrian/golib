# Lease backends and fencing

The memory backend is deterministic and process-local. It is appropriate for
tests and single-replica tools only.

The PostgreSQL adapter uses an owned table and `clock_timestamp()`. Rows become
inactive on release so their monotonic fencing token is retained. Apply
`postgres.SchemaMigration` before startup.

The Valkey adapter uses native `valkey-go` and atomic Lua scripts. `Open`
requires Valkey 9 or later and `maxmemory-policy noeviction`. A TTL-bound lease
hash and persistent same-slot counter prevent token recycling.

Occurrence leases implement one-owner dispatch. Task leases implement overlap
decisions. Expiry permits takeover after process death. An expired owner may
still be running, so ownership-sensitive writes must carry and validate the
fencing token. Manual recovery requires the current token and should be audited.
