# Hardening evidence

This document maps release claims to reproducible local gates. It is updated
with final command results only after the current tree passes.

| Claim | Evidence target |
| --- | --- |
| boundary/precedence/algebra correctness | formal tables, pairwise properties, mutation |
| timezone gaps/folds/history | vectors, Go differential matrix, fuzz |
| immutability/concurrency | alias tests and `make race` |
| strict parser/resource bounds | hostile tests, fuzz, coverage |
| persistence | SQL/pgx matrix and PostgreSQL 14-18 workflow |
| API and docs | `make api-compat docs` |
| dependency security | `make vuln`, lint, advisory NilAway |
| exact statement proof | `make coverage` reports 100.0% |
| no hidden lifecycle/resource | source scan, compiled-index and callback tests |

Hosted GitHub Actions are intentionally not represented as locally executed
evidence. They are the final verification boundary after all local work is
complete.

## Audit coverage

| Required audit | Executable or formal evidence |
| --- | --- |
| schedule states and ownership | `schedule_test.go`, `normalization_test.go`, range-state table |
| exception conflicts and precedence | `availability_test.go`, `precedence_property_test.go`, precedence table |
| overnight spill on both dates | `availability_test.go`, `composition_test.go`, overnight table |
| DST gaps, folds, skipped dates, history | `timezone_test.go`, `timezone_differential_test.go`, DST table |
| algebra, query agreement, termination | `algebra_property_test.go`, `query_range_test.go`, search fuzz |
| strict parsing and precision | `encoding_test.go`, root/encoding/postgres fuzz targets |
| legacy migration | Location, Track, Postal, and Spatie fixtures and compatibility matrix |
| SQL and pgx persistence | scanner/valuer tests, pgx codec tests, PostgreSQL workflow |
| immutable concurrent ownership | caller-alias tests, compiled-index stress, race gate |
| security and resources | hostile limits, source scan, vulnerability and leakless-lifecycle audit |

## Local evidence snapshot

The final local run uses the repository's pinned Go 1.26.5 toolchain and
published module revisions directly; no local `replace` or temporary Go
workspace is required.

| Command | Result |
| --- | --- |
| `make lint` | 0 issues |
| `make nilaway` | no advisory findings |
| `make coverage` | 100.0% in every production package |
| `make race` | pass |
| `FUZZ_TIME=2s make fuzz` | eight targets pass |
| `make mutation` | 754 mutants; 526 killed; score 0.697613; zero tool errors |
| `make vuln` | no vulnerabilities found |
| `make benchmark timezone docs api-compat` | pass |
| `POSTGRES_URL=... make integration` | PostgreSQL 14-18 JSONB matrix passes under race |

This snapshot was refreshed from the final local candidate on 2026-07-17.
Hosted GitHub Actions remain a separate boundary because a local run cannot
prove runner or service state.
