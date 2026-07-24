# Algorithm selection

Use Argon2id for all new hashes. The default is an explicit measured baseline,
not a timeless recommendation. Re-benchmark periodically and when CPU, memory,
Go, or `x/crypto` changes.

## Approved parameter table

| Field | Generated default | Verification ceiling | Accepted minimum |
| --- | ---: | ---: | ---: |
| Argon2 version | 19 | 19 only | 19 only |
| Time cost | 2 | 4 | 1 |
| Memory | 64 MiB | 128 MiB | 8 KiB per lane |
| Parallelism | 1 | 4 | 1 |
| Salt | 16 bytes | 64 bytes | 8 bytes |
| Output | 32 bytes | 64 bytes | 16 bytes |
| Bcrypt cost | Compatibility only | 14 | 4 |
| Password input | 1 KiB | 1 KiB | Empty is accepted |
| Encoded hash | At most 512 bytes | 512 bytes | Algorithm grammar |
| Active work | 4 operations | 4 operations | 1 operation |
| Waiting work | 16 callers | 16 callers | No queue |

Bcrypt additionally has a fixed 72-byte password ceiling for hashing and
verification, regardless of the general password-input limit. Custom policies
may lower or deliberately raise configurable ceilings, but operators must fit
the resulting per-operation and aggregate work inside their measured pod
budget. The parser always enforces the selected policy before primitive work.
Values above this table are outside the approved profile and require their own
security review, cgroup benchmark, concurrency stress, and rollout budget.

Bcrypt exists for current Laravel/PHP compatibility. A target Argon2id policy
marks successfully verified bcrypt hashes for upgrade. A bcrypt target never
marks Argon2id for downgrade, and higher bcrypt costs are preserved.

Argon2id rehash is component-wise monotonic for time, memory, salt length, and
output length. Parallelism mismatch is treated as a policy change because it
affects resource shape, but only when no other dimension would be lowered.
Incomparable parameter sets are preserved for explicit operator review.

No additional algorithm should be added without a demonstrated migration,
maintained implementation, authoritative vectors, strict encoding, parser
bounds, resource limits, fuzzing, and a documented removal/upgrade path.

Pre-hashing is intentionally absent. Adding it would create a new versioned,
incompatible password scheme and must never be transparent.
