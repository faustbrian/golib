# Release process

No stable release exists yet. A release candidate must start from a clean clone
with `GOWORK=off`, pass `make check`, retained fuzz budgets, mutation review,
benchmark budgets, documentation and example checks, platform CI, dependency
review, vulnerability and license review, SBOM generation, reproducibility,
provenance, and signing.

Freeze the release commit before evidence generation. Update the changelog with
every user-visible behavior and compatibility decision. Tag only the verified
commit, build archives from Git objects rather than a working tree, publish
checksums and an SBOM, attest provenance, sign the release, and verify every
artifact after download.

Comparative engine and binary-size evidence is recorded in the isolated
benchmark module. The completed manual accessibility matrix and its exact claim
boundary are recorded in `docs/accessibility-review.md`.

The local `make release-check` command adds retained fuzz budgets, the reviewed
mutation threshold, and benchmark execution to the standard quality gates.
`make api`, `make license`, `make sbom`, and `make reproducible` provide focused
entry points. The API export baseline is intentionally exact during pre-v1
development; an intentional public change must update it with review.

Signed `prompts/v*` tags trigger the release workflow. It verifies the tag,
runs the complete release gate, builds a deterministic source archive and
CycloneDX SBOM, records checksums, creates keyless Sigstore bundles, emits a
GitHub artifact attestation, uploads the evidence, and publishes the verified
tag as a GitHub release. CodeQL runs on changes, main, and a weekly schedule;
dependency review enforces moderate-severity and license policy on module
changes.
