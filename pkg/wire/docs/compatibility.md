# Versioning and release guide

## Compatibility contract

The project uses Semantic Versioning. Beginning with v1.0.0, compatibility
includes more than Go signatures:

- exported packages, types, constants, variables, functions, methods, and
  struct fields;
- default payload limits and strictness;
- `errors.Is` and `errors.As` classifications;
- deterministic emitted bytes where documented;
- JSON normalization behavior;
- SOAP envelope and fault interpretation;
- YAML alias, merge, duplicate-key, and document-stream handling;
- TOML metadata, datetime, and numeric conversion behavior;
- MessagePack map-key, duplicate-key, extension, width, compact-encoding,
  nesting, and collection-limit behavior;
- CBOR deterministic profiles, tag policy, and resource limits;
- BSON document validation, duplicate-key handling, and numeric conversion;
- supported charset labels;
- documented accepted and rejected shapes.

Fixing behavior that contradicts a specification can still be breaking for
users. Release notes must call out the impact and migration path.

## Version policy

- Patch releases contain compatible bug fixes, security fixes, documentation,
  and new regression fixtures.
- Minor releases add backward-compatible APIs or capabilities.
- Major releases can change or remove compatibility-governed behavior and must
  include migration notes.
- Pre-v1 versions can change APIs, but every change remains documented.

## Deprecation policy

Prefer a documented deprecation in at least one minor release before removing
an exported API. A deprecation comment must name the replacement and migration
constraint. Security issues can require faster removal and must explain the
exception in release notes.

## Release prerequisites

The release commit must have:

- a clean worktree;
- the intended Go support matrix passing;
- 100% production statement coverage with behavior-focused tests;
- formatting, `go vet`, and lint gates passing;
- fuzz smoke targets passing;
- benchmark compilation and smoke execution passing;
- documentation links and examples validated;
- dependency and vulnerability scans passing;
- `CHANGELOG.md` moved from Unreleased to the release version;
- migration notes for every breaking change.

## Release procedure

1. Choose a SemVer version from the compatibility impact.
2. Replace the Unreleased changelog section with the version and UTC date, then
   add a fresh Unreleased section.
3. Run the guarded target for the intended compatibility level:

   ```sh
   make release-patch
   make release-minor
   make release-major
   ```

   Each target calculates the next stable version, requires a clean `main`
   branch synchronized with `origin/main`, runs the complete release gate, and
   creates a local annotated tag. It never pushes the tag.
4. Merge the release commit through normal review.
5. Review the local tag, then push only that tag:

   ```sh
   git push origin v1.0.0
   ```

6. The tagged-release workflow re-runs quality and security gates, builds a
   source archive with checksums, and creates a GitHub Release from the matching
   changelog section.
7. Verify the release page, checksum artifact, module availability, and Go
   documentation rendering.

Never move or recreate a published tag. Publish a new patch version if a
release artifact or note needs correction.

## Release reproducibility

The release contains source only; consumers compile it with Go modules. The
workflow records the tag, commit, Go version, source archive, and SHA-256
checksum. Runtime dependencies are pinned through `go.mod` and recorded with
their selection rationale and residual risks in
[`dependencies.md`](dependencies.md). Dependency upgrades require full wire
compatibility, fuzz, benchmark, vulnerability, and license review.

## Emergency security releases

Use GitHub's private vulnerability reporting flow. Prepare the fix and advisory
privately, run the complete gate, then publish the advisory, patch release, and
changelog entry together. Do not disclose exploit details in a public issue
before users can upgrade.
