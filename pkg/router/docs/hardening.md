# Hardening Evidence

The release-equivalent local gate includes formatting, module tidiness, vet,
Staticcheck, strict golangci-lint, unit and differential tests, exact production
statement coverage, race, fuzz smoke, mutation, benchmarks, security scans,
documentation links, exported API compatibility, workflow validation, and
cross-repository integration compilation. NilAway is visible and advisory.

Security fixtures cover escaped slashes, traversal redirects, hostile hosts,
authority injection, dot segments, Unicode/IDNA policy, parameter injection,
open-redirect boundaries, query limits, and safe diagnostics. Concurrency tests
exercise canceled and active dispatch, handler context, URL generation, and
introspection against one compiled router. Transactional compile fixtures prove
all standard-library conflicts are resolved before middleware construction.
The mutation gate targets validation, method, host precedence, wildcards,
middleware order, and encoding decisions.

The structural safety gate rejects production goroutine launches, global HTTP
registration, unsafe, cgo, linkname, initialization hooks, and
reflection-driven discovery. Production state belongs only to explicit
builders and compiled router values; no watcher or process-global route cache
exists.

Fuzz smoke covers route patterns, named segment round trips, request targets,
host authorities, nested transactional group composition, and absolute URL
inputs. Differential fixtures cover every standard method class, an extension
token, every supported path-pattern class, literal hosts, redirects, query
preservation, escaped segments, 404, and 405. Benchmarks report allocations for
compilation, dispatch, middleware depth, route-table copying, and URL
generation. Exact budgets and behavior matrices are documented separately.

The executable matrices also cover custom 404 and 405 handlers after partial
writes, canceled contexts, and panics, plus middleware construction panics.
Malformed requests are proven to bypass custom miss handlers and route
middleware, even with an already canceled context. Application panics
deliberately propagate; recovery remains explicit middleware policy.

## Latest local release evidence

The 2026-07-18 Go 1.26.5 Apple M4 Max `make check-all` run produced:

| Gate | Result |
| --- | --- |
| Format, module tidiness, vet, Staticcheck, and golangci-lint | Passed; zero lint issues |
| Unit, compatibility, security, and property suites | Passed |
| Production statement coverage | 100.0% in both packages |
| Race | Passed in both packages |
| Fuzz smoke | All six targets passed at two seconds each |
| Mutation | 405 killed, 152 lived, zero uncovered or timed out; 72.71% efficacy and 100.00% mutator coverage |
| Cross-repository integration | Passed under the race detector |
| Vulnerability scan | No vulnerabilities found |
| Documentation, API fingerprint, safety, provenance, and workflows | Passed |
| NilAway advisory | Ran visibly with no findings |

Benchmarks from the same run are recorded in [Performance](performance.md).
Fresh command output remains authoritative; rerun `make check-all` for a new
tree. Retained machine-readable artifacts are `coverage.out` and
`mutation-results.json` when their targets execute.
