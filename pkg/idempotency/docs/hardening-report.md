# Durable-idempotency hardening report

This report records the evidence gathered for the 2026-07-15 hardening audit.
The tests named below are executable release evidence, not claims inferred from
documentation.

## Findings

| ID | Finding | Resolution |
| --- | --- | --- |
| H-001 | Fingerprint policy versions had no length bound. | Closed: versions are limited to 128 bytes at construction and persisted-record decode. |
| H-002 | Owner-token bounds differed by adapter and malformed persisted tokens could exceed them. | Closed: all adapters and codecs use the 256-byte semantic maximum and fail closed. |
| H-003 | The process-local memory store could retain an unbounded number of records. | Closed: `MaxRecords` defaults to 10,000 and cannot exceed 1,000,000. New keys fail at capacity without blocking existing-key replay or transitions. |
| E-001 | PostgreSQL did not have live tests for deadlock, serializable abort, or pool saturation. | Closed: failure tests prove rollback and absence of partial record mutation. |
| E-002 | Valkey reconnect tests covered response loss but not replica promotion. | Closed: a Valkey 9 replica is synchronized, the primary is killed, the replica is promoted, and the same ownership proof completes. |
| E-003 | State and crash documentation lacked a complete executable evidence map. | Closed: shared conformance rejects every illegal mutation and the crash matrix exercises ownership, heartbeat, fencing, completion, and failure boundaries. |

No finding permits an exactly-once claim for an external side effect. The
remaining recovery obligations are stated in the threat model and crash guide.

## Audit evidence

| Requirement | Executable evidence |
| --- | --- |
| Concurrent acquisition and hot keys | `RunStoreConformance/concurrent acquisition elects one owner`; memory, PostgreSQL, and Valkey hot-key benchmarks |
| Duplicate completion | `RunStoreConformance/concurrent completion permits one terminal transition` |
| Complete transition matrix | `RunStoreConformance/missing records reject every record operation`, `inactive and terminal states reject every mutation`, and `live records reject explicit expiry` |
| Stale-owner fencing | `RunStoreConformance/takeover rejects every stale ownership mutation`; `TestCrashPointMatrix/expired owner continues after takeover`; `FuzzTakeoverPermanentlyFencesOldOwner` |
| Lease expiry, extension, and late heartbeat | `RunStoreConformance/heartbeat extends the exclusive lease boundary`; `TestHeartbeatExtendsLeaseAndRejectsLateOwner` |
| Cancellation | `RunStoreConformance/canceled operations do not mutate records` and adapter cancellation tests |
| Clock skew | `TestStoreUsesBackendClockForLeaseAuthority`, `TestNativeExecutorUsesLockedBackendClock`, `TestValkeyLeaseTimestampsUseServerTime`, and `TestLeaseMutationScriptsReadValkeyClock` |
| Process crash points | `TestCrashPointMatrix`; middleware panic and fresh cleanup-context tests |
| PostgreSQL deadlock | `TestPostgresDeadlockAbortsOneCompetingTransaction` |
| PostgreSQL serialization failure | `TestPostgresSerializableRollbackIncludesCompletion` |
| PostgreSQL response loss and reconnect | `TestPostgresUnknownCommitCanBeInspectedAfterReconnect` |
| PostgreSQL pool saturation | `TestPostgresPoolSaturationFailsWithoutMutation` |
| PostgreSQL transaction rollback | `TestPostgresCompleteTxCommitsWithBusinessEffect`, deadlock, and serializable tests |
| PostgreSQL cleanup contention | `TestPostgresConcurrentCleanupDeletesEachExpiredRowOnce` |
| Valkey script atomicity and expiry races | Valkey shared conformance, TTL, server-clock, and concurrent takeover tests |
| Valkey response loss and reconnect | `TestValkeyUnknownResultsCanBeInspectedAfterReconnect` |
| Valkey failover | `TestValkeyReplicaPromotionPreservesOwnership` |
| Valkey cluster routing | `TestValkey9ClusterConformance` and `TestRecordKeyIsOpaqueAndClusterSafe` |
| Valkey eviction safety | `TestValkeyOpenRejectsEvictingServer` and native backend checks |
| Hostile canonicalization | `TestJSONRejectsHostileOrAmbiguousInput`, `TestJSONEnforcesAllResourceLimits`, and `FuzzJSONIsIdempotent` |
| Encodings and versioned fingerprints | `FuzzBytesFingerprintPreservesEncoding`, `FuzzFingerprintPolicyVersionsRemainDistinct`, and shared cross-version conflict conformance |
| Persisted compatibility | PostgreSQL and Valkey `TestRecordVersion1FixtureRemainsReadableAndWritable`; both persisted-record fuzzers |
| Bounded results and metadata | shared invalid-data conformance, adapter codec tests, and HTTP/JSON-RPC replay-bound tests |
| Bounded diagnostics | `TestObserverWritesOnlyBoundedFields`, `TestObserverRecordsBoundedMetricAttributes`, and `TestNewHMACKeyHasherProtectsLogicalIdentity` |

## Release-blocker disposition

| Blocker | Disposition |
| --- | --- |
| Two current owners complete successfully | Blocked by atomic completion; 32 concurrent completions produce one success. |
| A stale owner overwrites a newer owner | Blocked in every store mutation and demonstrated against a fenced business resource. |
| A fingerprint conflict replays as equivalent | Blocked before replay, including equal digests with different policy versions. |
| Unbounded wait, lease, result, retry, cleanup, or memory | No package wait or retry loop exists; leases, results, metadata, cleanup batches, transition contexts, and memory records are bounded. Caller polling and retries remain caller-owned and must have deadlines. |
| Unsupported exactly-once claim | Documentation consistently describes at-most-one current owner and deterministic replay, with explicit external-effect ambiguity. |
| Missing meaningful 100% coverage or failing gate | Closed locally: exact production coverage is 100.0%, all release-equivalent commands pass, and `actionlint` validates both workflows. The configured hosted gates must still pass for the pushed commit or release tag. |

## Recovery obligations

After an unknown backend result, reconnect and inspect the authoritative record
before retrying. After a possible business side effect, reconcile using a
stable business identity. A stale-owner or lease-expired error proves only that
the idempotency transition was rejected. After Valkey failover, confirm the
promoted node contains the record and that the deployment's replication and
persistence policy met its acknowledged-write durability target.
