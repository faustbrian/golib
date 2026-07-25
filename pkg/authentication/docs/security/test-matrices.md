# Security test matrices

## Protocol and interoperability

| Surface | Positive vectors | Negative vectors |
| --- | --- | --- |
| HTTP | RFC 7617 Basic, RFC 6750 Bearer, explicit API-key sources | duplicates, mixed sources, controls, oversize fields, malformed encodings |
| Challenge | deterministic schemes and escaped parameters | controls, invalid tokens, parameter count and size limits |
| JWT | RFC 7520 JWK, valid compact JWS, rotation | algorithm/key mismatch, ambiguous IDs, invalid metadata, critical headers, claim/date bounds |
| OIDC | OIDC Core claim set, discovery, known-key outage | issuer query/redirect, unknown keys, invalid dates/party/nonce, oversized responses |
| Static keys | current/previous overlap and atomic replacement | revoked, unknown, duplicate, empty, oversized sets |

The exact static data is recorded in [vectors.md](vectors.md). Executable
sources are `authhttp/interoperability_test.go`,
`jwt/interoperability_test.go`, and `oidc/interoperability_test.go`.

## Failure injection and lifecycle

| Failure | Expected behavior | Evidence |
| --- | --- | --- |
| Issuer timeout or 5xx | known OIDC key remains usable; unknown key is unavailable | `oidc/remote_test.go` |
| Refresh stampede | one network owner, bounded cancellation-aware waiters | `oidc/remote_concurrency_test.go` |
| JWT close during refresh | admitted work cancelled and drained before close returns | `jwt/remote_test.go` |
| Close deadline | returns context error without admitting more work | `jwt/remote_test.go` |
| Instrumenter failure | authentication result is unchanged | instrumentation and adapter tests |
| Callback rejection/unavailability | stable typed failure, no wrapped secret rendered | root and adapter tests |

## Required local gates

| Gate | Command |
| --- | --- |
| Format, vet, tests, boundaries, examples, API | `./scripts/check-all.sh` |
| Exact statement coverage | `./scripts/check-coverage.sh` |
| Race detector | `go test -race ./...` in every module |
| Fuzz smoke | `FUZZ_TIME=10s ./scripts/check-fuzz.sh` |
| Vulnerabilities | `govulncheck ./...` in every module |
| Static analysis | `golangci-lint run ./...` in every module |
| Workflow and shell syntax | `actionlint` and `shellcheck scripts/*.sh` |
| Reproducible archive | build twice and compare SHA-256 output |

Benchmarks in the repository cover static authentication, extraction, JWT,
and OIDC validation. They are regression signals rather than timing-side-
channel proofs; constant-time static comparison is established by fixed-size
digests, `subtle.ConstantTimeCompare`, and bounded full scans.
