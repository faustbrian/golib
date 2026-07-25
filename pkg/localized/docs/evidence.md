# Verification evidence

The release claim requires fresh output from the current tree:

| Claim | Command |
|---|---|
| Formatting | `make format-check` |
| No unsafe/cgo/linkname/globals | `./scripts/check-go-safety.sh` |
| Static analysis | `make vet && make lint` |
| Concurrent safety | `make test-race` |
| Statement coverage | `make coverage` |
| Clean declared dependency source | `make dependency-revisions` |
| Hostile parser survival | `make fuzz FUZZ_TIME=10s` |
| Decision-test strength | `make mutation` |
| Standards and legacy vectors | `make standards` |
| PostgreSQL JSONB | `make postgres POSTGRES_URL=...` |
| Disposable PostgreSQL 14-18 matrix | `make postgres-matrix` |
| Allocation baselines | `make benchmark BENCH_TIME=1s` |
| Allocation ceilings | `go test ./... -run '^TestAllocationBudgets$'` |
| Documentation | `make docs` |
| Public API drift | `make api-check` |
| Reachable vulnerabilities | `make vuln` |
| Advisory nil flows | `make nilaway-advisory` |
| Workflow syntax | `make workflow-lint` |

`make check` combines all local non-PostgreSQL, non-mutation blocking gates.
Mutation and each PostgreSQL version remain explicit because they are slower and
need dedicated resources.

For a final release-equivalent audit while sibling repositories contain
uncommitted work, run the same gates through clean archived revisions:

```sh
./scripts/check-dependency-revisions.sh make check
./scripts/check-dependency-revisions.sh make postgres-matrix
```

Run `make mutation` directly. Gremlins copies the module to mutate it, so a
workspace wrapper would redirect its tests away from the mutant tree.

The production coverage denominator excludes `localizedtest`: that package is
consumer test support whose remaining statements deliberately call
`testing.TB.Fatal`. All runtime packages, including adapters, must individually
report 100.0%. The mutation gate targets conditional negation across those
runtime packages at 100% mutant coverage and efficacy; allocation-only and
equivalent arithmetic/boundary operators are non-blocking.

The final hardening report MUST record exact commands, versions, failures,
skips, and remaining uncertainty. A hosted green workflow is not a substitute
for missing local requirement evidence, and missing hosted state does not block
local implementation.
