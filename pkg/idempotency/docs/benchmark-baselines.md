# Benchmark baselines

Benchmarks are release regression signals, not universal latency guarantees.
Backend results depend on hardware, connection topology, durability settings,
and concurrent load. Record the exact environment with every release baseline.

## Required commands

```sh
go test -run '^$' -bench . -benchmem -count=3 ./memory
POSTGRES_URL="$DSN" \
  go test -run '^$' -bench . -benchmem -count=3 ./postgres
VALKEY_ADDR="$ADDR" \
  go test -run '^$' -bench . -benchmem -count=3 ./valkey
```

Run each command at least three times in a quiet environment, retain the raw
output as a release artifact, and compare old and new samples with `benchstat`.
The baseline table below is updated only from a current full run. It must state
Go version, operating system, architecture, CPU, backend versions, topology,
and durability configuration. Do not compare a loopback standalone backend to
a production network or clustered topology as if they were equivalent.

## Current baseline

Captured 2026-07-15 with Go 1.26.5 on Darwin arm64, Apple M4 Max. Values are
the median of three samples. PostgreSQL was 17.10 in a local Docker container
with its image defaults. Valkey was 9.0 in a local standalone Docker container
with AOF enabled and `maxmemory-policy noeviction`. Both used loopback TCP.

| Benchmark | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Memory replay | 108.8 | 8 | 1 |
| Memory hot-key replay contention | 262.8 | 8 | 1 |
| Memory acquire/complete lifecycle | 660.6 | 822 | 9 |
| PostgreSQL acquire/complete lifecycle | 1,213,408 | 12,247 | 150 |
| PostgreSQL replay | 479,019 | 3,937 | 63 |
| PostgreSQL hot-key replay contention | 361,840 | 4,455 | 64 |
| PostgreSQL cleanup batch of 100 | 1,131,115 | 446 | 7 |
| Valkey replay | 123,422 | 5,970 | 69 |
| Valkey hot-key replay contention | 22,404 | 5,799 | 70 |

PostgreSQL result-storage medians from the same run were:

| Result bytes | ns/op | B/op | allocs/op |
| ---: | ---: | ---: | ---: |
| 0 | 1,144,627 | 13,217 | 148 |
| 1,024 | 1,216,471 | 21,849 | 152 |
| 65,536 | 1,815,722 | 527,175 | 158 |
| 1,048,576 | 7,281,812 | 9,375,234 | 170 |

Result-storage benchmarks separately cover empty, 1 KiB, 64 KiB, and 1 MiB
PostgreSQL results. Any statistically meaningful regression requires
investigation or an explicit budget change in `CHANGELOG.md`. A release must
not weaken semantic checks merely to recover benchmark throughput.
