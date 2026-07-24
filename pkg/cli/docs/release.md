# Release policy

Releases use repository tags named `cli/vX.Y.Z`. Maintainers update the
unreleased changelog, run `GOWORK=off make release-check` from `cli`, review
API and dependency changes, and create a signed annotated tag. The release
workflow verifies that tag before publishing anything.

`scripts/build-release.sh` produces a deterministic source archive from the
tagged `cli` Git tree. CI adds a CycloneDX SBOM and SHA-256 checksums, signs
each artifact keylessly with Sigstore, attaches GitHub build provenance, and
publishes the files to the matching GitHub release. No application binary is
published because this repository is a library; the reference generator is a
development tool, not a consumer executable.

Consumers can verify provenance with `gh attestation verify`, verify Sigstore
bundles with `cosign verify-blob`, and compare the checksums file. Patch
releases preserve documented behavior, minor releases add compatible API, and
major releases may intentionally break public contracts after migration
guidance and changelog notice. Security fixes follow `SECURITY.md`.

To reproduce a source archive locally from the repository root:

```sh
cd cli
./scripts/build-release.sh v1.2.3 "$(mktemp -d)/artifacts"
```
