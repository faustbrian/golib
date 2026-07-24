# Compatibility and release policy

The module follows Semantic Versioning. Public exported identifiers, typed
binding semantics, parser forms, Unicode policy, generated schemas, output
envelopes, error kinds and sentinels, default exit codes, completion protocol,
and deterministic ordering are compatibility contracts.

The minimum supported toolchain is Go 1.25. CI also tests the current stable Go
release. Releases run with `GOWORK=off`; local workspaces must not hide missing
module dependencies. The exported API is compared with its checked-in baseline.

Cobra is pinned behind `internal/engine`. An upgrade requires differential argv
tests, help and completion drift review, vulnerability and license review,
benchmarks, and a changelog entry. Cobra errors and types are not public
contracts.

Every user-visible addition, compatibility decision, security fix, deprecation,
and breaking change enters `CHANGELOG.md` under Unreleased before release.
Generated artifacts are byte-compared in CI. Releases include reproducible
archives, checksums, SBOM, provenance, and signatures.

Deprecations include a message and replacement path in help and manifests.
Removal waits for a breaking release unless the behavior is unsafe. Security
corrections may deliberately tighten hostile-input acceptance and will be
documented with migration guidance.
