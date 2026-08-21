# Release process

No production release should be tagged until the attached project acceptance
criteria and every required automation gate pass against the release commit.

## Required evidence

- deterministic format, module checksum, vet, Staticcheck, strict
  golangci-lint, advisory NilAway, test, exact meaningful coverage, race,
  build, fuzz, and leak gates;
- PostgreSQL migration, retention, backup, restore, and upgrade tests;
- fake and real `queue` management conformance;
- Redis and Valkey integration through `queue`, never direct clients;
- rolling worker protocol, disconnect, stale, delayed, duplicate, timeout,
  partial result, restart, and backend-failure tests;
- authorization mutation testing for every action;
- API fuzzing and browser security tests;
- large fleet, reconnect storm, queue, failure, maximum payload, history, and
  backend-outage load benchmarks with enforced allocation budgets;
- vulnerability and dependency scanning;
- documentation and API compatibility validation;
- reproducible multi-platform images, SBOM, provenance, signatures, and
  verification instructions.

The current CI covers Go formatting, module tidiness and checksum verification,
vet, Staticcheck, strict golangci-lint, advisory NilAway, tests, race, exact
100% statement coverage, builds, a fuzz smoke test, a high-severity browser
dependency audit, real PostgreSQL 16, 17, and
18 migration and persistence integration, an isolated PostgreSQL 18 native
backup-and-restore drill, the production one-shot audit and safe terminal-
command retention path, pinned Go
vulnerability scanning, 100% administrative mutation efficacy and coverage,
targeted HTTP lifecycle leak assertions, public Go API baseline compatibility,
authenticated managed-queue and rolling-protocol HTTP integration, real
Redis 6.2.22 and Valkey 9.1.0 lifecycle/status integration through `queue`,
including a concurrent retry/delete race with exactly one truthful winner,
real Chromium CORS, preflight, CSRF, and defensive-header tests, Dockerfile
checks, and a multi-platform OCI build. It also smoke-runs the
eight 10,000-worker, 100,000-audit-event, maximum-page, maximum-payload,
reconnect-storm, and backend-outage benchmarks with allocation budgets but
without a noisy hosted-runner latency threshold.
The OCI build path can produce BuildKit SBOM and provenance attestations. It
covers authenticated Redis Streams and Valkey Streams failure management, but
not the remaining transport-level queue and failure load items above. The
repository does not currently publish or sign those artifacts.

## Versioning and changelog

The module is unreleased. Its first public version will be `v1.0.0`, represented
by the monorepo tag `pkg/queue-control-plane/v1.0.0`. Update `CHANGELOG.md` in
the same pull request as every user-visible change. Before release, move
Unreleased entries into a dated version section, verify upgrade and rollback
guidance, and confirm `/version` reports the tag, commit, and RFC3339 build time.

Run `make release-dry-run MODULES=pkg/queue-control-plane` from the repository
root to validate the module archive, clean consumer, dependency order, and
proposed tag. `make release-public MODULES=pkg/queue-control-plane` performs
public-resolution verification only; `scripts/release.sh` deliberately refuses
to create tags or publish artifacts.

No reviewed publishing workflow, GHCR image, release archive, checksum bundle,
signature, or stable certificate identity exists yet. Release automation MUST
define those artifact names and identities before this guide can provide
copy-paste verification commands. Do not infer a package-local GitHub Actions
workflow path or treat a locally built image as a signed public release.
