# Release Process

1. Update `CHANGELOG.md`, the API baseline, compatibility notes, and docs.
2. Run `make check-all` with the pinned Go and tool versions.
3. Review coverage, mutation output, vulnerability results, fuzz smoke,
   benchmarks, and integration evidence.
4. Open and merge a reviewed pull request with every blocking workflow green.
5. Create a signed `vMAJOR.MINOR.PATCH` tag. The release workflow verifies the
   tag commit, reruns the blocking gate, builds provenance, and publishes the
   changelog entry.

Route names and exported APIs are SemVer contracts after v1. Security releases
must not disclose a private advisory before coordinated publication.
