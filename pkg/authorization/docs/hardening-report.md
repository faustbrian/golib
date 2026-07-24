# Hardening findings and evidence

This report records the security properties audited for the initial engine. It
is maintained with the implementation and should be refreshed for releases
that change decision semantics, persistence, or distribution.

## Findings

No known authorization bypass, tenant escape, partial snapshot activation,
unbounded evaluation path, or sensitive telemetry label remains. Evaluator
panics are contained as fail-closed errors. PostgreSQL initialization is guarded
by expected revision zero, and Valkey publication is only a monotonic wakeup
hint.

Residual operational risks are explicit: callers can bypass freshness by using
the raw engine, application request mappers can supply untrusted facts, and
policy-administration credentials can publish malicious but structurally valid
policy. Sensitive operations must authorize through `policy.Synchronizer`;
mapper trust and publisher approval remain application responsibilities.

## Tenant-isolation proof matrix

| Boundary | Isolation rule | Evidence |
| --- | --- | --- |
| ACL entry | exact tenant; global opt-in only | global/tenant/resource scope and cross-tenant tests |
| RBAC role graph | role, parent, permission, assignment tenant must agree | mismatch, cross-parent, inheritance, and manager tests |
| ABAC rule | exact request tenant | tenant rule tests and shared conformance |
| Snapshot | one complete immutable revision per decision or batch | concurrent evaluate/reload and rolling differential tests |
| PostgreSQL | one atomic manifest row with optimistic revision | unit failure injection and real service integration test |
| Valkey | revision only; payload cannot activate policy | duplicate, reorder, outage, polling, and real service tests |
| Advisory cache | exact revision key and repository verification | codec, stale-revision loader, and cache adapter tests |
| HTTP/RPC | explicit mapper and fail-closed handlers | denied, error, invalid request, and context tests |

## Automated evidence

- Exhaustive three-valued composition truth tables through length four.
- Shared ACL/RBAC/ABAC allow, deny, and not-applicable conformance.
- Model tests for duplicates, conflicts, cycles, diamonds, depth, cost,
  cardinality, cancellation, batches, revocation, and listing bounds.
- Strict decoder tests and fuzz targets for manifest, ACL, RBAC, and ABAC JSON.
- Iterative typed and portable ABAC condition preflight that rejects depth
  exhaustion before recursive semantic construction or validation.
- Pre-parse model and manifest byte limits plus compiler policy, per-document,
  and aggregate-document limits.
- Atomic replacement, concurrent replacement, concurrent evaluation/reload,
  dry-run, diff, and rolling-deployment differential tests.
- Fail-closed repository freshness tests for startup, exact staleness boundary,
  failed verification, same-revision verification, and clock rollback.
- PostgreSQL and Valkey unit failure injection plus environment-gated real
  integration suites and hosted PostgreSQL 16/17 and Valkey 7/8 matrices.
- Real-service optimistic-update races, canceled-write preservation, PostgreSQL
  backend reconnection, and Valkey client reconnection.
- Published `authentication`, `log`, and `telemetry` consumer-contract
  tests outside the core dependency graph.
- Race detector, 100 percent production statement coverage, lint, vet,
  vulnerability, API compatibility, examples, docs, and workflow checks.
- Pinned whole-module mutation gate with an 85 percent efficacy and 95 percent
  mutant-coverage floor.
- Controlled whole-module mutation run: 584 killed, 26 lived, 29 not covered,
  and 9 deliberate nontermination timeouts (95.74 percent efficacy and 95.46
  percent mutant coverage). Every lived decision-path mutant was reviewed; none
  changes a deny or non-applicable result into an allow.
- Benchmarks for cold and warm decisions, batches, policy sizes, inheritance,
  predicates, compilation, and reload.

## Compatibility evidence

`policy/testdata/v1` contains reviewable manifests for every first-class model.
The compatibility test decodes, compiles, re-encodes, and decodes each corpus
entry. `postgres/testdata/schema-v1-up.sql` pins the migration schema and is
compared byte-for-byte with the exported migration. Runtime snapshots are not a
serialized contract; the manifest corpus is their portable source form.

## Local commands

```sh
./scripts/check-format.sh
./scripts/check-docs.sh
go test ./...
go test -race ./...
go vet ./...
./scripts/check-coverage.sh
./scripts/check-mutation.sh
golangci-lint run ./...
govulncheck ./...
./scripts/check-api.sh
actionlint .github/workflows/*.yml
go test -run '^$' -bench '^Benchmark' -benchmem -benchtime=100ms ./...
```
