# Audit PostgreSQL Portability Input-Digest Migration

Observed at `2026-08-19T03:12:00Z` on `darwin/arm64` with Go `1.26.6`.

## Change

The `pkg/audit/postgres` integration tests now observe concurrent migration
locks through their existing migration transaction instead of acquiring a
fifth pool connection. Backup and restore verification now executes the
version-matched `pg_dump` and `pg_restore` binaries inside the owned
PostgreSQL test container instead of depending on the host client version.

No production Go file, SQL migration, public API, persistence contract, or
reference-service composition changed.

## Verification

The two previously failing integration scenarios passed against PostgreSQL 18.
The complete `pkg/audit/postgres` integration suite then passed. Its strict
module gate also passed format, tidy, safety, vet, test, race, exact 100%
statement coverage, lint, static analysis, vulnerability, secret, license,
SBOM, fuzz, documentation, API, and benchmark checks. Mutation testing killed
all 226 viable production mutants.

## Claim Boundary

This migration preserves the earlier `OA-REFERENCE-HTTP` evidence because that
scenario uses the in-memory audit implementation and neither its production
inputs nor exercised behavior changed. It records only the test-harness input
transition required to make PostgreSQL integration verification portable
across constrained CI pools and host PostgreSQL client versions.
