# Performance

Run `make benchmark BENCH_TIME=1s` on an idle pinned host. The suite reports
allocations and compares maintained implementations: Google UUID, oklog ULID,
Jetify TypeID, Segment KSUID, and matoous NanoID. Results are evidence for that
machine and commit, not permanent API guarantees.

Generation comparisons must retain semantic labels. This package's ULID,
UUIDv7, TypeID, and KSUID generators increment state inside a time bucket,
while some reference generators acquire fresh randomness for every value.
Faster monotonic results therefore do not prove a generally faster random
algorithm. Compare equivalent entropy, clock, locking, and ordering guarantees.

UUIDv7, ULID, TypeID, and KSUID can improve insertion locality relative to
uniform random keys, but locality depends on concurrent generators, database
page size, fill factor, collation, and workload. Benchmarks do not replace a
database workload replay. NanoID is random and makes no locality claim.

Parsing and formatting benchmarks use canonical values. Malformed-input costs
are separately fuzzed because early rejection paths are intentionally uneven.

## Reference run

The hardening reference run used commit `721663a`, Go 1.26.5, darwin/arm64,
an Apple M4 Max, and `BENCH_TIME=100ms make benchmark`. The raw command output
is the release evidence; representative results from that run are below in
nanoseconds per operation. They are observations, not regression thresholds.

| Operation | Family | identifier | Maintained reference |
| --- | --- | ---: | ---: |
| Generate | UUIDv4 | 330.1 | 509.1 |
| Generate | ULID | 44.23 | 124.9 |
| Generate | TypeID | 51.61 | 171.3 |
| Generate | KSUID | 41.94 | 270.2 |
| Generate | NanoID | 284.0 | 369.8 |
| Parse | UUID | 81.16 | 20.78 |
| Parse | ULID | 248.4 | 18.54 |
| Parse | TypeID | 217.1 | 78.17 |
| Parse | KSUID | 357.9 | 86.52 |
| Format | UUID | 40.27 | 61.93 |
| Format | ULID | 25.55 | 30.55 |
| Format | TypeID | 117.4 | 2.009 |
| Format | KSUID | 198.9 | 197.7 |
| Sort 1,024 keys | UUID | 17,275 | 8,212 |
| Sort 1,024 keys | ULID | 12,209 | 8,320 |
| Sort 1,024 keys | KSUID | 9,128 | 7,962 |

The database-locality proxy searches a sorted 4,096-key set in generation
order. The same run measured 70.42 ns for random UUIDv4, 66.40 ns for ordered
UUIDv7, 51.99 ns for ordered ULID, and 89.72 ns for ordered KSUID. This is a
CPU cache and branch-prediction proxy only; it is not a PostgreSQL claim.
