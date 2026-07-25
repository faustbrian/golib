# Releasing

1. Confirm `CHANGELOG.md` describes every implementation since the prior tag.
2. Run `go mod tidy -diff`, `git diff --check`, and `make check` from a clean
   supported checkout.
3. Review exported API, default, error, header, retry, redirect, fixture schema,
   and telemetry-label compatibility against `docs/compatibility.md`.
4. Move Unreleased notes to a SemVer version and release date in a dedicated
   commit.
5. Create a signed `vMAJOR.MINOR.PATCH` tag at that verified commit and push the
   tag without force.
6. The release workflow reruns the full gate and creates GitHub release notes.
7. Verify pkg.go.dev documentation and downstream smoke tests, then announce
   any migration or security notes.

Never publish from a dirty tree or bypass hooks, signatures, tests, or safety
checks. A failed release gets a new patch version; published tags are immutable.
