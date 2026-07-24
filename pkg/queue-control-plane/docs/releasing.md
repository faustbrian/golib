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
The OCI artifact includes BuildKit SBOM and maximal provenance attestations. It
covers authenticated Redis Streams and Valkey Streams failure management, but
not the remaining transport-level queue and failure load items above.
The tagged release workflow signs both image
digests after all current release-quality gates pass.

## Versioning and changelog

Use semantic versioning once the public management protocol and API are
frozen. Update `CHANGELOG.md` in the same pull request as every user-visible
change. Before tagging, move Unreleased entries into a dated version section,
verify upgrade and rollback guidance, and confirm `/version` reports the tag,
commit, and RFC3339 build time.

Tags must match `vMAJOR.MINOR.PATCH` with an optional semantic prerelease.
Pushing a matching tag builds the server and CLI images for amd64 and arm64,
pushes them to GHCR, attaches SBOM and provenance, signs each digest with the
short-lived GitHub Actions OIDC identity, and immediately verifies that exact
workflow identity. The workflow does not publish a mutable `latest` tag.

Verify a released server image before deployment:

```sh
VERSION=v1.2.3
cosign verify "ghcr.io/faustbrian/queue-control-plane:${VERSION}" \
  --certificate-identity "https://github.com/faustbrian/golib/pkg/queue-control-plane/.github/workflows/release.yml@refs/tags/${VERSION}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Replace the image name with `queue-control` to verify the CLI. Manual
unsigned images are not an acceptable fallback.

The release page contains standalone server and CLI archives for Linux, macOS,
and Windows on amd64 and arm64. Verify the signed checksum manifest before
extracting an archive:

```sh
VERSION=v1.2.3
cosign verify-blob \
  --bundle SHA256SUMS.bundle \
  --certificate-identity "https://github.com/faustbrian/golib/pkg/queue-control-plane/.github/workflows/release.yml@refs/tags/${VERSION}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  SHA256SUMS
shasum -a 256 -c SHA256SUMS
```

Archive metadata and server build metadata derive from the tagged commit time,
so rebuilding the same tag with the documented toolchain produces the same
archives and checksums.
