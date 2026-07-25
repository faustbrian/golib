# Compatibility matrix

| Component | Supported | Verification |
| --- | --- | --- |
| Go | 1.25, 1.26 | unit matrix on Linux, macOS, Windows; race on Linux |
| pgx | 5.10.x | pinned module build, tests, API review |
| PostgreSQL | 14, 15, 16, 17, 18 | Testcontainers matrix covering failure semantics and every transaction-mode combination |
| OpenTelemetry Go | 1.44.x | `otelpostgres` unit tests |
| Testcontainers Go | 0.43.x | integration and lifecycle tests |
| telemetry `gopostgres` | aligned pgx 5.10.x / OTel 1.44.x | independent query-tracer package test |

The PostgreSQL range follows the upstream pgx policy at the time of the first
release. Changing the minimum or maximum supported major requires a changelog
entry, compatibility documentation, matrix update, real integration run, and
review of SQLSTATE/session behavior.

Changing pgx minor versions requires reviewing release notes and public
interfaces, then running `make check` and every PostgreSQL major job. The
module exposes native pgx types, so upstream additions are available directly;
upstream removals or behavioral changes may still require a major release here.
The current pgx hook audit covers `BeforeConnect`, `AfterConnect`,
`PrepareConn`, and `AfterRelease`; other hooks retain native pgx ownership.

Only PostgreSQL is supported. CockroachDB, YugabyteDB, proxies, and poolers may
implement overlapping protocols but require their own explicit evidence and
are not implied by this matrix.
