# Hardening inventory and security review

The release inventory is the exported API baseline in `api/baseline.txt`, the
record and canonical structures in `record.go` and `canonical.go`, the sink and
delivery contracts in `sink.go` and `delivery.go`, privacy in `privacy.go`,
query and export in `query.go`, integrity in `integrity.go`, retention in
`retention.go`, safe observations in `observe.go`, the bounded memory adapter,
and the separately versioned PostgreSQL store, transaction writer, retention
administrator, embedded migration, roles, indexes, triggers, and functions.
`modules.json` and `packages.json` are the release manifest authority.

The cryptographic surface uses only `crypto/sha256`, `crypto/hmac`,
`crypto/rand`, and constant-time digest comparison from the Go standard
library. HMAC keys are caller-provided, copied before use, never stored in a
record, and selected by explicit key ID and recording time. The package does not
generate, derive, wrap, rotate, persist, or attest keys. SHA-256 chains detect
corruption only; HMAC adds authenticity only while key custody remains trusted;
neither primitive alone supplies non-repudiation.

Canonical version 1 is frozen by a readable golden record and an independently
computed chain digest. Tests cover key rotation, checkpoints, missing,
reordered, duplicated, altered, truncated, partially archived, and restored
records. PostgreSQL fault tests classify validation and statement failures as
rejected and post-commit ambiguity as unknown, including deadlock and
serialization SQLSTATEs. Real-database tests cover transactional migration
interruption, atomic caller-owned writes, duplicate reconciliation, stable
pagination, cancellation, protocol-compatible rolling writes across migration,
backup and restore, two-phase retention, legal holds,
backend termination and pool reconnection, and least-privilege read/update/delete
denial.

The supported PostgreSQL matrix is the upstream-supported majors 14 through 18
using the digest-pinned current-minor images declared in
`postgres/testdata/postgres-images.tsv`.
Run `make integration-matrix`; a missing image, database tool, container runtime,
or major result is a failure. Race and `goleak` checks cover all packages. Fuzz,
stress, soak, fault, exact statement coverage, viable mutation, benchmarks,
security, dependency, documentation, API, and clean-consumer gates are release
requirements rather than optional warnings.

Benchmarks measure canonical encoding, redaction, single and atomic batch
append, fully filtered pagination, streaming export, and chain verification.
Core workloads include explicit equivalent standard-library reference paths;
the PostgreSQL workloads execute against the selected real database image.
Results must be retained with Go version, machine, corpus, duration, latency,
throughput, and allocations; they are engineering evidence, not universal
service-level objectives.

## Owned release exclusions

These boundaries are not reported as passing adapter gates:

| Boundary | Owner | Reason | Risk | Expiry |
| --- | --- | --- | --- | --- |
| Partition rollover | audit maintainers | The supported schema is unpartitioned so global record-ID uniqueness remains enforceable. | A deployment-specific partitioned fork can lose global idempotency or bypass legal holds during rollover. | 2027-02-09, or before any partitioned adapter is released, whichever comes first. |
| Physical standby promotion | audit maintainers | The adapter owns no PostgreSQL topology; local evidence terminates a backend and proves pool reconnection and idempotent recovery. | A deployment's proxy, DNS, replication, or promotion policy may produce a longer or ambiguous outage. | 2027-02-09, or before claiming support for a managed failover topology, whichever comes first. |
| Previous-binary rolling deployment | audit maintainers | Version 1 has no previous released reader or writer binary; the frozen version-1 fixture is exercised through upgrade, backup, and restore instead. | The first post-v1 format could break old readers if released without a real two-binary matrix. | Before the first format or API release after v1. |
