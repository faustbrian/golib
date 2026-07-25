# Performance and complexity

| Operation | Complexity | Bound |
| --- | --- | --- |
| construction | `O(R log R + E log E)` | 64/day, 4,096 exceptions |
| daily query | `O(log E + R + F)` | bounded fragments |
| point query | daily query plus timezone conversion | one date |
| composition | `O(F_left + F_right)` per date | depth 16 |
| transition search | dates in horizon times daily query | 366 days |
| canonical encoding | schedule cardinality | 1 MiB output |

## Enforced budgets

| Resource | Hard limit | Failure |
| --- | --- | --- |
| ranges owned by one weekday | 64 | `CodeLimitExceeded` |
| exact-date exceptions | 4,096 | `CodeLimitExceeded` |
| exception source, revision, or set name | 128 bytes | typed construction/encoding error |
| metadata field | 256 bytes | `CodeLimitExceeded` |
| canonical or parsed JSON | 1 MiB | `CodeLimitExceeded` or `CodeInvalidEncoding` |
| JSON nesting | 64 levels | `CodeLimitExceeded` |
| composition expression | 16 levels | `CodeLimitExceeded` |
| returned interval fragments | 8,192 | `CodeLimitExceeded` |
| transition or instant-range horizon | 366 elapsed days | `CodeInvalidHorizon` or `CodeInvalidInterval` |
| human summary | 64 KiB | `CodeLimitExceeded` |

No operation retains input-dependent caches. Construction allocates in
proportion to bounded input cardinality. Daily queries allocate in proportion
to bounded fragments; transition and instant-range searches evaluate at most
368 civil dates to cover timezone boundary skew and then terminate with a typed
search or interval error. Composition cannot expand recursively beyond its
depth cap.

The prepared `compile.Index` owns an immutable canonical copy and relies on the
root's sorted exception index. It has no cache, lock, or cleanup lifecycle.

Run `make benchmark` for allocations. The seven categories cover construction,
normalization, daily lookup, transition search, large exception sets,
composition, and canonical encoding. Results are machine-specific and are not
contractual latency guarantees.

Mutation testing runs in a disposable repository copy and enforces a minimum
score of 0.65. Override `MUTATION_MIN_SCORE` only to raise the release threshold;
lowering it changes the repository's release policy and must be reviewed like
any other gate change.
