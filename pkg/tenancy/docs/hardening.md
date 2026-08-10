# Hardening evidence

The executable suite covers identity normalization and hostile bytes; scope
construction and conflicts; cancellation and deadlines; HTTP and JSON-RPC
duplicates, spoofing, and size limits; every integration domain; namespace
separation; bounded asynchronous close and shutdown; administrative resume and
partial failure; PostgreSQL pool reuse, rollback, readback, reset failure, and
RLS plans; randomized multi-tenant models; races; and fuzz targets.

Integration-domain evidence covers the owned `Integration` contract and its
state model. The clean-consumer gate additionally composes application-owned
adapters with the first-party cache backend, bounded search contract provider,
queue/CloudEvents conversion, workflow values, audit store, and OpenTelemetry
SDK. The OpenSearch matrix executes the same scoped search adapter against
OpenSearch 2.19.6 and 3.8.0 with identical logical index and document IDs for
two tenants. Provider paths not declared by those fixtures remain outside the
claim.

The build-tagged PostgreSQL test adds a restricted application login, forced
and restrictive RLS, cross-tenant reads and mutations, alternating prepared
plans, rollback, cancellation, stale state, backend replacement, and concurrent
pool proof. It is valid only when run against a real PostgreSQL service with
`POSTGRES_URL`; a skipped run is not interoperability evidence.

Coverage and mutation gates operate per production package. Exact statement
coverage does not by itself prove meaningful assertions, and mutation results
do not cover equivalent or invalid mutants without a reviewed record. Fuzzing
is bounded in CI and should be supplemented by longer scheduled runs. Race
testing proves only executed schedules. No evidence applies to consumer paths
that bypass explicit scope, namespace, transaction, or propagation seams.

Benchmark results must report Go version, OS, architecture, CPU, benchtime,
latency, and allocations. Compare only identical behavior and corpus inputs;
the repository stores benchmark output as gate evidence rather than claiming a
universal performance threshold.

The external administrative fixture uses a synchronized, fsync-before-rename
journal, bounded fan-out, fresh tenant contexts, stable operation identities,
and per-tenant actor, purpose, reference, attempt, and completion records. It
reopens the journal after partial failure and proves completed tenants are not
repeated while failed imports resume; a separate migration operation remains
tenant-attributed. This proves the documented application-owned pattern, not a
cluster-wide ledger implementation supplied by `tenancy`.
