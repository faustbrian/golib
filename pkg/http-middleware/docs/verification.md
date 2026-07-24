# Verification evidence

The release gate is reproducible from a clean checkout with `make check-all`.
No test requires a running database, proxy, cache, exporter, or other
production service.

| Evidence | Blocking command and acceptance |
|---|---|
| behavior and HTTP integration | `go test ./...` and `make integration`; all pass |
| sibling interoperability | `make sibling-integration`; pinned router and service contracts pass |
| race safety | `make race`; no race report |
| meaningful coverage | `make coverage`; every production package is 100.0% |
| fuzz smoke | `make fuzz`; every declared target completes without failure |
| mutation | `make mutation`; 59/59 curated security decisions killed |
| leaks | `make leak`; goleak reports no middleware-owned goroutine |
| standards policies | `make standards`; proxy, CORS, content, compression, and headers pass |
| response capabilities | real HTTP/1.1 and HTTP/2 plus nested interface matrix pass |
| privacy | observation tests exclude payload data and bound all labels |
| performance | `make benchmark`; latency and allocations are reported with parameters |
| dependencies | `make vuln`, module update audit, and license review pass |
| architecture | `make architecture`; no forbidden runtime mechanism or sibling dependency |
| static quality | vet, lint, Staticcheck, actionlint, docs, API, tidy, and format pass |
| advisory nil analysis | `make nilaway`; visible but intentionally non-blocking |

Fuzzing covers descriptor names, request IDs, request body limits, forwarding
fields, CORS origins and preflights, media negotiation, content coding, and
configured security headers. The curated mutation
set targets composition depth, duplicates, conditions, short circuits, trust
selection, CORS decisions, coding and media negotiation, limits, cancellation,
timeouts, status commitment, recovery, and observation privacy. Each mutant
runs in an isolated copy with a five-second test timeout.

Benchmarks cover empty and deep chains, request IDs, proxy parsing, CORS
preflights, compression, and contended admission. Results are machine-specific;
the release record must preserve Go version, OS, architecture, CPU, payload,
concurrency, and benchmark duration with any regression claim.

## Latest local release run

On 2026-07-18, `make check-all` passed with Go 1.26.5 on Darwin arm64,
Apple M4 Max. It reported 100.0% production statement coverage, 59/59 killed
mutants, eight two-second fuzz targets, no race, leak, vulnerability, lint,
Staticcheck, API, documentation, architecture, or NilAway finding, and green
real HTTP plus pinned sibling integration suites.

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| empty chain | 389.8 | 1,056 | 11 |
| 128-layer chain | 383.2 | 1,056 | 11 |
| request ID | 577.4 | 1,521 | 18 |
| trusted proxy parse | 989.5 | 1,945 | 18 |
| CORS preflight | 1,854 | 1,968 | 36 |
| gzip response | 42,645 | 817,428 | 58 |
| contended admission | 536.8 | 1,062 | 11 |

These 100 ms benchmark samples are evidence for this machine, not portable
service-level objectives. The enforced observation ceiling is separately
machine-independent.
