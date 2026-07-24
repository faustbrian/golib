# Performance baseline

The approved default and migration baseline was measured on 2026-07-16 with Go
1.26.5 on Apple M4 Max (`darwin/arm64`). One benchmark iteration was used to
avoid repeatedly consuming large Argon2 memory during smoke gates.

| Benchmark | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| `BenchmarkApprovedArgon2id/hash` | 66,096,291 | 67,114,920 | 47 |
| `BenchmarkApprovedArgon2id/verify` | 66,623,000 | 67,111,752 | 31 |
| `BenchmarkApprovedBcrypt/hash` | 51,314,709 | 7,824 | 17 |
| `BenchmarkApprovedBcrypt/verify` | 50,885,167 | 7,840 | 18 |

Run:

```sh
go test -run '^$' -bench 'BenchmarkApproved' -benchmem -benchtime=1x ./...
```

Numbers are not portable across CPUs, quotas, runtime versions, or competing
workloads. Release review records trends; deployment policy is selected from
container-constrained measurements described in
[Kubernetes sizing](kubernetes-sizing.md).

## Representative Kubernetes budget

`make kubernetes-bench` runs the same approved benchmarks in pinned Go Linux
with cgroup limits of 2 CPUs, 512 MiB memory, no swap, and `GOMAXPROCS=2`. The
gate verifies the cgroup files before running. On 2026-07-17, Docker 29.6.1 on
Apple M4 Max produced this `linux/arm64` one-shot baseline:

| Benchmark | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| `BenchmarkApprovedArgon2id/hash` | 111,527,833 | 67,112,760 | 46 |
| `BenchmarkApprovedArgon2id/verify` | 105,627,584 | 67,111,864 | 32 |
| `BenchmarkApprovedBcrypt/hash` | 65,199,458 | 5,816 | 19 |
| `BenchmarkApprovedBcrypt/verify` | 79,704,083 | 5,832 | 20 |

The CI run repeats the cgroup-constrained check on `linux/amd64`; these numbers
are capacity evidence for the stated representative limit, not a cross-node
latency promise.

The timing smoke suite interleaves Argon2id and bcrypt match/mismatch samples,
compares p10, p50, and p90, and fails only on an obvious fivefold regression.
It also proves malformed hashes stay on the intentionally faster pre-primitive
path. These are regression alarms, not statistical proof of constant-time
behavior.
