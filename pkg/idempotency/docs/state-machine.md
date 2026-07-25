# State machine

An idempotency record is identified by all five key fields: namespace, tenant,
operation, caller, and caller-supplied value. The fingerprint is versioned and
describes the stable business request, not transport details such as tracing
headers, connection metadata, or JSON whitespace.

## States

| State | Meaning | Ownable |
| --- | --- | --- |
| `acquired` | An attempt owns a fresh lease but has not reported work. | Yes |
| `running` | The current owner heartbeated after starting work. | Yes |
| `completed` | A bounded successful result is available for replay. | No |
| `failed` | A bounded terminal failure is available for replay. | No |
| `expired` | Cleanup observed an elapsed active lease. | No |
| `abandoned` | The owner explicitly released without a terminal result. | No |

`expired` and `abandoned` retain the previous attempt and fence for audit. A
subsequent acquisition increments both the attempt and fencing token. It never
reuses an ownership proof.

## Transition matrix

| Current state | Operation and condition | Next state | Acquisition outcome |
| --- | --- | --- | --- |
| missing | acquire | `acquired` | `acquired` |
| `acquired`, `running` | acquire; same fingerprint; lease live | unchanged | `in_progress` |
| `acquired`, `running` | acquire; same fingerprint; lease elapsed | `acquired` | `stale_owner_takeover` |
| `completed` | acquire; same fingerprint | unchanged | `replayed` |
| `failed` | acquire; same fingerprint | unchanged | `terminal_failure` |
| any existing state | acquire; different fingerprint | unchanged | `conflict` |
| `expired`, `abandoned` | acquire; same fingerprint | `acquired` | `acquired` |
| `acquired`, `running` | heartbeat; current proof; lease live | `running` | n/a |
| `acquired`, `running` | complete; current proof; lease live | `completed` | n/a |
| `acquired`, `running` | fail; current proof; lease live | `failed` | n/a |
| `acquired`, `running` | release; current proof; lease live | `abandoned` | n/a |
| `acquired`, `running` | expire; lease elapsed | `expired` | n/a |

Every operation not listed is illegal. An owner token or fencing-token mismatch
is `stale_owner`, even if the supplied proof belonged to an earlier legitimate
attempt. A matching proof after its lease boundary is `lease_expired`. Terminal
records cannot be overwritten or expired.

The lease boundary is exclusive: an ownership operation is accepted only when
the backend's authoritative current time is strictly before `lease_expires_at`.
This makes equality deterministic and prevents an adapter-specific grace
period.

## Fencing invariant

Within one retained record, each ownership attempt has a strictly greater
nonzero fencing token. Completion, failure, heartbeat, and release compare both
the opaque owner token and fencing token atomically with the state update.
Therefore two callers cannot both commit an idempotency record as the current
owner.

Deleting a record after its retention period ends that fencing domain. A later
record for the same logical key may begin again at fence `1`. Retention must
cover every period in which an application compares numeric fences, or the
application must include a record generation in its fencing invariant.

That invariant covers the idempotency record only. To protect a business side
effect, store the highest accepted fencing token with the affected business
entity and condition the write on the incoming token being newer. If the side
effect cannot enforce a fence or an equivalent invariant, a takeover can cause
duplicate external effects.

## Resource bounds

The current semantic maxima are:

| Resource | Maximum |
| --- | ---: |
| Each key identity part | 256 bytes |
| Fingerprint policy version | 128 bytes |
| Owner token | 256 bytes |
| Lease | 24 hours |
| Serialized result | 1 MiB |
| Metadata entries | 32 |
| Metadata key | 128 bytes |
| Metadata value | 1 KiB |

Limits are measured in bytes, not Unicode code points. Integrations may impose
smaller limits. Empty results and metadata are valid for deduplication-only
operations.

The memory adapter additionally bounds retained records: zero configuration
selects 10,000 records and the configurable maximum is 1,000,000. See the
[resource budgets](resource-budgets.md) for integration and cleanup limits.

## Executable proof

`idempotencytest.RunStoreConformance` applies this matrix to memory,
PostgreSQL, and Valkey. It rejects every ownership mutation against missing,
stale, inactive, terminal, or expired ownership; rejects live and terminal
expiry; permits only one of 32 concurrent completions; and proves conflict
precedence when a policy version changes. `memory.TestCrashPointMatrix` adds a
fenced business-resource model in which an expired owner continues after a
takeover but cannot overwrite the newer fence. The full evidence map is in the
[hardening report](hardening-report.md).
