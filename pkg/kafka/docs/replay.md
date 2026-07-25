# Replay

`ReplayReader` takes explicit topic, partition, inclusive start, and exclusive
end offsets. It directly assigns partitions, does not join a group, and does
not read or alter committed group offsets.

Every expected offset must be present in ascending order. A gap, unexpected
partition, fetch failure, handler failure, or panic stops replay. Operators
must diagnose retention, corruption, or an incorrect range rather than silently
skipping it.

Replay publication uses an ordinary producer and the original deterministic
partition key. Applications remain responsible for authorization, dry-run
counts, range digests, schema compatibility, idempotency, quarantine policy,
and immutable audit records.
