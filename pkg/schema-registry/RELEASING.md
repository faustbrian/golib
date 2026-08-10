# Releasing

1. Run `make check-release` with provider integration credentials and services
   explicitly configured.
2. Confirm exact statement coverage and viable mutation kills for the core and
   each provider module.
3. Review provider interoperability artifacts, dependency licenses,
   vulnerabilities, SBOM, provenance, benchmarks, API baseline, and the
   Unreleased changelog.
4. Produce a directory-prefixed semantic-version tag: `pkg/schema-registry/vX.Y.Z`.
5. Release provider modules independently with their own directory-prefixed
   tags. A core release does not imply provider support.
