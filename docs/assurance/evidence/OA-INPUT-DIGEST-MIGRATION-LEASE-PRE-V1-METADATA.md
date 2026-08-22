# Lease Pre-v1 Metadata Normalization Evidence

Observed at `2026-08-21T18:46:09Z` on `darwin/arm64` with Go `1.26.6`.

## Change Boundary

The `pkg/lease` module replaced its remaining owned-module pseudo-version
requirements with the repository's unpublished local version `v0.0.0`, removed
the obsolete exclusions that prohibited those local versions, refreshed the
corresponding checksums through the task-owned local source proxy, and moved
the premature `1.0.0` changelog text back under `Unreleased`.

Only `pkg/lease/go.mod`, `pkg/lease/go.sum`, and `pkg/lease/CHANGELOG.md`
changed. No Go source, test, fixture, generated source, public API, service
image, tool version, or runtime configuration changed.

## Resolution Proof

With `GOWORK=off`, the exact Go 1.26.6 toolchain resolved the normalized graph
through a task-owned local `v0.0.0` proxy. `go mod tidy -diff` was clean and
`go test ./... -count=1` passed for every package in `pkg/lease`. The task-owned
Go build cache, module cache, and local proxy were removed immediately after
the run.

The release proxy replaces owned unpublished versions with dependency-ordered
`v1.0.0` requirements. The manifest normalization therefore removes a stale
development locator without changing the owned source selected for the first
public release.

## Input Identity

The retained operational-assurance input digest for `pkg/lease` had already
been migrated to
`a232026e5b95c995db26fbd6fa7c576a190474c08efd2f2551cc957e1ca14741`.
The metadata and changelog normalization changes that digest to
`6aaf18768fb87e1a316db7a31356f97770f9798a1a2d703e0e009e4aca493b9f`.

This migration preserves the earlier `OA-REFERENCE-HTTP` observation because
the reference service does not execute lease manifest or changelog text and
the lease runtime implementation exercised by that composition is unchanged.

## Claim Boundary

This evidence authorizes only the exact `pkg/lease` operational-assurance
input-digest transition above. It does not authorize migration across a source,
test, fixture, API, external dependency, service, tool, or runtime behavior
change, and it does not replace the final repository or hosted CI gates.
