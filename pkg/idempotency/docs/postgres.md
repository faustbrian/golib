# PostgreSQL adapter

The `postgres` package stores one versioned JSONB record per opaque SHA-256 key
digest. Every transition runs in one transaction and takes a transaction-scoped
advisory lock derived from that digest before selecting the row `FOR UPDATE`.
This serializes both existing-row transitions and concurrent first acquisition.

PostgreSQL `clock_timestamp()` governs lease and retention decisions. Process
clock skew cannot authorize a stale owner.

## Five-minute setup

Apply `postgres.GoMigration()` through `migrations`, or apply
`postgres.SchemaMigration().Up` with another deployment migration system. Then
construct a store from the application's pgx pool:

```go
pool, err := pgxpool.New(ctx, databaseURL)
if err != nil {
	return err
}

store, err := idempotencypostgres.New(pool, idempotencypostgres.Options{
	Retention: 7 * 24 * time.Hour,
	OwnerTokens: func() (string, error) {
		var value [32]byte
		if _, err := rand.Read(value[:]); err != nil {
			return "", err
		}
		return hex.EncodeToString(value[:]), nil
	},
})
if err != nil {
	return err
}

service, err := idempotency.NewService(store)
```

The reversible descriptor has version `1`, name
`create_idempotency_records`, and `Up` and `Down` SQL fields. `GoMigration`
returns the corresponding immutable `migrations.Migration` with the default
transaction mode and a canonical checksum. Include it in the deployment
runner's source. Keep migration application in deployment, not request startup.

## Transaction and failure behavior

The advisory lock is correctness-critical for absent rows: `SELECT FOR UPDATE`
alone cannot lock a row that does not exist. Digest collisions only serialize
unrelated keys; the full validated key and fingerprint in the record still
decide semantic conflict.

Acquire, inspect, heartbeat, complete, fail, release, and expire return database
errors unchanged. `idempotency.Service` converts non-semantic errors to
`unavailable` and fails closed. A lost connection or commit error leaves the
transition unknown. Reconnect and inspect the key before deciding whether work
may run again. The integration suite proves this recovery path by committing
completion server-side, dropping the PostgreSQL commit response, and inspecting
the completed record through an independent pool.

The live failure suite also forces an advisory-lock deadlock, a serializable
write-skew abort, and connection-pool saturation. Aborted transactions retain
no partial completion or business write, and a timed-out pool acquisition does
not mutate the record.

Completion checks the owner token and fencing token while holding the row lock.
It cannot make an external side effect atomic automatically. Apply the fencing
token in the same application transaction as the side effect, or use a unique
constraint or conditional update that rejects stale owners.

For PostgreSQL business writes, `CompleteTx` performs the ownership check and
record update inside a caller-owned `pgx.Tx`. Commit the business effect or
outbox row and completion together. See the [transaction and outbox
recipe](outbox.md).

## Cleanup and capacity

Active records use `lease_expires_at + Retention` as their initial purge
deadline. Heartbeats extend it. Terminal, abandoned, and explicitly expired
records use transition time plus `Retention`.

Run cleanup continuously in bounded batches:

```go
for {
	deleted, err := store.Cleanup(ctx, 500)
	if err != nil {
		return err
	}
	if deleted < 500 {
		break
	}
}
```

Cleanup orders by `purge_at` and uses `FOR UPDATE SKIP LOCKED`, so multiple
workers may cooperate without waiting on the same rows. Valid batch sizes are
1 through 10,000. Alert on oldest overdue `purge_at`, table and index bytes,
dead tuples, cleanup errors, transaction latency, lock waits, and connection
pool saturation.

## Permissions, privacy, and compatibility

The database role needs `SELECT`, `INSERT`, `UPDATE`, and `DELETE` on
`idempotency_records`, plus normal use of transaction advisory locks. Use
`search_path` to place the fixed table name in an application-owned schema.

The primary key is opaque, but JSONB contains namespace, tenant, operation,
caller, source key, result, and metadata for strict reconstruction and rolling
compatibility. Treat the table as sensitive application data: restrict access,
encrypt storage and backups, avoid secrets in keys or metadata, and set
retention to the shortest operationally useful window.

Persisted schema version `1` rejects unknown versions and malformed keys,
fingerprints, states, counters, timestamps, results, and metadata. Deploy code
that can read every record version still inside retention before writing a new
version.
