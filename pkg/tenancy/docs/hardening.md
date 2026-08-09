# Hardening evidence

The executable suite covers identity normalization and hostile bytes; scope
construction and conflicts; cancellation and deadlines; HTTP and JSON-RPC
duplicates, spoofing, and size limits; every integration domain; namespace
separation; bounded asynchronous close and shutdown; administrative resume and
partial failure; PostgreSQL pool reuse, rollback, readback, reset failure, and
RLS plans; randomized multi-tenant models; races; and fuzz targets.

The build-tagged PostgreSQL test adds live forced-RLS, prepared-statement,
cross-tenant mutation, rollback, and one-connection pool proof. It is valid only
when run against a real PostgreSQL service with `POSTGRES_URL`; a skipped run is
not interoperability evidence.

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
