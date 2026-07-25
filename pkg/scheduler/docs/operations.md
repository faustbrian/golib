# Operations and recovery

Startup must compile the full registry, check backend safety, apply or verify
the PostgreSQL migration, and expose readiness only afterward. Monitor failure,
overlap, skipped, and completed event counts plus execution duration, lease
latency, callback timeouts, execution-capacity failures, and drain deadlines.

The HTTP and CLI surfaces provide list, next, due, validation, testing, and
fenced recovery. Protect them with application authentication and network
policy. Recovery requires the exact current token; stale tokens fail closed.

For a stuck lease:

1. inspect the owner, token, and expiry;
2. verify the owner is no longer performing effects;
3. stop or isolate the old owner;
4. recover with the observed token;
5. monitor the next occurrence and downstream idempotency result.

History is an interface contract; the provided memory buffer is bounded and is
not a durable audit store. Export important events through logs and telemetry.

A lease operation timeout has an unknown backend outcome. Retry or inspect the
same logical key; never substitute a new key. PostgreSQL cancellation normally
aborts the blocked statement atomically. A Valkey command already accepted by
the server can still commit after the client deadline, but its lease remains
fenced and discoverable under the original key.
