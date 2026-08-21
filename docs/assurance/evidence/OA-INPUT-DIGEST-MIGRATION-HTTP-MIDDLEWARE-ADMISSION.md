# HTTP Middleware Admission Input-Digest Migration

Observed at `2026-08-19T14:42:36Z` on `darwin/arm64` with Go `1.26.6`.

## Change Boundary

The `pkg/http-middleware/admission` regression suite now proves that a request
which acquires free capacity immediately does not enter the bounded waiter
queue or inherit its configured delay. This kills the previously surviving
logical-condition mutant at the free-capacity boundary.

No production source, public API, HTTP behavior, runtime configuration,
dependency, service image, or retained HTTP reference composition changed.
The operational-assurance input digest therefore moves from
`70a99bef8a8212a380db57e393c828e8edbf499d9e3abfaa7e758c7f5418b346` to
`28ba6aea48363328ba0a46ecf9f7ce65826aca9e87f696abf4a8e680135b3c5b`.

## Behavioral Proof

The focused admission test passed, and mutation testing killed all 27 viable
mutants in `pkg/http-middleware/admission` while reusing every unchanged
package checkpoint by exact content identity. The complete strict
`pkg/http-middleware` module contract passed every mandatory gate, including
exact 100% statement coverage and mutation effectiveness.

## Claim Boundary

This evidence authorizes only the exact one-way digest transition above for
`pkg/http-middleware`. It preserves the earlier HTTP reference observation
without relabeling its execution time, rerunning it, or extending its claims.
